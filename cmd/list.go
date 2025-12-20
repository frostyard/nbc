package cmd

import (
	"fmt"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available disks",
	Long:  `List all available physical disks on the system.`,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")

	disks, err := pkg.ListDisks()
	if err != nil {
		return fmt.Errorf("failed to list disks: %w", err)
	}

	if len(disks) == 0 {
		fmt.Println("No disks found.")
		return nil
	}

	fmt.Println("Available disks:")
	fmt.Println()

	for _, disk := range disks {
		fmt.Printf("Device: %s\n", disk.Device)
		fmt.Printf("  Size:      %s (%d bytes)\n", pkg.FormatSize(disk.Size), disk.Size)
		if disk.Model != "" {
			fmt.Printf("  Model:     %s\n", disk.Model)
		}
		fmt.Printf("  Removable: %v\n", disk.IsRemovable)

		if len(disk.Partitions) > 0 {
			fmt.Printf("  Partitions:\n")
			for _, part := range disk.Partitions {
				fmt.Printf("    - %s (%s)", part.Device, pkg.FormatSize(part.Size))
				if part.MountPoint != "" {
					fmt.Printf(" mounted at %s", part.MountPoint)
				}
				if part.FileSystem != "" && verbose {
					fmt.Printf(" [%s]", part.FileSystem)
				}
				fmt.Println()
			}
		} else {
			fmt.Printf("  Partitions: none\n")
		}
		fmt.Println()
	}

	return nil
}
