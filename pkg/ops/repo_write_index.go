package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
)

type RepoWriteIndex struct {
	common
	path string
}

func (r *RepoWriteIndex) Write() error {
	var sl ScriptLoad
	sl.common = r.common

	sl.lookup = &ScriptLookup{
		common: r.common,
		Path:   []string{r.path},
	}

	sl.Store = &config.Store{}

	scripts, err := sl.Search("")
	if err != nil {
		return err
	}

	var ri data.RepoIndex

	for _, sp := range scripts {
		ri.Entries = append(ri.Entries, data.RepoEntry{
			Name:         sp.Name(),
			Version:      sp.Version(),
			URL:          sp.URL(),
			Description:  sp.Description(),
			Dependencies: sp.DependencyNames(),
			Metadata:     sp.Metadata(),
			Vendor:       sp.Vendor(),
		})
	}

	ri.CreatedAt = time.Now()

	f, err := os.Create(filepath.Join(r.path, ".repo-index.json"))
	if err != nil {
		return err
	}

	defer f.Close()

	return json.NewEncoder(f).Encode(&ri)
}
