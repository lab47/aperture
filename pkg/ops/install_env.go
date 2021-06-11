package ops

import "lab47.dev/aperture/pkg/config"

type InstallEnv struct {
	// Directory to create build dirs in
	BuildDir string

	// Directory that contains installed packages
	Store *config.Store

	// Directory that packages can use to store data such as gems, config files,
	// etc.
	StateDir string

	// Start a shell
	StartShell bool

	// Contains paths to installed packages
	PackagePaths map[string]string

	// Indicates that the build process should retain the build dir
	RetainBuild bool

	// SkipPostInstall indicates that we should not run any post_install
	// functions. This is typically used when we're building a package
	// only to package it as a .car file.
	SkipPostInstall bool

	// PostInstallOnly indicates that we should not run any install
	// functions, only run post_install. This is typically used when we're
	// installing a .car and allow the package to adjust it into place.
	OnlyPostInstall bool

	// If set, install will generate a .car file for the packages install into
	// ExportPath. It performs the export before running post_install so the packages
	// are sealed properly.
	ExportPath string

	// When using ExportPath, this will be populated with the information about the car
	// files written.
	ExportedCars []*ExportedCar

	Config *config.Config
}
