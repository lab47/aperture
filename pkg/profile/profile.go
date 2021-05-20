package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Profile struct {
	path string
}

func OpenProfile(path string) (*Profile, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(path, 0755)
	if err != nil {
		return nil, err
	}

	return &Profile{path}, nil
}

func (p *Profile) Link(id string, root string) error {
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
