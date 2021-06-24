package data

// BuildInfo is made available in the APPERTURE_BUILD_INFO env var
// to any script that is built.
//
type BuildInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`

	Prefix   string `json:"prefix"`
	BuildDir string `json:"build_dir"`

	Dependencies map[string]*BuildInfoDependency `json:"dependencies"`
}

type BuildInfoDependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`
	Path    string `json:"path"`

	Dependencies []string `json:"dependencies"`
}
