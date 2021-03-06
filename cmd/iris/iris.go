package main

import (
	"bytes"
	"context"
	"debug/macho"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/morikuni/aec"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"lab47.dev/aperture/pkg/cc"
	"lab47.dev/aperture/pkg/cmd"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/data"
	"lab47.dev/aperture/pkg/direnv"
	"lab47.dev/aperture/pkg/gc"
	"lab47.dev/aperture/pkg/humanize"
	"lab47.dev/aperture/pkg/lockfile"
	"lab47.dev/aperture/pkg/ociutil"
	"lab47.dev/aperture/pkg/ops"
	"lab47.dev/aperture/pkg/profile"
)

func main() {
	if os.Args[0] != "iris" {
		bi := os.Getenv("APERTURE_BUILD_INFO")
		sp := os.Getenv("APERTURE_SHIM_PATH")
		cp := os.Getenv("APERTURE_CC_CACHE")

		err := cc.Run(os.Args, bi, sp, cp)
		log.Fatal(err)
	}

	c := cli.NewCLI("iris", "0.1.0")
	c.Args = os.Args[1:]
	c.Commands = map[string]cli.CommandFactory{
		"setup": func() (cli.Command, error) {
			return cmd.New(
				"setup",
				"perform any system or user setup",
				setupF,
			), nil
		},
		"add": func() (cli.Command, error) {
			return cmd.New(
				"add",
				"Adds a package to the global profile",
				addF,
			), nil

		},
		"install": func() (cli.Command, error) {
			return cmd.New(
				"install",
				"Install specified package or from project file",
				installF,
			), nil
		},
		"build": func() (cli.Command, error) {
			return cmd.New(
				"install",
				"Install specified package or from project file",
				buildF,
			), nil
		},
		"dev": func() (cli.Command, error) {
			return cmd.New(
				"dev",
				"Build a script in dev mode, deleting the data afterwards",
				devF,
			), nil
		},
		"search": func() (cli.Command, error) {
			return cmd.New(
				"search",
				"Search for packages",
				searchF,
			), nil
		},
		"shell": func() (cli.Command, error) {
			return cmd.New(
				"shell",
				"Run or get information about a shell for the project file",
				shellF,
			), nil
		},
		"inspect-car": func() (cli.Command, error) {
			return cmd.New(
				"inspect-car",
				"output information about a .car file",
				inspectCarF,
			), nil
		},
		"publish-car": func() (cli.Command, error) {
			return cmd.New(
				"publish-car",
				"publish one or many .car files",
				publishCarF,
			), nil
		},
		"env": func() (cli.Command, error) {
			return cmd.New(
				"env",
				"Output various environment information",
				envF,
			), nil
		},
		"gc": func() (cli.Command, error) {
			return cmd.New(
				"gc",
				"Run garbage collector to remove packages",
				gcF,
			), nil
		},
		"debug": func() (cli.Command, error) {
			return cmd.New(
				"debug",
				"Debug various things",
				debugF,
			), nil
		},
		"system-info": func() (cli.Command, error) {
			return cmd.New(
				"system-info",
				"Show information about this system",
				systemInfoF,
			), nil
		},
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}

	os.Exit(exitStatus)
}

func setupF(ctx context.Context, opts struct{}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return errors.Wrapf(err, "Unable to create or load configuration directory")
	}

	fmt.Printf("Config Dir: %s\n", cfg.ConfigDir())
	fmt.Printf("Aperture Data Dir: %s\n", cfg.DataDir)
	fmt.Printf("User Profiles Path: %s\n", cfg.ProfilesPath)

	id, err := cfg.SignerId()
	if err != nil {
		return errors.Wrapf(err, "Unable to calculate user keys")
	}

	fmt.Printf("User Signer Id: %s\n", id)

	return nil
}

