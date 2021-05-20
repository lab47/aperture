package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mitchellh/cli"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"
	"lab47.dev/aperture/pkg/direnv"
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
		"xinstall": func() (cli.Command, error) {
			return &xinstall{}, nil
		},
	}

	exitStatus, err := c.Run()
	if err != nil {
		log.Println(err)
	}

	os.Exit(exitStatus)
}

type xinstall struct {
	fExplain bool
}

func (i *xinstall) Help() string {
	return "install"
}

func (i *xinstall) Synopsis() string {
	return "install"
}

func (i *xinstall) Run(args []string) int {
	fs := pflag.NewFlagSet("install", pflag.ExitOnError)

	fs.BoolVarP(&i.fExplain, "explain", "E", false,
		"Explain what will be installed")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

	cr := &ops.ConfigRepo{
		Path: "./is-packages",
	}

	storeDir := "/opt/iris/store"

	err = os.MkdirAll(storeDir, 0755)
	if err != nil {
		log.Fatal(err)
	}

	var sl ops.ScriptLoad
	sl.StoreDir = storeDir

	data, err := sl.Load("protobuf", ops.WithConfigRepo(cr))
	if err != nil {
		log.Fatal(err)
	}

	var pci ops.PackageCalcInstall
	pci.StoreDir = storeDir

	toInstall, err := pci.Calculate(data)
	if err != nil {
		log.Fatal(err)
	}

	if i.fExplain {
		tw := tabwriter.NewWriter(os.Stdout, 2, 2, 1, ' ', 0)
		defer tw.Flush()

		fmt.Fprintf(tw, "NAME\tVERSION\tSTATUS\tDEPENDENCIES\n")

		for _, p := range toInstall.InstallOrder {
			flag := " "
			if toInstall.Installed[p] {
				flag = "I"
			}

			var shortDeps []string

			for _, dep := range toInstall.Dependencies[p] {
				scr := toInstall.Scripts[dep]

				if scr == nil || scr.Name() == "" {
					continue
				}

				shortDeps = append(shortDeps, scr.Name())
			}

			deps := strings.Join(shortDeps, " ")

			script := toInstall.Scripts[p]

			if script == nil || script.Name() == "" {
				continue
			}

			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", script.Name(), script.Version(), flag, deps)
		}

		return 0
	}

	buildRoot := filepath.Join(storeDir, "_build")

	err = os.MkdirAll(buildRoot, 0755)
	if err != nil {
		log.Fatal(err)
	}

	ienv := &ops.InstallEnv{
		BuildDir: buildRoot,
		StoreDir: storeDir,
	}

	err = os.MkdirAll(ienv.StoreDir, 0755)
	if err != nil {
		log.Fatal(err)
	}

	var pkgInst ops.PackagesInstall

	err = pkgInst.Install(context.TODO(), ienv, toInstall)
	if err != nil {
		log.Fatal(err)
	}

	prof, err := profile.OpenProfile("iris-profile")
	if err != nil {
		log.Fatal(err)
	}

	err = prof.Link(data.ID(), toInstall.InstallDirs[data.ID()])
	if err != nil {
		log.Fatal(err)
	}

	updates := prof.UpdateEnv(os.Environ())

	for _, u := range updates {
		fmt.Println(u)
	}

	return 0
}

type install struct {
	fExplain bool
}

func (i *install) Help() string {
	return "install"
}

func (i *install) Synopsis() string {
	return "install"
}

func (i *install) Run(args []string) int {
	fs := pflag.NewFlagSet("install", pflag.ExitOnError)

	fs.BoolVarP(&i.fExplain, "explain", "E", false,
		"Explain what will be installed")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return 1
	}

	storeDir := "/opt/iris/store"
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

	proj, err := cl.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	if i.fExplain {
		err := proj.Explain(ctx, ienv)
		if err != nil {
			log.Fatal(err)
		}

		return 0
	}

	requested, toInstall, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		log.Fatal(err)
	}

	prof, err := profile.OpenProfile(".iris-profile")
	if err != nil {
		log.Fatal(err)
	}

	for _, id := range requested {
		err = prof.Link(id, toInstall.InstallDirs[id])
		if err != nil {
			log.Fatal(err)
		}
	}

	updates := prof.UpdateEnv(os.Environ())

	for _, u := range updates {
		fmt.Println(u)
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
	storeDir := "/opt/iris/store"
	buildRoot := filepath.Join(storeDir, "_build")

	err := os.MkdirAll(buildRoot, 0755)
	if err != nil {
		log.Fatal(err)
	}

	ienv := &ops.InstallEnv{
		StoreDir: storeDir,
		BuildDir: buildRoot,
	}

	var cl ops.ProjectLoad

	proj, err := cl.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	requested, toInstall, err := proj.InstallPackages(ctx, ienv)
	if err != nil {
		log.Fatal(err)
	}

	prof, err := profile.OpenProfile(".iris-profile")
	if err != nil {
		log.Fatal(err)
	}

	for _, id := range requested {
		err = prof.Link(id, toInstall.InstallDirs[id])
		if err != nil {
			log.Fatal(err)
		}
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

/*
func hbmain() {
	cellar := homebrew.DefaultCellar()
	err := os.MkdirAll(cellar, 0755)
	if err != nil {
		log.Fatal(err)
	}

	r, err := homebrew.NewResolver(cellar, "./gen/packages")
	if err != nil {
		log.Fatal(err)
	}

	var d homebrew.Downloader

	res, err := r.Resolve("zsh-autosuggestions")
	if err != nil {
		log.Fatal(err)
	}

	res.Explain(os.Stdout, &d)

	urls, err := res.ComputeURLs()
	if err != nil {
		log.Fatal(err)
	}

	info, err := d.Stage("./tmp-data", urls)
	if err != nil {
		log.Fatal(err)
	}

	u, err := homebrew.NewUnpacker(cellar)
	if err != nil {
		log.Fatal(err)
	}

	for i, pkg := range res.ToInstall {
		fmt.Println(pkg.Name)

		url := urls[i]

		err := u.Unpack(pkg, url.Binary, info[url.URL])
		if err != nil {
			log.Fatal(err)
		}
	}

	// spew.Dump(info)
}
*/
