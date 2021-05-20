package homebrew

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

type InstallablePackage struct {
	rp  *ResolvedPackage
	url PackageURL

	cellar string
}

func (i *InstallablePackage) Name() string {
	return i.rp.Name
}

func (i *InstallablePackage) Version() string {
	return i.rp.Version
}

func (i *InstallablePackage) DependencyNames() []string {
	return i.rp.Dependencies
}

func (i *InstallablePackage) Install(tmpdir string) error {
	u, err := NewUnpacker(i.cellar)
	if err != nil {
		return err
	}

	path := calcPath(tmpdir, i.url.URL, ".tar.gz")

	fmt.Printf("Download: %s\n", path)

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	h := sha256.New()

	_, err = downloadTo(i.url.URL, io.MultiWriter(f, h))
	if err != nil {
		return err
	}

	if !i.url.Checksum.Matches(h) {
		err = fmt.Errorf("mismatched checksum of data")
	}

	fmt.Printf("Validateed checksum: %s\n", i.url.Checksum.Sha256)

	_, err = u.Unpack(i.rp, i.url.Binary, path)
	if err != nil {
		return err
	}

	return nil
}
