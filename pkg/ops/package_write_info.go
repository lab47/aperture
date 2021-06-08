package ops

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
)

type PackageWriteInfo struct {
	store *config.Store
}

func (p *PackageWriteInfo) Write(pkg *ScriptPackage) (*data.PackageInfo, error) {
	path, err := p.store.Locate(pkg.ID())
	if err != nil {
		return nil, err
	}

	path = filepath.Join(path, ".pkg-info.json")

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var sfd StoreFindDeps
	sfd.store = p.store

	var d ScriptCalcDeps
	d.store = p.store

	allDeps, err := d.BuildDeps(pkg)
	if err != nil {
		return nil, err
	}

	runtimeDeps, err := sfd.PruneDeps(pkg.ID(), allDeps)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to prune deps")
	}

	depIds := []string{}
	for _, dep := range runtimeDeps {
		depIds = append(depIds, dep.ID())
	}

	buildDeps := []string{}
	for _, dep := range allDeps {
		buildDeps = append(buildDeps, dep.ID())
	}

	var inputs []*data.PackageInput

	for _, input := range pkg.cs.Inputs {
		d := &data.PackageInput{
			Name: input.Name,
		}

		if input.Data != nil {
			d.SumType = input.Data.sumType
			d.Sum = input.Data.sumValue
		}

		if input.Data != nil {
			if input.Data.dir != "" {
				d.Dir = input.Data.dir
			} else {
				d.Path = input.Data.path
			}
		} else if input.Instance != nil {
			d.Id = input.Instance.ID()
		}

		inputs = append(inputs, d)
	}

	pi := &data.PackageInfo{
		Id:          pkg.ID(),
		Name:        pkg.Name(),
		Version:     pkg.Version(),
		Repo:        pkg.Repo(),
		DeclDeps:    pkg.DependencyNames(),
		RuntimeDeps: depIds,
		BuildDeps:   buildDeps,
		Constraints: pkg.Constraints(),
		Inputs:      inputs,
	}

	err = json.NewEncoder(f).Encode(&pi)

	pkg.PackageInfo = pi

	return pi, err
}
