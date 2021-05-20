package ops

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/lab47/exprcore/exprcore"
	"github.com/mitchellh/go-homedir"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/homebrew"
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
	constraints map[string]string

	cfg Config

	toInstall []*ScriptPackage

	homebrewPackages []string
}

type Project struct {
	Cellar    string
	Install   []*ScriptPackage
	Requested []string

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

func (c *ProjectLoad) Load() (*Project, error) {
	c.constraints = config.SystemConstraints()

	vars := exprcore.StringDict{
		"install":  exprcore.NewBuiltin("install", c.installFn),
		"homebrew": exprcore.NewBuiltin("homebrew", c.homebrewFn),
		"platform": &platform{},
	}

	_, prog, err := exprcore.SourceProgram("project.chell", nil, vars.Has)
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
	proj.Cellar = homebrew.DefaultCellar()
	proj.Install = c.toInstall
	proj.homebrewPackages = c.homebrewPackages

	return &proj, nil
}

type ScriptInstaller struct {
	*ScriptPackage
}

func (i *ScriptInstaller) Install(tmpdir string) error {
	var pci PackageCalcInstall
	pci.StoreDir = "/opt/iris/store"

	toInstall, err := pci.Calculate(i.ScriptPackage)
	if err != nil {
		return err
	}

	buildDir, err := ioutil.TempDir(tmpdir, "build-"+i.id)
	if err != nil {
		return err
	}

	ienv := &InstallEnv{
		BuildDir: buildDir,
		StoreDir: tmpdir,
	}

	err = os.MkdirAll(ienv.StoreDir, 0755)
	if err != nil {
		return err
	}

	var pkgInst PackagesInstall

	err = pkgInst.Install(context.TODO(), ienv, toInstall)
	if err != nil {
		return err
	}

	return nil
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
	dir, err := homedir.Expand("~/.config/iris/packages")
	if err != nil {
		return nil, err
	}

	cr := &ConfigRepo{
		Path: dir,
	}

	var sl ScriptLoad
	sl.lookup = &ScriptLookup{}

	data, err := sl.Load(name, WithConfigRepo(cr))
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

	dir, err := homedir.Expand("~/.config/iris/packages")
	if err != nil {
		return nil, err
	}

	cr := &ConfigRepo{
		Path: dir,
	}

	var sl ScriptLoad
	sl.lookup = &ScriptLookup{}

	data, err := sl.Load(name, WithConfigRepo(cr))
	if err != nil {
		return nil, err
	}

	return data, nil
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
	pci.StoreDir = ienv.StoreDir

	toInstall, err := pci.CalculateSet(p.Install)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 2, 2, 1, ' ', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "NAME\tVERSION\tSTATUS\tDEPENDENCIES\n")

	for _, p := range toInstall.InstallOrder {
		flag := " "
		if toInstall.Installed[p] {
			flag = "I"
		}

		var shortDeps []string

		for _, dep := range toInstall.Dependencies[p] {
			scr := toInstall.Scripts[dep]

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

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", script.Name(), script.Version(), flag, deps)
	}

	return nil
}

func (p *Project) InstallPackages(ctx context.Context, ienv *InstallEnv) (
	[]string, *PackagesToInstall, error,
) {
	var pci PackageCalcInstall
	pci.StoreDir = ienv.StoreDir

	var requested []string

	for _, p := range p.Install {
		requested = append(requested, p.ID())
	}

	toInstall, err := pci.CalculateSet(p.Install)
	if err != nil {
		return nil, nil, err
	}

	err = os.MkdirAll(ienv.StoreDir, 0755)
	if err != nil {
		return nil, nil, err
	}

	var pkgInst PackagesInstall

	err = pkgInst.Install(ctx, ienv, toInstall)
	if err != nil {
		return nil, nil, err
	}

	res, err := p.Resolve()
	if err != nil {
		return nil, nil, err
	}

	urls, err := res.ComputeURLs()
	if err != nil {
		return nil, nil, err
	}

	var d homebrew.Downloader

	hbtmp := filepath.Join(ienv.StoreDir, "_hb-cache")
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

		requested = append(requested, rp.Name)
		toInstall.InstallDirs[rp.Name] = pkgPath
	}

	return requested, toInstall, nil
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
