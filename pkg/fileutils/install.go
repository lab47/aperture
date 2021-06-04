package fileutils

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hashicorp/go-hclog"
)

type Install struct {
	Ctx     context.Context
	L       hclog.Logger
	Pattern string
	Dest    string
	Linked  bool
	ModeOr  os.FileMode
}

func (i *Install) shouldCancel() error {
	if i.Ctx == nil {
		return nil
	}

	select {
	case <-i.Ctx.Done():
		return i.Ctx.Err()
	default:
		return nil
	}
}

func (i *Install) Install() error {
	if i.L == nil {
		i.L = hclog.L()
	}

	_, err := os.Stat(i.Pattern)
	if err == nil {
		os.MkdirAll(filepath.Dir(i.Dest), 0755)
		if i.Linked {
			i.L.Debug("symlink", "old", i.Pattern, "new", i.Dest)

			oldRel, err := filepath.Rel(filepath.Dir(i.Dest), i.Pattern)
			if err != nil {
				oldRel = i.Pattern
			}

			return os.Symlink(oldRel, i.Dest)
		} else {
			return i.copyEntry(i.Pattern, i.Dest)
		}
	}

	entries, err := filepath.Glob(i.Pattern)
	if err != nil {
		return err
	}

	baseDir := filepath.Dir(i.Pattern)

	if _, err := os.Stat(i.Dest); err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(i.Dest, 0755)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	for _, ent := range entries {
		rel, err := filepath.Rel(baseDir, ent)
		if err != nil {
			return err
		}

		target := filepath.Join(i.Dest, rel)

		if i.Linked {
			i.L.Debug("symlink", "old", ent, "new", target)

			oldRel, err := filepath.Rel(filepath.Dir(target), ent)
			if err != nil {
				oldRel = ent
			}

			err = os.Symlink(oldRel, target)
		} else {
			err = i.copyEntry(ent, target)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (i *Install) copyEntry(from, to string) error {
	if err := i.shouldCancel(); err != nil {
		return err
	}

	i.L.Trace("copy entry", "from", from, "to", to)

	f, err := os.Open(from)
	if err != nil {
		return err
	}

	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	defer func() {
		// fix the times
		os.Chtimes(to, time.Time{}, fi.ModTime())
	}()

	switch fi.Mode() & os.ModeType {
	case 0: // regular file
		tg, err := os.OpenFile(
			to,
			os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
			fi.Mode().Perm()|i.ModeOr.Perm(),
		)
		if err != nil {
			return err
		}

		defer tg.Close()

		_, err = io.Copy(tg, f)
		if err != nil {
			if err != io.EOF {
				return err
			}
		}

		return nil
	case os.ModeDir:
		if _, err := os.Stat(to); err != nil {
			err = os.Mkdir(to, fi.Mode().Perm()|i.ModeOr.Perm())
			if err != nil {
				return err
			}
		}

		entries, err := f.Readdirnames(-1)
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		sort.Strings(entries)

		for _, name := range entries {
			err = i.copyEntry(filepath.Join(from, name), filepath.Join(to, name))
			if err != nil {
				return err
			}
		}

	case os.ModeSymlink:
		link, err := os.Readlink(from)
		if err != nil {
			return err
		}

		return os.Symlink(link, to)
	}

	return nil
}
