package cmd

import (
	"fmt"
	"strings"

	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"stat", "info"},
	Short:   "Show current system status",
	Long: `Display the current nbc system status including:
  - Installed container image reference and digest
  - Boot device and active root partition (slot A or B)
  - Root filesystem mount mode (read-only or read-write)
  - Bootloader type and filesystem type
  - Staged update status (if any downloaded update is ready)

With -v (verbose), also displays:
  - Installation date and kernel arguments
  - Remote update availability check

With --json flag, outputs structured JSON including update check results.

Example:
  nbc status
  nbc status -v     # Verbose output with update check
  nbc status --json # Machine-readable JSON output`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	jsonOutput := viper.GetBool("json")

	// Read system configuration
	config, err := pkg.ReadSystemConfig()
	if err != nil {
		if jsonOutput {
			return outputJSONError("failed to read system config", err)
		}
		return fmt.Errorf("failed to read system config: %w\n\nIs this system installed with nbc?", err)
	}

	// Get active root partition
	activeRoot, err := pkg.GetActiveRootPartition()
	if err != nil && verbose && !jsonOutput {
		fmt.Printf("Warning: could not determine active root partition: %v\n", err)
	}

	// Determine which root slot is active (root1 or root2)
	var activeSlot string
	if activeRoot != "" {
		// Try to detect the partition scheme to determine slot
		device := config.Device
		if device != "" {
			scheme, schemeErr := pkg.DetectExistingPartitionScheme(device)
			if schemeErr == nil {
				if strings.HasSuffix(activeRoot, strings.TrimPrefix(scheme.Root1Partition, "/dev/")) ||
					activeRoot == scheme.Root1Partition {
					activeSlot = "A (root1)"
				} else if strings.HasSuffix(activeRoot, strings.TrimPrefix(scheme.Root2Partition, "/dev/")) ||
					activeRoot == scheme.Root2Partition {
					activeSlot = "B (root2)"
				}
			}
		}
	}

	// Check if root is mounted read-only or read-write
	rootMountMode := pkg.IsRootMountedReadOnly()

	if jsonOutput {
		output := types.StatusOutput{
			Image:          config.ImageRef,
			Digest:         config.ImageDigest,
			Device:         config.Device,
			ActiveRoot:     activeRoot,
			ActiveSlot:     activeSlot,
			RootMountMode:  rootMountMode,
			BootloaderType: config.BootloaderType,
			FilesystemType: config.FilesystemType,
			InstallDate:    config.InstallDate,
			KernelArgs:     config.KernelArgs,
		}

		if output.FilesystemType == "" {
			output.FilesystemType = "ext4"
		}

		// Check for updates if verbose or always for JSON
		if config.ImageRef != "" {
			updateCheck := &types.UpdateCheck{}
			remoteDigest, err := pkg.GetRemoteImageDigest(config.ImageRef)
			if err != nil {
				updateCheck.Error = err.Error()
			} else {
				updateCheck.RemoteDigest = remoteDigest
				updateCheck.CurrentDigest = config.ImageDigest
				if config.ImageDigest == "" {
					updateCheck.Available = false // unknown
				} else {
					updateCheck.Available = config.ImageDigest != remoteDigest
				}
			}
			output.UpdateCheck = updateCheck
		}

		// Check for staged update
		updateCache := pkg.NewStagedUpdateCache()
		if stagedMetadata, err := updateCache.GetSingle(); err == nil && stagedMetadata != nil {
			output.StagedUpdate = &types.StagedUpdate{
				ImageRef:    stagedMetadata.ImageRef,
				ImageDigest: stagedMetadata.ImageDigest,
				SizeBytes:   stagedMetadata.SizeBytes,
				Ready:       stagedMetadata.ImageDigest != config.ImageDigest,
			}
		}

		// Check for pending reboot
		if rebootInfo, err := pkg.ReadRebootRequiredMarker(); err == nil && rebootInfo != nil {
			output.RebootPending = &types.RebootPendingInfo{
				PendingImageRef:    rebootInfo.PendingImageRef,
				PendingImageDigest: rebootInfo.PendingImageDigest,
				UpdateTime:         rebootInfo.UpdateTime,
				TargetPartition:    rebootInfo.TargetPartition,
			}
		}

		return outputJSON(output)
	}

	// Print status
	fmt.Println("nbc System Status")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	fmt.Printf("Image:       %s\n", config.ImageRef)
	if config.ImageDigest != "" {
		// Show shortened digest for cleaner output, full in verbose
		if verbose {
			fmt.Printf("Digest:      %s\n", config.ImageDigest)
		} else {
			digest := config.ImageDigest
			if len(digest) > 19 {
				digest = digest[:19] + "..."
			}
			fmt.Printf("Digest:      %s\n", digest)
		}
	} else {
		fmt.Printf("Digest:      (not recorded)\n")
	}

	fmt.Println()
	fmt.Printf("Device:      %s\n", config.Device)
	if activeRoot != "" {
		fmt.Printf("Active Root: %s", activeRoot)
		if activeSlot != "" {
			fmt.Printf(" [Slot %s]", activeSlot)
		}
		fmt.Println()
	}
	if rootMountMode != "" {
		mountModeDesc := "read-write"
		if rootMountMode == "ro" {
			mountModeDesc = "read-only"
		}
		fmt.Printf("Root Mount:  %s\n", mountModeDesc)
	}
	fmt.Printf("Bootloader:  %s\n", config.BootloaderType)
	if config.FilesystemType != "" {
		fmt.Printf("Filesystem:  %s\n", config.FilesystemType)
	} else {
		fmt.Printf("Filesystem:  ext4 (default)\n")
	}

	// Check for pending reboot and show warning
	if rebootInfo, err := pkg.ReadRebootRequiredMarker(); err == nil && rebootInfo != nil {
		fmt.Println()
		fmt.Println("** REBOOT REQUIRED **")
		fmt.Println("The above image will become active after reboot.")
		fmt.Printf("Updated:     %s\n", rebootInfo.UpdateTime)
		fmt.Printf("Target:      %s\n", rebootInfo.TargetPartition)
	}

	if verbose {
		fmt.Println()
		fmt.Printf("Installed:   %s\n", config.InstallDate)
		if len(config.KernelArgs) > 0 {
			fmt.Printf("Kernel Args: %s\n", strings.Join(config.KernelArgs, " "))
		}
	}

	// Check for available updates if verbose
	if verbose && config.ImageRef != "" {
		fmt.Println()
		fmt.Println("Checking for updates...")
		remoteDigest, err := pkg.GetRemoteImageDigest(config.ImageRef)
		if err != nil {
			fmt.Printf("  Could not check for updates: %v\n", err)
		} else if config.ImageDigest == "" {
			fmt.Printf("  Remote digest: %s\n", remoteDigest)
			fmt.Println("  Update status: unknown (no local digest recorded)")
		} else if config.ImageDigest == remoteDigest {
			fmt.Println("  ✓ System is up-to-date")
		} else {
			fmt.Println("  ⚠ Update available!")
			fmt.Printf("    Installed: %s\n", config.ImageDigest)
			fmt.Printf("    Available: %s\n", remoteDigest)
		}
	}

	// Check for staged update
	updateCache := pkg.NewStagedUpdateCache()
	if stagedMetadata, err := updateCache.GetSingle(); err == nil && stagedMetadata != nil {
		fmt.Println()
		if stagedMetadata.ImageDigest != config.ImageDigest {
			fmt.Println("📦 Update staged (ready to apply):")
			fmt.Printf("   Image:  %s\n", stagedMetadata.ImageRef)
			fmt.Printf("   Digest: %s\n", stagedMetadata.ImageDigest)
			fmt.Printf("   Size:   %.2f MB\n", float64(stagedMetadata.SizeBytes)/(1024*1024))
			fmt.Println("   Run 'nbc update --local-image' to apply.")
		} else {
			fmt.Println("📦 Staged update matches installed version")
			fmt.Println("   Run 'nbc cache clear --update' to remove.")
		}
	}

	return nil
}
