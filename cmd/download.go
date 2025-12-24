package cmd

import (
	"fmt"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	downloadImage      string
	downloadForInstall bool
	downloadForUpdate  bool
)

// DownloadOutput represents the JSON output structure for the download command
type DownloadOutput struct {
	ImageRef     string `json:"image_ref"`
	ImageDigest  string `json:"image_digest"`
	CacheDir     string `json:"cache_dir"`
	SizeBytes    int64  `json:"size_bytes"`
	Architecture string `json:"architecture"`
	OSName       string `json:"os_name,omitempty"`
}

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download a container image to local cache",
	Long: `Download a container image for offline installation or staged updates.

This command downloads a container image and saves it in OCI layout format
for later use. The image can be used for:

  - Offline installation: Embed on a live ISO for installation without
    internet access. Use --for-install to save to /var/cache/nbc/staged-install/

  - Staged updates: Download an update now, apply later at a convenient time.
    Use --for-update to save to /var/cache/nbc/staged-update/

Multiple installation images can be staged (e.g., different editions),
but only one update image at a time.

Examples:
  # Download image for embedding in an ISO
  nbc download --image quay.io/example/myimage:latest --for-install

  # Download update to apply later (uses image from system config)
  nbc download --for-update

  # Download specific update image
  nbc download --image quay.io/example/myimage:v2.0 --for-update

  # JSON output for scripting
  nbc download --image quay.io/example/myimage:latest --for-install --json`,
	RunE: runDownload,
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().StringVarP(&downloadImage, "image", "i", "", "Container image reference (required for --for-install, uses system config for --for-update)")
	downloadCmd.Flags().BoolVar(&downloadForInstall, "for-install", false, "Save to staged-install cache (for ISO embedding)")
	downloadCmd.Flags().BoolVar(&downloadForUpdate, "for-update", false, "Save to staged-update cache (for offline updates)")
}

func runDownload(cmd *cobra.Command, args []string) error {
	jsonOutput := viper.GetBool("json")
	verbose := viper.GetBool("verbose")

	// Validate mutually exclusive flags
	if !downloadForInstall && !downloadForUpdate {
		return fmt.Errorf("must specify either --for-install or --for-update")
	}
	if downloadForInstall && downloadForUpdate {
		return fmt.Errorf("--for-install and --for-update are mutually exclusive")
	}

	// Validate --image is provided for --for-install
	if downloadForInstall && downloadImage == "" {
		return fmt.Errorf("--image is required when using --for-install")
	}

	// For --for-update, use system config if --image not specified
	if downloadForUpdate && downloadImage == "" {
		config, err := pkg.ReadSystemConfig()
		if err != nil {
			if jsonOutput {
				return outputJSONError("failed to read system config", err)
			}
			return fmt.Errorf("no --image specified and failed to read system config: %w", err)
		}
		downloadImage = config.ImageRef
		if !jsonOutput {
			fmt.Printf("Using image from system config: %s\n", downloadImage)
		}
	}

	// Determine cache directory
	var cacheDir string
	if downloadForInstall {
		cacheDir = pkg.StagedInstallDir
	} else {
		cacheDir = pkg.StagedUpdateDir
	}

	// For staged updates, check that we're on an nbc-managed system and validate the update
	if downloadForUpdate {
		// Read current system config
		config, err := pkg.ReadSystemConfig()
		if err != nil {
			if jsonOutput {
				return outputJSONError("failed to read system config", err)
			}
			return fmt.Errorf("failed to read system config: %w\n\nIs this system installed with nbc?", err)
		}

		// Get remote digest of new image
		remoteDigest, err := pkg.GetRemoteImageDigest(downloadImage)
		if err != nil {
			if jsonOutput {
				return outputJSONError("failed to get remote image digest", err)
			}
			return fmt.Errorf("failed to get remote image digest: %w", err)
		}

		// Check if update is actually newer
		if config.ImageDigest == remoteDigest {
			if jsonOutput {
				return outputJSONError("no update available", fmt.Errorf("image digest matches installed version"))
			}
			return fmt.Errorf("no update available: image digest matches installed version (%s)", remoteDigest[:19]+"...")
		}

		if !jsonOutput {
			fmt.Printf("Update available:\n")
			fmt.Printf("  Current: %s\n", config.ImageDigest[:min(19, len(config.ImageDigest))]+"...")
			fmt.Printf("  New:     %s\n", remoteDigest[:min(19, len(remoteDigest))]+"...")
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
	}

	// Create cache and download
	cache := pkg.NewImageCache(cacheDir)
	cache.SetVerbose(verbose)

	if !jsonOutput {
		if downloadForInstall {
			fmt.Printf("Downloading image for staged installation...\n")
		} else {
			fmt.Printf("Downloading image for staged update...\n")
		}
	}

	metadata, err := cache.Download(downloadImage)
	if err != nil {
		if jsonOutput {
			return outputJSONError("failed to download image", err)
		}
		return fmt.Errorf("failed to download image: %w", err)
	}

	if jsonOutput {
		output := DownloadOutput{
			ImageRef:     metadata.ImageRef,
			ImageDigest:  metadata.ImageDigest,
			CacheDir:     cacheDir,
			SizeBytes:    metadata.SizeBytes,
			Architecture: metadata.Architecture,
			OSName:       metadata.OSReleasePrettyName,
		}
		return outputJSON(output)
	}

	// Human-readable output
	fmt.Println()
	fmt.Println("Download complete!")
	fmt.Printf("  Image:        %s\n", metadata.ImageRef)
	fmt.Printf("  Digest:       %s\n", metadata.ImageDigest)
	fmt.Printf("  Architecture: %s\n", metadata.Architecture)
	if metadata.OSReleasePrettyName != "" {
		fmt.Printf("  OS:           %s\n", metadata.OSReleasePrettyName)
	}
	fmt.Printf("  Size:         %.2f MB\n", float64(metadata.SizeBytes)/(1024*1024))
	fmt.Printf("  Cached at:    %s\n", cacheDir)

	if downloadForInstall {
		fmt.Println()
		fmt.Println("Image is ready for embedding in an ISO.")
		fmt.Println("Use 'nbc cache list --install-images' to see all staged images.")
	} else {
		fmt.Println()
		fmt.Println("Update is staged and ready to apply.")
		fmt.Println("Run 'nbc update --local-image' to apply the staged update.")
	}

	return nil
}
