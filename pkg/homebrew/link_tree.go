package homebrew

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func relSymlink(oldname, newname string) error {
	prel, err := filepath.Rel(filepath.Dir(newname), oldname)
	if err != nil {
		return err
	}

	err = os.Symlink(prel, newname)
	if err != nil {
		return err
	}

	return nil
}

func LinkTree(targetRoot, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if rel == "." {
			return nil
		}

		if strings.IndexByte(rel, '/') == -1 {
			// skip toplevel files
			if !info.IsDir() {
				return nil
			}

			switch rel {
			case "etc", "bin", "sbin", "share", "include", "lib":
				// ok
			default:
				return filepath.SkipDir
			}
		}

		target := filepath.Join(targetRoot, rel)

		fi, err := os.Lstat(target)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}

			err := relSymlink(path, target)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if !info.IsDir() {
			lt, err := os.Readlink(target)
			if err != nil || lt != path {
				absLt := filepath.Join(filepath.Dir(target), lt)
				if err != nil || absLt != path {
					fmt.Printf("skipping duplicate entries for %s (%s != %s)", target, lt, path)
				}
			}

			return nil
		}

		if fi.IsDir() {
			return nil
		}

		lfi, err := os.Stat(target)
		if err != nil {
			return err
		}

		if !lfi.IsDir() {
			return fmt.Errorf("unable to merge file and dir at path: %s", target)
		}

		odir, err := os.Readlink(target)
		if err != nil {
			return err
		}

		if !filepath.IsAbs(odir) {
			odir = filepath.Join(filepath.Dir(target), odir)
		}

		f, err := os.Open(target)
		if err != nil {
			return err
		}

		defer f.Close()

		names, err := f.Readdirnames(-1)
		if err != nil {
			return err
		}

		os.Remove(target)

		err = os.Mkdir(target, 0755)
		if err != nil {
			return err
		}

		for _, name := range names {
			err = relSymlink(filepath.Join(odir, name), filepath.Join(target, name))
			if err != nil {
				return err
			}
		}

		return nil
	})
}
