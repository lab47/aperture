package ops

import (
	"context"
	"fmt"
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
	common
	data *CarData
}

func (i *InstallCar) Install(ctx context.Context, ienv *InstallEnv) error {
	fmt.Printf("Installing car for %s...\n", i.data.info.ID)
	return i.data.Unpack(ctx, filepath.Join(ienv.StoreDir, i.data.info.ID))
}

type PackagesToInstall struct {
	PackageIDs   []string
	InstallOrder []string
	Installers   map[string]PackageInstaller
	Dependencies map[string][]string
	Scripts      map[string]*ScriptPackage
	Installed    map[string]bool
	InstallDirs  map[string]string
	CarInfo      map[string]*data.CarInfo
}

func (p *PackageCalcInstall) isInstalled(id string) (string, error) {
	sf := StoreFind{StoreDir: p.StoreDir}
	return sf.Find(id)
}

func (p *PackageCalcInstall) consider(
	pkg *ScriptPackage,
	pti *PackagesToInstall,
	seen map[string]int,
) error {
	installPath, err := p.isInstalled(pkg.ID())
	if err != nil {
		return err
	}

	var installed bool

	if installPath != "" {
		installed = true
		pti.InstallDirs[pkg.ID()] = installPath

		var pri PackageReadInfo
		pri.StoreDir = p.StoreDir

		_, err := pri.ReadPath(pkg, installPath)
		if err != nil {
			return err
		}
	}

	pti.PackageIDs = append(pti.PackageIDs, pkg.ID())

	pti.Installed[pkg.ID()] = installed
	pti.Scripts[pkg.ID()] = pkg

	var carData *CarData

	deps := pkg.Dependencies()

	if !installed {
		if p.carLookup != nil {
			carData, err = p.carLookup.Lookup(pkg)
			if err != nil {
				p.L().Debug("error attempting to lookup car", "error", err, "id", pkg.ID())
			}
		}

		if carData != nil {
			info, err := carData.Info()
			if err != nil {
				return err
			}

			pti.CarInfo[pkg.ID()] = info
			pti.Installers[pkg.ID()] = &InstallCar{common: p.common, data: carData}

			var pruned []*ScriptPackage

			set := map[string]struct{}{}

			for _, dep := range info.Dependencies {
				set[dep.ID] = struct{}{}
			}

			for _, dep := range deps {
				if _, ok := set[dep.ID()]; ok {
					pruned = append(pruned, dep)
				}
			}

			deps = pruned
		} else {
			pti.Installers[pkg.ID()] = &ScriptInstall{common: p.common, pkg: pkg}

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
	}

	for _, dep := range deps {
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
	pti.InstallDirs = make(map[string]string)
	pti.CarInfo = make(map[string]*data.CarInfo)

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
