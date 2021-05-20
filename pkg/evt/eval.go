package evt

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/go-hclog"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/cleanhttp"
	"lab47.dev/aperture/pkg/fileutils"
)

type Evaluator struct {
	L            hclog.Logger
	cwd          string
	outdir       string
	env          []string
	outputPrefix string
	path         string
}

type EvaluatorEnv struct {
	WorkingDir   string
	OutputDir    string
	Environ      []string
	OutputPrefix string
}

func NewEvaluator(L hclog.Logger, opts EvaluatorEnv) *Evaluator {
	ev := &Evaluator{
		L:            L,
		cwd:          opts.WorkingDir,
		outdir:       opts.OutputDir,
		env:          opts.Environ,
		outputPrefix: opts.OutputPrefix,
	}

	for _, kv := range opts.Environ {
		if strings.HasPrefix(kv, "PATH=") {
			ev.path = kv[5:]
		}
	}

	return ev
}

func (e *Evaluator) Eval(n EVTNode) error {
	if n == nil {
		return nil
	}

	switch n := n.(type) {
	case *Statements:
		for _, ev := range n.Statements {
			err := e.Eval(ev)
			if err != nil {
				return err
			}
		}

		return nil
	case *SetRoot:
		tgt := e.workPath(n.Dir)

		sf, err := ioutil.ReadDir(tgt)
		if err != nil {
			return err
		}

		var (
			ent os.FileInfo
			cnt int
		)
		for _, e := range sf {
			if e.Name()[0] != '.' {
				cnt++
				ent = e
			}
		}
		if cnt == 1 && ent.IsDir() {
			tgt = filepath.Join(tgt, ent.Name())
		}

		e.cwd = tgt

	case *ChangeDir:
		defer func(dir string) {
			e.cwd = dir
		}(e.cwd)

		e.cwd = e.workPath(n.Dir)

		return e.Eval(n.Body)
	case *MakeDir:
		err := os.MkdirAll(e.workPath(n.Dir), 0755)
		if err != nil {
			return err
		}
	case *Shell:
		cmd := exec.Command("bash")
		cmd.Stdin = strings.NewReader(n.Code)
		cmd.Env = e.env
		cmd.Dir = e.cwd

		return e.runCmd(cmd)
	case *System:
		exe := n.Arguments[0]
		var err error

		if filepath.Base(exe) == exe {
			exe, err = lookPath(exe, e.path)
			if err != nil {
				return err
			}
		}

		cmd := exec.Command("bash")
		cmd.Env = e.env
		cmd.Dir = e.cwd

		return e.runCmd(cmd)
	case *Patch:
		cmd := exec.Command("patch", "-p1")
		cmd.Stdin = strings.NewReader(n.Patch)
		cmd.Env = e.env
		cmd.Dir = e.cwd

		return e.runCmd(cmd)
	case *Replace:
		path := e.workPath(n.File)

		data, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		f, err := os.Create(path)
		if err != nil {
			return err
		}

		defer f.Close()

		if n.Replacer != nil {
			_, err = n.Replacer.WriteString(f, string(data))
			if err != nil {
				return err
			}
		} else {
			data := n.Regexp.ReplaceAll(data, n.Target)

			_, err = f.Write(data)
			if err != nil {
				return err
			}
		}

	case *Rmrf:
		target := filepath.Join(e.cwd, n.Target)

		err := os.RemoveAll(target)
		if err != nil {
			return err
		}
	case *SetEnv:
		if n.Append {
			if n.Key == "PATH" {
				e.path = e.path + ":" + n.Value
			}

			prefix := n.Key + "="

			for i, kv := range e.env {
				if strings.HasPrefix(kv, prefix) {
					e.env[i] += (string(filepath.ListSeparator) + n.Value)
					return nil
				}
			}
		} else if n.Prepend {
			if n.Key == "PATH" {
				e.path = n.Value + ":" + e.path
			}

			prefix := n.Key + "="

			for i, kv := range e.env {
				if strings.HasPrefix(kv, prefix) {
					e.env[i] = n.Value + string(filepath.ListSeparator) + kv
					return nil
				}
			}
		}
		e.env = append(e.env, n.Key+"="+n.Value)
	case *Link:
		target := e.outPath(n.Target)
		os.MkdirAll(filepath.Dir(target), 0755)

		err := os.Symlink(e.outPath(n.Original), target)
		if err != nil {
			return err
		}
	case *Unpack:
		path := e.workPath(n.Path)

		var (
			archive string
			dec     getter.Decompressor
		)

		matchingLen := 0
		for k := range getter.Decompressors {
			if strings.HasSuffix(path, "."+k) && len(k) > matchingLen {
				archive = k
				matchingLen = len(k)
			}
		}

		output := e.workPath(n.Output)

		if output == "" {
			output = filepath.Dir(path)
		}

		dec, ok := getter.Decompressors[archive]
		if !ok {
			return fmt.Errorf("No known decompressor for path: %s", path)
		}

		target := output

		if _, err := os.Stat(target); err != nil {
			return errors.Wrapf(err, "target missing")
		}

		err := dec.Decompress(target, path, true, 0)
		if err != nil {
			return errors.Wrapf(err, "unable to decompress %s", path)
		}
	case *Download:
		resp, err := cleanhttp.Get(n.URL)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		path := e.workPath(n.Path)

		f, err := os.Create(path)
		if err != nil {
			return err
		}

		defer f.Close()

		if n.Sum == nil {
			_, err = io.Copy(f, resp.Body)
			if err != nil {
				return err
			}

			return nil
		}

		var (
			w  io.Writer
			h  hash.Hash
			sv []byte
		)

		switch n.Sum.Type {
		case "b2":
			h, _ = blake2b.New256(nil)
			w = io.MultiWriter(f, h)
			sv, err = base58.Decode(n.Sum.Value)
			if err != nil {
				return err
			}
		case "sha256":
			sv, err = hex.DecodeString(n.Sum.Value)
			if err != nil {
				return err
			}
			h = sha256.New()
			w = io.MultiWriter(f, h)
		case "etag":
			w = f
			// ok
		default:
			return fmt.Errorf("unknown sum type: %s", n.Sum.Type)
		}

		io.Copy(w, resp.Body)

		switch n.Sum.Type {
		case "etag":
			if CompareEtag(n.Sum.Value, resp.Header.Get("Etag")) {
				return fmt.Errorf("bad etag sum: %s (%s <> %s)",
					path, n.Sum.Value, resp.Header.Get("Etag"))
			}
		default:
			if !bytes.Equal(sv, h.Sum(nil)) {
				return fmt.Errorf("bad sum: %s", path)
			}
		}
	case *InstallFiles:
		pattern := e.workPath(n.Pattern)
		target := e.workPath(n.Target)

		var inst fileutils.Install
		inst.L = e.L
		inst.Dest = target
		inst.Pattern = pattern
		inst.Linked = n.Symlink

		return inst.Install()
	case *WriteFile:
		target := e.outPath(n.Target)

		f, err := os.Create(target)
		if err != nil {
			return err
		}

		defer f.Close()

		_, err = f.Write(n.Data)

		return err
	}

	return nil
}

