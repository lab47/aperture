package metadata

type InstallDepedency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Repo    string `json:"repo"`
	Id      string `json:"id"`
}

type InstallInfo struct {
	Name         string             `json:"name"`
	Dependencies []InstallDepedency `json:"dependencies"`
	CarSize      int64              `json:"car_size"`
	CarHash      string             `json:"car_hash"`
}
