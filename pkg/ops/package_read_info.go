package ops

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
)

type PackageReadInfo struct {
	Store *config.Store
}

func (p *PackageReadInfo) Read(pkg *ScriptPackage) (*data.PackageInfo, error) {
	path, err := p.Store.Locate(pkg.ID())
	if err != nil {
		return nil, err
	}

	return p.ReadPath(pkg, path)
}

func (p *PackageReadInfo) ReadPath(pkg *ScriptPackage, root string) (*data.PackageInfo, error) {
	if pkg.PackageInfo != nil {
		return pkg.PackageInfo, nil
	}

	path := filepath.Join(root, ".pkg-info.json")

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "attempting to load info for script: %s", pkg.Name())
	}

	var pi data.PackageInfo

	err = json.NewDecoder(f).Decode(&pi)

	pkg.PackageInfo = &pi

	return &pi, err
}
