package ops

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
)

const Extension = ".chell"

var (
	ErrNotFound = errors.New("entry not found")
)

type httpDo interface {
	Do(*http.Request) (*http.Response, error)
}

type ScriptLookup struct {
	common

	client httpDo

	Path []string

	repoDetect RepoDetect
}

type ScriptData interface {
	Script() []byte
	Asset(name string) ([]byte, error)
	Repo() string
}

type dirScriptData struct {
	data []byte

	repo string
	dir  string
}

func (s *dirScriptData) Script() []byte {
	return s.data
}

func (s *dirScriptData) Repo() string {
	return s.repo
}

func (s *dirScriptData) Asset(name string) ([]byte, error) {
	return ioutil.ReadFile(filepath.Join(s.dir, name))
}

func (s *ScriptLookup) LoadDir(dir, name string) (ScriptData, error) {
	var short string

	if len(name) > 2 {
		short = name[:2]
	} else {
		short = name
	}

	possibles := []struct {
		path, dir string
	}{
		{
			path: filepath.Join(dir, name+Extension),
			dir:  dir,
		},
		{
			path: filepath.Join(dir, "packages", name+Extension),
			dir:  filepath.Join(dir, "packages"),
		},
		{
			path: filepath.Join(dir, "packages", name, name+Extension),
			dir:  filepath.Join(dir, "packages", name),
		},
		{
			path: filepath.Join(dir, "packages", short, name+Extension),
			dir:  filepath.Join(dir, "packages", short),
		},
		{
			path: filepath.Join(dir, "packages", short, name, name+Extension),
			dir:  filepath.Join(dir, "packages", short, name),
		},
	}

	for _, x := range possibles {
		s.L().Trace("load-dir", "path", x.path)
		data, err := ioutil.ReadFile(x.path)
		if err == nil {
			repo, err := s.repoDetect.Detect(dir)
			if err != nil {
				panic(err)
			}
			return &dirScriptData{data: data, dir: x.dir, repo: repo}, nil
		}
	}

	return nil, ErrNotFound
}

type ghScriptData struct {
	client httpDo

	data []byte

	base string
}

func (s *ghScriptData) Script() []byte {
	return s.data
}

func (s *ghScriptData) Repo() string {
	return s.base
}

func (s *ghScriptData) Asset(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", s.base, name)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("script not available: %d", resp.StatusCode)
	}

	var content struct {
		Content []byte `json:"content"`
	}

	err = json.NewDecoder(resp.Body).Decode(&content)
	if err != nil {
		return nil, err
	}

	return content.Content, nil
}

func (s *ScriptLookup) LoadGithub(repo, name string) (ScriptData, error) {
	return s.loadGithub(s.client, repo, name)
}

func (s *ScriptLookup) loadGithub(client httpDo, repo, name string) (ScriptData, error) {
	slash := strings.IndexByte(repo, '/')

	if slash == -1 {
		return nil, nil
	}

	host := repo[:slash]

	if host == "github.com" {
		host = "api.github.com"
	}

	ghname := repo[slash+1:]

	var short string

	if len(name) > 2 {
		short = name[:2]
	} else {
		short = name
	}

	possibles := []struct {
		path, dir string
	}{
		{
			path: name + Extension,
		},
		{
			path: filepath.Join("packages", name+Extension),
			dir:  filepath.Join("packages"),
		},
		{
			path: filepath.Join("packages", name, name+Extension),
			dir:  filepath.Join("packages", name),
		},
		{
			path: filepath.Join("packages", short, name+Extension),
			dir:  filepath.Join("packages", short),
		},
		{
			path: filepath.Join("packages", short, name, name+Extension),
			dir:  filepath.Join("packages", short, name),
		},
	}

	var lastError error

	for _, x := range possibles {
		url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", ghname, x.path)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			lastError = fmt.Errorf("script not available: %d", resp.StatusCode)
			continue
		}

		var content struct {
			Content string `json:"content"`
		}

		err = json.NewDecoder(resp.Body).Decode(&content)
		if err != nil {
			lastError = err
			continue
		}

		data, err := base64.StdEncoding.DecodeString(content.Content)
		if err != nil {
			lastError = err
			continue
		}

		dir := x.dir
		if x.dir != "" {
			dir = "/" + dir
		}

		return &ghScriptData{
			data:   data,
			client: client,
			base:   fmt.Sprintf("https://api.github.com/repos/%s/contents%s", ghname, dir),
		}, nil
	}

	return nil, lastError
}

var cnt int

func (s *ScriptLookup) Load(name string) (ScriptData, error) {
	if s.client == nil {
		s.client = http.DefaultClient
	}

	for _, p := range s.Path {
		s.L().Trace("load-search", "path", p, "name", name)

		r, err := s.loadGeneric(p, name)
		if err != nil {
			if err == ErrNotFound {
				continue
			}

			return nil, err
		}

		return r, nil
	}

	return nil, ErrNotFound
}

func (s *ScriptLookup) loadGeneric(p, name string) (ScriptData, error) {
	switch {
	case strings.HasPrefix(p, "./"):
		r, err := s.LoadDir(p, name)
		if err == nil {
			return r, nil
		}
	case strings.HasPrefix(p, "/"):
		r, err := s.LoadDir(p, name)
		if err == nil {
			return r, nil
		}
	case strings.HasPrefix(p, "github.com/"):
		r, err := s.loadGithub(s.client, p, name)
		if err == nil {
			return r, nil
		}
	}

	return nil, ErrNotFound
}

func (s *ScriptLookup) loadVanity(client httpDo, repo, name string) (ScriptData, error) {
	req, err := http.NewRequest("GET", "https://"+repo+"?chell-get=1", nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
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
			return s.loadGeneric(i.RepoRoot, name)
		}
	}

	return nil, fmt.Errorf("no import location")
}
