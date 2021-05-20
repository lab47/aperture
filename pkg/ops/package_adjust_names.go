package ops

import (
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PackageAdjustNames struct {
}

func (p *PackageAdjustNames) Adjust(dir string) error {
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

		if mf.Type != macho.TypeDylib {
			return nil
		}

		c := exec.Command("install_name_tool", "-id", path, path)
		_, err = c.Output()
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
