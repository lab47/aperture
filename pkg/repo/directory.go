package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"lab47.dev/aperture/pkg/data"
	"lab47.dev/aperture/pkg/metadata"
	"lab47.dev/aperture/pkg/sumfile"
)

type Directory struct {
	repoId   string
	rootPath string
	pkgPath  string

	config *metadata.RepoConfig
}

func NewDirectory(path string) (*Directory, error) {
	path = filepath.Clean(path)

	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	rootPath := path

	pkgDir := filepath.Join(path, "packages")

	if fi, err := os.Stat(pkgDir); err == nil && fi.IsDir() {
		path = pkgDir
	}

	d := &Directory{
		rootPath: rootPath,
		pkgPath:  path,
	}

	cfg, err := d.loadConfig()
	if err != nil {
		return nil, err
	}

	d.config = cfg

	d.repoId = cfg.Id

	if d.repoId == "" {
		err = d.detectRepoId()
		if err != nil {
			return nil, err
		}
	}

	return d, nil
}

var _ Repo = (*Directory)(nil)

func (d *Directory) parseGitUrl(url string) error {
	return nil
}

func (d *Directory) detectRepoId() error {
	f, err := os.Open(filepath.Join(d.rootPath, ".repo-info.json"))
	if err == nil {
		var ri data.RepoInfo

		err = json.NewDecoder(f).Decode(&ri)
		if err != nil {
			return err
		}

		d.repoId = ri.Id
		return nil
	}

	repo, err := git.PlainOpenWithOptions(d.rootPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return err
	}

	remote, err := repo.Remote("origin")
	if err == nil {
		urls := remote.Config().URLs
		if len(urls) != 0 {
			id, err := gitRemoteRepoId(urls[0])
			if err != nil {
				return err
			}

			d.repoId = id
			return nil
		}
	} else {
		if err != git.ErrRemoteNotFound {
			return err
		}
	}

	// welp. I guess we'll use the directory base name

	d.repoId = filepath.Base(d.rootPath)

	return nil
}

type DirEntry struct {
	repoId string
	script string
	dir    string
}

func (e *DirEntry) Script() (string, []byte, error) {
	data, err := ioutil.ReadFile(e.script)
	return filepath.Base(e.script), data, err
}

func (e *DirEntry) RepoId() string {
	return e.repoId
}

// Pulled over from net/http
func containsDotDot(v string) bool {
	if !strings.Contains(v, "..") {
		return false
	}
	for _, ent := range strings.FieldsFunc(v, isSlashRune) {
		if ent == ".." {
			return true
		}
	}
	return false
}

func isSlashRune(r rune) bool { return r == '/' || r == '\\' }

var ErrInvalidPath = errors.New("invalid asset requested")

func (e *DirEntry) Asset(name string) (string, []byte, error) {
	if containsDotDot(name) {
		return "", nil, ErrInvalidPath
	}

	path := filepath.Join(e.dir, name)

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	return path, data, nil
}

func (e *DirEntry) Sumfile() (*sumfile.Sumfile, error) {
	path := filepath.Join(e.dir, "sums")

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var sf sumfile.Sumfile

	err = sf.Load(f)
	if err != nil {
		return nil, err
	}

	return &sf, nil
}

func (e *DirEntry) SaveSumfile(sf *sumfile.Sumfile) error {
	path := filepath.Join(e.dir, "sums")

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	return sf.Save(f)
}

func (d *Directory) loadConfig() (*metadata.RepoConfig, error) {
	var rc metadata.RepoConfig

	f, err := os.Open(filepath.Join(d.rootPath, "config.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &rc, nil
		}

		return nil, err
	}

	defer f.Close()

	err = json.NewDecoder(f).Decode(&rc)
	if err != nil {
		return nil, err
	}

	return &rc, nil
}

func (d *Directory) Config() (*metadata.RepoConfig, error) {
	return d.config, nil
}

func (d *Directory) Lookup(name string) (Entry, error) {
	var short string

	if len(name) > 2 {
		short = name[:2]
	} else {
		short = name
	}

	possibles := []struct {
		path, dir string
	}{
		{
			path: filepath.Join(d.pkgPath, name+Extension),
			dir:  d.pkgPath,
		},
		{
			path: filepath.Join(d.pkgPath, name, name+Extension),
			dir:  filepath.Join(d.pkgPath, name),
		},
		{
			path: filepath.Join(d.pkgPath, short, name+Extension),
			dir:  filepath.Join(d.pkgPath, short),
		},
		{
			path: filepath.Join(d.pkgPath, short, name, name+Extension),
			dir:  filepath.Join(d.pkgPath, short, name),
		},
	}

	for _, x := range possibles {
		if _, err := os.Stat(x.path); err == nil {
			return &DirEntry{
				repoId: d.repoId,
				script: x.path,
				dir:    x.dir,
			}, nil
		}
	}

	return nil, ErrNotFound
}
