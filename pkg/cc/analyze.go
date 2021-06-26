package cc

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
)

type CCOption int

const (
	NoArgs CCOption = 1 << iota
	ForPP
	Prefix
	Separated
	CanBeSeparated
	TooHard
	Compile
	Output
	Language
)

type CompOpt struct {
	name    string
	options CCOption
}

func parseOption(opt string, opts ...CCOption) CompOpt {
	var combined CCOption

	for _, o := range opts {
		combined |= o
	}

	return CompOpt{
		name:    opt,
		options: combined,
	}
}

var knownOptions = []CompOpt{
	parseOption("--param", Separated),
	parseOption("--serialize-diagnostics", Separated),
	parseOption("-A", CanBeSeparated),
	parseOption("-D", CanBeSeparated),
	parseOption("-G", Separated),
	parseOption("-remap", ForPP),
	parseOption("-trigraphs", ForPP),
	parseOption("-U", CanBeSeparated),
	parseOption("-u", CanBeSeparated),
	parseOption("-x", CanBeSeparated, Language),
	parseOption("-z", CanBeSeparated),
	parseOption("-V", CanBeSeparated),
	parseOption("-Xpreprocessor", Separated, ForPP),
	parseOption("-Xassembler", Separated),
	parseOption("-Xlinker", Separated),
	parseOption("-aux-info", Separated),
	parseOption("-arch", Separated),
	parseOption("-b", Separated),
	parseOption("-MF", Separated),
	parseOption("-MT", Separated),

	parseOption("-B", CanBeSeparated),
	parseOption("-I", CanBeSeparated, ForPP),
	parseOption("-idirafter", CanBeSeparated, ForPP),
	parseOption("-iframework", CanBeSeparated, ForPP),
	parseOption("-imacros", CanBeSeparated, ForPP),
	parseOption("-imultilib", CanBeSeparated, ForPP),
	parseOption("-include", CanBeSeparated, ForPP),
	parseOption("-iplugindir=", Prefix),
	parseOption("-iprefix", CanBeSeparated, ForPP),
	parseOption("-iquote", CanBeSeparated, ForPP),
	parseOption("-isysroot", CanBeSeparated, ForPP),
	parseOption("-isystem", CanBeSeparated, ForPP),
	parseOption("-iwithprefix", CanBeSeparated, ForPP),
	parseOption("-iwithprefixbefore", CanBeSeparated, ForPP),
	parseOption("-install_name", Separated),
	parseOption("-L", CanBeSeparated),
	parseOption("-no-canonical-prefixes"),
	parseOption("--no-sysroot-suffix"),
	parseOption("-nostdinc", ForPP),
	parseOption("-nostdinc++", ForPP),
	parseOption("--sysroot=", Prefix),

	parseOption("-c", Compile),
	parseOption("-o", CanBeSeparated, Output),

	parseOption("-F", CanBeSeparated, ForPP),
	parseOption("-nobuiltininc"),

	parseOption("-fno-working-directory", ForPP),
	parseOption("-fworking-directory", ForPP),
	parseOption("-stdlib=", Prefix, ForPP),
}

// options that mean "don't bother"
var alwaysTooHard = []string{
	"-", "--coverage",
	"--save-temps",
	"-E",
	"-M",
	"-MM",
	"-P",

	// Pulled from sccache: gcc

	"-fplugin=libcc1plugin",
	"-fprofile-use",
	"-fprofile-arcs",
	"-fprofile-generate",
	"-frepo",
	"-fsyntax-only",
	"-ftest-coverage",
	"-save-temps",
	"@",

	// Pulled from sccache: clang
	"-fcxx-modules",
	"-fmodules",
	"-fprofile-intr-use",
}

func init() {
	for _, name := range alwaysTooHard {
		knownOptions = append(knownOptions, CompOpt{
			name:    name,
			options: TooHard,
		})
	}

	sort.Slice(knownOptions, func(i, j int) bool {
		return knownOptions[i].name < knownOptions[j].name
	})
}

type AnalyzedOperation struct {
	Condition CCOption
	Command   string
	Known     map[string][]string
	ForPP     []string
	Common    []string
	Processed []string
	Inputs    []string
	Outputs   []string
	TooHard   []string
}

