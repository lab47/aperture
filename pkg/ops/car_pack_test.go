package ops

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/data"
)

func TestCarPack(t *testing.T) {
	topdir, err := ioutil.TempDir("", "carpack")
	require.NoError(t, err)

	defer os.RemoveAll(topdir)

	dir := filepath.Join(topdir, "t")

	fsum := blake2b.Sum256([]byte("blah"))
	fake := base58.Encode(fsum[:])

	testBin := []byte(fmt.Sprintf("#!/bin/sh\ncat %s/%s-blah-1.0/whatever\n", dir, fake))

	t.Run("can pack a directory into a car", func(t *testing.T) {
		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		require.NoError(t, os.Mkdir(filepath.Join(dir, "bin"), 0755))

		err := ioutil.WriteFile(filepath.Join(dir, "bin/test"), testBin, 0644)
		require.NoError(t, err)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var (
			cp    CarPack
			buf   bytes.Buffer
			cinfo data.CarInfo
		)

		cp.PrivateKey = priv
		cp.PublicKey = pub

		dh, _ := blake2b.New256(nil)

		err = cp.Pack(&cinfo, dir, io.MultiWriter(&buf, dh))
		require.NoError(t, err)

		assert.Equal(t, dh.Sum(nil), cp.Sum)

		dir2 := filepath.Join(topdir, "i")
		require.NoError(t, os.Mkdir(dir2, 0755))
		defer os.RemoveAll(dir2)

		var ri CarUnpack
		err = ri.Install(bytes.NewReader(buf.Bytes()), dir2)
		require.NoError(t, err)
	})

	t.Run("can detect dependencies", func(t *testing.T) {
		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		require.NoError(t, os.Mkdir(filepath.Join(dir, "bin"), 0755))

		err := ioutil.WriteFile(filepath.Join(dir, "bin/test"), testBin, 0644)
		require.NoError(t, err)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var (
			cp    CarPack
			buf   bytes.Buffer
			cinfo data.CarInfo
		)

		cp.PrivateKey = priv
		cp.PublicKey = pub
		cp.DepRootDir = dir

		dh, _ := blake2b.New256(nil)

		err = cp.Pack(&cinfo, dir, io.MultiWriter(&buf, dh))
		require.NoError(t, err)

		require.Equal(t, 1, len(cp.Dependencies))

		assert.Equal(t, fake, cp.Dependencies[0])
	})

	t.Run("can map dependenices", func(t *testing.T) {
		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		require.NoError(t, os.Mkdir(filepath.Join(dir, "bin"), 0755))

		err := ioutil.WriteFile(filepath.Join(dir, "bin/test"), testBin, 0644)
		require.NoError(t, err)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var (
			cp    CarPack
			buf   bytes.Buffer
			cinfo data.CarInfo
		)

		cp.PrivateKey = priv
		cp.PublicKey = pub
		cp.DepRootDir = dir
		cp.MapDependencies = func(hash string) (string, string, string) {
			switch hash {
			case fake:
				return fake + "-fake-1.0", "foo/blah", "abcdf"
			default:
				panic("no")
			}
		}

		dh, _ := blake2b.New256(nil)

		err = cp.Pack(&cinfo, dir, io.MultiWriter(&buf, dh))
		require.NoError(t, err)

		require.Equal(t, 1, len(cp.Dependencies))

		assert.Equal(t, fake+"-fake-1.0", cp.Dependencies[0])

		dir2 := filepath.Join(topdir, "i")
		require.NoError(t, os.Mkdir(dir2, 0755))
		defer os.RemoveAll(dir2)

		var ri CarUnpack
		err = ri.Install(bytes.NewReader(buf.Bytes()), dir2)
		require.NoError(t, err)

		dep := ri.Info.Dependencies[0]

		assert.Equal(t, fake+"-fake-1.0", dep.ID)
		assert.Equal(t, "foo/blah", dep.Repo)
		assert.Equal(t, "abcdf", dep.Signer)
	})

}
