package cmd

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/nbc/pkg"
	"github.com/frostyard/nbc/pkg/types"
	"github.com/spf13/cobra"
)

type cacheListFlags struct {
	installImages bool
	updateImages  bool
}

type cacheClearFlags struct {
	install bool
	update  bool
}

type cacheRemoveFlags struct {
	cacheType string
}

var (
	cacheListF   cacheListFlags
	cacheClearF  cacheClearFlags
	cacheRemoveF cacheRemoveFlags
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage cached container images",
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

func init() {
	RootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheListCmd)
	cacheCmd.AddCommand(cacheRemoveCmd)
	cacheCmd.AddCommand(cacheClearCmd)

	// List flags
	cacheListCmd.Flags().BoolVar(&cacheListF.installImages, "install-images", false, "List staged installation images")
	cacheListCmd.Flags().BoolVar(&cacheListF.updateImages, "update-images", false, "List staged update images")

	// Remove flags
	cacheRemoveCmd.Flags().StringVar(&cacheRemoveF.cacheType, "type", "", "Cache type: 'install' or 'update' (auto-detected if not specified)")

	// Clear flags
	cacheClearCmd.Flags().BoolVar(&cacheClearF.install, "install", false, "Clear staged installation images")
	cacheClearCmd.Flags().BoolVar(&cacheClearF.update, "update", false, "Clear staged update images")
}

func runCacheList(cmd *cobra.Command, args []string) error {
	// Validate flags
	if !cacheListF.installImages && !cacheListF.updateImages {
		return fmt.Errorf("must specify either --install-images or --update-images")
	}
	if cacheListF.installImages && cacheListF.updateImages {
		return fmt.Errorf("--install-images and --update-images are mutually exclusive")
	}

	var cache *pkg.ImageCache
	var cacheType, cacheDir string

	if cacheListF.installImages {
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
		if clix.JSONOutput {
			return clix.OutputJSONError("failed to list cached images", err)
		}
		return fmt.Errorf("failed to list cached images: %w", err)
	}

	if clix.JSONOutput {
		output := types.CacheListOutput{
			CacheType: cacheType,
			CacheDir:  cacheDir,
			Images:    images,
		}
		if output.Images == nil {
			output.Images = []types.CachedImageMetadata{} // Ensure empty array, not null
		}
		clix.OutputJSON(output)
		return nil
	}

	// Human-readable output
	if len(images) == 0 {
		if cacheListF.installImages {
			fmt.Println("No staged installation images found.")
			fmt.Printf("Use 'nbc download --image <ref> --for-install' to stage an image.\n")
		} else {
			fmt.Println("No staged update images found.")
			fmt.Printf("Use 'nbc download --for-update' to stage an update.\n")
		}
		return nil
	}

	if cacheListF.installImages {
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

	// Try to find and remove from either cache
	var removed bool
	var removeErr error

	// Try install cache first
	installCache := pkg.NewStagedInstallCache()
	progress := clix.NewReporter()
	if cacheRemoveF.cacheType == "" || cacheRemoveF.cacheType == "install" {
		if err := installCache.Remove(cmd.Context(), digest, progress); err == nil {
			removed = true
		} else if cacheRemoveF.cacheType == "install" {
			removeErr = err
		}
	}

	// Try update cache
	if !removed && (cacheRemoveF.cacheType == "" || cacheRemoveF.cacheType == "update") {
		updateCache := pkg.NewStagedUpdateCache()
		if err := updateCache.Remove(cmd.Context(), digest, progress); err == nil {
			removed = true
		} else if cacheRemoveF.cacheType == "update" {
			removeErr = err
		}
	}

	if !removed {
		if removeErr != nil {
			if clix.JSONOutput {
				return clix.OutputJSONError("failed to remove cached image", removeErr)
			}
			return removeErr
		}
		err := fmt.Errorf("no cached image matches: %s", digest)
		if clix.JSONOutput {
			return clix.OutputJSONError("image not found", err)
		}
		return err
	}

	if clix.JSONOutput {
		clix.OutputJSON(map[string]any{
			"success": true,
			"removed": digest,
		})
		return nil
	}

	return nil
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	if !cacheClearF.install && !cacheClearF.update {
		return fmt.Errorf("must specify either --install or --update")
	}
	if cacheClearF.install && cacheClearF.update {
		return fmt.Errorf("--install and --update are mutually exclusive")
	}

	var cache *pkg.ImageCache
	var cacheType string

	if cacheClearF.install {
		cache = pkg.NewStagedInstallCache()
		cacheType = "install"
	} else {
		cache = pkg.NewStagedUpdateCache()
		cacheType = "update"
	}

	progress := clix.NewReporter()
	if err := cache.Clear(cmd.Context(), progress); err != nil {
		if clix.JSONOutput {
			return clix.OutputJSONError("failed to clear cache", err)
		}
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	if clix.JSONOutput {
		clix.OutputJSON(map[string]any{
			"success":    true,
			"cache_type": cacheType,
		})
		return nil
	}

	return nil
}
