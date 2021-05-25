package homebrew

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

type Resolver struct {
	cellar string
	root   string

	seen  map[string]*ResolvedPackage
	usage map[string]int
}

func NewResolver(cellar, path string) (*Resolver, error) {
	r := &Resolver{
		cellar: cellar,
		root:   path,
		seen:   map[string]*ResolvedPackage{},
		usage:  map[string]int{},
	}

	return r, nil
}

type ResolvedPackage struct {
	Package

	Installed        bool
	IncludedBy       []*ResolvedPackage
	IncludedByNames  []string
	DepedentPackages []*ResolvedPackage
}

type Resolution struct {
	Cellar    string
	ToInstall []*ResolvedPackage
}

func (r *Resolution) PruneInstalled() {
	var ti []*ResolvedPackage

	for _, pkg := range r.ToInstall {
		if pkg.Installed {
			continue
		}

		ti = append(ti, pkg)
	}

	r.ToInstall = ti
}

func (r *Resolver) Add(name string) error {
	_, err := r.loadPackage(name, r.seen, r.usage)
	return err
}

func (r *Resolver) Resolve() (*Resolution, error) {
	var res Resolution
	res.Cellar = r.cellar

	toInstall, err := r.order(r.seen, r.usage)
	if err != nil {
		return nil, err
	}

	res.ToInstall = toInstall

	return &res, nil
}

func (r *Resolver) loadPackage(
	name string,
	seen map[string]*ResolvedPackage,
	usage map[string]int,
) (*ResolvedPackage, error) {
	f, err := os.Open(filepath.Join(r.root, name+".json"))
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var pkg ResolvedPackage

	err = json.NewDecoder(f).Decode(&pkg.Package)
	if err != nil {
		return nil, errors.Wrapf(err, "Loading %s", name)
	}

	seen[name] = &pkg
	usage[name] = 0

	_, err = os.Stat(filepath.Join(r.cellar, pkg.Name, pkg.Version))
	pkg.Installed = err == nil

	for _, dep := range pkg.Dependencies {
		dp, ok := seen[dep]
		if !ok {
			dp, err = r.loadPackage(dep, seen, usage)
			if err != nil {
				return nil, err
			}
		}

		usage[dep]++

		dp.IncludedBy = append(dp.IncludedBy, &pkg)
		dp.IncludedByNames = append(dp.IncludedByNames, pkg.Name)
		pkg.DepedentPackages = append(pkg.DepedentPackages, dp)
	}

	return &pkg, nil
}

func (r *Resolver) order(
	seen map[string]*ResolvedPackage,
	usage map[string]int,
) ([]*ResolvedPackage, error) {
	var toCheck []string

	for name, dep := range usage {
		if dep == 0 {
			toCheck = append(toCheck, name)
		}
	}

	var toInstall []*ResolvedPackage

	for len(toCheck) > 0 {
		x := toCheck[len(toCheck)-1]
		toCheck = toCheck[:len(toCheck)-1]

		pkg := seen[x]

		toInstall = append(toInstall, pkg)

		for _, dep := range pkg.Dependencies {
			deg := usage[dep] - 1
			usage[dep] = deg

			if deg == 0 {
				toCheck = append(toCheck, dep)
			}
		}
	}

	var reorder []*ResolvedPackage

	for i := len(toInstall) - 1; i >= 0; i-- {
		reorder = append(reorder, toInstall[i])
	}

	return reorder, nil
}

func (r *Resolution) Explain(w io.Writer, d *Downloader) error {
	tr := tabwriter.NewWriter(w, 4, 2, 1, ' ', 0)
	defer tr.Flush()

	if d != nil {
		urls, err := r.ComputeURLs()
		if err != nil {
			return err
		}

		total, err := d.Prep(urls)
		if err != nil {
			return err
		}

		fmt.Fprintln(tr, "NAME\tVERSION\tSIZE\tINCLUDED BY")

		for i, pkg := range r.ToInstall {
			ib := strings.Join(pkg.IncludedByNames, ", ")
			sz, _ := d.Size(urls[i].URL)

			hs := humanSize(sz)
			if len(hs) < 10 {
				hs = "          "[:10-len(hs)] + hs
			}
			fmt.Fprintf(tr, "%s\t%s\t%s\t%s\n", pkg.Name, pkg.Version, hs, ib)
		}

		tr.Flush()

		fmt.Printf("\nTOTAL: %s\n", humanSize(total))
	} else {
		fmt.Fprintln(tr, "NAME\tVERSION\tINCLUDED BY")

		for _, pkg := range r.ToInstall {
			ib := strings.Join(pkg.IncludedByNames, ", ")
			fmt.Fprintf(tr, "%s\t%s\t%s\n", pkg.Name, pkg.Version, ib)
		}
	}

	return nil
}

var sysMap = map[string]string{
	"darwin": "macos",
	"linux":  "linux",
}

var (
	platform = sysMap[runtime.GOOS]
	version  = "unknown"

	MacOSVersion    string
	RequireCodeSign bool
)

func init() {
	data, err := exec.Command("sw_vers", "-productVersion").Output()
	if err == nil {
		ver := string(data)

		if strings.HasPrefix(ver, "10.") {
			version = ver[:5]
		} else {
			version = ver[:2]
		}

		MacOSVersion = version

		RequireCodeSign = !strings.HasPrefix(ver, "10.")
	}
}

func DefaultCellar() string {
	switch runtime.GOARCH {
	case "arm64":
		return "/opt/homebrew/Cellar"
	default:
		return "/usr/local/Cellar"
	}
}

func findBinary(cellar string, pkg *ResolvedPackage) (*Binary, error) {
	for _, bin := range pkg.Binaries {
		spew.Dump(bin)

		arch := bin.System.Arch
		if arch == "x86_64" {
			arch = "amd64"
		}

		if arch == runtime.GOARCH &&
			bin.System.Os == platform &&
			bin.System.Version == version {

			if bin.Options == nil {
				return bin, nil
			}

			if bin.Options.InstallPath != "" && bin.Options.InstallPath != cellar {
				continue
			}

			return bin, nil
		}
	}

	return nil, nil
}

type PackageURL struct {
	URL      string
	Checksum *Checksum
	Binary   *Binary
}

func (r *Resolution) ComputeURLs() ([]PackageURL, error) {
	var urls []PackageURL

	for _, pkg := range r.ToInstall {
		bin, err := findBinary(r.Cellar, pkg)
		if err != nil {
			return nil, err
		}

		if bin == nil {
			return nil, fmt.Errorf("unable to find: %s", pkg.Name)
		}

		chk := &bin.Checksum

		name := strings.Replace(pkg.Name, "@", "/", 1)

		urls = append(urls, PackageURL{
			URL:      fmt.Sprintf("https://ghcr.io/v2/homebrew/core/%s/blobs/sha256:%s", name, chk.Sha256),
			Checksum: chk,
			Binary:   bin,
		})
	}

	return urls, nil
}

func humanSize(sz int64) string {
	switch {
	case sz < 1024:
		return fmt.Sprintf("%dB", sz)
	case sz < 1024*1024:
		return fmt.Sprintf("%.2fKB", float64(sz)/1024)
	case sz < 1024*1024*1024:
		return fmt.Sprintf("%.2fMB", float64(sz)/(1024*1024))
	default:
		return fmt.Sprintf("%.2fGB", float64(sz)/(1024*1024*1024))
	}
}
