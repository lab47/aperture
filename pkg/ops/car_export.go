package ops

import (
	"os"
	"path/filepath"

	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
)

type CarExport struct {
	common

	cfg         *config.Config
	constraints map[string]string
}

func (c *CarExport) Export(pkg *ScriptPackage, path, dest string) (*ExportedCar, error) {
	var pri PackageReadInfo
	pi, err := pri.ReadPath(pkg, path)
	if err != nil {
		return nil, err
	}

	var deps []*data.CarDependency

	for _, d := range pi.RuntimeDeps {
		deps = append(deps, &data.CarDependency{
			ID: d,
		})
	}

	osName, osVer, arch := config.Platform()

	ci := &data.CarInfo{
		ID:           pkg.ID(),
		Name:         pkg.Name(),
		Version:      pkg.Version(),
		Repo:         pkg.Repo(),
		Constraints:  c.constraints,
		Dependencies: deps,
		Platform: &data.CarPlatform{
			OS:        osName,
			OSVersion: osVer,
			Arch:      arch,
		},
	}

	carPath := filepath.Join(dest, pkg.ID()+".car")

	f, err := os.Create(carPath)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var cp CarPack
	cp.PrivateKey = c.cfg.Private()
	cp.PublicKey = c.cfg.Public()

	err = cp.Pack(ci, path, f)
	if err != nil {
		return nil, err
	}

	exported := &ExportedCar{
		Package: pkg,
		Info:    ci,
		Path:    carPath,
		Sum:     cp.Sum,
	}

	return exported, nil
}
