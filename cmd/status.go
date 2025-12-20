package cmd

import (
	"fmt"
	"strings"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current system status",
	Long: `Display the current nbc system status including:
  - Installed container image reference
  - Image digest (SHA256)
  - Currently active root partition
  - Boot device

Example:
  nbc status
  nbc status -v  # Verbose output with more details`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")

	// Read system configuration
	config, err := pkg.ReadSystemConfig()
	if err != nil {
		return fmt.Errorf("failed to read system config: %w\n\nIs this system installed with nbc?", err)
	}

	// Get active root partition
	activeRoot, err := pkg.GetActiveRootPartition()
	if err != nil && verbose {
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
	fmt.Printf("Bootloader:  %s\n", config.BootloaderType)
	if config.FilesystemType != "" {
		fmt.Printf("Filesystem:  %s\n", config.FilesystemType)
	} else {
		fmt.Printf("Filesystem:  ext4 (default)\n")
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

	return nil
}
