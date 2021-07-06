package cc

import (
	"bufio"
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
	path string
}

func NewCache(root, path string) (*Cache, error) {
	return nil, io.EOF
	return &Cache{root: root, path: path}, nil
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

	execPath, err := LookPath(newArgs[0], c.path)

	cmd := exec.CommandContext(ctx, execPath, newArgs[1:]...)

	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}

	err = cmd.Start()
	if err != nil {
		return "", "", err
	}

	br := bufio.NewReaderSize(out, 1024*1024)

	h, _ := blake2b.New256(nil)

outer:
	for {
		line, err := br.ReadSlice('\n')
		if err != nil {
			switch err {
			case bufio.ErrBufferFull:
				// ok, process line as is.
			case io.EOF:
				break outer
			default:
				L.Error("observed buffering error", "error", err)
				out.Close()
				break outer
			}
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
