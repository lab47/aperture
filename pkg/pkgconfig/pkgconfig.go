package pkgconfig

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Path        string
	Id          string
	Name        string
	Description string
	URL         string
	Version     string
	Requires    []string
	Private     []string
	Conflict    []string
	Cflags      string
	Libs        string
	PrivLibs    string
}

func LoadAll(root string) ([]*Config, error) {
	var configs []*Config

	for _, sub := range []string{"lib/pkgconfig", "share/pkgconfig"} {
		err := filepath.Walk(filepath.Join(root, sub), func(path string, info fs.FileInfo, err error) error {
			if filepath.Ext(path) == ".pc" {
				cfg, err := Load(path)
				if err != nil {
					return err
				}

				configs = append(configs, cfg)
			}

			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	return configs, nil
}

func Load(path string) (*Config, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer r.Close()

	br := bufio.NewReader(r)

	vars := map[string]string{}

	var cfg Config

	cfg.Id = filepath.Base(path)
	cfg.Id = cfg.Id[:len(cfg.Id)-3] // -3 to remove .pc

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}

		var (
			name  string
			value string
			isVar bool
		)

	outer:
		for i, b := range line {
			switch b {
			case '=':
				name = line[:i]
				value = strings.TrimSpace(line[i+1:])
				isVar = true
				break outer
			case ':':
				name = line[:i]
				value = strings.TrimSpace(line[i+1:])
				break outer
			}
		}

		if name == "" {
			continue
		}

		value = expand(value, vars)

		if isVar {
			vars[name] = value
		} else {
			switch name {
			case "Name":
				cfg.Name = value
			case "Description":
				cfg.Description = value
			case "URL":
				cfg.URL = value
			case "Version":
				cfg.Version = value
			case "Requires":
				cfg.Requires = strings.Split(value, ",")
			case "Requires.private":
				cfg.Private = strings.Split(value, ",")
			case "Conflicts":
				cfg.Conflict = strings.Split(value, ",")
			case "Cflags":
				cfg.Cflags = value
			case "Libs":
				cfg.Libs = value
			case "Libs.private":
				cfg.PrivLibs = value
			}
		}
	}

	return &cfg, nil
}

func expand(input string, vars map[string]string) string {
	var sb strings.Builder

	var (
		state int
		si    int
	)

	for i, b := range input {
		switch state {
		case 0:
			if b == '$' {
				state = 1
			} else {
				sb.WriteRune(b)
			}
		case 1:
			if b == '{' {
				state = 2
				si = i + 1
			} else {
				sb.WriteRune('$')
				sb.WriteRune(b)
				state = 0
			}
		case 2:
			if b == '}' {
				sb.WriteString(vars[input[si:i]])
				state = 0
			}
		}
	}

	return sb.String()
}
