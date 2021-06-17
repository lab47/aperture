package config

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

type Store struct {
	Paths []string

	Default string
}

var ErrNoEntry = errors.New("no store entry for id")

func (s *Store) Locate(id string) (string, error) {
	for _, p := range s.Paths {
		path := filepath.Join(p, id)

		_, err := os.Stat(path)
		if err == nil {
			return path, nil
		}
	}

	return "", errors.Wrapf(ErrNoEntry, "id: %s, paths: %#v", id, s.Paths)
}

func (s *Store) PrependPath(path string) {
	set := make(map[string]struct{})

	paths := []string{path}

	set[path] = struct{}{}

	for {
		parent := filepath.Join(path, "_parent")

		_, err := os.Lstat(parent)
		if err != nil {
			break
		}

		// readlink through the symlink so the used paths are the
		// expected ones.

		tgt, err := os.Readlink(parent)
		if err == nil {
			path = tgt
		}

		set[path] = struct{}{}

		paths = append(paths, path)
	}

	for _, p := range s.Paths {
		if _, ok := set[p]; !ok {
			paths = append(paths, p)
		}
	}

	s.Paths = paths
}

func (s *Store) ExpectedPath(id string) string {
	return filepath.Join(s.Default, id)
}

func (s *Store) Pivot(path string) {
	s.Paths = []string{path}
	s.Default = path
}
