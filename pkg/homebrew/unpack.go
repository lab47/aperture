package homebrew

import (
	"archive/tar"
	"compress/gzip"
	"debug/macho"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

type Unpacker struct {
	root string
}

func NewUnpacker(root string) (*Unpacker, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	return &Unpacker{root: root}, nil
}

func (u *Unpacker) Unpack(pkg *ResolvedPackage, bin *Binary, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}

	tr := tar.NewReader(gr)

	var (
		receiptPath  string
		detectedPath string
	)

	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			return "", err
		}

		if strings.HasSuffix(hdr.Name, "/INSTALL_RECEIPT.json") {
			receiptPath = hdr.Name
		}

		tgt := filepath.Join(u.root, hdr.Name)

		fmt.Printf("| %s\n", tgt)

		fi := hdr.FileInfo()

		if fi.IsDir() {
			err = os.MkdirAll(tgt, 0755)
			if err != nil {
				return "", err
			}
		} else {
			switch fi.Mode() & os.ModeType {
			case 0:
				if detectedPath == "" {
					detectedPath = hdr.Name
				}

				if _, err := os.Stat(tgt); err == nil {
					continue
				}

				w, err := os.Create(tgt)
				if err != nil {
					return "", errors.Wrapf(err, "attempting to create")
				}

				io.Copy(w, tr)
				w.Close()

				if bin.Options == nil || !bin.Options.SkipRelocation {
					err = u.relocate(tgt)
					if err != nil {
						return "", err
					}
				}

				err = os.Chmod(tgt, fi.Mode())
				if err != nil {
					return "", err
				}
			case os.ModeSymlink:
				os.Remove(tgt)
				err = os.Symlink(hdr.Linkname, tgt)
				if err != nil {
					return "", err
				}
			}
		}
	}

	if receiptPath != "" {
		detectedPath = receiptPath
	}

	pkgPath := filepath.Join(u.root, filepath.Dir(detectedPath))

	hr := &HomebrewRelocator{
		Cellar: u.root,
	}

	err = hr.Relocate(pkgPath)
	if err != nil {
		return "", err
	}

	err = u.linkOpt(pkg.Name, pkgPath)
	if err != nil {
		return "", err
	}

	return pkgPath, u.linkTree(pkgPath)
}

func (u *Unpacker) linkOpt(name, pkgPath string) error {
	tgt := filepath.Join(filepath.Dir(u.root), "opt", name)

	err := os.MkdirAll(filepath.Dir(tgt), 0755)
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(filepath.Dir(tgt), pkgPath)
	if err != nil {
		return err
	}

	spew.Dump(tgt, rel, pkgPath)

	if x, err := os.Readlink(tgt); err == nil {
		if x == rel {
			return nil
		}

		err = os.Remove(tgt)
		if err != nil {
			return err
		}
	}

	return os.Symlink(rel, tgt)
}

func (u *Unpacker) linkTree(pkgPath string) error {
	return LinkTree(filepath.Dir(u.root), pkgPath)
}

func (u *Unpacker) oldLinkBin(pkgPath string) error {
	bin := filepath.Join(filepath.Dir(u.root), "bin")
	pkgBin := filepath.Join(pkgPath, "bin")

	names, err := ioutil.ReadDir(pkgBin)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	for _, fi := range names {
		tgt := filepath.Join(bin, fi.Name())

		rel, err := filepath.Rel(filepath.Dir(tgt), filepath.Join(pkgBin, fi.Name()))
		if err != nil {
			return err
		}

		spew.Dump(tgt, rel, fi.Name())

		if x, err := os.Readlink(tgt); err == nil {
			if x == rel {
				return nil
			}

			err = os.Remove(tgt)
			if err != nil {
				return err
			}
		}

		err = os.Symlink(rel, tgt)
		if err != nil {
			return err
		}
	}

	return nil
}

const (
	cellarMarker = "@@HOMEBREW_CELLAR@@"
	prefixMarker = "@@HOMEBREW_PREFIX@@"
)

func (u *Unpacker) relocate(tgt string) error {
	f, err := macho.Open(tgt)
	if err != nil {
		return nil
	}

	defer f.Close()

	// We handle all the other types
	if !(f.Type == macho.TypeExec || f.Type == macho.TypeDylib) {
		return nil
	}

	libs, err := f.ImportedLibraries()
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(tgt)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(u.root)
	if err != nil {
		return err
	}

	for _, lib := range libs {
		if strings.HasPrefix(lib, cellarMarker) {
			np := strings.Replace(lib, cellarMarker, root, 1)
			_, err := exec.Command("install_name_tool", "-change", lib, np, tgt).Output()
			if err != nil {
				return err
			}
		} else if strings.HasPrefix(lib, prefixMarker) {
			np := strings.Replace(lib, prefixMarker, filepath.Dir(root), 1)
			_, err := exec.Command("install_name_tool", "-change", lib, np, tgt).Output()
			if err != nil {
				return err
			}
		}
	}

	_, err = exec.Command("install_name_tool", "-id", abs, abs).Output()
	if err != nil {
		return err
	}

	if RequireCodeSign {
		_, err = exec.Command("codesign", "--sign", "-", "--force",
			"--preserve-metadata=entitlements,requirements,flags,runtime",
			abs).Output()
		if err != nil {
			return err
		}
	}

	return nil
}
