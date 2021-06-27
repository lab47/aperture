package cc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-hclog"
	"github.com/mr-tron/base58/base58"
	"golang.org/x/sys/unix"
	"lab47.dev/aperture/pkg/data"
)

type wrapper struct {
	arg0  string
	given []string
	mode  string

	bi *data.BuildInfo

	allowPrefixes map[string]struct{}

	mac bool
}

func newWrapper(args []string, str string) (*wrapper, error) {
	var bi data.BuildInfo

	if str != "" {
		serData, err := base64.StdEncoding.DecodeString(str)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(serData, &bi)
		if err != nil {
			return nil, err
		}
	}

	arg0 := filepath.Base(args[0])

	w := &wrapper{
		bi:    &bi,
		arg0:  arg0,
		given: args[1:],
		mode:  calcMode(arg0, args),
		mac:   runtime.GOOS == "darwin",
	}

	w.prep()

	return w, nil
}

func (w *wrapper) prep() {
	prefixes := make(map[string]struct{})

	prefixes[w.bi.Prefix] = struct{}{}
	prefixes[w.bi.BuildDir] = struct{}{}

	for _, dep := range w.bi.Dependencies {
		prefixes[dep.Path] = struct{}{}
	}
}

func includes(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}

	return false
}

func includesPair(args []string, t1, t2 string) bool {
	for i, a := range args[:len(args)-1] {
		if a == t1 || args[i+1] == t2 {
			return true
		}
	}

	return false
}

var cxx_regexp = regexp.MustCompile(`(c|g|clang)\+\+`)

func calcMode(arg0 string, args []string) string {
	switch arg0 {
	case "cpp":
		return "cpp"
	case "ld", "ld.gold", "gold":
		return "ld"
	default:
		switch {
		case includes(args, "-c"):
			if cxx_regexp.MatchString(arg0) {
				return "cxx"
			}
			return "cc"
		case includes(args, "-xc++header") || includesPair(args, "-x", "c++-header"):
			return "cxx"
		case includes(args, "-E"):
			return "ccE"
		case cxx_regexp.MatchString(arg0):
			return "cxxld"
		default:
			return "ccld"
		}
	}
}

var (
	apertureCC string
	cxxRe      = regexp.MustCompile(`\w\+\+(-\d+(\.\d)?)?`)
	gxxRe      = regexp.MustCompile(`(g)?cc(-\d+(\.\d)?)?`)
)

func tool(args []string) string {
	switch args[0] {
	case "ld":
		return "ld"
	case "gold", "ld.gold":
		return "ld.gold"
	case "cpp":
		return "cpp"
	default:
		if cxxRe.MatchString(args[0]) {
			switch apertureCC {
			case "clang":
				return "clang++"
			case "llvm-gcc":
				return "llvm-g++-4.2"
			default:
				matches := gxxRe.FindStringSubmatch(apertureCC)
				return "g++" + matches[2]
			}
		} else {
			return apertureCC
		}
	}
}

type matcher interface {
	match(string) bool
}

type stringMatcher string

func (s stringMatcher) match(tgt string) bool {
	return string(s) == tgt
}

type reMatcher struct {
	re *regexp.Regexp
}

func (r *reMatcher) match(tgt string) bool {
	return r.re.MatchString(tgt)
}

type matchSet []matcher

func (ms matchSet) match(tgt string) bool {
	for _, m := range ms {
		if m.match(tgt) {
			return true
		}
	}

	return false
}

func mkMatchers(args ...string) matchSet {
	var ms []matcher

	for _, a := range args {
		if a[0] == '/' {
			re := regexp.MustCompile(a[1 : len(a)-1])
			ms = append(ms, &reMatcher{re})
		} else {
			ms = append(ms, stringMatcher(a))
		}
	}

	return matchSet(ms)
}

var prune = mkMatchers(
	`/^-g\d?$/`, `/^-gstabs\d+/`, "-gstabs+", `/^-ggdb\d?/`,
	`/^-march=.+/`, `/^-mtune=.+/`, `/^-mcpu=.+/`,
	`/^-O[0-9zs]?$/`, "-fast", "-no-cpp-precomp",
	"-pedantic", "-pedantic-errors", "-Wno-long-double",
	"-Wno-unused-but-set-variable",
)

var oldGCC = mkMatchers(
	"-fopenmp", "-lgomp", "-mno-fused-madd", "-fforce-addr", "-fno-defer-pop",
	"-mno-dynamic-no-pic", "-fearly-inlining", `/^-f(?:no-)?inline-functions-called-once/`,
	`/^-finline-limit/`, `/^-f(?:no-)?check-new/`, "-fno-delete-null-pointer-checks",
	"-fcaller-saves", "-fthread-jumps", "-fno-reorder-blocks", "-fcse-skip-blocks",
	"-frerun-cse-after-loop", "-frerun-loop-opt", "-fcse-follow-jumps",
	"-fno-regmove", "-fno-for-scope", "-fno-tree-pre", "-fno-tree-dominator-opts",
	"-fuse-linker-plugin", "-frounding-math",
)

