package main

import (
	"fmt"
	"os"

	"github.com/frostyard/nbc/cmd"
)

// version is set by ldflags during build
var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	cmd.SetVersion(version)
	cmd.SetCommit(commit)
	cmd.SetDate(date)
	cmd.SetBuiltBy(builtBy)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
