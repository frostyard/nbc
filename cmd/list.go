package cmd

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "disks"},
	Short:   "List available disks",
	Long:    `List all available physical disks on the system.`,
	RunE:    runList,
}

func init() {
	RootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	disks, err := pkg.ListDisks()
	if err != nil {
		if clix.JSONOutput {
			return clix.OutputJSONError("failed to list disks", err)
		}
		return fmt.Errorf("failed to list disks: %w", err)
	}

	if clix.JSONOutput {
		output := types.ListOutput{
			Disks: make([]types.DiskOutput, 0, len(disks)),
		}
		for _, disk := range disks {
			diskOut := types.DiskOutput{
				Device:      disk.Device,
				Size:        disk.Size,
				SizeHuman:   pkg.FormatSize(disk.Size),
				Model:       disk.Model,
				IsRemovable: disk.IsRemovable,
				Partitions:  make([]types.PartitionOutput, 0, len(disk.Partitions)),
			}
			for _, part := range disk.Partitions {
				diskOut.Partitions = append(diskOut.Partitions, types.PartitionOutput{
					Device:     part.Device,
					Size:       part.Size,
					SizeHuman:  pkg.FormatSize(part.Size),
					MountPoint: part.MountPoint,
					FileSystem: part.FileSystem,
				})
			}
			output.Disks = append(output.Disks, diskOut)
		}
		clix.OutputJSON(output)
		return nil
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
				if part.FileSystem != "" && clix.Verbose {
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
