package ops

import (
	"bytes"
	"fmt"

	"github.com/davecgh/go-spew/spew"
)

type streamDetect struct {
	possibles []string
	min, max  int
	debug     bool

	set      map[string]struct{}
	sizes    map[int]struct{}
	checking bool

	deps map[string]struct{}
	buf  *bytes.Buffer
}

func (d *streamDetect) setup() {
	d.min = -1

	d.set = make(map[string]struct{})
	d.sizes = make(map[int]struct{})

	for _, p := range d.possibles {
		d.set[p] = struct{}{}
		d.sizes[len(p)] = struct{}{}

		if len(p) > d.max {
			d.max = len(p)
		}

		if d.min == -1 || len(p) < d.min {
			d.min = len(p)
		}
	}

	spew.Dump(d.min, d.max)
	spew.Dump(d.set)
	spew.Dump(d.sizes)
}

func (d *streamDetect) reset() {
	d.buf.Reset()
	d.checking = false
	d.debug = false
}

func (d *streamDetect) Write(b []byte) (int, error) {
	d.findIn(b)
	return len(b), nil
}

func (d *streamDetect) findIn(src []byte) {
	for _, b := range src {
		if !d.checking {
			if b == '/' {
				fmt.Printf("checking...\n")
				if d.debug {
					spew.Dump(src)
				}
				d.checking = true
			}

			continue
		}

		d.buf.WriteByte(b)

		curLen := d.buf.Len()

		switch {
		case curLen >= d.min:
			if _, ok := d.sizes[curLen]; ok {
				str := d.buf.String()

				if d.debug {
					fmt.Printf("buf: %d `%s`\n", curLen, d.buf.String())
				}

				if _, found := d.set[str]; found {
					fmt.Printf("found `%s`\n", str)
					d.deps[str] = struct{}{}
				}
			}
		case curLen > d.max:
			d.reset()
		}
	}
}
