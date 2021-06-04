package ops

type InstallEnv struct {
	// Directory to create build dirs in
	BuildDir string

	// Directory that contains installed packages
	StoreDir string

	// Start a shell
	StartShell bool

	// Contains paths to installed packages
	PackagePaths map[string]string

	// Indicates that the build process should retain the build dir
	RetainBuild bool
}