func (e *Evaluator) checkPath(path string) string {
	if !(strings.HasPrefix(path, e.cwd) || strings.HasPrefix(path, e.outdir)) {
		panic(fmt.Sprintf("invalid path used, outside work or output dir: %s", path))
	}

	return path
}

func (e *Evaluator) workPath(fspath FSPath) string {
	path := string(fspath)

	if !filepath.IsAbs(path) {
		path = filepath.Join(e.cwd, path)
	}

	return e.checkPath(path)
}

func (e *Evaluator) outPath(fspath FSPath) string {
	path := string(fspath)

	if !filepath.IsAbs(path) {
		path = filepath.Join(e.outdir, path)
	}

	return e.checkPath(path)
}

func (e *Evaluator) runCmd(cmd *exec.Cmd) error {
	or, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	er, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		br := bufio.NewReader(or)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s\n", e.outputPrefix, strings.TrimRight(line, " \n\t"))
			}

			if err != nil {
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		br := bufio.NewReader(er)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s\n", e.outputPrefix, strings.TrimRight(line, " \n\t"))
			}

			if err != nil {
				return
			}
		}
	}()

	err = cmd.Start()
	if err != nil {
		return err
	}

	wg.Wait()

	err = cmd.Wait()
	if err != nil {
		return err
	}

	return nil
}

var (
	ErrNotFound = errors.New("entry not found")
)

func findExecutable(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if m := d.Mode(); !m.IsDir() && m&0111 != 0 {
		return nil
	}
	return os.ErrPermission
}

// LookPath searches for an executable named file in the
// directories named by the PATH environment variable.
// If file contains a slash, it is tried directly and the PATH is not consulted.
// The result may be an absolute path or a path relative to the current directory.
func lookPath(file string, path string) (string, error) {
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
	return "", errors.Wrapf(ErrNotFound, "unable to find executable: %s", path)
}

func CompareEtag(a, b string) bool {
	if a[0] == '"' {
		a = a[1 : len(a)-2]
	}

	if b[0] == '"' {
		b = b[1 : len(b)-2]
	}

	return a == b
}
