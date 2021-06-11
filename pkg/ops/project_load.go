package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/lab47/exprcore/exprcore"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
	"lab47.dev/aperture/pkg/homebrew"
	"lab47.dev/aperture/pkg/progress"
)

type ConfigRepo struct {
	Github string
	Path   string
}

type Config struct {
	Repos map[string]*ConfigRepo
}

type Installable interface {
	Name() string
	Version() string
	DependencyNames() []string
	Install(tmpdir string) error
}

type ProjectLoad struct {
	common

	lockFile *data.LockFile
	path     []string

	constraints map[string]string

	// cfg Config
	// cfg *config.Config

	cfg   *config.Config
	store *config.Store

	toInstall []*ScriptPackage

	homebrewPackages []string
}

type Project struct {
	common

	Constraints map[string]string
	Cellar      string
	Install     []*ScriptPackage
	Requested   []string

	homebrewPackages []string
}

var repotype = exprcore.FromStringDict(exprcore.Root, nil)

type platform struct {
}

func (p *platform) String() string        { return "platform" }
func (p *platform) Type() string          { return "platform" }
func (p *platform) Freeze()               {}
func (p *platform) Truth() exprcore.Bool  { return exprcore.True }
func (p *platform) Hash() (uint32, error) { return 0, nil }

func (p *platform) Attr(name string) (exprcore.Value, error) {
	switch name {
	case "os":
		return exprcore.String(runtime.GOOS), nil
	case "arch":
		return exprcore.String(runtime.GOARCH), nil
	default:
		return exprcore.None, fmt.Errorf("unknown attr: %s", name)
	}
}

func (p *platform) AttrNames() []string {
	return []string{"os", "arch"}
}

func (c *ProjectLoad) Load(cfg *config.Config) (*Project, error) {
	c.store = cfg.Store()

	var lf data.LockFile
	f, err := os.Open("aperture-lock.json")
	if err == nil {
		err = json.NewDecoder(f).Decode(&lf)
		if err != nil {
			return nil, err
		}
	} else {
		lf = data.LockFile{
			&data.LockFileEntry{
				Name: "aperture",
				Path: "github.com/lab47/aperture-packages",
			},
		}
	}

	for _, ent := range lf {
		if len(ent.Path) >= 2 && ent.Path[0] == '~' {
			dir, err := homedir.Expand(ent.Path)
			if err != nil {
				return nil, err
			}

			ent.Path = dir
		}

		c.path = append(c.path, ent.Path)
	}

	c.constraints = config.SystemConstraints()

	vars := exprcore.StringDict{
		"install":  exprcore.NewBuiltin("install", c.installFn),
		"homebrew": exprcore.NewBuiltin("homebrew", c.homebrewFn),
		"platform": &platform{},
	}

	_, prog, err := exprcore.SourceProgram("project"+Extension, nil, vars.Has)
	if err != nil {
		return nil, err
	}

	var thread exprcore.Thread

	thread.Import = c.importPkg

	_, _, err = prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	var proj Project
	proj.Constraints = c.constraints
	proj.Cellar = homebrew.DefaultCellar()
	proj.Install = c.toInstall
	proj.homebrewPackages = c.homebrewPackages

	return &proj, nil
}

func (c *ProjectLoad) Single(cfg *config.Config, name string) (*Project, error) {
	c.store = cfg.Store()

	var lf data.LockFile
	f, err := os.Open("aperture-lock.json")
	if err == nil {
		err = json.NewDecoder(f).Decode(&lf)
		if err != nil {
			return nil, errors.Wrapf(err, "decoding aperture-lock.json")
		}

		for _, ent := range lf {
			if len(ent.Path) >= 2 && ent.Path[0] == '~' {
				dir, err := homedir.Expand(ent.Path)
				if err != nil {
					return nil, errors.Wrapf(err, "expanding with homedir")
				}

				ent.Path = dir
			}

			c.path = append(c.path, ent.Path)
		}
	} else {
		c.path = cfg.LoadPath()
	}

	c.constraints = config.SystemConstraints()

	pkg, err := c.loadScript(name)
	if err != nil {
		return nil, errors.Wrapf(err, "attempting to load '%s'", name)
	}

	var proj Project
	proj.Constraints = c.constraints
	proj.Cellar = homebrew.DefaultCellar()
	proj.Install = []*ScriptPackage{pkg}

	return &proj, nil
}

func (l *ProjectLoad) installFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	for _, arg := range args {
		switch i := arg.(type) {
		case *LoadedPackage:
			l.homebrewPackages = append(l.homebrewPackages, i.name)
		case *ScriptPackage:
			l.toInstall = append(l.toInstall, i)
		case exprcore.String:
			sp, err := l.loadScript(string(i))
			if err != nil {
				return nil, err
			}

			l.toInstall = append(l.toInstall, sp)
		default:
			sp, err := ProcessPrototype(arg, l.constraints)
			if err != nil {
				return nil, err
			}

			l.toInstall = append(l.toInstall, sp)
		}
	}

	return exprcore.None, nil
}

