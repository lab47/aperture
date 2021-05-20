package data

type CarDependency struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Signer string `json:"signer"`
}

type CarInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`

	Repo string `json:"repo"`

	Signer string `json:"signer"`

	Dependencies []*CarDependency `json:"dependencies"`

	Constraints map[string]string `json:"constraints"`
}
