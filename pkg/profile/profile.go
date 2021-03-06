package profile

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/mr-tron/base58"
	"lab47.dev/aperture/pkg/config"
)

type profileChange struct {
	id, path string
}

type Profile struct {
	path string

	changes []profileChange
}

func OpenProfile(cfg *config.Config, path string) (*Profile, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(path, 0755)
	if err != nil {
		return nil, err
	}

	hashName := base58.Encode([]byte(path))

	os.Symlink(filepath.Join(path, ".refs"), filepath.Join(cfg.RootsPath(), hashName))

	return &Profile{path: path}, nil
}

func (p *Profile) Link(id string, root string) error {
	p.changes = append(p.changes, profileChange{id: id, path: root})
	return nil
}

func (p *Profile) linkOne(id string, root string) error {
	refs := filepath.Join(p.path, ".refs")

	if tgt, err := os.Readlink(filepath.Join(refs, id)); err == nil {
		if tgt == root {
			return nil
		}
	}

	err := LinkTree(p.path, root)
	if err != nil {
		return err
	}

	err = os.MkdirAll(refs, 0755)
	if err != nil {
		return err
	}

	tgt := filepath.Join(refs, id)

	os.Remove(tgt)

	return os.Symlink(root, tgt)
}

// Add adds any requested links to the profile, it does not
// prune out entries like Commit.
func (p *Profile) Add() error {
	for _, ch := range p.changes {
		err := p.linkOne(ch.id, ch.path)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Profile) Commit() error {
	known := map[string]struct{}{}

	files, _ := ioutil.ReadDir(filepath.Join(p.path, ".refs"))

	var refs []string

	for _, fi := range files {
		known[fi.Name()] = struct{}{}
		refs = append(refs, fi.Name())
	}

	for _, ch := range p.changes {
		delete(known, ch.id)
	}

	// If we deleted something, nuke the profile dir and we'll rebuild it now
	if len(known) > 0 {
		err := os.RemoveAll(p.path)
		if err != nil {
			return err
		}

		err = os.MkdirAll(filepath.Join(p.path, ".refs"), 0755)
		if err != nil {
			return err
		}
	}

	for _, ch := range p.changes {
		err := p.linkOne(ch.id, ch.path)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Profile) UpdateEnv(env []string) []string {
	var updates []string

	binDir := filepath.Join(p.path, "bin")

	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			val := kv[5:]
			updates = append(updates, fmt.Sprintf("PATH=%s:%s", binDir, val))
		}
	}

	return updates
}

func (p *Profile) ComputeEnv(env []string) []string {
	var updates []string

	binDir := filepath.Join(p.path, "bin")

	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			val := kv[5:]
			kv = fmt.Sprintf("PATH=%s:%s", binDir, val)

			os.Setenv("PATH", binDir+":"+val)
		}

		updates = append(updates, kv)
	}

	return updates
}

func (p *Profile) EnvMap(env []string) map[string]string {
	m := map[string]string{}

	binDir := filepath.Join(p.path, "bin")

	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq == -1 {
			continue
		}
		k := kv[:eq]

		v := kv[eq+1:]

		if k == "PATH" {
			v = fmt.Sprintf("%s%s%s", binDir, string(filepath.ListSeparator), v)
		}

		m[k] = v
	}

	return m
}
