package ops

import (
	"os"
	"path/filepath"
)

type StoreFind struct {
	StoreDir string
}

func (s *StoreFind) Find(id string) (string, error) {
	if s.StoreDir == "" {
		return "", nil
	}

	sd := s.StoreDir
	for {
		path := filepath.Join(sd, id, ".pkg-info.json")

		_, err := os.Stat(path)
		if err == nil {
			return filepath.Dir(path), nil
		}

		if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Join(sd, "_parent")

		_, err = os.Stat(parent)
		if err != nil {
			return "", nil
		}

		sd = parent
	}
}