func searchF(ctx context.Context, opts struct {
	JQ       bool `short:"j" description:"query using a jq-style string"`
	NameOnly bool `short:"n" long:"name-only" description:"only print the names of matching packages"`
	Pos      struct {
		Query string `positional-arg-name:"query"`
	} `positional-args:"yes"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	var cl ops.ProjectLoad

	_, err = cl.Load(ctx, cfg)
	if err != nil {
		return err
	}

	var cond ops.SearchCond

	if opts.JQ {
		cond, err = ops.SearchJQ(opts.Pos.Query)
		if err != nil {
			return err
		}
	} else {
		cond, err = ops.SearchRegexp(opts.Pos.Query)
		if err != nil {
			return err
		}
	}

	results, err := cl.Search(ctx, cond)
	if err != nil {
		return err
	}

	if opts.NameOnly {
		for _, sp := range results {
			fmt.Println(sp.Name)
		}

		return nil
	}

	for _, sp := range results {
		fmt.Printf("%s: %s\n", sp.Name, sp.Description)
		fmt.Printf("  version: %s\n", sp.Version)
		fmt.Printf("  dependencies: %s\n", strings.Join(sp.Dependencies, ", "))

		if sp.Vendor != "" {
			fmt.Printf("  vendor: %s\n", sp.Vendor)
		}

		if sp.URL != "" {
			fmt.Printf("  url: %s\n", sp.URL)
		}
		fmt.Println()
	}

	return nil
}

func forceRemove(path string, info fs.FileInfo) error {
	err := os.Remove(path)
	if err == nil {
		return nil
	}

	err = os.Chmod(path, info.Mode().Perm()|0200)
	if err != nil {
		return err
	}

	return os.Remove(path)
}

func forceRemoveDir(path string) error {
	var dirs []string
	err := filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			err = os.Chmod(path, info.Mode().Perm()|0200)
			if err != nil {
				return err
			}

			dirs = append(dirs, path)
			return nil
		}

		return forceRemove(path, info)
	})

	if err != nil {
		return err
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		err = os.Remove(dirs[i])
		if err != nil {
			return err
		}
	}

	return nil
}

func buildF(ctx context.Context, opts struct {
	Explain bool   `short:"E" long:"explain" description:"explain what will be installed"`
	Export  string `long:"export" description:"write .car files to the given directory"`
	Publish bool   `short:"P" long:"publish" description:"publish exported car files to repo"`
	Global  bool   `short:"G" long:"global" description:"install into the user's global profile"`
	Build   bool   `short:"B" long:"build-only" description:"build packages only, don't manage any profiles"`
	Clean   bool   `short:"C" long:"clean" description:"temporarily setup a clean store first"`

	Pos struct {
		Package string `positional-arg-name:"name"`
	} `positional-args:"yes"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	if opts.Clean {
		curStore := filepath.Join(cfg.DataDir, fmt.Sprintf("store-%d", os.Getpid()))
		defer func() {
			prev := curStore + ".prev"
			err = os.Rename(cfg.StorePath(), prev)
			if err != nil {
				fmt.Printf("error renaming store to prev: %s\n", err)
			}
			err = os.Rename(curStore, cfg.StorePath())
			if err != nil {
				fmt.Printf("error renaming saved to store: %s\n", err)
			}

			err = forceRemoveDir(prev)
			if err != nil {
				fmt.Printf("error removing temp store: %s\n", err)
			}
		}()
		os.Rename(cfg.StorePath(), curStore)

		fmt.Println(
			aec.Bold.Apply(
				fmt.Sprintf("???? Saved current store: %s", curStore),
			),
		)
	}

	buildRoot := cfg.BuildPath()

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		return err
	}

	stateDir := cfg.StatePath()

	err = os.MkdirAll(stateDir, 0755)
	if err != nil {
		return err
	}

	exportDir := opts.Export

	if exportDir == "" {
		exportDir = os.Getenv("IRIS_EXPORT_DIR")
	}

	ienv := &ops.InstallEnv{
		Store:      cfg.Store(),
		BuildDir:   buildRoot,
		StateDir:   stateDir,
		Config:     cfg,
		ExportPath: exportDir,
	}

	var cl ops.ProjectLoad

	proj, err := cl.LoadSet(ctx, cfg, opts.Pos.Package)
	if err != nil {
		return err
	}

	err = proj.Explain(ctx, ienv)
	if err != nil {
		return err
	}

	if opts.Explain {
		return nil
	}

	var showLock bool
	cleanup, err := lockfile.Take(ctx, ".iris-lock", func() {
		if !showLock {
			fmt.Printf("Lock detected, waiting...\n")
			showLock = true
		}
	})
	if err != nil {
		return err
	}

	defer cleanup()

	if opts.Publish && exportDir == "" {
		dir, err := ioutil.TempDir("", "iris")
		if err != nil {
			return err
		}

		exportDir = dir

		defer os.RemoveAll(dir)
	}

	if exportDir != "" {
		err := os.MkdirAll(exportDir, 0755)
		if err != nil {
			return err
		}

		ienv.ExportPath = exportDir
	}

	fmt.Println(
		aec.Bold.Apply(
			fmt.Sprintf("??? Beginning package building..."),
		),
	)

	if exportDir != "" {
		fmt.Println(
			aec.Bold.Apply(
				fmt.Sprintf("???? Saving .car files to: %s", exportDir),
			),
		)
	}

	_, pti, stats, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		return err
	}

	if exportDir != "" && opts.Publish {
		extra, err := proj.FindCachedBuildOnlyDeps(pti, exportDir)
		if err != nil {
			return err
		}

		toExport := append(extra, ienv.ExportedCars...)

		return publishCars(ctx, cfg, toExport)
	}

	fmt.Println(
		aec.Bold.Apply(
			fmt.Sprintf("???? Finished building! %d new packages, %d existing packages (elapse: %s)",
				stats.Installed, stats.Existing, stats.Elapsed.Round(time.Second).String(),
			),
		),
	)

	return nil
}

