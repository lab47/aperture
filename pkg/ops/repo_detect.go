package ops

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"lab47.dev/aperture/pkg/data"
)

type RepoDetect struct {
	known map[string]string
}

func (r *RepoDetect) Detect(path string) (string, error) {
	if r.known == nil {
		r.known = make(map[string]string)
	}

	id, ok := r.known[path]
	if ok {
		return id, nil
	}

	f, err := os.Open(filepath.Join(path, ".repo-info.json"))
	if err == nil {
		var ri data.RepoInfo

		err = json.NewDecoder(f).Decode(&ri)
		if err != nil {
			return "", err
		}

		r.known[path] = ri.Id

		return ri.Id, nil
	}

	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err == nil {
		remote, err := repo.Remote("origin")
		if err == nil {
			urls := remote.Config().URLs
			if len(urls) != 0 {
				id, err := gitRemoteRepoId(urls[0])
				if err != nil {
					return "", err
				}

				r.known[path] = id

				return id, nil
			}
		} else {
			if err != git.ErrRemoteNotFound {
				return "", err
			}
		}
	}

	// welp. I guess we'll use the directory base name

	id = filepath.Base(path)
	r.known[path] = id

	return id, nil
}

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
