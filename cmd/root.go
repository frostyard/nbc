package cmd

import (
	"github.com/spf13/cobra"
)

// RootCmd is the root command for nbc. Exported for main.go to pass to clix.App.Run().
var RootCmd = &cobra.Command{
	Use:   "nbc",
	Short: "A bootc container installer for physical disks",
	Long: `nbc is a tool for installing bootc compatible containers to physical disks.
It automates the process of preparing disks and deploying bootable container images.`,
}