func addF(ctx context.Context, opts struct {
	Explain bool `short:"E" long:"explain" description:"explain what will be installed"`
	Pos     struct {
		Package string `positional-arg-name:"name"`
	} `positional-args:"yes"`
}) error {
	if opts.Pos.Package == "" {
		return fmt.Errorf("package name required")
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	buildRoot := cfg.BuildPath()

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		return err
	}

	stateDir := cfg.StatePath()

	err = os.MkdirAll(stateDir, 0755)
	if err != nil {
		return err
	}

	pkgPath := cfg.GlobalPackagesPath()

	var gp data.GlobalPackages

	f, err := os.Open(pkgPath)
	if err == nil {
		defer f.Close()

		err = json.NewDecoder(f).Decode(&gp)
		if err != nil {
			return errors.Wrapf(err, "attempting to decode global packages list")
		}

		f.Close()
	}

	ienv := &ops.InstallEnv{
		Store:    cfg.Store(),
		BuildDir: buildRoot,
		StateDir: stateDir,
		Config:   cfg,
	}

	var cl ops.ProjectLoad

	profilePath := cfg.GlobalProfilePath()

	proj, err := cl.Single(ctx, cfg, opts.Pos.Package)
	if err != nil {
		return err
	}

	if opts.Explain {
		err := proj.Explain(ctx, ienv)
		if err != nil {
			return err
		}

		return nil
	}

	var showLock bool
	cleanup, err := lockfile.Take(ctx, ".iris-lock", func() {
		if !showLock {
			fmt.Printf("Lock detected, waiting...\n")
			showLock = true
		}
	})
	if err != nil {
		return err
	}

	defer cleanup()

	fmt.Println(
		aec.Bold.Apply(
			fmt.Sprintf("??? Beginning package installation..."),
		),
	)

	requested, toInstall, stats, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		return err
	}

	prof, err := profile.OpenProfile(cfg, profilePath)
	if err != nil {
		return err
	}

	for _, pkg := range gp.Packages {
		id := pkg.Id

		path, err := ienv.Store.Locate(id)
		if err != nil {
			fmt.Printf("! Missing package from global list, pruning: %s\n", id)
		} else {
			err = prof.Link(id, path)
			if err != nil {
				return err
			}
		}
	}

	for _, id := range requested {
		gp.Packages = append(gp.Packages, &data.GlobalPackage{
			Name: toInstall.Scripts[id].Name(),
			Id:   id,
		})

		err = prof.Link(id, toInstall.InstallDirs[id])
		if err != nil {
			return err
		}
	}

	err = prof.Commit()
	if err != nil {
		return err
	}

	f, err = os.Create(cfg.GlobalPackagesPath())
	if err != nil {
		return errors.Wrapf(err, "attempting to update global package list")
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	err = enc.Encode(gp)
	if err != nil {
		return errors.Wrapf(err, "attempting to update global package list")
	}

	fmt.Println(
		aec.Bold.Apply(
			fmt.Sprintf("???? Finished installing! %d new packages, %d existing packages (elapse: %s)",
				stats.Installed, stats.Existing, stats.Elapsed.Round(time.Second).String(),
			),
		),
	)

	return nil
}

