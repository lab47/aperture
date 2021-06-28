package ops

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/morikuni/aec"
	"github.com/pkg/errors"
)

var ErrInstallError = errors.New("installation error")

type PackagesInstall struct {
	common

	ienv *InstallEnv

	Installed []string
	Failed    string
}

type InstallStats struct {
	Existing  int
	Installed int

	Elapsed time.Duration
}

func (p *PackagesInstall) Install(
	ctx context.Context, ienv *InstallEnv, toInstall *PackagesToInstall,
) (*InstallStats, error) {
	var is InstallStats

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
			is.Existing++
			continue
		}

		is.Installed++

		storeDir := p.ienv.Store.ExpectedPath(id)
		toInstall.InstallDirs[id] = storeDir

		ienv.PackagePaths[id] = storeDir

		fn, ok := toInstall.Installers[id]
		if !ok {
			continue
		}

		fmt.Println(
			aec.Bold.Apply(
				fmt.Sprintf("🔥 Installing package %s (%d/%d) (elapse: %s)",
					id, i+1, total, time.Since(start)),
			),
		)

		p.L().Debug("running installer", "id", id)

		err := fn.Install(ctx, p.ienv)
		if err != nil {
			p.Failed = id
			if !ienv.RetainBuild {
				os.RemoveAll(storeDir)
			}
			return nil, err
		}

		p.Installed = append(p.Installed, id)
	}

	is.Elapsed = time.Since(start)

	return &is, nil
}
