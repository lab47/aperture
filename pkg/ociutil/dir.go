package ociutil

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/data"
)

const (
	CarInfoJson    = ".car-info.json"
	SignatureEntry = "~signature"
)

var (
	ErrNoSignature      = errors.New("no signature detected")
	ErrInvalidSignature = errors.New("invalid signature detected")
)

// writeImagesToTar writes the images to the tarball
func WriteDir(img v1.Image, dir string) (*data.CarInfo, error) {
	h, _ := blake2b.New256(nil)

	// Write the layers.
	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}

	var (
		sig      []byte
		infoData []byte
	)

	for _, l := range layers {
		r, err := l.Uncompressed()
		if err != nil {
			return nil, err
		}

		i, s, err := writeTarToDir(h, dir, r)
		if err != nil {
			return nil, err
		}

		if i != nil {
			infoData = i
		}

		if s != nil {
			sig = s
		}
	}

	h.Write(infoData)

	var info data.CarInfo

	err = json.Unmarshal(infoData, &info)
	if err != nil {
		return nil, err
	}

	if info.Signer == "" || len(sig) == 0 {
		os.RemoveAll(dir)
		return nil, ErrNoSignature
	}

	signer, err := base58.Decode(info.Signer)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	if !ed25519.Verify(ed25519.PublicKey(signer), h.Sum(nil), sig) {
		os.RemoveAll(dir)
		return nil, ErrInvalidSignature
	}

	return &info, nil
}

func writeTarToDir(h hash.Hash, dir string, r io.Reader) ([]byte, []byte, error) {
	tr := tar.NewReader(r)

	var (
		sig      []byte
		infoData []byte
	)

top:
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, nil, err
		}

		switch hdr.Name {
		case CarInfoJson:
			var buf bytes.Buffer

			io.Copy(&buf, tr)

			infoData = buf.Bytes()

			continue top
		case SignatureEntry:
			sig, err = ioutil.ReadAll(tr)
			if err != nil {
				return nil, nil, err
			}

			continue top
		}

		path := filepath.Join(dir, hdr.Name)
		dir := filepath.Dir(path)

		if _, err := os.Stat(dir); err != nil {
			err = os.MkdirAll(dir, 0755)
			if err != nil {
				return nil, nil, err
			}
		}

		switch hdr.Typeflag {
		case tar.TypeReg:
			fmt.Fprintf(h, hdr.Name)
			h.Write([]byte{0})

			mode := hdr.FileInfo().Mode()
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return nil, nil, err
			}

			io.Copy(io.MultiWriter(h, f), tr)

			err = f.Close()
			if err != nil {
				return nil, nil, err
			}
		case tar.TypeSymlink:
			fmt.Fprintf(h, hdr.Name)
			h.Write([]byte{1})
			fmt.Fprintf(h, hdr.Linkname)
			h.Write([]byte{0})

			err = os.Symlink(filepath.Join(path, hdr.Linkname), path)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	return infoData, sig, nil
}
