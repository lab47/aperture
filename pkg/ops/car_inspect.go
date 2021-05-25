package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"lab47.dev/aperture/pkg/data"
)

type CarInspect struct {
	Info      data.CarInfo
	Signature []byte
}

func (r *CarInspect) Show(in io.Reader, show io.Writer) error {
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

		switch hdr.Typeflag {
		case tar.TypeReg:
			fmt.Fprintf(dh, hdr.Name)
			dh.Write([]byte{0})

			mode := hdr.FileInfo().Mode()

			io.Copy(io.MultiWriter(dh, ioutil.Discard), tr)

			fmt.Fprintf(show, "%s\t%d\t%s\n", mode.String(), hdr.Size, hdr.Name)

		case tar.TypeSymlink:
			fmt.Fprintf(dh, hdr.Name)
			dh.Write([]byte{1})
			fmt.Fprintf(dh, hdr.Linkname)
			dh.Write([]byte{0})

			mode := hdr.FileInfo().Mode()

			fmt.Fprintf(show, "%s\t%d\t%s => %s\n", mode.String(), hdr.Size, hdr.Name, hdr.Linkname)
		}
	}

	dh.Write(infoData)

	fmt.Fprintf(show, "\nName:\t%s\n", r.Info.Name)
	fmt.Fprintf(show, "Version:\t%s\n", r.Info.Version)
	fmt.Fprintf(show, "ID:\t%s\n", r.Info.ID)

	var deps []string
	for _, d := range r.Info.Dependencies {
		deps = append(deps, d.ID)
	}

	fmt.Fprintf(show, "Dependencies:\t%s\n", strings.Join(deps, ", "))

	var keys []string

	for k := range r.Info.Constraints {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var constraints []string

	for _, k := range keys {
		constraints = append(constraints, k+"="+r.Info.Constraints[k])
	}

	fmt.Fprintf(show, "Constaints:\t%s\n", strings.Join(constraints, ", "))

	if r.Info.Signer == "" || len(sig) == 0 {
		fmt.Fprintf(show, "\n! Warning: No Signature Detected\n")
		return nil
	}

	signer, err := base58.Decode(r.Info.Signer)
	if err != nil {
		fmt.Fprintf(show, "\n! Warning: Invalid Signature Detected\n")
		return nil
	}

	if !ed25519.Verify(ed25519.PublicKey(signer), dh.Sum(nil), sig) {
		fmt.Fprintf(show, "\n! Warning: Invalid Signature Detected\n")
		return nil
	}

	fmt.Fprintf(show, "Signature:\t%s\n", base58.Encode(sig))

	return nil
}
