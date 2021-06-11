package ops

import (
	"lab47.dev/aperture/pkg/config"
)

type ScriptCalcDeps struct {
	store *config.Store
}

func (i *ScriptCalcDeps) pkgRuntimeDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	runtimeDeps := pkg.Dependencies()

	var pri PackageReadInfo
	pri.Store = i.store

	pi, err := pri.Read(pkg)
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

	return runtimeDeps, nil
}

func (i *ScriptCalcDeps) RuntimeDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	direct, err := i.pkgRuntimeDeps(pkg)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	return i.walkFromDeps(direct, seen)
}

func (i *ScriptCalcDeps) EvalDeps(pkgs []*ScriptPackage) ([]*ScriptPackage, error) {
	seen := make(map[string]*ScriptPackage)
	usage := make(map[string]int)
	deps := make(map[string][]*ScriptPackage)

	// fully build seen and usage

	for _, pkg := range pkgs {
		seen[pkg.ID()] = pkg
		usage[pkg.ID()] = 0
	}

	for len(pkgs) > 0 {
		pkg := pkgs[0]
		pkgs = pkgs[1:]

		rd, err := i.pkgRuntimeDeps(pkg)
		if err != nil {
			return nil, err
		}

		deps[pkg.ID()] = rd

		for _, dep := range rd {
			if _, ok := seen[dep.ID()]; !ok {
				seen[dep.ID()] = dep
				pkgs = append(pkgs, dep)
			}

			usage[dep.ID()]++
		}
	}

	var toCheck []string

	for name, count := range usage {
		if count == 0 {
			toCheck = append(toCheck, name)
		}
	}

	var output []*ScriptPackage

	for len(toCheck) > 0 {
		x := toCheck[len(toCheck)-1]
		toCheck = toCheck[:len(toCheck)-1]

		pkg := seen[x]

		output = append(output, pkg)

		for _, dep := range deps[pkg.ID()] {
			deg := usage[dep.ID()] - 1
			usage[dep.ID()] = deg

			if deg == 0 {
				toCheck = append(toCheck, dep.ID())
			}
		}
	}

	for i := len(output)/2 - 1; i >= 0; i-- {
		opp := len(output) - 1 - i
		output[i], output[opp] = output[opp], output[i]
	}

	return output, nil
}

func (i *ScriptCalcDeps) BuildDeps(pkg *ScriptPackage) ([]*ScriptPackage, error) {
	seen := make(map[string]struct{})
	return i.walkFromDeps(pkg.Dependencies(), seen)
}

func (i *ScriptCalcDeps) walkFromDeps(deps []*ScriptPackage, seen map[string]struct{}) ([]*ScriptPackage, error) {
	var output []*ScriptPackage

	for len(deps) > 0 {
		dep := deps[0]
		deps = deps[1:]

		if _, ok := seen[dep.ID()]; ok {
			continue
		}

		seen[dep.ID()] = struct{}{}

		output = append(output, dep)

		runtimDeps, err := i.pkgRuntimeDeps(dep)
		if err != nil {
			return nil, err
		}

		for _, x := range runtimDeps {
			if _, ok := seen[x.ID()]; ok {
				continue
			}

			deps = append(deps, x)
		}
	}

	return output, nil
}