func installF(ctx context.Context, opts struct {
	Explain bool   `short:"E" long:"explain" description:"explain what will be installed"`
	Export  string `long:"export" description:"write .car files to the given directory"`
	Publish bool   `long:"publish" description:"publish exported car files to repo"`
	Global  bool   `short:"G" long:"global" description:"install into the user's global profile"`
	Build   bool   `short:"B" long:"build-only" description:"build packages only, don't manage any profiles"`

	Pos struct {
		Package string `positional-arg-name:"name"`
	} `positional-args:"yes"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	buildRoot := cfg.BuildPath()

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		return err
	}

	stateDir := cfg.StatePath()

	err = os.MkdirAll(stateDir, 0755)
	if err != nil {
		return err
	}

	ienv := &ops.InstallEnv{
		Store:    cfg.Store(),
		BuildDir: buildRoot,
		StateDir: stateDir,
		Config:   cfg,
	}

	var (
		proj *ops.Project
		cl   ops.ProjectLoad
	)

	profilePath := ".iris-profile"

	if opts.Global {
		profilePath = cfg.GlobalProfilePath()
	}

	if opts.Pos.Package != "" {
		proj, err = cl.Single(ctx, cfg, opts.Pos.Package)
	} else {
		proj, err = cl.Load(ctx, cfg)
	}

	if err != nil {
		return err
	}

	if opts.Explain {
		err := proj.Explain(ctx, ienv)
		if err != nil {
			return err
		}

		return nil
	}

	var showLock bool
	cleanup, err := lockfile.Take(ctx, ".iris-lock", func() {
		if !showLock {
			fmt.Printf("Lock detected, waiting...\n")
			showLock = true
		}
	})
	if err != nil {
		return err
	}

	defer cleanup()

	exportDir := opts.Export

	if exportDir == "" {
		exportDir = os.Getenv("IRIS_EXPORT_DIR")
	}

	if opts.Publish && exportDir == "" {
		dir, err := ioutil.TempDir("", "iris")
		if err != nil {
			return err
		}

		exportDir = dir

		defer os.RemoveAll(dir)
	}

	if exportDir != "" {
		err := os.MkdirAll(exportDir, 0755)
		if err != nil {
			return err
		}

		ienv.ExportPath = exportDir
	}

	fmt.Println(
		aec.Bold.Apply(
			fmt.Sprintf("??? Beginning package installation..."),
		),
	)

	if exportDir != "" {
		fmt.Println(
			aec.Bold.Apply(
				fmt.Sprintf("???? Saving .car files to: %s", exportDir),
			),
		)
	}

	requested, toInstall, stats, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		return err
	}

	if exportDir != "" && opts.Publish {
		return publishCars(ctx, cfg, ienv.ExportedCars)
	}

	if !opts.Build {
		prof, err := profile.OpenProfile(cfg, profilePath)
		if err != nil {
			return err
		}

		for _, id := range requested {
			err = prof.Link(id, toInstall.InstallDirs[id])
			if err != nil {
				return err
			}
		}

		if opts.Pos.Package != "" {
			err = prof.Add()
		} else {
			err = prof.Commit()
		}

		if err != nil {
			return err
		}
	}

	fmt.Println(
		aec.Bold.Apply(
			fmt.Sprintf("???? Finished installing! %d new packages, %d existing packages (elapse: %s)",
				stats.Installed, stats.Existing, stats.Elapsed.Round(time.Second).String(),
			),
		),
	)

	return nil
}

func publishCars(ctx context.Context, cfg *config.Config, cars []*ops.ExportedCar) error {
	var cp ops.CarPublish
	cp.Username = os.Getenv("GITHUB_USER")
	cp.Password = os.Getenv("GITHUB_TOKEN")

	for _, car := range cars {
		rc := car.Package.RepoConfig()
		if rc == nil {
			fmt.Printf("package missing repo config: %s\n", car.Package.Name())
			continue
		}

		cfg, err := rc.Config()
		if err != nil {
			return err
		}

		fmt.Printf("Publishing %s\n", car.Path)
		err = cp.PublishCar(ctx, car.Path, cfg.OCIRoot)
		if err != nil {
			return err
		}
	}

	return nil
}

func shellF(ctx context.Context, opts struct {
	DumpEnv bool     `short:"E" long:"dump-env" description:"dump updated env in direnv format"`
	Setup   bool     `short:"s" long:"setup" description:"output shell code to eval to update the env"`
	Global  bool     `short:"G" long:"global" description:"execute in the context of the global profile"`
	Args    []string `positional-args:"yes"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	if opts.Global {
		if opts.Setup {
			fmt.Printf("export PATH=%s/bin:%s/bin:%s\n", cfg.GlobalProfilePath(), cfg.StatePath(), os.Getenv("PATH"))
			return nil
		}

		fmt.Println("only -s accepted with -G")
		return nil
	}

	buildRoot := cfg.BuildPath()

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		return err
	}

	stateDir := cfg.StatePath()

	err = os.MkdirAll(stateDir, 0755)
	if err != nil {
		return err
	}

	ienv := &ops.InstallEnv{
		Store:    cfg.Store(),
		BuildDir: buildRoot,
		StateDir: stateDir,
	}

	var cl ops.ProjectLoad

	proj, err := cl.Load(ctx, cfg)
	if err != nil {
		return err
	}

	var showLock bool
	cleanup, err := lockfile.Take(ctx, ".iris-lock", func() {
		if !showLock {
			fmt.Printf("Lock detected, waiting...\n")
			showLock = true
		}
	})
	if err != nil {
		return err
	}

	defer cleanup()

	requested, toInstall, _, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		return err
	}

	prof, err := profile.OpenProfile(cfg, ".iris-profile")
	if err != nil {
		return err
	}

	for _, id := range requested {
		err = prof.Link(id, toInstall.InstallDirs[id])
		if err != nil {
			return err
		}
	}

	err = prof.Commit()
	if err != nil {
		return err
	}

	cleanup()

	if opts.Setup {
		updates := prof.UpdateEnv(os.Environ())

		for _, u := range updates {
			fmt.Println(u)
		}

		return nil
	}

	if opts.DumpEnv {
		var w io.Writer

		path := os.Getenv("DIRENV_DUMP_FILE_PATH")

		if path == "" {
			w = os.Stdout
		} else {
			f, err := os.Create(path)
			if err != nil {
				return err
			}

			defer f.Close()

			w = f
		}

		fmt.Fprintln(w, direnv.Dump(prof.EnvMap(os.Environ())))
		return nil
	}

	env := prof.ComputeEnv(os.Environ())

	path, err := exec.LookPath(opts.Args[0])
	if err != nil {
		return err
	}

	return unix.Exec(path, opts.Args, env)
}

