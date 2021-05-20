package repo

import (
	"errors"

	"lab47.dev/aperture/pkg/metadata"
)

const Extension = ".chell"

var (
	ErrNotFound = errors.New("entry not found")
)

type Repo interface {
	Lookup(name string) (Entry, error)
	Config() (*metadata.RepoConfig, error)
}

func Open(path string) (Repo, error) {
	return NewDirectory(path)
}
