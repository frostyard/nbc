package main

import (
	"fmt"
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/cmd"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	app := clix.App{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	}
	if err := app.Run(cmd.RootCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
