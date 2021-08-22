# Aperture - Software Management Framework

Aperture is somewhere between nix, guix, and homebrew. It uses a source-tracked
approach to installing pre-built software. This means that the exact instructions
used to compiled a package are used as it's identifier. Ergo, to find a pre-built
version of a package, it's identifier is computed by inspecting the instructions.

This means that there is no definitive version of any package, only the current
versions that match the source. This allows, for instance, for many versions
of a library to be installed and not conflict.

## Terminology

- *Aperture*: The overarching project name
- *iris*: The command line tool used to manage packages
- *package*: A set of files that are created by instructions in a .xcr file
- *car*: A pre-built file tree that corresponds to a package
- *repository*: A set of .xcr files and configuration on where car files can be downloaded
- *name*: The name of a package, used to identify the package within a repository
- *id*: Identifier for a package, which includes the hash of the instructions, the name, and the version
- *profile*: For a package to be usable, the files for the package are symlinked into a profile directory. This profile directory can then access all the packages that have been installed into the profile.
- *global packages*: Packages installed onto a system and then link to a global profile in the users home directory


## Usage

### Installing

For now, you must have `go` installed. Then you can simply run `go install github.com/lab47/aperture/cmd/iris@latest`

### Searching Packages

By default, https://github.com/lab47/aperture-packages is used as the package repository.

Packages can be discovered using `iris search`, for instance `iris search awscli`.

### Install Global Packages

Global packages are ones that are available in all of a users sessions. The files are installed into the
computers store and then link to a global profile in the users home directory.

`iris add <name>`. For example, to add neovim: `iris add neovim`.

### Project Packages

Rather than using global packages, users can use project settings. These come in the form of a
file that contains the list of packages to install, the file is `project.xcr`.

A `project.xcr` file uses the same programming language as packages, allowing the user
the ability to call functions and compose new packages.

For instance, here is an example of a `project.xcr` file that installs a set of `go` tools:

```
# This loads the go package from the repository. The go package contains a set of
# exported functions that can be called, such as build_module.
import go

install(
  "protobuf", # as a string, the installer will lookup a package of this name
  go, # as a local variable that contains a script

  # These lines generate a package for each call to build_module that
  # will be installed. See https://github.com/lab47/aperture-packages/blob/main/packages/go/go.export.xcr#L3-L27
  # for the definition of build_module.
  go.build_module("gotest.tools/gotestsum", "v1.6.4"),
  go.build_module("github.com/golang/protobuf/protoc-gen-go", "78b1f09"),
  go.build_module("github.com/mitchellh/protoc-gen-go-json", "069933b8c"),
  go.build_module("github.com/hashicorp/go-changelog/cmd/changelog-build", "56335215"),
  go.build_module("golang.org/x/tools/cmd/stringer", "35839b70"),
  go.build_module(
    path: "github.com/vektra/mockery/cmd/mockery",
    version: "v1.1.2",
    buildFlags: ["-ldflags=-s -w -X github.com/vektra/mockery/mockery.SemVer=1.1.2"],
  )
)
```
