package fileutils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstall(t *testing.T) {
	root, err := ioutil.TempDir("", "fileutils")
	require.NoError(t, err)

	defer os.RemoveAll(root)

	tmpdir := filepath.Join(root, "t")
	cleanup := func() {
		os.RemoveAll(tmpdir)
		os.MkdirAll(tmpdir, 0755)
	}

	tmpdira := filepath.Join(tmpdir, "a")
	tmpdirb := filepath.Join(tmpdir, "b")

	wf := func(name, content string) {
		t.Helper()

		name = filepath.Join(tmpdir, name)

		os.MkdirAll(filepath.Dir(name), 0755)
		err = ioutil.WriteFile(name, []byte(content), 0644)
		require.NoError(t, err)
	}

	assertFile := func(t *testing.T, name, content string) {
		t.Helper()

		name = filepath.Join(tmpdir, name)

		data, err := ioutil.ReadFile(name)
		require.NoError(t, err)

		assert.Equal(t, content, string(data))
	}

	L := hclog.New(&hclog.LoggerOptions{Level: hclog.Info})

	t.Run("copies matching files to a new location", func(t *testing.T) {
		defer cleanup()

		wf("a/file", "this is a file")
		wf("a/sub/file", "this is a file also")

		in := &Install{
			L:       L,
			Pattern: filepath.Join(tmpdira, "*"),
			Dest:    tmpdirb,
		}

		err := in.Install()
		require.NoError(t, err)

		assertFile(t, "b/file", "this is a file")
		assertFile(t, "b/sub/file", "this is a file also")
	})

	t.Run("can link rather than copy", func(t *testing.T) {
		defer cleanup()

		wf("a/file", "this is a file")
		wf("a/sub/file", "this is a file also")

		in := &Install{
			L:       L,
			Pattern: filepath.Join(tmpdira, "*"),
			Dest:    tmpdirb,
			Linked:  true,
		}

		err := in.Install()
		require.NoError(t, err)

		assertFile(t, "b/file", "this is a file")
		assertFile(t, "b/sub/file", "this is a file also")

		tp := filepath.Join(tmpdirb, "sub")

		fi, err := os.Lstat(tp)
		require.NoError(t, err)

		assert.Equal(t, os.ModeSymlink, fi.Mode()&os.ModeType)
	})

	t.Run("copies a specified directory to a new location with a new name", func(t *testing.T) {
		defer cleanup()

		wf("a/file", "this is a file")
		wf("a/sub/file", "this is a file also")

		in := &Install{
			L:       L,
			Pattern: tmpdira,
			Dest:    tmpdirb,
		}

		err := in.Install()
		require.NoError(t, err)

		assertFile(t, "b/file", "this is a file")
		assertFile(t, "b/sub/file", "this is a file also")
	})

	t.Run("copies a specified directory to a new location with a new subdir", func(t *testing.T) {
		defer cleanup()

		wf("a/file", "this is a file")
		wf("a/sub/file", "this is a file also")

		in := &Install{
			L:       L,
			Pattern: tmpdira,
			Dest:    filepath.Join(tmpdirb, "lib", "brew"),
		}

		err := in.Install()
		require.NoError(t, err)

		assertFile(t, "b/lib/brew/file", "this is a file")
		assertFile(t, "b/lib/brew/sub/file", "this is a file also")
	})

	t.Run("copies a specified file to a new location with a new name", func(t *testing.T) {
		defer cleanup()

		wf("a/file", "this is a file")
		wf("a/sub/file", "this is a file also")

		in := &Install{
			L:       L,
			Pattern: filepath.Join(tmpdira, "file"),
			Dest:    filepath.Join(tmpdirb, "nf"),
		}

		err := in.Install()
		require.NoError(t, err)

		assertFile(t, "b/nf", "this is a file")
	})

	t.Run("symlinks a specified file to a new location with a new name", func(t *testing.T) {
		defer cleanup()

		wf("a/file", "this is a file")
		wf("a/sub/file", "this is a file also")

		in := &Install{
			L:       L,
			Pattern: filepath.Join(tmpdira, "file"),
			Dest:    filepath.Join(tmpdirb, "nf"),
			Linked:  true,
		}

		err := in.Install()
		require.NoError(t, err)

		tp := filepath.Join(tmpdirb, "nf")

		fi, err := os.Lstat(tp)
		require.NoError(t, err)

		assert.Equal(t, os.ModeSymlink, fi.Mode()&os.ModeType)
		assertFile(t, "b/nf", "this is a file")
	})

}
