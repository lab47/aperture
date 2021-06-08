package ops

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"lab47.dev/aperture/pkg/config"
)

type StoreFindDeps struct {
	store *config.Store
}

func (s *StoreFindDeps) PruneDeps(id string, deps []*ScriptPackage) ([]*ScriptPackage, error) {
	seen := make(map[string]struct{})

	var trbuf bytes.Buffer

	path, err := s.store.Locate(id)
	if err != nil {
		return nil, err
	}

	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		defer f.Close()

		var writers []io.Writer

		for _, path := range s.store.Paths {
			var dr depDetect
			dr.deps = seen
			dr.file = path
			dr.prefix = []byte(path + "/")
			dr.buf = &trbuf

			writers = append(writers, &dr)
		}

		io.Copy(io.MultiWriter(writers...), f)

		return nil
	})

	var runtimeDeps []*ScriptPackage

	for _, sp := range deps {
		if _, ok := seen[sp.Signature()]; ok {
			runtimeDeps = append(runtimeDeps, sp)
		}
	}

	return runtimeDeps, nil
}
