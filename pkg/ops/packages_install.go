package ops

import (
	"context"
	"fmt"
	"os"
	"time"

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

	start := time.Now()

	total := len(toInstall.InstallOrder)

	for i, id := range toInstall.InstallOrder {
		if toInstall.Installed[id] {
			continue
		}

		storeDir := p.ienv.Store.ExpectedPath(id)
		toInstall.InstallDirs[id] = storeDir

		ienv.PackagePaths[id] = storeDir

		fn, ok := toInstall.Installers[id]
		if !ok {
			continue
		}

		fmt.Printf("Installing package %s (%d/%d) (elapse: %s)\n",
			id, i+1, total, time.Since(start))

		p.L().Debug("running installer", "id", id)

		err := fn.Install(ctx, p.ienv)
		if err != nil {
			p.Failed = id
			if !ienv.RetainBuild {
				os.RemoveAll(storeDir)
			}
			return err
		}

		p.Installed = append(p.Installed, id)
	}

	return nil
}
