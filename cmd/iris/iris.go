package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"text/tabwriter"

	"github.com/mitchellh/cli"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"lab47.dev/aperture/pkg/config"
	"lab47.dev/aperture/pkg/direnv"
	"lab47.dev/aperture/pkg/gc"
	"lab47.dev/aperture/pkg/humanize"
	"lab47.dev/aperture/pkg/lockfile"
	"lab47.dev/aperture/pkg/ops"
	"lab47.dev/aperture/pkg/profile"
)

func main() {
	c := cli.NewCLI("app", "1.0.0")
	c.Args = os.Args[1:]
	c.Commands = map[string]cli.CommandFactory{
		"install": func() (cli.Command, error) {
			return &install{}, nil
		},
		"shell": func() (cli.Command, error) {
			return &shell{}, nil
		},
		"direnv-dump": func() (cli.Command, error) {
			return &shell{dump: true}, nil
		},
		"inspect-car": func() (cli.Command, error) {
			return &inspectCar{}, nil
		},
		"publish-car": func() (cli.Command, error) {
			return &publishCar{}, nil
		},
		"env": func() (cli.Command, error) {
			return &env{}, nil
		},
		"gc": func() (cli.Command, error) {
			return &gcCmd{}, nil
		},
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}

	os.Exit(exitStatus)
}

type install struct {
	fExplain bool
	fExport  string
	fPublish bool
	fGlobal  bool
}

func (i *install) Help() string {
	return "install"
}

func (i *install) Synopsis() string {
	return "install"
}

func cancelOnSignal(cancel func(), signals ...os.Signal) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, signals...)

	go func() {
		for range c {
			cancel()
		}
	}()
}

func (i *install) Run(args []string) int {
	fs := pflag.NewFlagSet("install", pflag.ExitOnError)

	fs.BoolVarP(&i.fExplain, "explain", "E", false,
		"Explain what will be installed")

	fs.StringVar(&i.fExport, "export", "",
		"write .car files to this directory")

	fs.BoolVar(&i.fPublish, "publish", false,
		"publish the exported .car files to the repo's publish address")

	fs.BoolVarP(&i.fGlobal, "global-profile", "G", false,
		"install into the user's global profile")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

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

	var (
		proj *ops.Project
		cl   ops.ProjectLoad
	)

	profilePath := ".iris-profile"

	if i.fGlobal {
		profilePath = cfg.GlobalProfilePath()
	}

	if fs.NArg() == 0 {
		proj, err = cl.Load(cfg)
	} else {
		proj, err = cl.Single(cfg, fs.Arg(0))
	}

	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	cancelOnSignal(cancel, os.Interrupt, unix.SIGQUIT, unix.SIGTERM)

	if i.fExplain {
		err := proj.Explain(ctx, ienv)
		if err != nil {
			log.Fatal(err)
		}

		return 0
	}

	var showLock bool
	cleanup, err := lockfile.Take(ctx, ".iris-lock", func() {
		if !showLock {
			fmt.Printf("Lock detected, waiting...\n")
			showLock = true
		}
	})
	if err != nil {
		log.Fatal(err)
	}

	defer cleanup()

	requested, toInstall, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		log.Fatal(err)
	}

	exportDir := i.fExport

	if i.fPublish && exportDir == "" {
		dir, err := ioutil.TempDir("", "iris")
		if err != nil {
			log.Fatal(err)
		}

		exportDir = dir

		defer os.RemoveAll(dir)
	}

	if exportDir != "" {
		err := os.MkdirAll(exportDir, 0755)
		if err != nil {
			log.Fatal(err)
		}

		cars, err := proj.Export(ctx, cfg, exportDir)
		if err != nil {
			log.Fatal(err)
		}

		if i.fPublish {
			return publishCars(ctx, cars)
		}

		return 0
	}

	prof, err := profile.OpenProfile(cfg, profilePath)
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

	updates := prof.UpdateEnv(os.Environ())

	for _, u := range updates {
		fmt.Println(u)
	}

	return 0
}

