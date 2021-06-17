package ops

import (
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"lab47.dev/aperture/pkg/rpath"
)

type PackageAdjustNames struct {
	common
}

func (p *PackageAdjustNames) Adjust(dir string) error {
	if runtime.GOOS == "darwin" {
		return p.adjustMac(dir)
	}

	return p.adjustLinux(dir)
}

func (p *PackageAdjustNames) adjustMac(dir string) error {
	path, err := exec.LookPath("install_name_tool")
	if err != nil || path == "" {
		return nil
	}

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if !info.Mode().IsRegular() {
			return nil
		}

		if info.Mode().Perm()&0111 == 0 {
			return nil
		}

		if !strings.HasSuffix(path, ".dylib") {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		defer f.Close()

		mf, err := macho.NewFile(f)
		if err != nil {
			return nil
		}

		if mf.Type == macho.TypeDylib {
			c := exec.Command("install_name_tool", "-id", path, path)
			_, err = c.Output()
			if err != nil {
				return err
			}
		}

		libs, err := mf.ImportedLibraries()
		if err != nil {
			return err
		}

		for _, lib := range libs {
			if filepath.IsAbs(lib) {
				continue
			}

			relative := filepath.Join(filepath.Dir(path), lib)
			if _, err := os.Stat(relative); err == nil {
				newLib := "@loader_path/" + lib

				p.common.L().Debug("changing library", "library", path, "old", lib, "new", newLib)

				c := exec.Command("install_name_tool", "-change", lib, newLib, path)
				_, err = c.Output()
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (p *PackageAdjustNames) adjustLinux(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if !info.Mode().IsRegular() {
			return nil
		}

		if info.Mode().Perm()&0111 == 0 {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		defer f.Close()

		// Read and decode ELF identifier
		var ident [16]uint8
		if _, err := f.ReadAt(ident[0:], 0); err != nil {
			// not elf
			return nil
		}

		if ident[0] != '\x7f' || ident[1] != 'E' || ident[2] != 'L' || ident[3] != 'F' {
			// not elf
			return nil
		}

		err = rpath.Shrink(path, []string{dir})
		if err != nil {
			p.L().Error("Error attempting to shrink rpath", "error", err)
		}

		return nil
	})
}
