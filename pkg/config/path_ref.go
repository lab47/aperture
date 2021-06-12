package config

import (
	"fmt"
	"strings"
)

func CalcPath(name, ref string) (*PackagePath, error) {
	if ref == "" {
		return nil, fmt.Errorf("no ref supplied")
	}

	if ref[0] == '/' {
		return &PackagePath{
			Name:     name,
			Type:     "local",
			Location: ref,
		}, nil
	}

	switch {
	case strings.HasPrefix(ref, "github.com/"):
		var repo, ver string

		idx := strings.LastIndexByte(ref, '@')
		if idx == -1 {
			repo = ref
			ver = "main"
		} else {
			repo = ref[:idx]
			ver = ref[idx+1:]
		}

		return &PackagePath{
			Name:     name,
			Type:     "git",
			Location: "https://" + repo,
			Version:  ver,
		}, nil
	}

	return nil, fmt.Errorf("Unknown ref type: %s", ref)
}
