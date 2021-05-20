package ops

import (
	"os"
	"path/filepath"
)

type StoreFreeze struct {
	storeDir string
}

func (s *StoreFreeze) Freeze(id string) error {
	var dirs []string
	err := filepath.Walk(filepath.Join(s.storeDir, id), func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			dirs = append(dirs, path)
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		return os.Chmod(path, info.Mode().Perm()&0555)
	})
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		os.Chmod(dir, 0555)
	}

	return nil
}
