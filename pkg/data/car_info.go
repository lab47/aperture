package data

type CarDependency struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Signer string `json:"signer"`
}

type CarPlatform struct {
	OS        string `json:"os"`
	OSVersion string `json:"os_version"`
	Arch      string `json:"architecture"`
}

type CarInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`

	Repo string `json:"repo"`

	Signer string `json:"signer"`

	Dependencies []*CarDependency `json:"dependencies"`

	Platform *CarPlatform `json:"platform"`

	Constraints map[string]string `json:"constraints"`
}