func inspectCarF(ctx context.Context, opts struct {
	Args struct {
		File string `positional-arg-name:"file"`
	} `positional-args:"yes"`
}) error {
	f, err := os.Open(opts.Args.File)
	if err != nil {
		return err
	}

	defer f.Close()

	var ci ops.CarInspect

	tw := tabwriter.NewWriter(os.Stdout, 4, 2, 1, ' ', 0)
	defer tw.Flush()

	ci.Show(f, tw)

	return nil
}

func publishCarF(ctx context.Context, opts struct {
	Built   bool   `short:"B" long:"built" description:"publish all built packages"`
	Loaded  string `short:"L" long:"loaded" description:"publish previous exported cars by a project"`
	Package string `short:"p" description:"export and publish a car for a package"`
	Dir     string `long:"dir" description:"Use this package to store car files"`
}) error {
	fs := pflag.NewFlagSet("inspect-car", pflag.ExitOnError)

	var cp ops.CarPublish
	cp.Username = os.Getenv("GITHUB_USER")
	cp.Password = os.Getenv("GITHUB_TOKEN")

	if !opts.Built && fs.NArg() > 0 {
		err := cp.PublishCar(context.Background(), fs.Arg(0), "ghcr.io/lab47/aperture-packages")
		if err != nil {
			return err
		}
	}

	if opts.Loaded != "" {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		buildRoot := cfg.BuildPath()

		err = os.MkdirAll(buildRoot, 0755)
		if err != nil {
			return err
		}

		stateDir := cfg.StatePath()

		err = os.MkdirAll(stateDir, 0755)
		if err != nil {
			return err
		}

		var (
			proj *ops.Project
			cl   ops.ProjectLoad
		)

		proj, err = cl.Load(ctx, cfg)
		if err != nil {
			return err
		}

		exportDir := opts.Loaded

		ienv := &ops.InstallEnv{
			Store: &config.Store{
				Paths:   []string{"/nonexistant"},
				Default: "/nonexistant",
			},
		}

		toInstall, err := proj.CalculateSet(ctx, ienv)
		if err != nil {
			return err
		}

		var cp ops.CarPublish
		cp.Username = os.Getenv("GITHUB_USER")
		cp.Password = os.Getenv("GITHUB_TOKEN")

		for _, pkg := range toInstall.Scripts {
			rc := pkg.RepoConfig()
			if rc == nil {
				continue
			}

			cfg, err := rc.Config()
			if err != nil {
				return err
			}

			path := filepath.Join(exportDir, pkg.ID()+".car")

			if _, err := os.Stat(path); err != nil {
				fmt.Printf("Missing car: %s\n", path)
				continue
			}

			fmt.Printf("Publishing %s (%s) to %s\n", pkg.ID(), path, cfg.OCIRoot)
			err = cp.PublishCar(ctx, path, cfg.OCIRoot)
			if err != nil {
				return err
			}
		}

		return nil
	}

	if opts.Package != "" {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		buildRoot := cfg.BuildPath()

		err = os.MkdirAll(buildRoot, 0755)
		if err != nil {
			return err
		}

		stateDir := cfg.StatePath()

		err = os.MkdirAll(stateDir, 0755)
		if err != nil {
			return err
		}

		var (
			proj *ops.Project
			cl   ops.ProjectLoad
		)

		proj, err = cl.Single(ctx, cfg, opts.Package)
		if err != nil {
			return err
		}

		dir := opts.Dir

		if dir == "" {
			dir, err = ioutil.TempDir("", "iris")
			if err != nil {
				return err
			}

			defer os.RemoveAll(dir)
		}

		cars, err := proj.Export(ctx, cfg, dir)
		if err != nil {
			return err
		}

		return publishCars(ctx, cfg, cars)

	}

	var ss ops.StoreScan

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	pkgs, err := ss.Scan(ctx, cfg, true)
	if err != nil {
		return err
	}

	for _, pkg := range pkgs {
		fmt.Println(pkg.Package.ID())
	}

	dir, err := ioutil.TempDir("", "iris")
	if err != nil {
		return err
	}

	defer os.RemoveAll(dir)

	proj := &ops.Project{}

	for _, pkg := range pkgs {
		proj.Install = append(proj.Install, pkg.Package)
	}

	cars, err := proj.Export(ctx, cfg, dir)
	if err != nil {
		return err
	}

	return publishCars(ctx, cfg, cars)
}