var skipWarn1 = mkMatchers(`/^-Wl,-z,defs/`)
var skipWarn2 = mkMatchers(`/-W.*`)

var okWarn = mkMatchers(
	`/^-W[alp],/`, `/^-Wno-/`, "-Werror=implicit-function-declaration",
)

var sysRoot = mkMatchers(`/^-isysroot/`, `/^--sysroot/`)

func refurbArgs(args []string) []string {
	var out []string

	for i := 0; i < len(args); i++ {
		cur := args[i]

		switch {
		case prune.match(cur):
			// ok
		case oldGCC.match(cur):
			// skip for now
		case cur == "-Xpreprocessor" || cur == "-Xclang":
			i++
			out = append(out, cur, args[i])
		case cur == "--fast-math":
			out = append(out, "-ffast-math")
		case skipWarn1.match(cur):
			// ok
		case okWarn.match(cur):
			out = append(out, cur)
		case skipWarn2.match(cur):
			// ok
		case cur == "-macosx_version_min" || cur == "-dylib_install_name":
			i++
			out = append(out, cur, args[i])
		case cur == "-multiply_definedsupress":
			out = append(out, "-Wl,-multiply_defined,supress")
		case cur == "-undefineddynamic_lookup":
			out = append(out, "-Wl,-undefined,dynamic_lookup")
			/*
				case sysRoot.match(cur):
					i++
					next := args[i]
					if runtime.GOOS == "darwin" {
						if !strings.Contains(next, "osx") {
							out = append(out, "-isysroot"+next)
						}
					} else {
						out = append(out, cur, next)
					}
			*/
		case cur == "-dylib":
			out = append(out, "-Wl,"+cur)
		case strings.HasPrefix(cur, "-I"):
			val := cur[2:]
			if val == "" {
				i++
				val = args[i]
			}

			out = append(out, "-I"+val)
		case strings.HasPrefix(cur, "-L"):
			val := cur[2:]
			if val == "" {
				i++
				val = args[i]
			}

			out = append(out, "-L"+val)
		default:
			out = append(out, cur)
		}
	}

	return out
}

func extractFile(args []string) string {
	cmode := false
	for _, arg := range args {
		if arg == "-c" {
			cmode = true
		}
	}

	if cmode {
		return args[len(args)-1]
	}

	return ""
}

var (
	targetPrefix string
	storePath    string
)

