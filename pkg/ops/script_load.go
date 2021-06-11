package ops

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/hashicorp/go-hclog"
	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
	"lab47.dev/aperture/pkg/repo"
)

type ScriptLoad struct {
	common

	Store *config.Store

	lookup *ScriptLookup
	cfg    *Config

	loaded map[string]*ScriptPackage
}

func loadedKey(name, ns string, args map[string]string, path string) string {
	var keys []string

	for k := range args {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(name)
	sb.WriteString("-")
	sb.WriteString(ns)

	if len(keys) > 1 {
		sb.WriteByte('-')

		for _, k := range keys {
			sb.WriteString(k)
			sb.WriteByte('=')
			sb.WriteString(args[k])
		}
	}

	sb.WriteString(path)

	return sb.String()
}

type ScriptPackage struct {
	loader *ScriptLoad

	requestName string
	id          string
	sig         string
	repo        string
	prototype   *exprcore.Prototype

	cs ScriptCalcSig

	helpers    exprcore.StringDict
	helpersSum []byte

	constraints map[string]string

	PackageInfo *data.PackageInfo

	Instance *Instance

	repoConfig repo.Repo
}

func (s *ScriptPackage) Name() string {
	return s.cs.Name
}

func (s *ScriptPackage) Version() string {
	return s.cs.Version
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (s *ScriptPackage) String() string {
	return fmt.Sprintf("<script: %s>", s.requestName)
}

// Type returns a short string describing the value's type.
func (s *ScriptPackage) Type() string {
	return "script"
}

// Freeze causes the value, and all values transitively
// reachable from it through collections and closures, to be
// marked as frozen.  All subsequent mutations to the data
// structure through this API will fail dynamically, making the
// data structure immutable and safe for publishing to other
// exprcore interpreters running concurrently.
func (s *ScriptPackage) Freeze() {
}

// Truth returns the truth value of an object.
func (s *ScriptPackage) Truth() exprcore.Bool {
	return exprcore.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the exprcore program.
func (s *ScriptPackage) Hash() (uint32, error) {
	return 0, io.EOF
}

func (s *ScriptPackage) ID() string {
	return s.id
}

func (s *ScriptPackage) Signature() string {
	return s.sig
}

func (s *ScriptPackage) Repo() string {
	return s.repo
}

func (s *ScriptPackage) RepoConfig() repo.Repo {
	return s.repoConfig
}

func (s *ScriptPackage) Constraints() map[string]string {
	return s.constraints
}

// Dependencies returns any ScriptPackages that this one depends on, as
// declared via the dependencies keyword.
func (s *ScriptPackage) Dependencies() []*ScriptPackage {
	// Make a copy so that the caller can't accidentally manipulate our canonical list of
	// dependencies.
	slice := make([]*ScriptPackage, len(s.cs.Dependencies))
	copy(slice, s.cs.Dependencies)
	return slice
}

func (s *ScriptPackage) DependencyNames() []string {
	var out []string

	for _, c := range s.cs.Dependencies {
		out = append(out, c.requestName)
	}

	return out
}

var ErrBadScript = errors.New("script error detected")

type Option func(c *loadCfg)

type loadCfg struct {
	namespace         string
	args, constraints map[string]string
	configRepo        *ConfigRepo
}

func WithArgs(args map[string]string) Option {
	return func(c *loadCfg) {
		c.args = args
	}
}

func WithConstraints(args map[string]string) Option {
	return func(c *loadCfg) {
		c.constraints = args
	}
}

func WithNamespace(ns string) Option {
	return func(c *loadCfg) {
		c.namespace = ns
	}
}

func WithConfigRepo(cr *ConfigRepo) Option {
	return func(c *loadCfg) {
		c.configRepo = cr
	}
}

type loadContext struct {
	repo        *ConfigRepo
	constraints map[string]string
}

func (s *ScriptLoad) Load(name string, opts ...Option) (*ScriptPackage, error) {
	if s.loaded == nil {
		s.loaded = make(map[string]*ScriptPackage)
	}

	if s.lookup == nil {
		s.lookup = &ScriptLookup{}
	}

	var lc loadCfg

	for _, o := range opts {
		o(&lc)
	}

	var path string

	if lc.configRepo != nil {
		path = lc.configRepo.Path
	}

	cacheKey := loadedKey(name, lc.namespace, lc.args, path)

	sp, ok := s.loaded[cacheKey]
	if ok {
		if sp == nil {
			return nil, fmt.Errorf("recursive dependencies detected")
		}

		return sp, nil
	}

	var (
		data ScriptData
		err  error
		cr   *ConfigRepo = lc.configRepo
	)

	if lc.namespace != "" {
		cr, ok = s.cfg.Repos[lc.namespace]
		if !ok {
			return nil, fmt.Errorf("unknown namespace: %s", lc.namespace)
		}
	} else if cr == nil && s.cfg != nil {
		cr, _ = s.cfg.Repos["root"]
	}

	if cr != nil {
		s.L().Debug("looking up script", "config-repo", cr.Path, "name", name)
	} else {
		s.L().Debug("looking up script", "name", name)
	}

	if cr != nil {
		if cr.Github != "" {
			data, err = s.lookup.LoadGithub(cr.Github, name)
			if err != nil {
				return nil, err
			}
		} else if cr.Path != "" {
			data, err = s.lookup.LoadDir(cr.Path, name)
			if err != nil {
				return nil, err
			}
		}
	} else {
		data, err = s.lookup.Load(name)
		if err != nil {
			return nil, err
		}
	}

	if data == nil {
		return nil, fmt.Errorf("Unable to find script: %s", name)
	}

	pkgobj := exprcore.FromStringDict(exprcore.Root, nil)

	args := exprcore.NewDict(len(lc.args))

	for k, v := range lc.args {
		args.SetKey(exprcore.String(k), exprcore.String(v))
	}

	sysobj := exprcore.FromStringDict(exprcore.Root, exprcore.StringDict{
		"platform": exprcore.String(runtime.GOARCH),
	})

	vars := exprcore.StringDict{
		"pkg":      pkgobj,
		"args":     args,
		"file":     exprcore.NewBuiltin("file", s.fileFn),
		"dir":      exprcore.NewBuiltin("dir", s.dirFn),
		"inputs":   exprcore.NewBuiltin("inputs", s.inputsFn),
		"join":     exprcore.NewBuiltin("join", joinFn),
		"fmt":      exprcore.NewBuiltin("fmt", fmtFn),
		"basename": exprcore.NewBuiltin("basename", basenameFn),
		"fetch":    exprcore.NewBuiltin("fetch", s.fetchFn),
		"sys":      sysobj,
	}

	_, prog, err := exprcore.SourceProgram(name+Extension, data.Script(), vars.Has)
	if err != nil {
		return nil, err
	}

	var thread exprcore.Thread

	lctx := &loadContext{
		repo:        cr,
		constraints: lc.constraints,
	}

	if cr != nil {
		thread.Import = func(thread *exprcore.Thread, namespace, pkg string, args *exprcore.Dict) (exprcore.Value, error) {
			return s.importUnderRepo(thread, lctx, namespace, pkg, args)
		}
	} else {
		thread.Import = func(thread *exprcore.Thread, namespace, pkg string, args *exprcore.Dict) (exprcore.Value, error) {
			return s.importPkg(thread, lctx, namespace, pkg, args)
		}
	}

	thread.SetLocal("constraints", lc.constraints)
	thread.SetLocal("script-data", data)

	s.loaded[cacheKey] = nil

	_, pkgval, err := prog.Init(&thread, vars)
	if err != nil {
		return nil, err
	}

	ppkg, ok := pkgval.(*exprcore.Prototype)
	if !ok {
		return nil, errors.Wrapf(ErrBadScript, "script '%s' did not return an object: %T", name, ppkg)
	}

	sp = &ScriptPackage{
		requestName: name,
		repo:        data.Repo(),
		loader:      s,
		constraints: lc.constraints,
		prototype:   ppkg,
		repoConfig:  data.RepoConfig(),
	}

	s.loaded[cacheKey] = sp

	err = s.loadHelpers(sp, lctx, name, data, vars)
	if err != nil {
		return nil, err
	}

	sp.cs.common.logger = s.common.logger

	sig, id, err := sp.cs.Calculate(ppkg, sp.helpersSum, lc.constraints)
	if err != nil {
		return nil, err
	}

	sp.sig = sig
	sp.id = id

	return sp, nil
}

func ProcessPrototype(pkgval exprcore.Value, constraints map[string]string) (*ScriptPackage, error) {
	ppkg, ok := pkgval.(*exprcore.Prototype)
	if !ok {
		return nil, errors.Wrapf(ErrBadScript, "script did not return an object")
	}

	sp := &ScriptPackage{
		prototype: ppkg,
	}

	sig, id, err := sp.cs.Calculate(ppkg, nil, constraints)
	if err != nil {
		return nil, err
	}

	sp.sig = sig
	sp.id = id
	sp.requestName = sp.cs.Name

	return sp, nil
}

func (s *ScriptLoad) importUnderRepo(thread *exprcore.Thread, lctx *loadContext, ns, name string, args *exprcore.Dict) (exprcore.Value, error) {
	var opts []Option

	constraints := lctx.constraints
	if constraints != nil {
		opts = append(opts, WithConstraints(constraints))
	}

	if args != nil {
		loadArgs := make(map[string]string)

		for _, pair := range args.Items() {
			k, ok := pair[0].(exprcore.String)
			if !ok {
				return nil, fmt.Errorf("load arg key not a string")
			}

			v, ok := pair[1].(exprcore.String)
			if !ok {
				return nil, fmt.Errorf("load arg value not a string")
			}

			loadArgs[string(k)] = string(v)
		}

		opts = append(opts, WithArgs(loadArgs))
	}

	if ns != "" {
		opts = append(opts, WithNamespace(ns))
	} else {
		opts = append(opts, WithConfigRepo(lctx.repo))
	}

	x, err := s.Load(name, opts...)
	return x, err
}

func (s *ScriptLoad) importPkg(thread *exprcore.Thread, lctx *loadContext, ns, name string, args *exprcore.Dict) (exprcore.Value, error) {
	var opts []Option

	constraints := lctx.constraints
	if constraints != nil {
		opts = append(opts, WithConstraints(constraints))
	}

	if args != nil {
		loadArgs := make(map[string]string)

		for _, pair := range args.Items() {
			k, ok := pair[0].(exprcore.String)
			if !ok {
				return nil, fmt.Errorf("load arg key not a string")
			}

			v, ok := pair[1].(exprcore.String)
			if !ok {
				return nil, fmt.Errorf("load arg value not a string")
			}

			loadArgs[string(k)] = string(v)
		}

		opts = append(opts, WithArgs(loadArgs))
	}

	if ns != "" {
		opts = append(opts, WithNamespace(ns))
	}

	x, err := s.Load(name, opts...)
	return x, err
}

var ErrSumFormat = fmt.Errorf("sum must a tuple with (sum-type, sum)")

func (l *ScriptLoad) idFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("only keyword args are supported")
	}

	sort.Slice(kwargs, func(i, j int) bool {
		return kwargs[i].String() < kwargs[j].String()
	})

	h, _ := blake2b.New256(nil)

	for _, item := range kwargs {
		fmt.Fprintf(h, "%s\n", item.String())
	}

	return exprcore.String(base58.Encode(h.Sum(nil))), nil
}

func (l *ScriptLoad) fileFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		path, darwin, linux string
		into                string
	)

	if err := exprcore.UnpackArgs(
		"file", args, kwargs,
		"path?", &path,
		"darwin?", &darwin,
		"linux?", &linux,
		"into?", &into,
	); err != nil {
		return nil, err
	}

	if path == "" {
		switch runtime.GOOS {
		case "darwin":
			if darwin != "" {
				path = darwin
			}
		case "linux":
			if linux != "" {
				path = linux
			}
		}
	}

	sdata := thread.Local("script-data").(ScriptData)

	data, err := sdata.Asset(path)
	if err != nil {
		return nil, err
	}

	h, _ := blake2b.New256(nil)
	h.Write(data)

	sumVal := base58.Encode(h.Sum(nil))

	name := "fetch-file"

	inst, err := NewDataInstance(name, into, data)
	if err != nil {
		return nil, err
	}

	inst.Version = sumVal[:8]

	inst.Signature = sumVal

	return inst, nil
}

