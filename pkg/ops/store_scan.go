package ops

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
)

type StoreScan struct{}

type ScannedPackage struct {
	Path    string
	Info    *data.PackageInfo
	Package *ScriptPackage
}

func (s *StoreScan) Scan(cfg *config.Config, validate bool) ([]*ScannedPackage, error) {
	store := cfg.StorePath()

	f, err := os.Open(store)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var out []*ScannedPackage

	for {
		names, err := f.Readdirnames(50)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		for _, n := range names {
			info := filepath.Join(store, n, ".pkg-info.json")

			g, err := os.Open(info)
			if err != nil {
				continue
			}

			var pk data.PackageInfo
			err = json.NewDecoder(g).Decode(&pk)

			g.Close()

			if err != nil {
				return nil, err
			}

			if pk.Id != n || pk.Name == "" {
				continue
			}

			out = append(out, &ScannedPackage{
				Path: filepath.Join(store, n),
				Info: &pk,
			})
		}
	}

	if !validate {
		return out, nil
	}

	return s.validate(cfg, out)
}

func (s *StoreScan) validate(
	cfg *config.Config, out []*ScannedPackage,
) ([]*ScannedPackage, error) {
	checked := map[string]*ScannedPackage{}

	pkgs := map[string]*ScriptPackage{}

	var pl ProjectLoad

	for _, sp := range out {
		pkg, ok := pkgs[sp.Info.Name]
		if !ok {
			proj, err := pl.Single(cfg, sp.Info.Name)
			if err != nil {
				return nil, err
			}

			for _, found := range proj.Install {
				pkgs[found.Name()] = found
			}

			pkg, ok = pkgs[sp.Info.Name]
			if !ok {
				return nil, fmt.Errorf("project load didn't load needed package")
			}
		}

		if sp.Info.Id == pkg.ID() {
			sp.Package = pkg
			checked[sp.Info.Name] = sp
		}
	}

	var proper []*ScannedPackage

	for _, sp := range checked {
		proper = append(proper, sp)
	}

	sort.Slice(proper, func(i, j int) bool {
		return proper[i].Info.Name < proper[j].Info.Name
	})

	return proper, nil
}
