package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/data"
)

var (
	ErrInvalidSignature = errors.New("invalid signature")
	ErrNoSignature      = errors.New("no signature")
)

type CarUnpack struct {
	Info      data.CarInfo
	Signature []byte
}

const (
	CarInfoJson    = ".car-info.json"
	SignatureEntry = "~signature"
)

func (r *CarUnpack) Install(in io.Reader, dir string) error {
	h, _ := blake2b.New256(nil)

	gz, err := gzip.NewReader(io.TeeReader(in, h))
	if err != nil {
		return err
	}

	tr := tar.NewReader(gz)

	dh, _ := blake2b.New256(nil)

	var sig []byte

	var infoData []byte
top:
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		switch hdr.Name {
		case CarInfoJson:
			var buf bytes.Buffer

			io.Copy(&buf, tr)

			infoData = buf.Bytes()

			err = json.Unmarshal(infoData, &r.Info)
			if err != nil {
				return err
			}

			continue top
		case SignatureEntry:
			sig, err = ioutil.ReadAll(tr)
			if err != nil {
				return err
			}

			continue top
		}

		path := filepath.Join(dir, hdr.Name)
		dir := filepath.Dir(path)

		if _, err := os.Stat(dir); err != nil {
			err = os.MkdirAll(dir, 0755)
			if err != nil {
				return err
			}
		}

		switch hdr.Typeflag {
		case tar.TypeReg:
			fmt.Fprintf(dh, hdr.Name)
			dh.Write([]byte{0})

			mode := hdr.FileInfo().Mode()
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}

			io.Copy(io.MultiWriter(dh, f), tr)

			err = f.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			fmt.Fprintf(dh, hdr.Name)
			dh.Write([]byte{1})
			fmt.Fprintf(dh, hdr.Linkname)
			dh.Write([]byte{0})

			err = os.Symlink(filepath.Join(path, hdr.Linkname), path)
			if err != nil {
				return err
			}
		}
	}

	dh.Write(infoData)

	if r.Info.Signer == "" || len(sig) == 0 {
		os.RemoveAll(dir)
		return ErrNoSignature
	}

	signer, err := base58.Decode(r.Info.Signer)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}

	if !ed25519.Verify(ed25519.PublicKey(signer), dh.Sum(nil), sig) {
		os.RemoveAll(dir)
		return ErrInvalidSignature
	}

	r.Signature = sig

	return nil
}