func hashDir(l hclog.Logger, dir string) ([]byte, error) {
	h, _ := blake2b.New256(nil)

	filepath.Walk(dir, func(fpath string, info os.FileInfo, err error) error {
		switch {
		case info.Mode().IsRegular():
			fmt.Fprintf(h, "file: %s %d\n", fpath, info.Mode().Perm())
			f, err := os.Open(fpath)
			if err != nil {
				return err
			}

			io.Copy(h, f)

		case info.Mode().IsDir():
			fmt.Fprintf(h, "dir: %s\n", fpath)
		}

		l.Trace("hash-dir", "path", fpath, "sum", base58.Encode(h.Sum(nil)))
		return nil
	})

	return h.Sum(nil), nil
}

func (l *ScriptLoad) dirFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var path string

	if err := exprcore.UnpackArgs(
		"dir", args, kwargs,
		"path?", &path,
	); err != nil {
		return nil, err
	}

	if path == "" {
		path = "."
	}

	sf := &ScriptFile{
		logger: l.common.L(),
		dir:    path,
	}

	_, _, ok := sf.Sum()
	if !ok {
		return nil, fmt.Errorf("unable to sum directory")
	}

	l.common.L().Trace("dir-fn", "path", path, "sum", sf.sumValue)

	return sf, nil
}

