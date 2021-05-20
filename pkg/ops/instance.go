package ops

import (
	"fmt"
	"hash/fnv"

	"github.com/lab47/exprcore/exprcore"
	"github.com/mr-tron/base58"
)

type Instance struct {
	Id           string
	Name         string
	Version      string
	Signature    string
	Fn           *exprcore.Function
	Dependencies []*ScriptPackage

	Path string
	Data []byte
}

func (i *Instance) ID() string {
	if i.Signature == "" {
		panic("instance sign not set")
	}

	return fmt.Sprintf("%s-%s-%s", i.Signature, i.Name, i.Version)
}

func NewDataInstance(name string, path string, data []byte) (*Instance, error) {
	inst := &Instance{Name: name, Path: path, Data: data}
	return inst, nil
}

func NewInstance(name string, fn *exprcore.Function) (*Instance, error) {
	inst := &Instance{Name: name, Fn: fn}

	b, err := fn.HashCode()
	if err != nil {
		return nil, err
	}

	inst.Version = base58.Encode(b)[:8]

	return inst, nil
}

// String returns the string representation of the value.
// exprcore string values are quoted as if by Python's repr.
func (i *Instance) String() string {
	return fmt.Sprintf("<instance %s-%s>", i.Name, i.Version)
}

// Type returns a short string describing the value's type.
func (i *Instance) Type() string {
	return fmt.Sprintf("<instance>")
}

// Freeze causes the value, and all values transitively
// reachable from it through collections and closures, to be
// marked as frozen.  All subsequent mutations to the data
// structure through this API will fail dynamically, making the
// data structure immutable and safe for publishing to other
// exprcore interpreters running concurrently.
func (i *Instance) Freeze() {
}

// Truth returns the truth value of an object.
func (i *Instance) Truth() exprcore.Bool {
	return exprcore.True
}

// Hash returns a function of x such that Equals(x, y) => Hash(x) == Hash(y).
// Hash may fail if the value's type is not hashable, or if the value
// contains a non-hashable value. The hash is used only by dictionaries and
// is not exposed to the exprcore program.
func (i *Instance) Hash() (uint32, error) {
	h := fnv.New32()
	fmt.Fprintf(h, "%s%s", i.Name, i.Version)

	fn, err := i.Fn.Hash()
	if err != nil {
		return 0, err
	}

	return fn ^ h.Sum32(), nil
}
