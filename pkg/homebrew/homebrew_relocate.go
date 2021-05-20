package homebrew

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type InstallReceipt struct {
	ChangedFiles []string `json:"changed_files"`
}

type HomebrewRelocator struct {
	Cellar string
}

const ReceiptJson = "INSTALL_RECEIPT.json"

func (h *HomebrewRelocator) Relocate(root string) error {
	path := filepath.Join(root, ReceiptJson)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	defer f.Close()

	var rec InstallReceipt

	err = json.NewDecoder(f).Decode(&rec)
	if err != nil {
		return err
	}

	repo := filepath.Dir(h.Cellar)

	replacer := strings.NewReplacer(
		"@@HOMEBREW_PREFIX@@", root,
		"@@HOMEBREW_CELLAR@@", h.Cellar,
		"@@HOMEBREW_REPOSITORY@@", repo,
		"@@HOMEBREW_LIBRARY@@", filepath.Join(repo, "Library"),
	)

	for _, file := range rec.ChangedFiles {
		fpath := filepath.Join(root, file)

		fi, err := os.Stat(fpath)
		if err != nil {
			return err
		}

		data, err := ioutil.ReadFile(fpath)
		if err != nil {
			return err
		}

		data = []byte(replacer.Replace(string(data)))

		err = os.Chmod(fpath, fi.Mode().Perm()|0200)
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(fpath, data, fi.Mode().Perm())
		if err != nil {
			return err
		}

		err = os.Chmod(fpath, fi.Mode().Perm())
		if err != nil {
			return err
		}
	}

	return nil
}
