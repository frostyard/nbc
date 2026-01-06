package cmd

import (
	"fmt"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cacheListInstallImages bool
	cacheListUpdateImages  bool
)

// CacheListOutput represents the JSON output structure for cache list
type CacheListOutput struct {
	CacheType string                    `json:"cache_type"`
	CacheDir  string                    `json:"cache_dir"`
	Images    []pkg.CachedImageMetadata `json:"images"`
}

var cacheCmd = &cobra.Command{
	Use:     "cache",
	Aliases: []string{"c"},
	Short:   "Manage cached container images",
	Long: `Manage cached container images for offline installation and staged updates.

Subcommands:
  list    - List cached images
  remove  - Remove a cached image by digest
  clear   - Clear all cached images

Examples:
  nbc cache list --install-images
  nbc cache list --update-images
  nbc cache remove sha256-abc123...
  nbc cache clear --install
  nbc cache clear --update`,
}

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached container images",
	Long: `List cached container images.

Use --install-images to list images staged for installation (e.g., on ISO).
Use --update-images to list images staged for updates.

With --json flag, outputs a JSON object suitable for GUI installers.

Examples:
  nbc cache list --install-images
  nbc cache list --install-images --json
  nbc cache list --update-images`,
	RunE: runCacheList,
}

var cacheRemoveCmd = &cobra.Command{
	Use:   "remove <digest>",
	Short: "Remove a cached image by digest",
	Long: `Remove a cached image by its digest or digest prefix.

You can specify the full digest (sha256:abc123...) or a unique prefix.

Examples:
  nbc cache remove sha256:abc123...
  nbc cache remove sha256-abc1`,
	Args: cobra.ExactArgs(1),
	RunE: runCacheRemove,
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all cached images",
	Long: `Clear all cached images from a cache directory.

Use --install to clear staged installation images.
Use --update to clear staged update images.

Examples:
  nbc cache clear --install
  nbc cache clear --update`,
	RunE: runCacheClear,
}

var (
	cacheClearInstall bool
	cacheClearUpdate  bool
	cacheRemoveType   string
)

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheListCmd)
	cacheCmd.AddCommand(cacheRemoveCmd)
	cacheCmd.AddCommand(cacheClearCmd)

	// List flags
	cacheListCmd.Flags().BoolVar(&cacheListInstallImages, "install-images", false, "List staged installation images")
	cacheListCmd.Flags().BoolVar(&cacheListUpdateImages, "update-images", false, "List staged update images")

	// Remove flags
	cacheRemoveCmd.Flags().StringVar(&cacheRemoveType, "type", "", "Cache type: 'install' or 'update' (auto-detected if not specified)")

	// Clear flags
	cacheClearCmd.Flags().BoolVar(&cacheClearInstall, "install", false, "Clear staged installation images")
	cacheClearCmd.Flags().BoolVar(&cacheClearUpdate, "update", false, "Clear staged update images")
}

func runCacheList(cmd *cobra.Command, args []string) error {
	jsonOutput := viper.GetBool("json")

	// Validate flags
	if !cacheListInstallImages && !cacheListUpdateImages {
		return fmt.Errorf("must specify either --install-images or --update-images")
	}
	if cacheListInstallImages && cacheListUpdateImages {
		return fmt.Errorf("--install-images and --update-images are mutually exclusive")
	}

	var cache *pkg.ImageCache
	var cacheType, cacheDir string

	if cacheListInstallImages {
		cache = pkg.NewStagedInstallCache()
		cacheType = "install"
		cacheDir = pkg.StagedInstallDir
	} else {
		cache = pkg.NewStagedUpdateCache()
		cacheType = "update"
		cacheDir = pkg.StagedUpdateDir
	}

	images, err := cache.List()
	if err != nil {
		if jsonOutput {
			return outputJSONError("failed to list cached images", err)
		}
		return fmt.Errorf("failed to list cached images: %w", err)
	}

	if jsonOutput {
		output := CacheListOutput{
			CacheType: cacheType,
			CacheDir:  cacheDir,
			Images:    images,
		}
		if output.Images == nil {
			output.Images = []pkg.CachedImageMetadata{} // Ensure empty array, not null
		}
		return outputJSON(output)
	}

	// Human-readable output
	if len(images) == 0 {
		if cacheListInstallImages {
			fmt.Println("No staged installation images found.")
			fmt.Printf("Use 'nbc download --image <ref> --for-install' to stage an image.\n")
		} else {
			fmt.Println("No staged update images found.")
			fmt.Printf("Use 'nbc download --for-update' to stage an update.\n")
		}
		return nil
	}

	if cacheListInstallImages {
		fmt.Printf("Staged Installation Images (%s):\n\n", cacheDir)
	} else {
		fmt.Printf("Staged Update Images (%s):\n\n", cacheDir)
	}

	for i, img := range images {
		fmt.Printf("%d. %s\n", i+1, img.ImageRef)
		fmt.Printf("   Digest:       %s\n", img.ImageDigest)
		fmt.Printf("   Architecture: %s\n", img.Architecture)
		if img.OSReleasePrettyName != "" {
			fmt.Printf("   OS:           %s\n", img.OSReleasePrettyName)
		}
		fmt.Printf("   Size:         %.2f MB\n", float64(img.SizeBytes)/(1024*1024))
		fmt.Printf("   Downloaded:   %s\n", img.DownloadDate)
		fmt.Println()
	}

	return nil
}

func runCacheRemove(cmd *cobra.Command, args []string) error {
	digest := args[0]
	jsonOutput := viper.GetBool("json")

	// Try to find and remove from either cache
	var removed bool
	var removeErr error

	// Try install cache first
	installCache := pkg.NewStagedInstallCache()
	if cacheRemoveType == "" || cacheRemoveType == "install" {
		if err := installCache.Remove(digest); err == nil {
			removed = true
		} else if cacheRemoveType == "install" {
			removeErr = err
		}
	}

	// Try update cache
	if !removed && (cacheRemoveType == "" || cacheRemoveType == "update") {
		updateCache := pkg.NewStagedUpdateCache()
		if err := updateCache.Remove(digest); err == nil {
			removed = true
		} else if cacheRemoveType == "update" {
			removeErr = err
		}
	}

	if !removed {
		if removeErr != nil {
			if jsonOutput {
				return outputJSONError("failed to remove cached image", removeErr)
			}
			return removeErr
		}
		err := fmt.Errorf("no cached image matches: %s", digest)
		if jsonOutput {
			return outputJSONError("image not found", err)
		}
		return err
	}

	if jsonOutput {
		return outputJSON(map[string]interface{}{
			"success": true,
			"removed": digest,
		})
	}

	return nil
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	jsonOutput := viper.GetBool("json")

	if !cacheClearInstall && !cacheClearUpdate {
		return fmt.Errorf("must specify either --install or --update")
	}
	if cacheClearInstall && cacheClearUpdate {
		return fmt.Errorf("--install and --update are mutually exclusive")
	}

	var cache *pkg.ImageCache
	var cacheType string

	if cacheClearInstall {
		cache = pkg.NewStagedInstallCache()
		cacheType = "install"
	} else {
		cache = pkg.NewStagedUpdateCache()
		cacheType = "update"
	}

	if err := cache.Clear(); err != nil {
		if jsonOutput {
			return outputJSONError("failed to clear cache", err)
		}
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	if jsonOutput {
		return outputJSON(map[string]interface{}{
			"success":    true,
			"cache_type": cacheType,
		})
	}

	return nil
}
