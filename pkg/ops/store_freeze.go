package ops

import (
	"os"
	"path/filepath"

	"lab47.dev/aperture/pkg/config"
)

type StoreFreeze struct {
	store *config.Store
}

func (s *StoreFreeze) Freeze(id string) error {
	path, err := s.store.Locate(id)
	if err != nil {
		return err
	}

	var dirs []string
	err = filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
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