func (w *wrapper) keepPath(path string) bool {
	if targetPrefix != "" && strings.HasPrefix(path, targetPrefix) {
		return true
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for prefix := range w.allowPrefixes {
		if strings.HasPrefix(abs, prefix) {
			return true
		}
	}

	return false
}

func (w *wrapper) underConfigure() bool {
	_, ok := os.LookupEnv("as_nl")
	return ok
}

var stdRe = mkMatchers(`/c[89]9/`)

func (w *wrapper) cflags() []string {
	args := []string{}

	if stdRe.match(w.arg0) {
		args = append(args, "-std="+w.arg0)
	}

	return args
}

func (w *wrapper) cxxflags() []string {
	args := w.cflags()

	return args
}

func joinPaths(parts []string) string {
	var sb strings.Builder

	var j int

	for _, part := range parts {
		_, err := os.Stat(part)
		if err != nil {
			continue
		}

		if j > 0 {
			sb.WriteRune(':')
		}

		j++

		sb.WriteString(part)
	}

	return sb.String()
}

func (w *wrapper) ldflags() []string {
	if w.mac {
		switch w.mode {
		case "ld":
			return []string{"-headerpad_max_install_names"}
		case "ccld", "cxxld":
			return []string{"-Wl,-headerpad_max_install_names"}
		}

		return nil
	} else {
		// We have to pull out selfLib and always do it explicitly because
		// joinPaths will prune out paths that don't exist and our lib path
		// by definition does not yet exist but we still want it as an rpath.
		selfLib := filepath.Join(w.bi.Prefix, "lib")

		var rpath []string

		// We use /. on the end here to confuse any tools that might try to match
		// on the directory and remove it (*cough* meson *cough*)
		for _, dep := range w.bi.Dependencies {
			rpath = append(rpath, filepath.Join(dep.Path, "lib")+"/.")
		}

		path := joinPaths(rpath)
		if path == "" {
			path = selfLib + "/."
		} else {
			path = selfLib + "/.:" + path
		}

		if w.mode == "ld" {
			return []string{"-rpath=" + path}
		} else {
			return []string{"-Wl,-rpath=" + path}
		}
	}

}

func dup(args []string) []string {
	n := make([]string, len(args))
	copy(n, args)
	return n
}

func join(parts ...[]string) []string {
	var out []string

	for _, sl := range parts {
		for _, p := range sl {
			out = append(out, p)
		}
	}

	return out
}

func (w *wrapper) newArgs() []string {
	if len(w.given) == 1 && w.given[0] == "-v" {
		return w.given
	}

	var args []string

	args = dup(w.given)

	switch w.mode {
	case "ccld":
		return join(w.cflags(), args, w.ldflags())
	case "cxxld":
		return join(w.cxxflags(), args, w.ldflags())
	case "cc":
		return join(w.cflags(), args)
	case "cxx":
		return join(w.cxxflags(), args)
	case "ccE":
		return args
	case "cpp":
		return args
	case "ld":
		return join(w.ldflags(), args)
	default:
		panic("Shouldn't be here")
	}
}

func findExecutable(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if m := d.Mode(); !m.IsDir() && m&0111 != 0 {
		return nil
	}
	return fs.ErrPermission
}

// LookPath searches for an executable named file in the
// directories named by the PATH environment variable.
// If file contains a slash, it is tried directly and the PATH is not consulted.
// The result may be an absolute path or a path relative to the current directory.
func LookPath(file string, path string) (string, error) {
	// NOTE(rsc): I wish we could use the Plan 9 behavior here
	// (only bypass the path if file begins with / or ./ or ../)
	// but that would not match all the Unix shells.

	if strings.Contains(file, "/") {
		err := findExecutable(file)
		if err == nil {
			return file, nil
		}
		return "", err
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			// Unix shell semantics: path element "" means "."
			dir = "."
		}
		path := filepath.Join(dir, file)
		if err := findExecutable(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func Run(args []string, info, shimPath, cachePath string) error {
	var logw io.WriteCloser = os.Stderr

	logPath := os.Getenv("APERTURE_CC_LOG")

	if logPath != "" {
		logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}

		logw = logF
	}

	L := hclog.New(&hclog.LoggerOptions{
		Name:   "cc-wrapper",
		Output: logw,
		Level:  hclog.Trace,
	})

	w, err := newWrapper(args, info)
	if err != nil {
		L.Error("error in creating new wrapper", "error", err)
		return err
	}

	dir, err := os.Getwd()
	if err != nil {
		L.Error("unable to getwd", "error", err)
		return err
	}

	newArgs := w.newArgs()

	updated := append([]string{w.arg0}, newArgs...)

	var (
		output    string
		cacheInfo string
	)

	cache, err := NewCache(cachePath)
	if err == nil {
		output, cacheInfo, err = cache.CalculateCacheInfo(context.Background(), L, updated)
		if err == nil {
			found, err := cache.Retrieve(cacheInfo, output)
			if found && err == nil {
				L.Info("retrieved value from cache", "cache-info", cacheInfo, "output", output, "args", spew.Sdump(updated))
				os.Exit(0)
				return nil
			}
		} else {
			L.Error("error analyzing arguments", "error", err)
		}
	} else {
		L.Error("cache disabled via error", "error", err)
	}

	L.Info("processing tool", "mode", w.mode, "dir", dir, "mac", w.mac, "arg0", w.arg0, "given", spew.Sdump(w.given), "new-args", spew.Sdump(updated), "config", spew.Sdump(w.bi), "cache-info", cacheInfo)

	t := w.arg0

	var (
		env  []string
		path string
	)

	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") {
			updated := e

			if shimPath != "" {
				updated = strings.ReplaceAll(e, shimPath+":", "")
			}

			path = updated[5:]
			env = append(env, updated)
		} else {
			env = append(env, e)
		}
	}

	execPath, err := LookPath(t, path)
	if err != nil {
		L.Error("error in looking up tool", "error", err, "tool", t, "path", path)
		return err
	}

	L.Info("executing tool", "exec", execPath, "args", updated, "env", env, "output", output)

	if cacheInfo == "" || output == "" {
		logw.Close()

		return unix.Exec(execPath, updated, env)
	}

	ds := exec.Command(execPath, updated[1:]...)
	ds.Stdin = os.Stdin
	ds.Stdout = os.Stdout
	ds.Stderr = os.Stderr

	err = ds.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
			return nil
		} else {
			return err
		}
	}

	sum, err := cache.Store(cacheInfo, output)
	if err != nil {
		L.Error("error storing to cache", "err", err)
	} else {
		outputKey := base58.Encode(sum)
		L.Info("output info", "path", output, "sum", outputKey)
	}

	os.Exit(0)

	return nil
}
