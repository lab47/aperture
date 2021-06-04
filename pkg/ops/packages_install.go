package ops

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

var ErrInstallError = errors.New("installation error")

type PackagesInstall struct {
	common

	ienv *InstallEnv

	Installed []string
	Failed    string
}

func (p *PackagesInstall) Install(ctx context.Context, ienv *InstallEnv, toInstall *PackagesToInstall) error {
	p.ienv = ienv

	if toInstall.InstallDirs == nil {
		toInstall.InstallDirs = map[string]string{}
	}

	if ienv.PackagePaths == nil {
		ienv.PackagePaths = map[string]string{}
	}

	for n, path := range toInstall.InstallDirs {
		ienv.PackagePaths[n] = path
	}

	for _, id := range toInstall.InstallOrder {
		if toInstall.Installed[id] {
			continue
		}

		storeDir := filepath.Join(p.ienv.StoreDir, id)
		toInstall.InstallDirs[id] = storeDir

		ienv.PackagePaths[id] = storeDir

		fn, ok := toInstall.Installers[id]
		if !ok {
			continue
		}

		p.L().Debug("running installer", "id", id)

		err := fn.Install(ctx, p.ienv)
		if err != nil {
			p.Failed = id
			os.RemoveAll(storeDir)
			return err
		}

		p.Installed = append(p.Installed, id)
	}

	return nil
}
