package main

import (
	"log"
	"os"

	"lab47.dev/aperture/pkg/rpath"
)

func main() {
	err := rpath.Shrink(os.Args[1], nil)
	if err != nil {
		log.Fatal(err)
	}
}