func Analyze(args []string) (*AnalyzedOperation, error) {
	command := args[0]
	args = args[1:]

	var ao AnalyzedOperation
	ao.Known = map[string][]string{}
	ao.Command = command
	ao.Processed = append(ao.Processed, command)

	max := len(knownOptions)
	for i := 0; i < len(args); i++ {
		arg := args[i]

		idx := sort.Search(max, func(i int) bool {
			co := knownOptions[i]
			check := arg

			if co.options&(CanBeSeparated|Prefix) != 0 {
				if len(check) >= len(co.name) {
					check = check[:len(co.name)]
				}
			}
			return co.name >= check
		})

		if idx == max {
			if len(arg) > 0 && arg[0] != '-' {
				ao.Inputs = append(ao.Inputs, arg)
			}
			continue
		}

		co := knownOptions[idx]

		var val string

		if co.options&CanBeSeparated == CanBeSeparated {
			if strings.HasPrefix(arg, co.name) {
				val = arg[len(co.name):]

				if val == "" {
					i++
					val = args[i]
				}
			} else {
				continue
			}
		} else if co.name == arg {
			if co.options&Separated == Separated {
				i++
				val = args[i]
			}
		} else {
			ao.Common = append(ao.Common, arg)
			ao.Processed = append(ao.Processed, arg)
			continue
		}

		if co.options&Output == Output {
			ao.Outputs = append(ao.Outputs, val)
		}

		var retain []string

		if val != "" && co.options&CanBeSeparated == CanBeSeparated && len(co.name) == 2 {
			retain = []string{co.name + val}
		} else {
			retain = []string{co.name}
			if val != "" {
				retain = append(retain, val)
			}
		}

		if co.options&(Compile|Output) == 0 {
			ao.Processed = append(ao.Processed, retain...)

			if co.options&ForPP == ForPP {
				ao.ForPP = append(ao.ForPP, retain...)
			} else {
				ao.Common = append(ao.Common, retain...)
			}
		}

		if co.options&TooHard == TooHard {
			ao.TooHard = append(ao.TooHard, retain...)
		}

		ao.Condition |= co.options
		ao.Known[co.name] = append(ao.Known[co.name], val)
	}

	if ao.Condition&Compile == Compile && len(ao.Inputs) == 1 && len(ao.Outputs) == 0 {
		in := ao.Inputs[0]
		ext := filepath.Ext(in)

		ao.Outputs = []string{in[:len(in)-len(ext)] + ".o"}
	}

	return &ao, nil
}

func (a *AnalyzedOperation) Cachable() error {
	if a.Condition&Compile != Compile {
		return errors.Wrapf(ErrNotCacheable, "not compiling")
	}

	if a.Condition&TooHard == TooHard {
		return errors.Wrapf(ErrNotCacheable, "detected too hard options: %s", strings.Join(a.TooHard, ", "))
	}

	if len(a.Inputs) != 1 {
		return errors.Wrapf(ErrNotCacheable, "not only one input: %d", len(a.Inputs))
	}

	if len(a.Known["-o"]) > 1 {
		return errors.Wrapf(ErrNotCacheable, "not only one output: %d", len(a.Known["-o"]))
	}

	return nil
}

func (a *AnalyzedOperation) Output() string {
	outputs := a.Known["-o"]
	if len(outputs) != 1 {
		return ""
	}

	return outputs[0]
}

func (a *AnalyzedOperation) PreprocessArgs() []string {
	out := make([]string, len(a.Processed))

	copy(out, a.Processed)

	out = append(out, "-E", a.Inputs[0])

	return out
}

// Nonce hashes the common args to provide a salt to the hashed
// output from the complier's preprocessing run.
func (a *AnalyzedOperation) Nonce() []byte {
	h, _ := blake2b.New256(nil)

	out := make([]byte, blake2b.Size256)

	sum := make([]byte, blake2b.Size256)

	for _, str := range a.Common {
		fmt.Fprintln(h, str)

		for i, b := range h.Sum(sum[:0]) {
			out[i] ^= b
		}

		h.Reset()
	}

	return out
}
