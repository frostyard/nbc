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
	updateImage        string
	updateDevice       string
	updateSkipPull     bool
	updateCheckOnly    bool
	updateKernelArgs   []string
	updateDownloadOnly bool
	updateLocalImage   bool
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

Use --download-only to download an update without applying it. The update
will be staged in /var/cache/nbc/staged-update/ and can be applied later
with --local-image.

With --json flag, outputs streaming JSON Lines for progress updates.

Example:
  nbc update
  nbc update --check              # Just check if update available
  nbc update --download-only      # Download but don't apply
  nbc update --local-image        # Apply staged update
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
	updateCmd.Flags().BoolVar(&updateDownloadOnly, "download-only", false, "Download update to cache without applying")
	updateCmd.Flags().BoolVar(&updateLocalImage, "local-image", false, "Apply update from staged cache (/var/cache/nbc/staged-update/)")
	_ = viper.BindPFlag("force", updateCmd.Flags().Lookup("force"))
}

func runUpdate(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")
	force := viper.GetBool("force")
	jsonOutput := viper.GetBool("json")

	// Create progress reporter for early error output
	progress := pkg.NewProgressReporter(jsonOutput, 7)

	// Validate mutually exclusive flags
	if updateDownloadOnly && updateLocalImage {
		err := fmt.Errorf("--download-only and --local-image are mutually exclusive")
		if jsonOutput {
			progress.Error(err, "Invalid options")
		}
		return err
	}

	if updateDownloadOnly && updateCheckOnly {
		err := fmt.Errorf("--download-only and --check are mutually exclusive")
		if jsonOutput {
			progress.Error(err, "Invalid options")
		}
		return err
	}

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
	if imageRef == "" && !updateLocalImage {
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

	// Handle --download-only: download image to staged-update cache
	if updateDownloadOnly {
		// Read current system config to compare digests
		config, err := pkg.ReadSystemConfig()
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to read system config")
			}
			return fmt.Errorf("failed to read system config: %w", err)
		}

		// Get remote digest of new image
		remoteDigest, err := pkg.GetRemoteImageDigest(imageRef)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to get remote image digest")
			}
			return fmt.Errorf("failed to get remote image digest: %w", err)
		}

		// Check if update is actually newer (unless force is set)
		if !force && config.ImageDigest == remoteDigest {
			if jsonOutput {
				output := UpdateCheckOutput{
					UpdateNeeded:  false,
					Image:         imageRef,
					Device:        device,
					CurrentDigest: config.ImageDigest,
					NewDigest:     remoteDigest,
					Message:       "System is up-to-date, no download needed",
				}
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(output)
			}
			fmt.Println("System is up-to-date, no download needed.")
			return nil
		}

		if !jsonOutput {
			fmt.Printf("Update available:\n")
			fmt.Printf("  Current: %s\n", config.ImageDigest[:min(19, len(config.ImageDigest))]+"...")
			fmt.Printf("  New:     %s\n", remoteDigest[:min(19, len(remoteDigest))]+"...")
			fmt.Println()
		}

		// Clear any existing staged update
		updateCache := pkg.NewStagedUpdateCache()
		existing, _ := updateCache.GetSingle()
		if existing != nil {
			if verbose && !jsonOutput {
				fmt.Printf("Removing existing staged update: %s\n", existing.ImageDigest)
			}
			_ = updateCache.Clear()
		}

		// Download to staged-update cache
		updateCache.SetVerbose(verbose)
		metadata, err := updateCache.Download(imageRef)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to download update")
			}
			return fmt.Errorf("failed to download update: %w", err)
		}

		if jsonOutput {
			output := map[string]interface{}{
				"success":      true,
				"image_ref":    metadata.ImageRef,
				"image_digest": metadata.ImageDigest,
				"size_bytes":   metadata.SizeBytes,
				"cache_dir":    pkg.StagedUpdateDir,
				"message":      "Update downloaded and staged",
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(output)
		}

		fmt.Println()
		fmt.Println("Update downloaded and staged!")
		fmt.Printf("  Image:  %s\n", metadata.ImageRef)
		fmt.Printf("  Digest: %s\n", metadata.ImageDigest)
		fmt.Printf("  Size:   %.2f MB\n", float64(metadata.SizeBytes)/(1024*1024))
		fmt.Println()
		fmt.Println("Run 'nbc update --local-image' to apply the staged update.")
		return nil
	}

	// Handle --local-image: apply update from staged cache
	var localLayoutPath string
	var localMetadata *pkg.CachedImageMetadata
	if updateLocalImage {
		updateCache := pkg.NewStagedUpdateCache()
		metadata, err := updateCache.GetSingle()
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to read staged update")
			}
			return fmt.Errorf("failed to read staged update: %w", err)
		}
		if metadata == nil {
			err := fmt.Errorf("no staged update found in %s", pkg.StagedUpdateDir)
			if jsonOutput {
				progress.Error(err, "No staged update")
			}
			return err
		}

		localLayoutPath = updateCache.GetLayoutPath(metadata.ImageDigest)
		localMetadata = metadata
		imageRef = metadata.ImageRef

		if !jsonOutput {
			fmt.Printf("Using staged update: %s\n", metadata.ImageRef)
			fmt.Printf("  Digest: %s\n", metadata.ImageDigest)
		}
	}

	// Create updater
	updater := pkg.NewSystemUpdater(device, imageRef)
	updater.SetVerbose(verbose)
	updater.SetDryRun(dryRun)
	updater.SetForce(force)
	updater.SetJSONOutput(jsonOutput)

	// Set local image if using staged update
	if localLayoutPath != "" {
		updater.SetLocalImage(localLayoutPath, localMetadata)
	}

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

	// Run update (skip pull if using local image)
	skipPull := updateSkipPull || localLayoutPath != ""
	if err := updater.PerformUpdate(skipPull); err != nil {
		if jsonOutput {
			progress.Error(err, "Update failed")
		}
		return err
	}

	// Clean up staged update cache on success
	if localLayoutPath != "" {
		updateCache := pkg.NewStagedUpdateCache()
		if err := updateCache.Clear(); err != nil {
			// Non-fatal, just warn
			if verbose && !jsonOutput {
				fmt.Printf("Warning: failed to clean up staged update cache: %v\n", err)
			}
		} else if verbose && !jsonOutput {
			fmt.Println("Cleaned up staged update cache.")
		}
	}

	return nil
}