type LoadedPackage struct {
	name string
	// installables []Installable
}

func (l *LoadedPackage) String() string {
	return fmt.Sprintf("<loaded package: %s>", l.name)
}

func (l *LoadedPackage) Type() string {
	return "loaded-package"
}

func (l *LoadedPackage) Freeze() {}

func (l *LoadedPackage) Truth() exprcore.Bool {
	return exprcore.True
}

func (l *LoadedPackage) Hash() (uint32, error) {
	return exprcore.String(l.name).Hash()
}

func (l *ProjectLoad) loadScript(name string) (*ScriptPackage, error) {
	var sl ScriptLoad
	sl.common = l.common

	sl.lookup = &ScriptLookup{
		common: l.common,
		Path:   l.path,
	}

	sl.Store = l.store

	data, err := sl.Load(name, WithConstraints(l.constraints))
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (l *ProjectLoad) homebrewFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var name string

	if err := exprcore.UnpackArgs(
		"homebrew", args, kwargs,
		"name", &name,
	); err != nil {
		return nil, err
	}

	return &LoadedPackage{name: name}, nil
}

func (s *ProjectLoad) importPkg(thread *exprcore.Thread, ns, name string, args *exprcore.Dict) (exprcore.Value, error) {
	if ns == "homebrew" {
		/*
			cellar := homebrew.DefaultCellar()
			err := os.MkdirAll(cellar, 0755)
			if err != nil {
				log.Fatal(err)
			}

			r, err := homebrew.NewResolver(cellar, "./gen/packages")
			if err != nil {
				log.Fatal(err)
			}

			pkgs, err := homebrew.GetInstallables(r, cellar, name)
			if err != nil {
				return nil, err
			}

			var insts []Installable

			for _, p := range pkgs {
				insts = append(insts, p)
			}
		*/

		return &LoadedPackage{name: name}, nil
	}

	return s.loadScript(name)
}

func (p *Project) Resolve() (*homebrew.Resolution, error) {
	err := os.MkdirAll(p.Cellar, 0755)
	if err != nil {
		return nil, err
	}

	dir, err := homedir.Expand("~/.config/iris/homebrew-packages")
	if err != nil {
		return nil, err
	}

	r, err := homebrew.NewResolver(p.Cellar, dir)
	if err != nil {
		return nil, err
	}

	for _, name := range p.homebrewPackages {
		err := r.Add(name)
		if err != nil {
			return nil, err
		}
	}

	return r.Resolve()
}

func (p *Project) Explain(ctx context.Context, ienv *InstallEnv) error {
	var pci PackageCalcInstall
	pci.common = p.common

	var cl CarLookup
	pci.carLookup = &cl

	pci.Store = ienv.Store

	toInstall, err := pci.CalculateSet(p.Install)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 2, 2, 1, ' ', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "ID\tNAME\tVERSION\tSTATUS\tDEPENDENCIES\n")

	for _, p := range toInstall.InstallOrder {
		flag := " "
		if toInstall.Installed[p] {
			flag = "I"
		} else {
			if _, ok := toInstall.CarInfo[p]; ok {
				flag = "C"
			}
		}

		var shortDeps []string

		for _, id := range toInstall.Dependencies[p] {
			scr := toInstall.Scripts[id]

			if scr == nil || scr.Name() == "" {
				continue
			}

			shortDeps = append(shortDeps, scr.Name())
		}

		deps := strings.Join(shortDeps, " ")

		script := toInstall.Scripts[p]

		if script == nil || script.Name() == "" {
			continue
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", script.ID()[:8], script.Name(), script.Version(), flag, deps)
	}

	res, err := p.Resolve()
	if err != nil {
		return err
	}

	for _, pkg := range res.ToInstall {
		flag := " "
		if pkg.Installed {
			flag = "I"
		}

		ib := strings.Join(pkg.IncludedByNames, ", ")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", pkg.Name, pkg.Version, flag, ib)
	}

	return nil
}

func (p *Project) CalculateSet(ctx context.Context, ienv *InstallEnv) (*PackagesToInstall, error) {
	var pci PackageCalcInstall
	pci.Store = ienv.Store

	var requested []string

	for _, p := range p.Install {
		requested = append(requested, p.ID())
	}

	return pci.CalculateSet(p.Install)
}

