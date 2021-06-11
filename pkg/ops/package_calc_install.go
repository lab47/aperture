package ops

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
)

var ErrCorruption = errors.New("corruption detected")

type PackageCalcInstall struct {
	common

	Store *config.Store

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

	pkg  *ScriptPackage
	data *CarData
}

func (i *InstallCar) Install(ctx context.Context, ienv *InstallEnv) error {
	fmt.Printf("Installing car for %s...\n", i.data.info.ID)
	path, err := ienv.Store.Locate(i.data.info.ID)
	if err != nil {
		return err
	}

	err = i.data.Unpack(ctx, path)
	if err != nil {
		return err
	}

	if i.pkg.cs.PostInstall != nil {
		var mod InstallEnv = *ienv

		mod.OnlyPostInstall = true

		var si ScriptInstall
		si.common = i.common

		si.pkg = i.pkg
		return si.Install(ctx, &mod)
	}

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
	CarInfo      map[string]*data.CarInfo
}

func (p *PackageCalcInstall) isInstalled(id string) (string, error) {
	return p.Store.Locate(id)
}

// runtimeDeps returns the packages that the given package needs at runtime. If pkg
// is not installed, then we return all dependencies.
func (p *PackageCalcInstall) runtimeDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	runtimeDeps := pkg.Dependencies()

	installPath, err := p.isInstalled(pkg.ID())
	if err != nil || installPath == "" {
		for _, dep := range pkg.cs.Instances {
			sp := &ScriptPackage{
				id:       dep.ID(),
				Instance: dep,
			}

			sp.cs.Dependencies = dep.Dependencies
			sp.cs.Install = dep.Fn

			runtimeDeps = append(runtimeDeps, sp)
		}

	} else {
		var pri PackageReadInfo
		pri.Store = p.Store

		pi, err := pri.ReadPath(pkg, installPath)
		if err != nil {
			return nil, err
		}

		var pruned []*ScriptPackage

	outer:
		for _, sp := range runtimeDeps {
			for _, id := range pi.RuntimeDeps {
				if id == sp.ID() {
					pruned = append(pruned, sp)
					continue outer
				}
			}
		}

		runtimeDeps = pruned
	}

	sort.Slice(runtimeDeps, func(i, j int) bool {
		var (
			in = runtimeDeps[i].Name()
			jn = runtimeDeps[j].Name()
		)

		if in == jn {
			return runtimeDeps[i].Version() < runtimeDeps[j].Version()
		}

		return in < jn
	})

	return runtimeDeps, nil
}

func (p *PackageCalcInstall) calcSet(
	pkgs []*ScriptPackage,
	pti *PackagesToInstall,
	seen map[string]int,
) error {

	// Step one, gather all possible dependencies needed by looking at
	// ScriptPackage Dependencies and runtime deps of any installed packages.
	set := make(map[string]struct{})
	pkgDeps := make(map[string][]*ScriptPackage)

	var (
		toProcess []*ScriptPackage
		full      []*ScriptPackage
	)

	for _, pkg := range pkgs {
		set[pkg.ID()] = struct{}{}

		deps, err := p.runtimeDeps(pkg)
		if err != nil {
			return err
		}

		pkgDeps[pkg.ID()] = deps

		toProcess = append(toProcess, deps...)
	}

	for len(toProcess) > 0 {
		pkg := toProcess[0]
		toProcess = toProcess[1:]

		if _, ok := set[pkg.ID()]; ok {
			continue
		}

		full = append(full, pkg)

		set[pkg.ID()] = struct{}{}

		runtimeDeps, err := p.runtimeDeps(pkg)
		if err != nil {
			return err
		}

		pkgDeps[pkg.ID()] = runtimeDeps

		for _, d := range runtimeDeps {
			if _, ok := set[d.ID()]; ok {
				continue
			}

			toProcess = append(toProcess, d)
		}
	}

	// Step 2, for the uninstalled packages, gather any car info about them.

	type lookupData struct {
		pkg  *ScriptPackage
		car  *data.CarInfo
		data *CarData
		err  error
	}

	var mu sync.Mutex

	carInfo := make(map[string]lookupData)

	requests := make(chan *ScriptPackage, len(set))

	var wg sync.WaitGroup

	// Do a maximum of 20 car lookups at a time.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for pkg := range requests {
				carData, err := p.carLookup.Lookup(pkg)
				if err != nil {
					return
				}

				if carData == nil {
					return
				}

				info, err := carData.Info()
				if err != nil {
					return
				}

				mu.Lock()
				carInfo[pkg.ID()] = lookupData{
					pkg:  pkg,
					car:  info,
					data: carData,
				}
				mu.Unlock()
			}
		}()
	}

	for _, pkg := range full {
		ip, err := p.isInstalled(pkg.ID())
		if err != nil {
			if errors.Is(err, config.ErrNoEntry) {
				continue
			}

			return err
		}

		if ip != "" {
			continue
		}

		requests <- pkg
	}

	close(requests)

	wg.Wait()

	// Step 3, go back through the toplevel requested packages and attempt to
	// use any car dependencies

	set = make(map[string]struct{})

	full = nil
	toProcess = nil

	for _, pkg := range pkgs {
		seen[pkg.ID()] = 0
		toProcess = append(toProcess, pkg)
	}

	for len(toProcess) > 0 {
		pkg := toProcess[0]
		toProcess = toProcess[1:]

		installPath, err := p.isInstalled(pkg.ID())
		if err != nil {
			if !errors.Is(err, config.ErrNoEntry) {
				return err
			}
		}

		pti.InstallDirs[pkg.ID()] = installPath
		pti.Installed[pkg.ID()] = installPath != ""
		pti.PackageIDs = append(pti.PackageIDs, pkg.ID())
		pti.Scripts[pkg.ID()] = pkg

		deps := pkgDeps[pkg.ID()]

		car, ok := carInfo[pkg.ID()]
		if ok {
			pti.CarInfo[pkg.ID()] = car.car
			pti.Installers[pkg.ID()] = &InstallCar{
				common: p.common,
				pkg:    pkg,
				data:   car.data,
			}

			var pruned []*ScriptPackage

		outer:
			for _, sp := range deps {
				for _, cd := range car.car.Dependencies {
					if cd.ID == sp.ID() {
						pruned = append(pruned, sp)
						continue outer
					}
				}
			}

			deps = pruned
		} else {
			pti.Installers[pkg.ID()] = &ScriptInstall{common: p.common, pkg: pkg}
		}

		for _, d := range deps {
			pti.Dependencies[pkg.ID()] = append(pti.Dependencies[pkg.ID()], d.ID())

			if _, ok := seen[d.ID()]; ok {
				seen[d.ID()]++
				continue
			}

			seen[d.ID()] = 1

			toProcess = append(toProcess, d)
		}
	}

	return nil
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
		pri.Store = p.Store

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

	err := p.calcSet(pkgs, &pti, seen)
	if err != nil {
		return nil, err
	}

	// for _, pkg := range pkgs {
	// seen[pkg.ID()] = 0

	// err := p.consider(pkg, &pti, seen)
	// if err != nil {
	// return nil, err
	// }
	// }

	var toCheck []string

	var sorted []string

	for id := range seen {
		sorted = append(sorted, id)
	}

	sort.Strings(sorted)

	for _, id := range sorted {
		deg := seen[id]

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
