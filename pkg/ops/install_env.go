package ops

type InstallEnv struct {
	// Directory to create build dirs in
	BuildDir string

	// Directory that contains installed packages
	StoreDir string

	// Start a shell
	StartShell bool
}
