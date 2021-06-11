package ops

import (
	"io/fs"
	"os"
	"path/filepath"
)

type PackageRemoveCruft struct {
	common
}

func (p *PackageRemoveCruft) RemoveCruft(path string) error {
	return filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Remove an .la files in lib/
		if filepath.Ext(path) == ".la" && filepath.Base(filepath.Dir(path)) == "lib" {
			return os.Remove(path)
		}

		return nil
	})
}
