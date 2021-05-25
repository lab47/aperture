package homebrew

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/davecgh/go-spew/spew"
)

type RuntimeDep struct {
	FullName string `json:"full_name"`
	Version  string `json:"version"`
}

type Tab struct {
	Version      string        `json:"homebrew_version"`
	ChangedFiles []string      `json:"changed_files"`
	ModTime      int64         `json:"source_modified_time"`
	Compiler     string        `json:"compiler"`
	Arch         string        `json:"arch"`
	RuntimeDeps  []*RuntimeDep `json:"runtime_dependencies"`

	BuiltOn struct {
		OS        string `json:"os"`
		OSVersion string `json:"os_version"`
		CPU       string `json:"cpu_family"`
		XCode     string `json:"xcode"`
		CLT       string `json:"clt"`
	} `json:"built_on"`
}

type Manifest struct {
	Manifests []struct {
		Annotations struct {
			Digest string `json:"sh.brew.bottle.digest"`
			Tab    string `json:"sh.brew.tab"`
		} `json:"annotations"`
	} `json:"manifests"`
}

func FetchTab(cellar string, pkg *ResolvedPackage) (*Tab, error) {
	url := fmt.Sprintf("https://ghcr.io/v2/homebrew/core/%s/manifests/%s", pkg.Name, pkg.Version)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer QQ==")
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	spew.Dump(url, resp.StatusCode)

	var man Manifest

	err = json.NewDecoder(resp.Body).Decode(&man)
	if err != nil {
		return nil, err
	}

	bin, err := findBinary(cellar, pkg)
	if err != nil {
		return nil, err
	}

	for _, m := range man.Manifests {
		if m.Annotations.Digest == bin.Checksum.Sha256 {
			var tab Tab

			err = json.Unmarshal([]byte(m.Annotations.Tab), &tab)
			return &tab, err
		}
	}

	return nil, nil
}
