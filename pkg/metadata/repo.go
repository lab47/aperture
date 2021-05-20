package metadata

import (
	"net/url"
	"path"
	"strings"
)

type RepoConfig struct {
	Id      string   `json:"id"`
	CarURLS []string `json:"car_urls"`
}

func (r *RepoConfig) CalculateCarURLs(name string) []string {
	var expanded []string

	for _, n := range r.CarURLS {
		if strings.Contains(n, "$name") {
			expanded = append(expanded, strings.Replace(n, "$name", name, -1))
		} else {
			u, err := url.Parse(n)
			if err != nil {
				continue
			}

			u.Path = path.Join(u.Path, name)
			expanded = append(expanded, u.String())
		}
	}

	return expanded
}
