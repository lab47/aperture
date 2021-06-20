package ops

import (
	"os"
	"path/filepath"
)

type PackageFixPerms struct {
	common
}

// Fix adjusts the permissions of the files in path. Mostly it
// fixes the perms of anything in the bin/ dir.
func (p *PackageFixPerms) Fix(path string) error {
	paths, err := os.ReadDir(filepath.Join(path, "bin"))
	if err != nil {
		return nil
	}

	for _, ent := range paths {
		if ent.Type().IsRegular() {
			cur := ent.Type().Perm()

			if cur&0111 != 0111 {
				err := os.Chmod(filepath.Join(path, ent.Name()), cur|0111)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
