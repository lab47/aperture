package status

import (
	"bytes"
	"io"

	"github.com/morikuni/aec"
)

type Display struct {
	height uint
	saved  bool
	output io.Writer
}

var nl = []byte("\n")

func (d *Display) Write(b []byte) (int, error) {
	if d.saved {
		_, err := d.output.Write([]byte(aec.Up(up).String()))
		if err != nil {
			return 0, err
		}
	} else {
		up := d.height + uint(bytes.Count(b, nl))
		_, err := d.output.Write([]byte(aec.Up(up).String()))
		if err != nil {
			return 0, err
		}
	}

	n, err := d.output.Write(b)
	if err != nil {
		return n, err
	}

	var bits []aec.ANSI

	if b[len(b)-1] != '\n' {
		d.saved = true
		bits = append(bits, aec.Save)
	} else {
		d.saved = false
	}

	bits = append(bits,
		aec.Down(d.height),
		aec.Column(0),
	)

	reset := aec.EmptyBuilder.With(bits...).ANSI.String()

	_, err = d.output.Write([]byte(reset))
	if err != nil {
		return n, err
	}

	return len(b), nil
}
