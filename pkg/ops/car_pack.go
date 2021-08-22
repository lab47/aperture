package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/data"
)

type CarPack struct {
	PrivateKey      ed25519.PrivateKey
	PublicKey       ed25519.PublicKey
	DepRootDir      string
	MapDependencies func(string) (string, string, string)

	Sum          []byte
	Dependencies []string
}

func (c *CarPack) Pack(cinfo *data.CarInfo, dir string, w io.Writer) error {
	var files []string

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		switch info.Mode() & os.ModeType {
		case 0, os.ModeSymlink:
			files = append(files, path)
		}

		return nil
	})

	sort.Strings(files)

	h, _ := blake2b.New256(nil)

	gz := gzip.NewWriter(io.MultiWriter(w, h))
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	var trbuf bytes.Buffer

	dh, _ := blake2b.New256(nil)

	var deps map[string]struct{}

	if c.DepRootDir != "" {
		deps = make(map[string]struct{})
	}

	for _, file := range files {
		trbuf.Reset()

		fi, err := os.Lstat(file)
		if err != nil {
			return err
		}

		err = func() error {
			var old string

			// We read /x/a/b and it's a symlink. We readlink it and get back
			// /x/c/d. So file = "/x/a/b" and old = "/x/c/d".
			// We figure out relative base by looking at the dir of file, so "/x/a".
			// since old is absolutely, we calculate the relative path of it with
			// respect to the base. In other words if you are in "/x/a", how do you
			// get to "/x/c/d". The result will be "../c/d". That relative value
			// we store as the link target.
			// If old is already relative, say "../c/d", we first normalize it back
			// to absolutely by joining it with base, so we do join("/x/a", "../c/d"),
			// which will return "/x/c/d".

			if fi.Mode()&os.ModeSymlink != 0 {
				old, err = os.Readlink(file)
				if err != nil {
					return err
				}

				// Normalize all links to be relative to the target
				base := filepath.Dir(file)

				if !filepath.IsAbs(old) {
					old = filepath.Join(base, old)
				}

				rel, err := filepath.Rel(base, old)
				if err == nil {
					old = rel
				}
			}

			hdr, err := tar.FileInfoHeader(fi, old)
			if err != nil {
				return err
			}

			hdr.Uid = 0
			hdr.Gid = 0
			hdr.Uname = ""
			hdr.Gname = ""
			hdr.AccessTime = time.Time{}
			hdr.ChangeTime = time.Time{}
			hdr.ModTime = time.Time{}
			hdr.Name = file[len(dir)+1:]
			hdr.Format = tar.FormatPAX

			if old == "" {
				fmt.Fprintf(dh, hdr.Name)
				dh.Write([]byte{0})
			} else {
				fmt.Fprintf(dh, hdr.Name)
				dh.Write([]byte{1})
				fmt.Fprintf(dh, hdr.Linkname)
				dh.Write([]byte{0})
			}

			err = tw.WriteHeader(hdr)
			if err != nil {
				return fmt.Errorf("error writing file header: %s: %w", hdr.Name, err)
			}

			if old != "" {
				return nil
			}

			var w io.Writer

			if deps != nil {
				var dr depDetect
				dr.deps = deps
				dr.file = hdr.Name
				dr.prefix = []byte(c.DepRootDir + "/")
				dr.buf = &trbuf

				w = io.MultiWriter(tw, dh, &dr)
			} else {
				w = io.MultiWriter(tw, dh)
			}

			f, err := os.Open(file)
			if err != nil {
				return err
			}

			defer f.Close()

			_, err = io.Copy(w, f)

			return err
		}()

		if err != nil {
			return err
		}
	}

	cinfo.Signer = base58.Encode(c.PublicKey)

	if deps != nil {
		for k := range deps {
			if cinfo.ID != "" && strings.HasPrefix(cinfo.ID, k) {
				continue
			}

			if c.MapDependencies != nil {
				id, repo, signer := c.MapDependencies(k)
				k = id

				cinfo.Dependencies = append(cinfo.Dependencies, &data.CarDependency{
					ID:     id,
					Repo:   repo,
					Signer: signer,
				})
			}

			c.Dependencies = append(c.Dependencies, k)
		}

		sort.Strings(c.Dependencies)
	}

	var hdr tar.Header

	hdr.Uid = 0
	hdr.Gid = 0
	hdr.Uname = ""
	hdr.Gname = ""
	hdr.AccessTime = time.Time{}
	hdr.ChangeTime = time.Time{}
	hdr.ModTime = time.Time{}
	hdr.Name = ".car-info.json"
	hdr.Format = tar.FormatPAX
	hdr.Typeflag = tar.TypeReg
	hdr.Mode = 0400

	data, err := json.MarshalIndent(cinfo, "", "  ")
	if err != nil {
		return err
	}

	dh.Write(data)

	hdr.Size = int64(len(data))

	err = tw.WriteHeader(&hdr)
	if err != nil {
		return err
	}

	_, err = tw.Write(data)
	if err != nil {
		return err
	}

	signature := ed25519.Sign(c.PrivateKey, dh.Sum(nil))

	var hdr2 tar.Header

	hdr2.Uid = 0
	hdr2.Gid = 0
	hdr2.Uname = ""
	hdr2.Gname = ""
	hdr2.AccessTime = time.Time{}
	hdr2.ChangeTime = time.Time{}
	hdr2.ModTime = time.Time{}
	hdr2.Name = SignatureEntry
	hdr2.Format = tar.FormatPAX
	hdr2.Typeflag = tar.TypeReg
	hdr2.Mode = 0400
	hdr2.Size = int64(len(signature))

	err = tw.WriteHeader(&hdr2)
	if err != nil {
		return err
	}

	_, err = tw.Write(signature)
	if err != nil {
		return err
	}

	err = tw.Flush()
	if err != nil {
		return errors.Wrapf(err, "tar writer flush")
	}

	err = gz.Close()
	if err != nil {
		return errors.Wrapf(err, "gzip flush")
	}

	c.Sum = h.Sum(nil)

	return nil
}