func (p *Project) InstallPackages(ctx context.Context, ienv *InstallEnv) (
	[]string, *PackagesToInstall, error,
) {
	var pci PackageCalcInstall
	pci.common = p.common
	pci.Store = ienv.Store

	var cl CarLookup
	pci.carLookup = &cl

	var requested []string

	for _, p := range p.Install {
		requested = append(requested, p.ID())
	}

	toInstall, err := pci.CalculateSet(p.Install)
	if err != nil {
		return nil, nil, err
	}

	err = os.MkdirAll(ienv.Store.Default, 0755)
	if err != nil {
		return nil, nil, err
	}

	var pkgInst PackagesInstall
	pkgInst.common = p.common

	err = pkgInst.Install(ctx, ienv, toInstall)
	if err != nil {
		return nil, nil, err
	}

	res, err := p.Resolve()
	if err != nil {
		return nil, nil, err
	}

	add := map[string]struct{}{}

	for _, name := range p.homebrewPackages {
		add[name] = struct{}{}
	}

	for _, rp := range res.ToInstall {
		_, ok := add[rp.Name]
		if ok && rp.Installed {
			requested = append(requested, rp.Name)
			toInstall.InstallDirs[rp.Name] =
				filepath.Join(p.Cellar, rp.Name, rp.Version)
		}
	}

	res.PruneInstalled()

	urls, err := res.ComputeURLs()
	if err != nil {
		return nil, nil, err
	}

	if len(urls) > 0 {
		var d homebrew.Downloader

		hbtmp := filepath.Join(ienv.Store.Default, "_hb-cache")
		err = os.MkdirAll(hbtmp, 0755)
		if err != nil {
			return nil, nil, err
		}

		files, err := d.Stage(hbtmp, urls)
		if err != nil {
			return nil, nil, err
		}

		for i, rp := range res.ToInstall {
			u := urls[i]
			pkgPath, err := p.install(ctx, rp, u.Binary, files[u.URL])
			if err != nil {
				return nil, nil, err
			}

			if _, ok := add[rp.Name]; ok {
				requested = append(requested, rp.Name)
				toInstall.InstallDirs[rp.Name] = pkgPath
			}
		}
	}

	return requested, toInstall, nil
}

type ExportedCar struct {
	Package *ScriptPackage
	Info    *data.CarInfo
	Path    string
	Sum     []byte
}

func (p *Project) Export(ctx context.Context, cfg *config.Config, dest string) ([]*ExportedCar, error) {
	var pri PackageReadInfo
	pri.Store = cfg.Store()

	var scd ScriptCalcDeps
	scd.store = cfg.Store()

	pkgs, err := scd.EvalDeps(p.Install)
	if err != nil {
		return nil, err
	}

	var export []*ExportedCar

	pb := progress.Count(ctx, int64(len(pkgs)), "Exporting")
	defer pb.Close()

	for _, pkg := range pkgs {
		pb.On(pkg.requestName)

		carPath := filepath.Join(dest, pkg.ID()+".car")
		if _, err := os.Stat(carPath); err == nil {
			export = append(export, &ExportedCar{
				Package: pkg,
				Path:    carPath,
			})

			continue
		}

		pi, err := pri.Read(pkg)
		if err != nil {
			return nil, err
		}

		var deps []*data.CarDependency

		for _, d := range pi.RuntimeDeps {
			deps = append(deps, &data.CarDependency{
				ID: d,
			})
		}

		osName, osVer, arch := config.Platform()

		ci := &data.CarInfo{
			ID:           pkg.ID(),
			Name:         pkg.Name(),
			Version:      pkg.Version(),
			Repo:         pkg.Repo(),
			Constraints:  p.Constraints,
			Dependencies: deps,
			Platform: &data.CarPlatform{
				OS:        osName,
				OSVersion: osVer,
				Arch:      arch,
			},
		}

		f, err := os.Create(carPath)
		if err != nil {
			return nil, err
		}

		defer f.Close()

		var cp CarPack
		cp.PrivateKey = cfg.Private()
		cp.PublicKey = cfg.Public()

		path, err := pri.Store.Locate(pkg.ID())
		if err != nil {
			return nil, err
		}

		err = cp.Pack(ci, path, f)
		if err != nil {
			return nil, err
		}

		export = append(export, &ExportedCar{
			Package: pkg,
			Info:    ci,
			Path:    carPath,
			Sum:     cp.Sum,
		})

		pb.Tick()
	}

	return export, nil
}

func (p *Project) installPackagesHB(ctx context.Context, tmpdir string) error {
	res, err := p.Resolve()

	urls, err := res.ComputeURLs()
	if err != nil {
		return err
	}

	var d homebrew.Downloader

	files, err := d.Stage(tmpdir, urls)
	if err != nil {
		return err
	}

	for i, rp := range res.ToInstall {
		u := urls[i]
		_, err := p.install(ctx, rp, u.Binary, files[u.URL])
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Project) ExplainHomebrew() error {
	res, err := p.Resolve()
	if err != nil {
		return err
	}

	var d homebrew.Downloader

	return res.Explain(os.Stdout, &d)
}

func (p *Project) install(
	ctx context.Context,
	pkg *homebrew.ResolvedPackage,
	bin *homebrew.Binary,
	path string,
) (string, error) {
	u, err := homebrew.NewUnpacker(p.Cellar)
	if err != nil {
		return "", err
	}

	pkgPath, err := u.Unpack(pkg, bin, path)
	if err != nil {
		return "", err
	}

	return pkgPath, nil
}
