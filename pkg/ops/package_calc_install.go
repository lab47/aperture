package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/data"
)

var ErrCorruption = errors.New("corruption detected")

type PackageCalcInstall struct {
	common

	StoreDir string

	carLookup *CarLookup
}

type PackageInstaller interface {
	Install(ctx context.Context, ienv *InstallEnv) error
}

type PackageInfo interface {
	PackageInfo() (name, repo, signer string)
}

type InstallCar struct {
	data *CarData
}

func (i *InstallCar) Install(ctx context.Context, ienv *InstallEnv) error {
	return nil
}

type PackagesToInstall struct {
	PackageIDs   []string
	InstallOrder []string
	Installers   map[string]PackageInstaller
	Dependencies map[string][]string
	Scripts      map[string]*ScriptPackage
	Installed    map[string]bool
	InstallDirs  map[string]string
}

func (p *PackageCalcInstall) isInstalled(id string) (bool, error) {
	if p.StoreDir == "" {
		return false, nil
	}

	path := filepath.Join(p.StoreDir, id, ".pkg-info.json")

	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (p *PackageCalcInstall) consider(
	pkg *ScriptPackage,
	pti *PackagesToInstall,
	seen map[string]int,
) error {
	installed, err := p.isInstalled(pkg.ID())
	if err != nil {
		return err
	}

	pti.PackageIDs = append(pti.PackageIDs, pkg.ID())

	if p.carLookup != nil && pkg.Repo() != "" {
		carData, err := p.carLookup.Lookup(pkg.Repo(), pkg.ID())
		if err != nil {
			return errors.Wrapf(err, "error looking up car: %s/%s", pkg.Repo(), pkg.ID())
		}

		if carData != nil {
			var skip bool

			carInfo, err := carData.Info()
			if err != nil {
				if err == NoCarData {
					skip = true
				} else {
					return errors.Wrapf(err, "error looking up car info: %s/%s", pkg.Repo(), pkg.ID())
				}
			}

			if !skip {
				pti.Installers[pkg.ID()] = &InstallCar{
					data: carData,
				}

				for _, cdep := range carInfo.Dependencies {
					pti.Dependencies[pkg.ID()] = append(pti.Dependencies[pkg.ID()], cdep.ID)

					if _, ok := seen[cdep.ID]; ok {
						seen[cdep.ID]++
						continue
					}

					seen[cdep.ID] = 1

					err = p.considerCarDep(cdep, pti, seen)
					if err != nil {
						return err
					}
				}
				return nil
			}
		}
	}

	if !installed {
		pti.Installers[pkg.ID()] = &ScriptInstall{common: p.common, pkg: pkg}
	}

	pti.Installed[pkg.ID()] = installed
	pti.Scripts[pkg.ID()] = pkg

	for _, dep := range pkg.Dependencies() {
		pti.Dependencies[pkg.ID()] = append(pti.Dependencies[pkg.ID()], dep.ID())

		if _, ok := seen[dep.ID()]; ok {
			seen[dep.ID()]++
			continue
		}

		seen[dep.ID()] = 1

		err = p.consider(dep, pti, seen)
		if err != nil {
			return err
		}
	}

	if !installed {
		for _, dep := range pkg.cs.Instances {
			pti.Dependencies[pkg.ID()] = append(pti.Dependencies[pkg.ID()], dep.ID())
			if _, ok := seen[dep.ID()]; ok {
				seen[dep.ID()]++
				continue
			}

			seen[dep.ID()] = 1

			sp := &ScriptPackage{
				id:       dep.ID(),
				Instance: dep,
			}

			sp.cs.Dependencies = dep.Dependencies
			sp.cs.Install = dep.Fn

			err = p.consider(sp, pti, seen)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *PackageCalcInstall) considerCarDep(
	car *data.CarDependency,
	pti *PackagesToInstall,
	seen map[string]int,
) error {
	installed, err := p.isInstalled(car.ID)
	if err != nil {
		return err
	}

	if installed {
		return nil
	}

	pti.PackageIDs = append(pti.PackageIDs, car.ID)

	carData, err := p.carLookup.Lookup(car.Repo, car.ID)
	if err != nil {
		return err
	}

	if carData == nil {
		return fmt.Errorf("cars can only depend on other cars, but missing: %s/%s", car.Repo, car.ID)
	}

	pti.Installers[car.ID] = &InstallCar{
		data: carData,
	}

	carInfo, err := carData.Info()
	if err != nil {
		return errors.Wrapf(err, "fetching car info: %s/%s", car.Repo, car.ID)
	}

	for _, cdep := range carInfo.Dependencies {
		pti.Dependencies[car.ID] = append(pti.Dependencies[car.ID], cdep.ID)

		if _, ok := seen[cdep.ID]; ok {
			seen[cdep.ID]++
			continue
		}

		seen[cdep.ID] = 1

		err = p.considerCarDep(cdep, pti, seen)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *PackageCalcInstall) Calculate(pkg *ScriptPackage) (*PackagesToInstall, error) {
	return p.CalculateSet([]*ScriptPackage{pkg})
}

func (p *PackageCalcInstall) CalculateSet(pkgs []*ScriptPackage) (*PackagesToInstall, error) {
	var pti PackagesToInstall
	pti.Installers = make(map[string]PackageInstaller)
	pti.Dependencies = make(map[string][]string)
	pti.Scripts = make(map[string]*ScriptPackage)
	pti.Installed = make(map[string]bool)

	seen := map[string]int{}
	for _, pkg := range pkgs {
		seen[pkg.ID()] = 0

		err := p.consider(pkg, &pti, seen)
		if err != nil {
			return nil, err
		}
	}

	var toCheck []string

	for id, deg := range seen {
		if deg == 0 {
			toCheck = append(toCheck, id)
		}
	}

	visited := 0

	var toInstall []string

	for len(toCheck) > 0 {
		x := toCheck[len(toCheck)-1]
		toCheck = toCheck[:len(toCheck)-1]

		toInstall = append(toInstall, x)

		visited++

		for _, dep := range pti.Dependencies[x] {
			deg := seen[dep] - 1
			seen[dep] = deg

			if deg == 0 {
				toCheck = append(toCheck, dep)
			}
		}
	}

	for i := len(toInstall) - 1; i >= 0; i-- {
		pti.InstallOrder = append(pti.InstallOrder, toInstall[i])
	}

	return &pti, nil
}