func systemInfoF(ctx context.Context, opts struct{}) error {
	constraints := config.SystemConstraints()

	var keys []string

	for k := range constraints {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	fmt.Printf("System contraints:\n")
	for _, k := range keys {
		fmt.Printf("  %s: %s\n", k, constraints[k])
	}

	return nil
}

func envF(ctx context.Context, opts struct {
	Global bool `short:"G" long:"global-profile" description:"output location of global profile"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	if opts.Global {
		fmt.Println(cfg.GlobalProfilePath())
	}

	return nil
}

func gcF(ctx context.Context, opts struct {
	DryRun bool `short:"T" long:"dry-run" description:"output packages that would be removed"`
	Min    bool `short:"m" long:"outdated" description:"remove out-dated packages only"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	col, err := gc.NewCollector(cfg.DataDir)

	var toKeep []string

	if opts.Min {
		toKeep, err = col.MarkMinimal(ctx, cfg)
	} else {
		toKeep, err = col.Mark()
	}

	if err != nil {
		return err
	}

	if opts.DryRun {
		fmt.Println("## Packages Kept")
		for _, p := range toKeep {
			fmt.Println(p)
		}

		total, err := col.DiskUsage(toKeep)
		if err != nil {
			return err
		}

		sz, unit := humanize.Size(total)

		fmt.Printf("=> Disk Usage: %.2f%s\n", sz, unit)

		toRemove, err := col.SweepUnmarked(ctx, toKeep)
		if err != nil {
			return err
		}

		fmt.Println("\n## Packages Removed")
		for _, p := range toRemove {
			fmt.Println(p)
		}

		total, err = col.DiskUsage(toRemove)
		if err != nil {
			return err
		}

		sz, unit = humanize.Size(total)

		fmt.Printf("=> Disk Usage: %.2f%s\n", sz, unit)

		return nil
	}

	total, err := col.DiskUsage(toKeep)
	if err != nil {
		return err
	}

	sz, unit := humanize.Size(total)

	fmt.Printf("## Packages Kept: %.2f%s\n", sz, unit)
	for _, p := range toKeep {
		fmt.Println(p)
	}

	res, err := col.SweepAndRemove(ctx, toKeep)
	if err != nil {
		return err
	}

	sz, unit = humanize.Size(res.BytesRecovered)

	fmt.Printf("\nSpace Recovered: %.2f%s\n", sz, unit)
	fmt.Printf("  Files Removed: %d\n", res.EntriesRemoved)

	return nil
}

func devF(ctx context.Context, opts struct {
	Pos struct {
		Script string `positional-arg-name:"script"`
	} `positional-args:"yes"`
	DryRun bool `long:"dry-run" description:"explain operations but don't do them"`
	Trace  bool `long:"trace" description:"log in trace mode"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	level := hclog.Debug

	if opts.Trace {
		level = hclog.Trace
	}

	L := hclog.New(&hclog.LoggerOptions{
		Name:  "iris-debug",
		Level: level,
	})

	var cl ops.ProjectLoad
	cl.SetLogger(L)

	name := opts.Pos.Script

	proj, err := cl.Single(ctx, cfg, name)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(fmt.Sprintf("./%s-build", strings.ReplaceAll(name, "/", "-")))
	if err != nil {
		return err
	}

	ienv := &ops.InstallEnv{
		Store:       cfg.Store(),
		BuildDir:    filepath.Join(root, "build"),
		StateDir:    filepath.Join(root, "state"),
		RetainBuild: true,
		StartShell:  true,
	}

	err = proj.Explain(ctx, ienv)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return nil
	}

	err = os.MkdirAll(ienv.BuildDir, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(ienv.Store.Default, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(ienv.StateDir, 0755)
	if err != nil {
		return err
	}

	requested, _, _, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		return err
	}

	path, err := ienv.Store.Locate(requested[0])
	if err != nil {
		return err
	}

	return forceRemoveDir(path)
}

func debugF(ctx context.Context, opts struct {
	Script      string `short:"s" long:"script" description:"output info about a script"`
	TestInstall string `short:"t" long:"test" description:"install a script in a test env"`
	Reuse       bool   `long:"reuse" description:"reuse any packages from the default store in test"`
	TestDir     string `long:"test-dir" description:"use the given directory as the test dir" default:"iris-test"`
	DryRun      bool   `long:"dry-run" description:"explain operations but don't do them"`
	ScanLibs    string `long:"scan-libs" description:"scan a directory and output all linked libs"`
	Shell       bool   `long:"shell" description:"run a shell before and after each install"`
	Trace       bool   `long:"trace" description:"log in trace mode"`
	ShowCar     string `short:"c" long:"car" description:"attempt to discover a car file for a script"`
	ExtractCar  bool   `long:"extract" description:"extract the car as well as inspecting it"`
	Clone       string `long:"clone" description:"clone a package"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	level := hclog.Debug

	if opts.Trace {
		level = hclog.Trace
	}

	L := hclog.New(&hclog.LoggerOptions{
		Name:  "iris-debug",
		Level: level,
	})

	if opts.Script != "" {
		var cl ops.ProjectLoad
		cl.SetLogger(L)

		proj, err := cl.Single(ctx, cfg, opts.Script)
		if err != nil {
			return err
		}

		for _, i := range proj.Install {
			fmt.Println(i.ID(), i.Name(), i.Version())
		}

		return nil
	}

	if opts.TestInstall != "" {
		var cl ops.ProjectLoad
		cl.SetLogger(L)

		name := opts.TestInstall

		if _, err := os.Stat(opts.TestInstall); err == nil {
			path := "./" + filepath.Clean(opts.TestInstall)

			pathExtra, err := filepath.Abs(filepath.Dir(path))
			if err != nil {
				return err
			}

			cfg.Path = pathExtra + ":" + cfg.Path
			name = path
		}

		root := opts.TestDir

		fmt.Printf("Installing packages into: %s\n", root)

		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}

		store := cfg.Store()

		store.Pivot(filepath.Join(root, "install"))

		spew.Dump(store)

		fmt.Printf("Loading for test install: %s\n", name)
		// fmt.Printf("Loading path: %s\n", strings.Join(cfg.LoadPath(), ":"))

		proj, err := cl.Single(ctx, cfg, name)
		if err != nil {
			return err
		}

		ienv := &ops.InstallEnv{
			Store:       store,
			BuildDir:    filepath.Join(root, "build"),
			StateDir:    filepath.Join(root, "state"),
			RetainBuild: true,
			StartShell:  opts.Shell,
		}

		err = proj.Explain(ctx, ienv)
		if err != nil {
			return err
		}

		if opts.DryRun {
			return nil
		}

		err = os.MkdirAll(ienv.BuildDir, 0755)
		if err != nil {
			return err
		}

		err = os.MkdirAll(ienv.Store.Default, 0755)
		if err != nil {
			return err
		}

		err = os.MkdirAll(ienv.StateDir, 0755)
		if err != nil {
			return err
		}

		_, _, _, err = proj.InstallPackages(ctx, ienv)
		return err
	}

	if opts.ScanLibs != "" {
		return filepath.Walk(opts.ScanLibs, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			if info.Mode().Perm()&0111 == 0 {
				return nil
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}

			mf, err := macho.NewFile(f)
			if err != nil {
				return nil
			}

			libs, err := mf.ImportedLibraries()
			if err != nil {
				return err
			}

			fmt.Printf("%s\t%s\n", path, strings.Join(libs, "  "))

			return nil
		})
	}

	if opts.ShowCar != "" {
		var cl ops.ProjectLoad
		cl.SetLogger(L)

		pkgName := opts.ShowCar

		if _, err := os.Stat(opts.ShowCar); err == nil {
			path := "./" + filepath.Clean(opts.TestInstall)

			pathExtra, err := filepath.Abs(filepath.Dir(path))
			if err != nil {
				return err
			}

			cfg.Path = pathExtra + ":" + cfg.Path
			pkgName = path
		}

		fmt.Printf("Loading for test install: %s\n", pkgName)
		// fmt.Printf("Loading path: %s\n", strings.Join(cfg.LoadPath(), ":"))

		proj, err := cl.Single(ctx, cfg, pkgName)
		if err != nil {
			return err
		}

		pkg := proj.Install[0]

		fmt.Printf("Attempting to resolve car for: %s\n", pkg.ID())

		cfg, err := pkg.RepoConfig().Config()
		if err != nil {
			return err
		}

		fmt.Printf("OCI root: %s\n", cfg.OCIRoot)

		target := fmt.Sprintf("%s:%s", cfg.OCIRoot, pkg.ID())

		ref, err := name.ParseReference(target)
		if err != nil {
			return err
		}

		desc, err := remote.Get(ref)
		if err != nil {
			return err
		}

		L.Info("descriptor",
			"type", desc.MediaType,
			"platform", desc.Platform,
			"digest", desc.Digest.String(),
			"urls", desc.URLs,
			"annotations", desc.Annotations,
		)

		man, err := v1.ParseManifest(bytes.NewReader(desc.Manifest))
		if err != nil {
			return err
		}

		var info data.CarInfo

		infoData, ok := man.Annotations["dev.lab47.car.info"]
		if !ok {
			fmt.Printf("missing car info annotation\n")
			return nil
		}

		err = json.Unmarshal([]byte(infoData), &info)
		if err != nil {
			return err
		}

		spew.Dump(info)

		if opts.ExtractCar {
			img, err := desc.Image()
			if err != nil {
				return err
			}

			dir := info.ID

			cInfo, err := ociutil.WriteDir(img, dir)
			if err != nil {
				return err
			}

			if info.ID != cInfo.ID {
				return fmt.Errorf("manifest has different info that car file")
			}
		}

		return nil
	}

	if opts.Clone != "" {
		var pm config.PathMap
		pm.Dir = "./path-map"

		out, err := pm.Map(ctx, &config.PackagePath{
			Type:    "git",
			Name:    opts.Clone,
			Version: "main",
		})
		if err != nil {
			return err
		}

		fmt.Println(out)
		return nil
	}

	return nil

}
