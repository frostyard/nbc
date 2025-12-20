package main

import (
	"fmt"
	"os"

	"github.com/frostyard/nbc/cmd"
)

// version is set by ldflags during build
var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}