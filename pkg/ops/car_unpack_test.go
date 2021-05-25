package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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

const testBin = "#!/bin/sh\necho 'hello'\n"

func TestCarUnpack(t *testing.T) {
	topdir, err := ioutil.TempDir("", "remotecar")
	require.NoError(t, err)

	defer os.RemoveAll(topdir)

	dir := filepath.Join(topdir, "t")

	newCar := func(id string, priv ed25519.PrivateKey, pub ed25519.PublicKey) io.Reader {
		var buf bytes.Buffer

		gz := gzip.NewWriter(&buf)
		defer gz.Close()

		tw := tar.NewWriter(gz)
		defer tw.Close()

		dh, _ := blake2b.New256(nil)

		var hdr tar.Header
		hdr.Size = int64(len(testBin))
		hdr.Name = "bin/test"
		hdr.Format = tar.FormatPAX
		hdr.Typeflag = tar.TypeReg

		fmt.Fprintf(dh, "bin/test")
		dh.Write([]byte{0})

		tw.WriteHeader(&hdr)
		fmt.Fprintf(tw, testBin)

		fmt.Fprintf(dh, testBin)

		var ci data.CarInfo
		ci.ID = id

		if len(pub) != 0 {
			ci.Signer = base58.Encode(pub)
		}

		data, err := json.MarshalIndent(&ci, "", "  ")
		require.NoError(t, err)

		var hdr2 tar.Header
		hdr2.Name = ".car-info.json"
		hdr2.Format = tar.FormatPAX
		hdr2.Mode = 0400
		hdr2.Typeflag = tar.TypeReg
		hdr2.Size = int64(len(data))

		err = tw.WriteHeader(&hdr2)
		require.NoError(t, err)

		_, err = tw.Write(data)
		require.NoError(t, err)

		_, err = dh.Write(data)
		require.NoError(t, err)

		if priv != nil {
			sig := ed25519.Sign(priv, dh.Sum(nil))

			var hdr3 tar.Header
			hdr3.Name = SignatureEntry
			hdr3.Format = tar.FormatPAX
			hdr3.Mode = 0400
			hdr3.Typeflag = tar.TypeReg
			hdr3.Size = int64(len(sig))

			err = tw.WriteHeader(&hdr3)
			require.NoError(t, err)

			_, err = tw.Write(sig)
			require.NoError(t, err)
		}

		err = tw.Flush()
		require.NoError(t, err)

		err = gz.Flush()
		require.NoError(t, err)

		return &buf
	}

	newCarLink := func(id string, priv ed25519.PrivateKey, pub ed25519.PublicKey, tg string) io.Reader {
		var buf bytes.Buffer

		gz := gzip.NewWriter(&buf)
		defer gz.Close()

		tw := tar.NewWriter(gz)
		defer tw.Close()

		dh, _ := blake2b.New256(nil)

		var hdr tar.Header
		hdr.Size = int64(len(testBin))
		hdr.Name = "bin/test"
		hdr.Format = tar.FormatPAX
		hdr.Typeflag = tar.TypeSymlink
		hdr.Linkname = tg

		fmt.Fprintf(dh, "bin/test")
		dh.Write([]byte{1})
		fmt.Fprintf(dh, tg)
		dh.Write([]byte{0})

		require.NoError(t, tw.WriteHeader(&hdr))

		var ci data.CarInfo
		ci.ID = id

		if len(pub) != 0 {
			ci.Signer = base58.Encode(pub)
		}

		data, err := json.MarshalIndent(&ci, "", "  ")
		require.NoError(t, err)

		var hdr2 tar.Header
		hdr2.Name = ".car-info.json"
		hdr2.Format = tar.FormatPAX
		hdr2.Mode = 0400
		hdr2.Typeflag = tar.TypeReg
		hdr2.Size = int64(len(data))

		err = tw.WriteHeader(&hdr2)
		require.NoError(t, err)

		_, err = tw.Write(data)
		require.NoError(t, err)

		_, err = dh.Write(data)
		require.NoError(t, err)

		if priv != nil {
			sig := ed25519.Sign(priv, dh.Sum(nil))

			var hdr3 tar.Header
			hdr3.Name = "~signature"
			hdr3.Format = tar.FormatPAX
			hdr3.Mode = 0400
			hdr3.Typeflag = tar.TypeReg
			hdr3.Size = int64(len(sig))

			err = tw.WriteHeader(&hdr3)
			require.NoError(t, err)

			_, err = tw.Write(sig)
			require.NoError(t, err)
		}

		err = tw.Flush()
		require.NoError(t, err)

		err = gz.Flush()
		require.NoError(t, err)

		return &buf
	}

	t.Run("expands a car into a directory", func(t *testing.T) {
		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		var ri CarUnpack

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		f := newCar("abcdef-test-0.1", priv, pub)

		err = ri.Install(f, dir)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(dir, "bin/test"))
		require.NoError(t, err)

		assert.Equal(t, "abcdef-test-0.1", ri.Info.ID)
	})

	t.Run("checks the signature of the data", func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		var ri CarUnpack

		f := newCar("abcdef-test-0.1", priv, pub)

		err = ri.Install(f, dir)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(dir, "bin/test"))
		require.NoError(t, err)

		dh, _ := blake2b.New256(nil)
		fmt.Fprintf(dh, "bin/test")
		dh.Write([]byte{0})
		fmt.Fprintf(dh, testBin)

		data, err := json.MarshalIndent(ri.Info, "", "  ")
		require.NoError(t, err)

		dh.Write(data)

		assert.Equal(t, "abcdef-test-0.1", ri.Info.ID)
		assert.Equal(t, base58.Encode(pub), ri.Info.Signer)

		sig := ed25519.Sign(priv, dh.Sum(nil))
		assert.Equal(t, sig, ri.Signature)
	})

	t.Run("errors out if the signature check fails", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		var ri CarUnpack

		pubRogue, _, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		f := newCar("abcdef-test-0.1", priv, pubRogue)

		err = ri.Install(f, dir)
		require.Error(t, err)

		_, err = os.Stat(filepath.Join(dir, "bin/test"))
		require.Error(t, err)
	})

	t.Run("validates link targets", func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		require.NoError(t, os.Mkdir(dir, 0755))
		defer os.RemoveAll(dir)

		var ri1 CarUnpack

		f1 := newCarLink("abcdef-test-0.1", priv, pub, "bin/a")

		err = ri1.Install(f1, dir)
		require.NoError(t, err)

		dir2 := filepath.Join(topdir, "b")
		require.NoError(t, os.Mkdir(dir2, 0755))
		defer os.RemoveAll(dir2)

		var ri2 CarUnpack
		f2 := newCarLink("abcdef-test-0.1", priv, pub, "bin/b")

		err = ri2.Install(f2, dir2)
		require.NoError(t, err)

		assert.NotEqual(t, ri1.Signature, ri2.Signature)
	})
}
