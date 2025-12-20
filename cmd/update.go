package cmd

import (
	"fmt"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	updateImage      string
	updateDevice     string
	updateSkipPull   bool
	updateCheckOnly  bool
	updateKernelArgs []string
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update system to a new container image using A/B partitions",
	Long: `Update the system by installing a new container image to the inactive root partition.

This command performs an A/B system update:
  1. Auto-detects the boot device (or use --device to override)
  2. Detects which root partition is currently active
  3. Checks if an update is available (compares image digests)
  4. Pulls the new container image (unless --skip-pull is specified)
  5. Extracts the new filesystem to the inactive root partition
  6. Updates the bootloader to boot from the new partition
  7. Keeps the old partition as a rollback option

Use --check to only check if an update is available without installing.

After update, reboot to activate the new system. The previous system remains
available in the boot menu for rollback if needed.

Example:
  nbc update
  nbc update --check              # Just check if update available
  nbc update --image quay.io/example/myimage:v2.0
  nbc update --skip-pull
  nbc update --device /dev/sda    # Override auto-detection
  nbc update --force              # Reinstall even if up-to-date`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringVarP(&updateImage, "image", "i", "", "Container image reference (uses saved config if not specified)")
	updateCmd.Flags().StringVarP(&updateDevice, "device", "d", "", "Target disk device (auto-detected if not specified)")
	updateCmd.Flags().BoolVar(&updateSkipPull, "skip-pull", false, "Skip pulling the image (use already pulled image)")
	updateCmd.Flags().BoolVarP(&updateCheckOnly, "check", "c", false, "Only check if an update is available (don't install)")
	updateCmd.Flags().StringArrayVarP(&updateKernelArgs, "karg", "k", []string{}, "Kernel argument to pass (can be specified multiple times)")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")
	force := viper.GetBool("force")

	var device string
	var err error

	// Resolve device path - auto-detect if not specified
	if updateDevice != "" {
		device, err = pkg.GetDiskByPath(updateDevice)
		if err != nil {
			return fmt.Errorf("invalid device: %w", err)
		}
		if verbose {
			fmt.Printf("Using specified device: %s\n", device)
		}
	} else {
		// Auto-detect boot device
		device, err = pkg.GetCurrentBootDeviceInfo(verbose)
		if err != nil {
			return fmt.Errorf("failed to auto-detect boot device: %w (use --device to specify manually)", err)
		}
		if !verbose {
			fmt.Printf("Auto-detected boot device: %s\n", device)
		}
	}

	// If image not specified, try to load from system config
	imageRef := updateImage
	if imageRef == "" {
		config, err := pkg.ReadSystemConfig()
		if err != nil {
			return fmt.Errorf("no image specified and failed to read system config: %w", err)
		}
		imageRef = config.ImageRef
		fmt.Printf("Using image from system config: %s\n", imageRef)
	}

	// Create updater
	updater := pkg.NewSystemUpdater(device, imageRef)
	updater.SetVerbose(verbose)
	updater.SetDryRun(dryRun)
	updater.SetForce(force)

	// If --check flag, only check if update is needed
	if updateCheckOnly {
		needed, digest, err := updater.IsUpdateNeeded()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		if needed {
			fmt.Println()
			fmt.Printf("Update available: %s\n", digest)
			fmt.Println("Run 'nbc update' to install the update.")
			// Exit with code 0 (update available)
			return nil
		}
		// System is up-to-date
		return nil
	}

	// Add kernel arguments
	for _, arg := range updateKernelArgs {
		updater.AddKernelArg(arg)
	}

	// Run update
	if err := updater.PerformUpdate(updateSkipPull); err != nil {
		return err
	}

	if !dryRun {
		fmt.Println()
		fmt.Println("=================================================================")
		fmt.Println("System update complete!")
		fmt.Println("Reboot your system to activate the new version.")
		fmt.Println("The previous version is available in the boot menu for rollback.")
		fmt.Println("=================================================================")
	}

	return nil
}