func (l *ScriptLoad) inputsFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {

	sm := exprcore.NewDict(len(kwargs))

	for _, ent := range kwargs {
		sm.SetKey(ent[0], ent[1])
	}

	return sm, nil
}

func (l *ScriptLoad) fetchFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var (
		fn    *exprcore.Function
		sum   exprcore.Value
		deps  *exprcore.List
		ftype exprcore.String
	)

	if err := exprcore.UnpackArgs(
		"fetch", args, kwargs,
		"fetch", &fn,
		"hash?", &sum,
		"dependencies?", &deps,
		"type?", &ftype,
	); err != nil {
		return nil, err
	}

	name := "fetch"

	if ftype != "" {
		name = name + "-" + string(ftype)
	}

	inst, err := NewInstance(name, fn)
	if err != nil {
		return nil, err
	}

	sumType, sumValue, err := DecodeSum(sum)
	if err != nil {
		return nil, err
	}

	h, _ := blake2b.New256(nil)

	fmt.Fprintln(h, name)

	fmt.Fprintf(h, "%s-%s", sumType, sumValue)

	inst.Signature = base58.Encode(h.Sum(nil))

	if deps != nil {
		var scripts []*ScriptPackage

		iter := deps.Iterate()
		defer iter.Done()
		var x exprcore.Value
		for iter.Next(&x) {
			if script, ok := x.(*ScriptPackage); ok {
				scripts = append(scripts, script)
			}
		}

		inst.Dependencies = scripts
	}

	return inst, nil
}

func fmtFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var format string

	if len(kwargs) > 1 {
		return nil, fmt.Errorf("fmt: too many keyword args")
	}

	if len(kwargs) == 1 {
		pair := kwargs[0]
		if pair[0].(exprcore.String) == "format" {
			if str, ok := pair[1].(exprcore.String); ok {
				format = string(str)
			}
		} else {
			return nil, fmt.Errorf("fmt: unknown argument '%s'", pair[0])
		}
	} else {
		if str, ok := args[0].(exprcore.String); ok {
			format = string(str)
		} else {
			return nil, fmt.Errorf("fmt: format must be a string")
		}

		args = args[1:]
	}

	var parts []interface{}

	for _, a := range args {
		switch v := a.(type) {
		case exprcore.String:
			parts = append(parts, string(v))
		case exprcore.Int:
			parts = append(parts, v.String())
		default:
			return nil, fmt.Errorf("fmt only accepts strings and ints, got a %T", a)
		}
	}

	return exprcore.String(fmt.Sprintf(format, parts...)), nil
}

func joinFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {
	var parts []string

	for _, a := range args {
		if s, ok := a.(exprcore.String); ok {
			parts = append(parts, string(s))
		} else {
			return nil, fmt.Errorf("join only accepts strings, got a %T", a)
		}
	}

	return exprcore.String(filepath.Join(parts...)), nil
}

