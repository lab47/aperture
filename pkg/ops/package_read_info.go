package ops

import (
	"encoding/json"
	"os"
	"path/filepath"

	"lab47.dev/aperture/pkg/data"
)

type PackageReadInfo struct {
	storeDir string
}

func (p *PackageReadInfo) Read(pkg *ScriptPackage) (*data.PackageInfo, error) {
	if pkg.PackageInfo != nil {
		return pkg.PackageInfo, nil
	}

	path := filepath.Join(p.storeDir, pkg.ID(), ".pkg-info.json")

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var pi data.PackageInfo

	err = json.NewDecoder(f).Decode(&pi)

	pkg.PackageInfo = &pi

	return &pi, err
}
