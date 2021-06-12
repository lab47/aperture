package config

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/mod/module"
	"lab47.dev/aperture/pkg/data"
)

type PackagePath struct {
	Name     string
	Type     string
	Location string
	Version  string

	// Populated by PathMap to indicate the version that was actually used for Version
	ResolvedVersion string
}

type PathMap struct {
	Dir string
}

func (p *PathMap) run(ctx context.Context, dir string, cmds ...string) error {
	cmd := exec.CommandContext(ctx, cmds[0], cmds[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (p *PathMap) capture(ctx context.Context, dir string, cmds ...string) ([]byte, error) {
	var buf bytes.Buffer

	cmd := exec.CommandContext(ctx, cmds[0], cmds[1:]...)
	cmd.Dir = dir
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Map takes a PackagePath and maps it to a local filesystem path, performing
// any operations necessary to make that happen.
func (p *PathMap) Map(ctx context.Context, pp *PackagePath) (string, error) {
	switch pp.Type {
	case "", "local":
		expanded, err := homedir.Expand(pp.Location)
		if err != nil {
			return "", err
		}

		fi, err := os.Stat(expanded)
		if err != nil {
			return "", err
		}

		if !fi.IsDir() {
			return "", fmt.Errorf("Path is not a directory: %s", expanded)
		}

		return expanded, nil
	case "git":
		name := pp.Location

		u, err := url.Parse(pp.Location)
		if err == nil {
			name = filepath.Join(u.Host, u.Path)
		}

		escName, err := module.EscapePath(name)
		if err != nil {
			return "", err
		}

		ver := pp.Version
		if ver == "" {
			ver = "main"
		}

		escVer, err := module.EscapeVersion(pp.Version)
		if err != nil {
			return "", err
		}

		destPath := filepath.Join(p.Dir, "repo", escName+"@"+escVer)

		resolvedVersion := pp.ResolvedVersion
		if resolvedVersion == "" {
			resolvedVersion = pp.Version
		}

		// This effectively duplicates the functionality go uses for download
		// modules. It's in cmd/go/internal/modfetch/codehost/git.go
		key := pp.Type + ":" + pp.Location

		cache := filepath.Join(p.Dir, "cache/vcs", p.hash(key))

		if _, err := os.Stat(destPath); err == nil {
			// We still want to populate the resolved info
			info, err := p.capture(ctx, cache, "git", "-c", "log.showsignature=false", "log", "-n1", "--format=format:%H %ct %D", resolvedVersion)
			if err != nil {
				return "", err
			}

			// TODO: use the other fields like go mod does.
			f := strings.Fields(string(info))
			pp.ResolvedVersion = f[0]

			// TODO: use the other fields like go mod does.
			return destPath, nil
		}

		if _, err := os.Stat(cache); err != nil {
			err = os.MkdirAll(cache, 0777)
			if err != nil {
				return "", err
			}

			err := ioutil.WriteFile(filepath.Join(cache, ".info"), []byte(key), 0644)
			if err != nil {
				return "", err
			}

			err = p.run(ctx, cache, "git", "init", "--bare")
			if err != nil {
				os.RemoveAll(cache)
				return "", err
			}

			err = p.run(ctx, cache, "git", "remote", "add", "origin", "--", pp.Location)
			if err != nil {
				os.RemoveAll(cache)
				return "", err
			}
		}

		err = p.run(ctx, cache, "git", "fetch", "-f", pp.Location, "refs/heads/*:refs/heads/*", "refs/tags/*:refs/tags/*")
		if err != nil {
			os.RemoveAll(cache)
			return "", err
		}

		info, err := p.capture(ctx, cache, "git", "-c", "log.showsignature=false", "log", "-n1", "--format=format:%H %ct %D", resolvedVersion)
		if err != nil {
			return "", err
		}

		// TODO: use the other fields like go mod does.
		f := strings.Fields(string(info))
		pp.ResolvedVersion = f[0]

		zipData, err := p.capture(ctx, cache, "git", "archive", "--format=zip", "--prefix=prefix/", resolvedVersion)
		if err != nil {
			return "", err
		}

		zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
		if err != nil {
			return "", err
		}

		err = os.MkdirAll(destPath, 0777)
		if err != nil {
			return "", err
		}

		prefix := ""
		for _, zf := range zr.File {
			if prefix == "" {
				i := strings.IndexByte(zf.Name, '/')
				if i == -1 {
					return "", fmt.Errorf("missing top-level prefix")
				}

				prefix = zf.Name[:i+1]
			}

			if zf.Mode().IsDir() {
				continue
			}

			name := strings.TrimPrefix(zf.Name, prefix)

			dp := filepath.Join(destPath, name)

			err := os.MkdirAll(filepath.Dir(dp), 0777)
			if err != nil {
				return "", err
			}

			mode := zf.Mode()

			if mode.Type() == os.ModeSymlink {
				r, err := zf.Open()
				if err != nil {
					return "", err
				}

				bpath, err := io.ReadAll(r)
				if err != nil {
					return "", err
				}

				path := string(bpath)

				abs := path
				if !filepath.IsAbs(path) {
					abs = filepath.Join(filepath.Dir(dp), path)
				}

				if !strings.HasPrefix(destPath, abs) {
					return "", fmt.Errorf("symlink points outside tree")
				}

				err = os.Symlink(path, dp)
				if err != nil {
					return "", err
				}
			} else if mode.IsRegular() {
				r, err := zf.Open()
				if err != nil {
					return "", err
				}

				w, err := os.OpenFile(dp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode|0444)
				if err != nil {
					return "", err
				}

				_, err = io.Copy(w, r)
				if err != nil {
					w.Close()
					return "", err
				}

				err = w.Close()
				if err != nil {
					return "", err
				}
			}
		}

		w, err := os.Create(filepath.Join(destPath, ".repo-info.json"))
		if err != nil {
			return "", err
		}

		var repoInfo data.RepoInfo
		repoInfo.Id = name

		err = json.NewEncoder(w).Encode(repoInfo)
		if err != nil {
			return "", err
		}

		w.Close()

		return destPath, nil
	}

	return "", fmt.Errorf("Unsupported type: %s", pp.Type)
}

var repl = strings.NewReplacer(
	"/", "-",
)

func (p *PathMap) composePath(pp *PackagePath) string {
	return repl.Replace(pp.Location)
}

func (p *PathMap) hash(key string) string {
	sum := blake2b.Sum256([]byte(key))
	return base58.Encode(sum[:])
}