func basenameFn(thread *exprcore.Thread, b *exprcore.Builtin, args exprcore.Tuple, kwargs []exprcore.Tuple) (exprcore.Value, error) {

	var path string

	if err := exprcore.UnpackArgs(
		"basename", args, kwargs, "path", &path); err != nil {
		return nil, err
	}

	return exprcore.String(filepath.Base(path)), nil
}

type ScriptFile struct {
	logger hclog.Logger

	path     string
	sumType  string
	sumValue string
	into     string
	chdir    bool

	dir string

	data []byte
}

func (s *ScriptFile) Sum() (string, []byte, bool) {
	if s.dir != "" {
		if s.sumValue != "" {
			data, err := base58.Decode(s.sumValue)
			if err != nil {
				return "", nil, false
			}

			return "dir", data, true
		} else {
			data, err := hashDir(s.logger, s.dir)
			if err != nil {
				return "", nil, false
			}

			s.sumValue = base58.Encode(data)

			return "dir", data, true
		}
	}

	switch s.sumType {
	case "etag":
		if len(s.sumValue) < 2 {
			return "", nil, false
		}

		sv := s.sumValue
		if sv[0] != '"' {
			sv = "\"" + sv
		}

		if sv[len(sv)-1] != '"' {
			sv = sv + "\""
		}

		return "etag", []byte(sv), true
	case "sha256":
		d, err := hex.DecodeString(s.sumValue)
		if err != nil {
			return "", nil, false
		}

		return "sha256", d, true
	default:
		if s.sumValue == "" {
			panic("oh no")
		}

		b, err := base58.Decode(s.sumValue)
		if err != nil {
			return "", nil, false
		}

		return s.sumType, b, true
	}
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (s *ScriptFile) String() string {
	return fmt.Sprintf("file(path: %s)", s.path)
}

// Type returns a short string describing the value's type.
func (s *ScriptFile) Type() string {
	return "script:file"
}

func (s *ScriptFile) Freeze() {}

func (s *ScriptFile) Truth() exprcore.Bool {
	return exprcore.True
}

func (s *ScriptFile) Hash() (uint32, error) {
	h := fnv.New32()
	h.Write([]byte(s.path))
	return h.Sum32(), nil
}

func (l *ScriptLoad) loadHelpers(s *ScriptPackage, lctx *loadContext, name string, data ScriptData, vars exprcore.StringDict) error {
	exportName := name + ".export" + Extension
	b, err := data.Asset(exportName)
	if err != nil {
		return nil
	}

	isPD := vars.Has

	_, prog, err := exprcore.SourceProgram(exportName, b, isPD)
	if err != nil {
		return err
	}

	var thread exprcore.Thread

	if lctx.repo != nil {
		thread.Import = func(thread *exprcore.Thread, namespace, pkg string, args *exprcore.Dict) (exprcore.Value, error) {
			return l.importUnderRepo(thread, lctx, namespace, pkg, args)
		}
	} else {
		thread.Import = func(thread *exprcore.Thread, namespace, pkg string, args *exprcore.Dict) (exprcore.Value, error) {
			return l.importPkg(thread, lctx, namespace, pkg, args)
		}
	}

	gbls, _, err := prog.Init(&thread, vars)
	if err != nil {
		return err
	}

	s.helpers = gbls

	var keys []string

	for k := range gbls {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	h, _ := blake2b.New256(nil)

	for _, k := range keys {
		v := gbls[k]

		if fn, ok := v.(*exprcore.Function); ok {
			d, err := fn.HashCode()
			if err != nil {
				return err
			}

			h.Write(d)
		}
	}

	s.helpersSum = h.Sum(nil)

	return nil
}

func (s *ScriptPackage) Attr(name string) (exprcore.Value, error) {
	switch name {
	case "prefix":
		path, err := s.loader.Store.Locate(s.ID())
		if err != nil {
			if errors.Is(err, config.ErrNoEntry) {
				path = s.loader.Store.ExpectedPath(s.ID())
			} else {
				return nil, err
			}
		}

		return exprcore.String(path), nil
	}

	if s.helpers == nil {
		return nil, nil
	}

	val, ok := s.helpers[name]
	if !ok {
		return nil, nil
	}

	return val, nil
}

func (s *ScriptPackage) AttrNames() []string {
	if s.helpers == nil {
		return nil
	}

	return s.helpers.Keys()
}
