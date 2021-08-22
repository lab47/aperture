package data

type GlobalPackage struct {
	Name string `json:"name"`
	Id   string `json:"id"`
}

type GlobalPackages struct {
	Packages []*GlobalPackage `json:"packages"`
}
