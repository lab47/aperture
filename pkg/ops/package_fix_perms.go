package ops

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

type PackageFixPerms struct {
	common
}

// Fix adjusts the permissions of the files in path. Mostly it
// fixes the perms of anything in the bin/ dir.
func (p *PackageFixPerms) Fix(path string) error {
	binPath := filepath.Join(path, "bin")

	paths, err := ioutil.ReadDir(binPath)
	if err != nil {
		return nil
	}

	for _, ent := range paths {
		if ent.Mode().IsRegular() {
			cur := ent.Mode().Perm()

			tgt := filepath.Join(binPath, ent.Name())

			newPerms := (cur | 0111)

			err := os.Chmod(tgt, newPerms)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
