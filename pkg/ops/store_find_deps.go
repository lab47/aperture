package ops

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

type StoreFindDeps struct {
	storeDir string
}

func (s *StoreFindDeps) PruneDeps(id string, deps []*ScriptPackage) ([]*ScriptPackage, error) {
	seen := make(map[string]struct{})

	var trbuf bytes.Buffer

	filepath.Walk(filepath.Join(s.storeDir, id), func(path string, info os.FileInfo, err error) error {
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

		var dr depDetect
		dr.deps = seen
		dr.file = path
		dr.prefix = []byte(s.storeDir + "/")
		dr.buf = &trbuf

		io.Copy(&dr, f)

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
