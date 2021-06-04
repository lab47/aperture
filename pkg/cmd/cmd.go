package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"

	"github.com/jessevdk/go-flags"
	"golang.org/x/sys/unix"
	"lab47.dev/aperture/pkg/progress"
)

type Cmd struct {
	syn, name string
	f         reflect.Value

	opts   reflect.Value
	parser *flags.Parser
}

func New(name, syn string, f interface{}) *Cmd {
	rv := reflect.ValueOf(f)

	if rv.Kind() != reflect.Func {
		panic("must pass a function")
	}

	rt := rv.Type()

	if rt.NumIn() != 2 {
		panic("must provide two arguments only")
	}

	if rt.NumOut() != 1 {
		panic("must return one argument only")
	}

	in := rt.In(1)

	if in.Kind() != reflect.Struct {
		panic("argument must be a struct")
	}

	sv := reflect.New(in)

	parser := flags.NewNamedParser(name, flags.Default)
	parser.ShortDescription = syn
	parser.LongDescription = syn

	_, err := parser.AddGroup("Application Options", "", sv.Interface())
	if err != nil {
		panic(err)
	}

	return &Cmd{
		syn:    syn,
		name:   name,
		f:      rv,
		opts:   sv,
		parser: parser,
	}
}

func (w *Cmd) Help() string {
	var buf bytes.Buffer
	w.parser.WriteHelp(&buf)
	return buf.String()
}

func (w *Cmd) Synopsis() string {
	return w.syn
}

func (w *Cmd) Run(args []string) int {
	_, err := w.parser.ParseArgs(args)
	if err != nil {
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelOnSignal(cancel, os.Interrupt, unix.SIGQUIT, unix.SIGTERM)

	ctx = progress.Open(ctx, os.Stderr)

	rets := w.f.Call([]reflect.Value{reflect.ValueOf(ctx), w.opts.Elem()})

	if err, ok := rets[0].Interface().(error); ok {
		if err != nil {
			fmt.Printf("! Error: %+v\n", err)
			return 1
		}
	}

	return 0
}

func cancelOnSignal(cancel func(), signals ...os.Signal) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, signals...)

	go func() {
		for range c {
			cancel()
		}
	}()
}
