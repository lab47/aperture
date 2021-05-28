package ops

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"lab47.dev/aperture/pkg/data"
	"lab47.dev/aperture/pkg/metadata"
)

var NoCarData = errors.New("no car data found")

type CarReader interface {
	Lookup(name string) (io.ReadCloser, error)
	Info(name string) (*data.CarInfo, error)
}

type CarLookup struct {
	overrides map[string]CarReader
	client    httpDo
}

type CarData struct {
	name string
	r    CarReader
}

func (r *CarData) Open() (io.ReadCloser, error) {
	return r.r.Lookup(r.name)
}

func (r *CarData) Info() (*data.CarInfo, error) {
	return r.r.Info(r.name)
}

func (c *CarLookup) Lookup(repo, name string) (*CarData, error) {
	cr, ok := c.overrides[repo]
	if ok {
		return &CarData{
			name: name,
			r:    cr,
		}, nil
	}

	if strings.HasPrefix(repo, "github.com/") {
		cfg, err := checkGHConfig(c.client, repo)
		if err != nil {
			return nil, err
		}

		if cfg != nil {
			return &CarData{
				name: name,
				r: &httpRoots{
					client: c.client,
					roots:  cfg.CarURLS,
				},
			}, nil
		}

		gcl, err := checkGHRelease(c.client, repo, name)
		if err != nil {
			return nil, err
		}

		if gcl != nil {
			return &CarData{
				name: name,
				r:    gcl,
			}, nil
		}
	} else {
		return c.checkVanity(repo, name)
	}

	return nil, nil
}

func (c *CarLookup) checkVanity(repo, name string) (*CarData, error) {
	req, err := http.NewRequest("GET", "https://"+repo+"?aperture-get=1", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 404 {
		return nil, nil
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vanity returned error: %d", resp.StatusCode)
	}

	defer resp.Body.Close()

	imports, err := parseMetaImports(resp.Body)
	if err != nil {
		return nil, err
	}

	for _, i := range imports {
		if i.Prefix == repo {
			return c.Lookup(i.RepoRoot, name)
		}
	}

	return nil, fmt.Errorf("no import location")
}

type httpRoots struct {
	client httpDo
	roots  []string
}

func (g *httpRoots) Lookup(name string) (io.ReadCloser, error) {
	var topError error

	for _, r := range g.roots {
		u, err := url.Parse(r)
		if err != nil {
			topError = err
			continue
		}

		u.Path = filepath.Join(u.Path, name+".car")

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			topError = err
			continue
		}

		resp, err := g.client.Do(req)
		if err != nil {
			topError = err
			continue
		}

		if resp.StatusCode != 200 {
			topError = fmt.Errorf("car returned status code: %d", resp.StatusCode)
			continue
		}

		return resp.Body, nil
	}

	return nil, topError
}

func (g *httpRoots) Info(name string) (*data.CarInfo, error) {
	var topError error

	for _, r := range g.roots {
		u, err := url.Parse(r)
		if err != nil {
			topError = err
			continue
		}

		u.Path = filepath.Join(u.Path, name+".car-info.json")

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			topError = err
			continue
		}

		resp, err := g.client.Do(req)
		if err != nil {
			topError = err
			continue
		}

		if resp.StatusCode != 200 {
			topError = fmt.Errorf("car returned status code: %d", resp.StatusCode)
			continue
		}

		defer resp.Body.Close()

		var ai data.CarInfo

		err = json.NewDecoder(resp.Body).Decode(&ai)
		if err != nil {
			return nil, err
		}

		return &ai, nil
	}

	return nil, topError

}

func checkGHConfig(client httpDo, repo string) (*metadata.RepoConfig, error) {
	slash := strings.IndexByte(repo, '/')

	if slash == -1 {
		return nil, nil
	}

	host := repo[:slash]

	if host == "github.com" {
		host = "api.github.com"
	}

	name := repo[slash+1:]

	url := fmt.Sprintf("https://%s/repos/%s/contents/aperture.json", host, name)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, nil
	}

	var ghContent struct {
		Content string `json:"content"`
	}

	err = json.NewDecoder(resp.Body).Decode(&ghContent)
	if err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(ghContent.Content)
	if err != nil {
		return nil, err
	}

	var cfg metadata.RepoConfig

	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func checkGHRelease(client httpDo, repo, name string) (*GithubReleasesReader, error) {
	colon := strings.LastIndexByte(name, '-')
	if colon == -1 {
		return nil, nil
	}

	ver := name[colon+1:]

	url := fmt.Sprintf("https://%s/releases/download/%s/%s.car", repo, ver, name)

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, nil
	}

	return &GithubReleasesReader{
		client: client,
		url:    url,
	}, nil
}

type GithubReleasesReader struct {
	client httpDo
	url    string
}

func (g *GithubReleasesReader) Lookup(name string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", g.url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("error fetching car: %s: %d", g.url, resp.StatusCode)
	}

	return resp.Body, nil
}

func (g *GithubReleasesReader) Info(name string) (*data.CarInfo, error) {
	req, err := http.NewRequest("GET", g.url+"-info.json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("error fetching car: %s: %d", g.url, resp.StatusCode)
	}

	var ai data.CarInfo

	err = json.NewDecoder(resp.Body).Decode(&ai)

	return &ai, nil
}
