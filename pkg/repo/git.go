package repo

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var scpSyntaxRe = regexp.MustCompile(`^([a-zA-Z0-9_]+)@([a-zA-Z0-9._-]+):(.*)$`)

func gitRemoteRepoId(configUrl string) (string, error) {
	var id string
	if m := scpSyntaxRe.FindStringSubmatch(configUrl); m != nil {
		id = fmt.Sprintf("%s/%s", m[2], m[3])
	} else {
		repoURL, err := url.Parse(configUrl)
		if err != nil {
			return "", err
		}

		id = fmt.Sprintf("%s/%s", repoURL.Host, repoURL.Path)
	}

	return strings.TrimSuffix(id, ".git"), nil
}
