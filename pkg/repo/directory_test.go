package repo

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectory(t *testing.T) {
	top, err := ioutil.TempDir("", "dir")
	require.NoError(t, err)

	defer os.RemoveAll(top)

	dir := filepath.Join(top, "repo")

	t.Run("load name.ch", func(t *testing.T) {
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ioutil.WriteFile(
			filepath.Join(dir, "a"+Extension),
			[]byte(`data`),
			0644)

		do := &Directory{pkgPath: dir}

		ent, err := do.Lookup("a")
		require.NoError(t, err)

		e := ent.(*DirEntry)

		assert.Equal(t, dir, e.dir)
		assert.Equal(t, filepath.Join(dir, "a"+Extension), e.script)
	})

	t.Run("load name/name.ch", func(t *testing.T) {
		sub := filepath.Join(dir, "a")

		err := os.MkdirAll(sub, 0755)
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		ioutil.WriteFile(
			filepath.Join(sub, "a"+Extension),
			[]byte(`data`),
			0644)

		do := &Directory{pkgPath: dir}

		ent, err := do.Lookup("a")
		require.NoError(t, err)

		e := ent.(*DirEntry)

		assert.Equal(t, sub, e.dir)
		assert.Equal(t, filepath.Join(sub, "a"+Extension), e.script)
	})
}
