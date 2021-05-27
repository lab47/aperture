package gc

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lab47.dev/aperture/pkg/data"
)

type Collector struct {
	dataDir string
}

func NewCollector(dataDir string) (*Collector, error) {
	dataDir = filepath.Clean(dataDir)
	return &Collector{dataDir: dataDir}, nil
}

func (c *Collector) Mark() ([]string, error) {
	seen, err := c.markInUse()
	if err != nil {
		return nil, err
	}

	var total []string

	for k := range seen {
		total = append(total, k)
	}

	sort.Strings(total)

	return total, nil
}

func (c *Collector) markInUse() (map[string]struct{}, error) {
	roots := filepath.Join(c.dataDir, "roots")

	seen := map[string]struct{}{}

	f, err := os.Open(roots)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	for {
		names, err := f.Readdirnames(100)
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		for _, name := range names {
			path := filepath.Join(roots, name)

			fi, err := os.Stat(path)
			if err != nil {
				return nil, err
			}

			if fi.IsDir() {
				rt, err := os.Readlink(path)
				if err == nil {
					path = rt
				}

				err = c.markDir(path, seen)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return seen, nil
}

func (c *Collector) DiskUsage(dirs []string) (int64, error) {
	var total int64

	for _, d := range dirs {
		err := filepath.WalkDir(
			filepath.Join(c.dataDir, "store", d),
			func(path string, d fs.DirEntry, err error,
			) error {
				fi, err := d.Info()
				if err == nil {
					total += fi.Size()
				}
				return nil
			})
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

func (c *Collector) markDir(dir string, seen map[string]struct{}) error {
	prefix := c.dataDir + "/store/"

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeType == os.ModeSymlink {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			if strings.HasPrefix(target, prefix) {
				tail := target[len(prefix):]
				idx := strings.IndexByte(tail, filepath.Separator)

				id := tail
				if idx != -1 {
					id = tail[:idx]
				}

				seen[id] = struct{}{}

				return c.gatherDeps(tail, seen)
			}
		}

		return nil
	})
}

func (c *Collector) gatherDeps(name string, deps map[string]struct{}) error {
	f, err := os.Open(filepath.Join(c.dataDir, "store", name, ".pkg-info.json"))
	if err != nil {
		return err
	}

	defer f.Close()

	var ii data.PackageInfo
	err = json.NewDecoder(f).Decode(&ii)
	if err != nil {
		return err
	}

	f.Close()

	for _, x := range ii.RuntimeDeps {
		if _, ok := deps[x]; !ok {
			deps[x] = struct{}{}
			err = c.gatherDeps(x, deps)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Collector) Sweep() ([]string, error) {
	inUse, err := c.markInUse()
	if err != nil {
		return nil, err
	}

	storeDir := filepath.Join(c.dataDir, "store")

	var notInUse []string

	f, err := os.Open(storeDir)
	if err != nil {
		return nil, err
	}

	for {
		names, err := f.Readdirnames(100)
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		for _, name := range names {
			fi, err := os.Stat(filepath.Join(storeDir, name))
			if err != nil {
				return nil, err
			}

			if !fi.IsDir() {
				continue
			}

			if _, ok := inUse[name]; !ok {
				notInUse = append(notInUse, name)
			}
		}
	}

	sort.Strings(notInUse)

	return notInUse, nil
}

type SweepResult struct {
	Removed        []string
	BytesRecovered int64
	EntriesRemoved int64
}

func (c *Collector) removePackage(name string, sr *SweepResult) error {
	root := filepath.Join(c.dataDir, "store", name)

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		sr.EntriesRemoved++
		sr.BytesRecovered += info.Size()
		return nil
	})

	os.Remove(root + ".json")

	return os.RemoveAll(root)
}

func (c *Collector) SweepAndRemove() (*SweepResult, error) {
	notInUse, err := c.Sweep()
	if err != nil {
		return nil, err
	}

	var sr SweepResult
	sr.Removed = notInUse

	for _, name := range notInUse {
		err = c.removePackage(name, &sr)
		if err != nil {
			return nil, err
		}
	}

	return &sr, nil
}
