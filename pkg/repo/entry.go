package repo

import (
	"lab47.dev/aperture/pkg/sumfile"
)

type Entry interface {
	RepoId() string
	Script() (string, []byte, error)
	Asset(name string) (string, []byte, error)
	Sumfile() (*sumfile.Sumfile, error)
	SaveSumfile(sf *sumfile.Sumfile) error
}
