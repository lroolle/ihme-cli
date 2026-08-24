package main

import (
	"fmt"
	"os"

	root "github.com/lroolle/ihme-cli/cmd/root"
	"github.com/lroolle/ihme-cli/internal/cmdutil"
)

var version = "dev"

func main() {
	if err := root.NewCmdRoot(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, cmdutil.Explain(err))
		os.Exit(cmdutil.ExitCode(err))
	}
}
