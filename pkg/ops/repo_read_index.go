package ops

import (
	"encoding/json"
	"os"
	"path/filepath"

	"lab47.dev/aperture/pkg/data"
)

type RepoReadIndex struct {
	common
	path string
}

func (r *RepoReadIndex) Read() (*data.RepoIndex, error) {
	path := filepath.Join(r.path, ".repo-index.json")

	f, err := os.Open(path)
	if err != nil {

		var rwi RepoWriteIndex
		rwi.path = r.path

		err = rwi.Write()
		if err != nil {
			return nil, err
		}

		f, err = os.Open(path)
		if err != nil {
			return nil, err
		}
	}

	defer f.Close()

	var ri data.RepoIndex

	err = json.NewDecoder(f).Decode(&ri)
	if err != nil {
		return nil, err
	}

	return &ri, nil
}
