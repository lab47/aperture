package ops

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScriptInstall(t *testing.T) {
	top, err := ioutil.TempDir("", "chell")
	require.NoError(t, err)

	defer os.RemoveAll(top)

	t.Run("executes the install function", func(t *testing.T) {
		var lookup ScriptLookup
		lookup.Path = []string{"./testdata/script_install"}

		var sl ScriptLoad
		sl.lookup = &lookup

		pkg, err := sl.Load("touch")
		require.NoError(t, err)

		target := filepath.Join(top, "in")

		err = os.Mkdir(target, 0755)
		require.NoError(t, err)

		defer os.RemoveAll(target)

		build := filepath.Join(top, "build")

		err = os.Mkdir(build, 0755)
		require.NoError(t, err)

		defer os.RemoveAll(build)

		ienv := &InstallEnv{
			BuildDir: build,
			StoreDir: target,
		}

		si := &ScriptInstall{
			pkg: pkg,
		}

		ctx := context.Background()

		err = si.Install(ctx, ienv)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(target, pkg.ID(), "flag"))
		require.NoError(t, err)
	})
}
