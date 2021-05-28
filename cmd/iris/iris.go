package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"text/tabwriter"

	"github.com/mitchellh/cli"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"lab47.dev/aperture/pkg/cmd"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/direnv"
	"lab47.dev/aperture/pkg/gc"
	"lab47.dev/aperture/pkg/humanize"
	"lab47.dev/aperture/pkg/lockfile"
	"lab47.dev/aperture/pkg/ops"
	"lab47.dev/aperture/pkg/profile"
)

func main() {
	c := cli.NewCLI("iris", "0.1.0")
	c.Args = os.Args[1:]
	c.Commands = map[string]cli.CommandFactory{
		"install": func() (cli.Command, error) {
			return cmd.New(
				"install",
				"Install specified package or from package file",
				installF,
			), nil
		},
		"shell": func() (cli.Command, error) {
			return &shell{}, nil
		},
		"direnv-dump": func() (cli.Command, error) {
			return &shell{dump: true}, nil
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
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}

	os.Exit(exitStatus)
}

func installF(ctx context.Context, opts struct {
	Explain bool   `short:"E" long:"explain" description:"explain what will be installed"`
	Export  string `long:"export" description:"write .car files to the given directory"`
	Publish bool   `long:"publish" description:"publish exported car files to repo"`
	Global  bool   `short:"G" long:"global" description:"install into the user's global profile"`

	Pos struct {
		Package string `positional-arg-name:"name"`
	} `positional-args:"yes"`
}) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	storeDir := cfg.StorePath()
	buildRoot := filepath.Join(storeDir, "_build")

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		return err
	}

	ienv := &ops.InstallEnv{
		StoreDir: storeDir,
		BuildDir: buildRoot,
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
		proj, err = cl.Single(cfg, opts.Pos.Package)
	} else {
		proj, err = cl.Load(cfg)
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

	requested, toInstall, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		log.Fatal(err)
	}

	exportDir := opts.Export

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

		cars, err := proj.Export(ctx, cfg, exportDir)
		if err != nil {
			return err
		}

		if opts.Publish {
			return publishCars(ctx, cars)
		}

		return nil
	}

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

	err = prof.Commit()
	if err != nil {
		return err
	}

	updates := prof.UpdateEnv(os.Environ())

	for _, u := range updates {
		fmt.Println(u)
	}

	return nil
}

func publishCars(ctx context.Context, cars []*ops.ExportedCar) error {
	var cp ops.CarPublish
	cp.Username = os.Getenv("GITHUB_USER")
	cp.Password = os.Getenv("GITHUB_TOKEN")

	for _, car := range cars {
		fmt.Printf("Publishing %s\n", car.Path)
		err := cp.PublishCar(ctx, car.Path, "ghcr.io/lab47/aperture-packages")
		if err != nil {
			return err
		}
	}

	return nil
}

type shell struct {
	dump bool
}

func (i *shell) Help() string {
	return "shell"
}

func (i *shell) Synopsis() string {
	return "shell"
}

func (i *shell) Run(args []string) int {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	storeDir := cfg.StorePath()
	buildRoot := filepath.Join(storeDir, "_build")

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		log.Fatal(err)
	}

	ienv := &ops.InstallEnv{
		StoreDir: storeDir,
		BuildDir: buildRoot,
	}

	var cl ops.ProjectLoad

	proj, err := cl.Load(cfg)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	requested, toInstall, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		log.Fatal(err)
	}

	prof, err := profile.OpenProfile(cfg, ".iris-profile")
	if err != nil {
		log.Fatal(err)
	}

	for _, id := range requested {
		err = prof.Link(id, toInstall.InstallDirs[id])
		if err != nil {
			log.Fatal(err)
		}
	}

	err = prof.Commit()
	if err != nil {
		log.Fatal(err)
	}

	if i.dump {
		var w io.Writer

		path := os.Getenv("DIRENV_DUMP_FILE_PATH")

		if path == "" {
			w = os.Stdout
		} else {
			f, err := os.Create(path)
			if err != nil {
				log.Fatal(err)
			}

			defer f.Close()

			w = f
		}

		fmt.Fprintln(w, direnv.Dump(prof.EnvMap(os.Environ())))
		return 0
	}

	env := prof.ComputeEnv(os.Environ())

	path, err := exec.LookPath(args[0])
	if err != nil {
		log.Fatal(err)
	}

	err = unix.Exec(path, args, env)
	log.Fatal(err)

	return 0
}

func inspectCarF(ctx context.Context, opts struct {
	Args struct {
		File string `positional-arg-name:"file"`
	} `positional-args:"yes"`
}) error {
	f, err := os.Open(opts.Args.File)
	if err != nil {
		log.Fatal(err)
	}

	defer f.Close()

	var ci ops.CarInspect

	tw := tabwriter.NewWriter(os.Stdout, 4, 2, 1, ' ', 0)
	defer tw.Flush()

	ci.Show(f, tw)

	return nil
}

func publishCarF(ctx context.Context, opts struct {
	Built bool `short:"B" long:"built" description:"publish all built packages"`
}) error {
	fs := pflag.NewFlagSet("inspect-car", pflag.ExitOnError)

	var cp ops.CarPublish
	cp.Username = os.Getenv("GITHUB_USER")
	cp.Password = os.Getenv("GITHUB_TOKEN")

	if !opts.Built {
		err := cp.PublishCar(context.Background(), fs.Arg(0), "ghcr.io/lab47/aperture-packages")
		if err != nil {
			return err
		}
	}

	var ss ops.StoreScan

	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	pkgs, err := ss.Scan(cfg, true)
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

	return publishCars(ctx, cars)
}

func envF(ctx context.Context, opts struct {
	Global bool `short:"G" long:"global-profile" description:"output location of global profile"`
}) int {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	if opts.Global {
		fmt.Println(cfg.GlobalProfilePath())
	}

	return 0
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
		toKeep, err = col.MarkMinimal(cfg)
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
