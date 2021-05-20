package ops

import (
	"bytes"
)

type depDetect struct {
	file   string
	prefix []byte

	deps map[string]struct{}
	buf  *bytes.Buffer

	state        int
	restOfPrefix []byte
	hashParts    []byte
}

const (
	detectStart = iota
	detectPrefix
	detectHash
)

func (d *depDetect) Write(b []byte) (int, error) {
	d.findIn(b)
	return len(b), nil
}

func (d *depDetect) findIn(src []byte) {
	prefix := d.prefix

	if d.state == detectPrefix {
		prefix = d.restOfPrefix
	}

	for _, b := range src {
		switch d.state {
		case detectStart:
			if prefix[0] == b {
				prefix = prefix[1:]
				d.buf.WriteByte(b)

				d.state = detectPrefix
			}
		case detectPrefix:
			if prefix[0] == b {
				d.buf.WriteByte(b)
				prefix = prefix[1:]

				if len(prefix) == 0 {
					d.state = detectHash
					d.hashParts = nil
					d.buf.Reset()
				}
			} else {
				prefix = d.prefix
				d.state = detectStart
				d.buf.Reset()
			}
		case detectHash:
			_, found := validHashChars[b]
			if found {
				d.buf.WriteByte(b)
				d.hashParts = append(d.hashParts, b)
			} else {
				hash := string(d.hashParts)
				d.deps[hash] = struct{}{}

				d.state = detectPrefix
				prefix = d.prefix
			}
		}
	}

	d.restOfPrefix = prefix
}

var (
	validHashChars map[byte]struct{}
)

const hashAlphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func init() {
	validHashChars = map[byte]struct{}{}

	for _, c := range hashAlphabet {
		validHashChars[byte(c)] = struct{}{}
	}
}
