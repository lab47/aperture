package rpath

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// Shrink opens an ELF binary at path and minimizes the declared rpath to only include
// libraries that are referenced by the needs declarations.
func Shrink(path string, keep []string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	ef, err := ParseELF64File(data)
	if err != nil {
		return err
	}

	var (
		dynId  = -1
		strtab []byte
	)

	for i, sec := range ef.Sections {
		switch sec.Type {
		case DynamicLinkingTableSection:
			dynId = i
		case StringTableSection:
			name, err := ef.GetSectionName(uint16(i))
			if err != nil {
				return err
			}

			if name == ".dynstr" {
				strtab, err = ef.GetSectionContent(uint16(i))
				if err != nil {
					return err
				}
			}
		}
	}

	// No dyn, no rpath to shrink.
	if dynId == -1 {
		return nil
	}

	dynent, err := ef.GetDynamicTable(uint16(dynId))
	if err != nil {
		return err
	}

	var (
		rpath      string
		rpathIdx   uint64
		runpath    string
		runpathIdx uint64
		needed     []string
	)

	for _, e := range dynent {
		switch e.Tag {
		case DT_RPATH:
			runpathIdx = e.Value
			runpath = NullStr(strtab[e.Value:])
		case DT_RUNPATH:
			rpathIdx = e.Value
			rpath = NullStr(strtab[e.Value:])
		case DT_NEEDED:
			needed = append(needed, NullStr(strtab[e.Value:]))
		}
	}

	// If runpath isn't defined, then use rpath
	if runpath == "" {
		runpath = rpath
		runpathIdx = rpathIdx
	}

	parts := filepath.SplitList(runpath)

	var toInclude []string

outer:
	for _, dir := range parts {
		if len(dir) < 1 {
			continue
		}

		// Presume the user was doing something... interesting if the rpath doesn't use
		// absolute paths (Could also be $ORIGIN), and just don't prune it.
		if dir[0] != '/' {
			toInclude = append(toInclude, dir)
			continue
		}

		// Always include the paths we request to always keep.
		for _, prefix := range keep {
			if strings.HasPrefix(prefix, dir) {
				toInclude = append(toInclude, dir)
				continue outer
			}
		}

		found := false

		for _, lib := range needed {
			_, err := os.Stat(filepath.Join(dir, lib))
			if err == nil {
				found = true
				break
			}
		}

		if found {
			toInclude = append(toInclude, dir)
		}
	}

	width := len(runpath)

	runpath = strings.Join(toInclude, string(filepath.ListSeparator))

	pos := strtab[runpathIdx:]
	copy(pos, []byte(runpath))

	start := len(runpath)

	// zero it out so strings scans don't see the unused dirs.
	for i := range pos[start:width] {
		pos[start+i] = 0
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}

	defer f.Close()

	si, err := f.Stat()
	if err != nil {
		return err
	}

	perm := si.Mode().Perm()

	err = f.Chmod(perm & 0200)
	if err != nil {
		return err
	}

	_, err = f.Write(ef.Raw)
	if err != nil {
		return err
	}

	return f.Chmod(perm)
}
