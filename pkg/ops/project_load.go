package ops

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/itchyny/gojq"
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

type ProjectSource struct {
	Name string
	Ref  string
}

type ProjectLoad struct {
	common

	sources []*ProjectSource

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

func (c *ProjectLoad) setupPath(ctx context.Context, cfg *config.Config) error {
	var pp []*config.PackagePath

	for _, src := range c.sources {
		path, err := config.CalcPath(src.Name, src.Ref)
		if err != nil {
			return err
		}

		pp = append(pp, path)
	}

	if len(pp) == 0 {
		paths, err := cfg.PackagePath()
		if err != nil {
			return err
		}

		pp = paths
	}

	saveLock := false

	var lf data.LockFile
	f, err := os.Open("aperture-lock.json")
	if err == nil {
		defer f.Close()
		err = json.NewDecoder(f).Decode(&lf)
		if err != nil {
			return err
		}

	outer:
		for _, ent := range lf.Sources {
			for _, path := range pp {
				if path.Location == ent.Ref && path.Version == ent.RequestedVersion {
					path.ResolvedVersion = ent.ResolvedVersion
					continue outer
				}
			}

			// An entry in sources didn't have a lock entry
			saveLock = true
		}
	} else {
		saveLock = true
	}

	c.path, err = cfg.MapPaths(ctx, pp)
	if err != nil {
		return err
	}

	if !saveLock {
		return nil
	}

	var nlf data.LockFile
	nlf.CreatedAt = time.Now()

	for _, path := range pp {
		nlf.Sources = append(nlf.Sources, &data.LockFileEntry{
			Name:             path.Name,
			Ref:              path.Location,
			RequestedVersion: path.Version,
			ResolvedVersion:  path.ResolvedVersion,
		})
	}

	of, err := os.Create("aperture-lock.json")
	if err != nil {
		return err
	}

	defer of.Close()

	return json.NewEncoder(of).Encode(&nlf)
}

func (c *ProjectLoad) Load(ctx context.Context, cfg *config.Config) (*Project, error) {
	c.cfg = cfg
	c.store = cfg.Store()

	c.constraints = config.SystemConstraints()

	vars := exprcore.StringDict{
		"install":  exprcore.NewBuiltin("install", c.installFn),
		"source":   exprcore.NewBuiltin("source", c.sourceFn),
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

func (c *ProjectLoad) Single(ctx context.Context, cfg *config.Config, name string) (*Project, error) {
	c.cfg = cfg
	c.store = cfg.Store()

	c.constraints = config.SystemConstraints()

	pkg, err := c.loadScript(ctx, "", name)
	if err != nil {
		return nil, errors.Wrapf(err, "attempting to load '%s'", name)
	}

	var proj Project
	proj.Constraints = c.constraints
	proj.Cellar = homebrew.DefaultCellar()
	proj.Install = []*ScriptPackage{pkg}

	return &proj, nil
}

func (c *ProjectLoad) LoadSet(ctx context.Context, cfg *config.Config, path string) (*Project, error) {
	c.cfg = cfg
	c.store = cfg.Store()

	c.constraints = config.SystemConstraints()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	r := bufio.NewReader(f)

	var proj Project
	proj.common = c.common
	proj.Constraints = c.constraints
	proj.Cellar = homebrew.DefaultCellar()

	for {
		line, _ := r.ReadString('\n')
		if line == "" {
			break
		}

		line = strings.TrimSpace(line)

		pkg, err := c.loadScript(ctx, "", line)
		if err != nil {
			return nil, errors.Wrapf(err, "attempting to load '%s'", line)
		}

		proj.Install = append(proj.Install, pkg)
	}

	return &proj, nil
}

func (l *ProjectLoad) sourceFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	// Clearly any previously used path
	l.path = nil

	for _, arg := range args {
		switch i := arg.(type) {
		case exprcore.String:
			l.sources = append(l.sources, &ProjectSource{
				Ref: string(i),
			})
		}
	}

	for _, tup := range kwargs {
		key := tup[0].(exprcore.String)

		switch i := tup[1].(type) {
		case exprcore.String:
			l.sources = append(l.sources, &ProjectSource{
				Name: string(key),
				Ref:  string(i),
			})
		}
	}

	return exprcore.None, nil
}

func (l *ProjectLoad) installFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	for _, arg := range args {
		switch i := arg.(type) {
		case *PackageSelector:
			l.homebrewPackages = append(l.homebrewPackages, i.Name)
		case *ScriptPackage:
			l.toInstall = append(l.toInstall, i)
		case exprcore.String:
			sp, err := l.loadScript(context.Background(), "", string(i))
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

type PackageSelector struct {
	Namespace string
	Name      string
}

func (l *PackageSelector) String() string {
	return fmt.Sprintf("<package-selector: %s>", l.Name)
}

func (l *PackageSelector) Type() string {
	return "package-selector"
}

func (l *PackageSelector) Freeze() {}

func (l *PackageSelector) Truth() exprcore.Bool {
	return exprcore.True
}

func (l *PackageSelector) Hash() (uint32, error) {
	return exprcore.String(l.Name).Hash()
}

type SearchCond interface {
	Match(ent *data.RepoEntry) bool
}

type searchRegexp struct {
	re *regexp.Regexp
}

func (se *searchRegexp) Match(ent *data.RepoEntry) bool {
	return se.re.MatchString(ent.Name)
}

func SearchRegexp(str string) (SearchCond, error) {
	re, err := regexp.Compile(str)
	if err != nil {
		return nil, err
	}

	return &searchRegexp{re}, nil
}

type searchJQ struct {
	code *gojq.Code
}

func (se *searchJQ) Match(ent *data.RepoEntry) bool {
	data, err := json.Marshal(ent)
	if err != nil {
		panic(err)
	}

	var x interface{}

	err = json.Unmarshal(data, &x)
	if err != nil {
		panic(err)
	}

	iter := se.code.Run(x)

	v, ok := iter.Next()
	if !ok {
		return false
	}

	if b, ok := v.(bool); ok {
		return b
	}

	return false
}

func SearchJQ(str string) (SearchCond, error) {
	p, err := gojq.Parse(str)
	if err != nil {
		return nil, err
	}
	code, err := gojq.Compile(p)
	if err != nil {
		return nil, err
	}

	return &searchJQ{code}, nil
}

func (l *ProjectLoad) Search(ctx context.Context, code SearchCond) ([]*data.RepoEntry, error) {
	if l.path == nil {
		err := l.setupPath(ctx, l.cfg)
		if err != nil {
			return nil, err
		}
	}

	var results []*data.RepoEntry

	for _, path := range l.path {
		var rri RepoReadIndex
		rri.common = l.common
		rri.path = path

		idx, err := rri.Read()
		if err != nil {
			return nil, err
		}

		for _, ent := range idx.Entries {
			if code.Match(&ent) {
				ent := ent // to make a copy so we can take it's address
				results = append(results, &ent)
			}
		}
	}

	return results, nil
}

func (l *ProjectLoad) loadScript(ctx context.Context, ns, name string) (*ScriptPackage, error) {
	if l.path == nil {
		err := l.setupPath(ctx, l.cfg)
		if err != nil {
			return nil, err
		}
	}

	var sl ScriptLoad
	sl.common = l.common

	sl.lookup = &ScriptLookup{
		common: l.common,
		Path:   l.path,
	}

	sl.Store = l.store

	data, err := sl.Load(name, WithConstraints(l.constraints))
	if err != nil {
		return nil, errors.Wrapf(err, "while loading from project.xcr")
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

	return &PackageSelector{Name: name}, nil
}

func (s *ProjectLoad) importPkg(thread *exprcore.Thread, ns, name string, args *exprcore.Dict) (exprcore.Value, error) {
	return s.loadScript(context.Background(), ns, name)
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

	if ienv.ExportPath != "" {
		pci.CarCache = []string{ienv.ExportPath}
	}

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
	[]string, *PackagesToInstall, *InstallStats, error,
) {
	var pci PackageCalcInstall
	pci.common = p.common
	pci.Store = ienv.Store

	if ienv.ExportPath != "" {
		pci.CarCache = []string{ienv.ExportPath}
	}

	var cl CarLookup
	pci.carLookup = &cl

	var requested []string

	for _, p := range p.Install {
		requested = append(requested, p.ID())
	}

	toInstall, err := pci.CalculateSet(p.Install)
	if err != nil {
		return nil, nil, nil, err
	}

	err = os.MkdirAll(ienv.Store.Default, 0755)
	if err != nil {
		return nil, nil, nil, err
	}

	var pkgInst PackagesInstall
	pkgInst.common = p.common

	for _, pkg := range toInstall.InstallOrder {
		fmt.Println(pkg)
	}

	stats, err := pkgInst.Install(ctx, ienv, toInstall)
	if err != nil {
		return nil, nil, stats, err
	}

	return requested, toInstall, stats, nil
}

type ExportedCar struct {
	Package *ScriptPackage
	Info    *data.CarInfo
	Path    string
	Sum     []byte
}

func stringSet(b []string) map[string]struct{} {
	set := make(map[string]struct{})

	for _, s := range b {
		set[s] = struct{}{}
	}

	return set
}

func pkgSet(a []*ScriptPackage) map[string]*ScriptPackage {
	s := make(map[string]*ScriptPackage)
	for _, pkg := range a {
		s[pkg.ID()] = pkg
	}
	return s
}

func subtractDeps(a []*ScriptPackage, b []string) []*ScriptPackage {
	var diff []*ScriptPackage

	bSet := stringSet(b)

	for _, pkg := range a {
		if _, ok := bSet[pkg.ID()]; !ok {
			diff = append(diff, pkg)
		}
	}

	return diff
}

func (p *Project) FindCachedBuildOnlyDeps(pti *PackagesToInstall, dir string) ([]*ExportedCar, error) {
	buildDeps := map[string]*ScriptPackage{}
	toProcess := []*ScriptPackage{}

	for _, id := range pti.InstallOrder {
		pkg := pti.Scripts[id]

		boDeps := subtractDeps(pkg.Dependencies(), pti.Dependencies[id])

		for _, pkg := range boDeps {
			toProcess = append(toProcess, pkg)
			buildDeps[pkg.ID()] = pkg
		}
	}

	for len(toProcess) > 0 {
		pkg := toProcess[0]
		toProcess = toProcess[1:]

		for _, dep := range pkg.Dependencies() {
			if _, ok := buildDeps[dep.ID()]; !ok {
				toProcess = append(toProcess, dep)
				buildDeps[dep.ID()] = dep
			}
		}
	}

	for _, id := range pti.InstallOrder {
		delete(buildDeps, id)
	}

	var exported []*ExportedCar

	for _, dep := range buildDeps {
		path := filepath.Join(dir, dep.ID()+".car")
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		var cu CarUnpack

		err = cu.Install(f, MetadataOnly)

		f.Close()

		if err != nil {
			return nil, err
		}

		var ec ExportedCar
		ec.Path = path
		ec.Sum = cu.Sum
		ec.Info = &cu.Info
		ec.Package = dep

		exported = append(exported, &ec)
	}

	return exported, nil
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

type SearchResult struct {
	Results []string
}
