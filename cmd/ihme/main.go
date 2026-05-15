package main

import (
	"fmt"
	"os"

	root "github.com/lroolle/ihme-cli/cmd/root"
)

var version = "dev"

func main() {
	if err := root.NewCmdRoot(version).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
