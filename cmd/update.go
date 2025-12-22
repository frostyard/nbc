package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// UpdateCheckOutput represents the JSON output structure for the update --check command
type UpdateCheckOutput struct {
	UpdateNeeded  bool   `json:"update_needed"`
	Image         string `json:"image"`
	Device        string `json:"device"`
	CurrentDigest string `json:"current_digest,omitempty"`
	NewDigest     string `json:"new_digest,omitempty"`
	Message       string `json:"message,omitempty"`
}

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

With --json flag, outputs streaming JSON Lines for progress updates.

Example:
  nbc update
  nbc update --check              # Just check if update available
  nbc update --image quay.io/example/myimage:v2.0
  nbc update --skip-pull
  nbc update --device /dev/sda    # Override auto-detection
  nbc update --force              # Reinstall even if up-to-date
  nbc update --json               # Machine-readable streaming output`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().StringVarP(&updateImage, "image", "i", "", "Container image reference (uses saved config if not specified)")
	updateCmd.Flags().StringVarP(&updateDevice, "device", "d", "", "Target disk device (auto-detected if not specified)")
	updateCmd.Flags().BoolVar(&updateSkipPull, "skip-pull", false, "Skip pulling the image (use already pulled image)")
	updateCmd.Flags().BoolVarP(&updateCheckOnly, "check", "c", false, "Only check if an update is available (don't install)")
	updateCmd.Flags().StringArrayVarP(&updateKernelArgs, "karg", "k", []string{}, "Kernel argument to pass (can be specified multiple times)")
	updateCmd.Flags().BoolP("force", "f", false, "Force reinstall even if system is up-to-date")
	_ = viper.BindPFlag("force", updateCmd.Flags().Lookup("force"))
}

func runUpdate(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")
	force := viper.GetBool("force")
	jsonOutput := viper.GetBool("json")

	// Create progress reporter for early error output
	progress := pkg.NewProgressReporter(jsonOutput, 7)

	var device string
	var err error

	// Resolve device path - auto-detect if not specified
	if updateDevice != "" {
		device, err = pkg.GetDiskByPath(updateDevice)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Invalid device")
			}
			return fmt.Errorf("invalid device: %w", err)
		}
		if verbose && !jsonOutput {
			fmt.Printf("Using specified device: %s\n", device)
		}
	} else {
		// Auto-detect boot device
		device, err = pkg.GetCurrentBootDeviceInfo(verbose && !jsonOutput)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to auto-detect boot device")
			}
			return fmt.Errorf("failed to auto-detect boot device: %w (use --device to specify manually)", err)
		}
		if !verbose && !jsonOutput {
			fmt.Printf("Auto-detected boot device: %s\n", device)
		}
	}

	// If image not specified, try to load from system config
	imageRef := updateImage
	if imageRef == "" {
		config, err := pkg.ReadSystemConfig()
		if err != nil {
			if jsonOutput {
				progress.Error(err, "No image specified and failed to read system config")
			}
			return fmt.Errorf("no image specified and failed to read system config: %w", err)
		}
		imageRef = config.ImageRef
		if !jsonOutput {
			fmt.Printf("Using image from system config: %s\n", imageRef)
		}
	}

	// Create updater
	updater := pkg.NewSystemUpdater(device, imageRef)
	updater.SetVerbose(verbose)
	updater.SetDryRun(dryRun)
	updater.SetForce(force)
	updater.SetJSONOutput(jsonOutput)

	// If --check flag, only check if update is needed
	if updateCheckOnly {
		needed, digest, err := updater.IsUpdateNeeded()
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to check for updates")
			}
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if jsonOutput {
			// Get current digest for JSON output
			config, _ := pkg.ReadSystemConfig()
			currentDigest := ""
			if config != nil {
				currentDigest = config.ImageDigest
			}
			output := UpdateCheckOutput{
				UpdateNeeded:  needed,
				Image:         imageRef,
				Device:        device,
				CurrentDigest: currentDigest,
				NewDigest:     digest,
			}
			if needed {
				output.Message = "Update available"
			} else {
				output.Message = "System is up-to-date"
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(output)
		}

		if needed {
			fmt.Println()
			fmt.Printf("Update available: %s\n", digest)
			fmt.Println("Run 'nbc update' to install the update.")
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
		if jsonOutput {
			progress.Error(err, "Update failed")
		}
		return err
	}

	return nil
}
