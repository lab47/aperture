package cc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/klauspost/compress/zstd"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
)

type Cache struct {
	root string
}

func NewCache(root string) (*Cache, error) {
	return &Cache{root: root}, nil
}

var ErrNotCacheable = errors.New("not cacheable")

func (c *Cache) CalculateCacheInfo(ctx context.Context, L hclog.Logger, args []string) (string, string, error) {
	op, err := Analyze(args)
	if err != nil {
		return "", "", err
	}

	err = op.Cachable()
	if err != nil {
		return "", "", err
	}

	newArgs := op.PreprocessArgs()

	cmd := exec.CommandContext(ctx, newArgs[0], newArgs[1:]...)

	var errBuf bytes.Buffer

	cmd.Stderr = &errBuf

	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}

	err = cmd.Start()
	if err != nil {
		return "", "", err
	}

	br := bufio.NewReader(out)

	h, _ := blake2b.New256(nil)

	for {
		line, err := br.ReadSlice('\n')
		if err != nil {
			break
		}

		// Skip any cpp line markers because they contain file names
		// that won't be portable
		if len(line) > 1 && line[0] == '#' {
			continue
		}

		h.Write(line)
	}

	err = cmd.Wait()
	if err != nil {
		L.Error("error detected running preprocessing", "args", newArgs, "error", err)
		return "", "", err
	} else {
		L.Info("executed preprocessor", "args", newArgs)
	}

	h.Write(op.Nonce())

	return op.Output(), base58.Encode(h.Sum(nil)), nil
}

func (c *Cache) Retrieve(info, output string) (bool, error) {
	path := filepath.Join(c.root, info)

	i, err := os.Open(path)
	if err != nil {
		return false, nil
	}

	defer i.Close()

	zi, err := zstd.NewReader(i)
	if err != nil {
		return false, nil
	}

	defer zi.Close()

	o, err := os.Create(output)
	if err != nil {
		return false, nil
	}

	defer o.Close()

	_, err = io.Copy(o, zi)
	if err != nil {
		return false, nil
	}

	return true, nil
}

func (c *Cache) Store(info, output string) ([]byte, error) {
	path := filepath.Join(c.root, info)

	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, err
	}

	i, err := os.Open(output)
	if err != nil {
		return nil, nil
	}

	defer i.Close()

	o, err := os.Create(path)
	if err != nil {
		return nil, nil
	}

	defer o.Close()

	zo, err := zstd.NewWriter(o)
	if err != nil {
		return nil, nil
	}

	defer zo.Close()

	h, _ := blake2b.New256(nil)

	_, err = io.Copy(zo, io.TeeReader(i, h))
	if err != nil {
		return nil, nil
	}

	return h.Sum(nil), nil
}
