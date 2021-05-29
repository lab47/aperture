package data

type PackageInput struct {
	Name    string `json:"name"`
	SumType string `json:"sum_type"`
	Sum     string `json:"sum"`
	Dir     string `json:"dir,omitempty"`
	Path    string `json:"path,omitempty"`
	Id      string `json:"id,omitempty"`
}

type PackageInfo struct {
	Id          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Repo        string            `json:"repo"`
	DeclDeps    []string          `json:"declared_deps"`
	RuntimeDeps []string          `json:"runtime_deps"`
	BuildDeps   []string          `json:"build_deps"`
	Constraints map[string]string `json:"constraints"`
	Inputs      []*PackageInput   `json:"inputs"`
}