func publishCars(ctx context.Context, cars []*ops.ExportedCar) int {
	var cp ops.CarPublish
	cp.Username = os.Getenv("GITHUB_USER")
	cp.Password = os.Getenv("GITHUB_TOKEN")

	for _, car := range cars {
		fmt.Printf("Publishing %s\n", car.Path)
		err := cp.PublishCar(ctx, car.Path, "ghcr.io/lab47/aperture-packages")
		if err != nil {
			log.Fatal(err)
		}
	}

	return 0
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

type inspectCar struct{}

func (i *inspectCar) Help() string {
	return "inspect the contents of a car file"
}

func (i *inspectCar) Synopsis() string {
	return "inspect the contents of a car file"
}

func (i *inspectCar) Run(args []string) int {
	fs := pflag.NewFlagSet("inspect-car", pflag.ExitOnError)

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	defer f.Close()

	var ci ops.CarInspect

	tw := tabwriter.NewWriter(os.Stdout, 4, 2, 1, ' ', 0)
	defer tw.Flush()

	ci.Show(f, tw)

	return 0
}

type publishCar struct{}

func (i *publishCar) Help() string {
	return "inspect the contents of a car file"
}

func (i *publishCar) Synopsis() string {
	return "inspect the contents of a car file"
}

func (i *publishCar) Run(args []string) int {
	fs := pflag.NewFlagSet("inspect-car", pflag.ExitOnError)

	built := fs.BoolP("built", "B", false, "publish all built packages")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

	var cp ops.CarPublish
	cp.Username = os.Getenv("GITHUB_USER")
	cp.Password = os.Getenv("GITHUB_TOKEN")

	if !*built {
		err = cp.PublishCar(context.Background(), fs.Arg(0), "ghcr.io/lab47/aperture-packages")
		if err != nil {
			log.Fatal(err)
		}
	}

	var ss ops.StoreScan

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	pkgs, err := ss.Scan(cfg, true)
	if err != nil {
		log.Fatal(err)
	}

	for _, pkg := range pkgs {
		fmt.Println(pkg.Package.ID())
	}

	dir, err := ioutil.TempDir("", "iris")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir)

	proj := &ops.Project{}

	for _, pkg := range pkgs {
		proj.Install = append(proj.Install, pkg.Package)
	}

	ctx, cancel := context.WithCancel(context.Background())

	cancelOnSignal(cancel, os.Interrupt, unix.SIGQUIT, unix.SIGTERM)

	cars, err := proj.Export(ctx, cfg, dir)
	if err != nil {
		log.Fatal(err)
	}

	return publishCars(ctx, cars)
}

type env struct{}

func (i *env) Help() string {
	return "provide environment information"
}

func (i *env) Synopsis() string {
	return "provide environment information"
}

func (i *env) Run(args []string) int {
	fs := pflag.NewFlagSet("env", pflag.ExitOnError)

	gp := fs.BoolP("global-profile", "G", false, "output location of global-profile")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	if *gp {
		fmt.Println(cfg.GlobalProfilePath())
	}

	return 0
}

type gcCmd struct{}

func (i *gcCmd) Help() string {
	return "provide environment information"
}

func (i *gcCmd) Synopsis() string {
	return "provide environment information"
}

func (i *gcCmd) Run(args []string) int {
	fs := pflag.NewFlagSet("gc", pflag.ExitOnError)

	gp := fs.BoolP("dry-run", "T", false, "output packages that would be removed")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	col, err := gc.NewCollector(cfg.DataDir)

	if *gp {
		toKeep, err := col.Mark()
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("## Packages Kept")
		for _, p := range toKeep {
			fmt.Println(p)
		}

		total, err := col.DiskUsage(toKeep)
		if err != nil {
			log.Fatal(err)
		}

		sz, unit := humanize.Size(total)

		fmt.Printf("=> Disk Usage: %.2f%s\n", sz, unit)

		toRemove, err := col.Sweep()
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("\n## Packages Removed")
		for _, p := range toRemove {
			fmt.Println(p)
		}

		total, err = col.DiskUsage(toRemove)
		if err != nil {
			log.Fatal(err)
		}

		sz, unit = humanize.Size(total)

		fmt.Printf("=> Disk Usage: %.2f%s\n", sz, unit)
	}

	return 0
}
