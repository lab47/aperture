package homebrew

import (
	"bytes"
	"encoding/hex"
	"hash"
)

type System struct {
	Os      string `json:"os"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

type Checksum struct {
	Sha256 string `json:"sha256"`
}

type Options struct {
	SkipRelocation bool   `json:"skip_relocation"`
	InstallPath    string `json:"install_path"`
}

type Binary struct {
	System   System   `json:"system"`
	Checksum Checksum `json:"checksum"`
	Options  *Options `json:"options"`
}

type License struct {
	Name  string     `json:"name,omitempty"`
	With  *License   `json:"with,omitempty"`
	AnyOf []*License `json:"any_of,omitempty"`
	AllOf []*License `json:"all_of,omitempty"`
}

type Package struct {
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Url          string    `json:"url"`
	Version      string    `json:"version"`
	License      *License  `json:"license"`
	Checksum     *Checksum `json:"checksum"`
	Dependencies []string  `json:"dependencies"`
	Binaries     []*Binary `json:"binaries"`

	Path string
}

func (c *Checksum) Matches(h hash.Hash) bool {
	b, err := hex.DecodeString(c.Sha256)
	if err != nil {
		return false
	}

	return bytes.Equal(b, h.Sum(nil))
}
