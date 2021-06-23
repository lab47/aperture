package ops

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/pkgconfig"
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

		// Don't scan any pkg-info files we might find.
		if filepath.Base(path) == ".pkg-info.json" {
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

	refPkgs := map[string]struct{}{}

	configs, err := pkgconfig.LoadAll(path)
	if err == nil {
		for _, cfg := range configs {
			for _, sel := range cfg.Requires {
				name := strings.Fields(sel)[0]
				refPkgs[name] = struct{}{}
			}
		}
	}

	var runtimeDeps []*ScriptPackage

	for _, sp := range deps {
		if _, ok := seen[sp.Signature()]; ok {
			runtimeDeps = append(runtimeDeps, sp)
		} else {
			// See if it's a pkg-config ref'd package

			subPath, err := s.store.Locate(sp.ID())
			if err != nil {
				return nil, err
			}

			configs, err := pkgconfig.LoadAll(subPath)
			if err != nil {
				return nil, err
			}

			for _, cfg := range configs {
				if _, ok := refPkgs[cfg.Id]; ok {
					runtimeDeps = append(runtimeDeps, sp)
				}
			}
		}
	}

	return runtimeDeps, nil
}
