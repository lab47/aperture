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

	for _, id := range toInstall.InstallOrder {
		storeDir := filepath.Join(p.ienv.StoreDir, id)
		toInstall.InstallDirs[id] = storeDir

		if toInstall.Installed[id] {
			continue
		}

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
