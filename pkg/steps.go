package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// SetupTargetSystem performs the common post-extraction system setup sequence
// shared between install and update flows. This includes:
// - Installing dracut etc-overlay module (with optional regeneration)
// - Setting up system directories
// - Preparing machine-id for first boot
// - Populating /.etc.lower for overlay
// - Installing tmpfiles.d config for /run/nbc-booted marker
//
// Operations specific to install (InstallEtcMountUnit, SavePristineEtc)
// or update (MergeEtcFromActive) must be called separately.
func SetupTargetSystem(ctx context.Context, mountPoint string, dryRun, verbose bool, progress Reporter) error {
	// Install the embedded dracut module for /etc overlay persistence
	// Check if container already has it first
	moduleSetupSh := filepath.Join(mountPoint, "usr", "lib", "dracut", "modules.d", "95etc-overlay", "module-setup.sh")
	if _, err := os.Stat(moduleSetupSh); err == nil {
		progress.Message("Container already has etc-overlay dracut module, skipping regeneration")
	} else {
		progress.Message("Installing etc-overlay dracut module and regenerating initramfs")
		if err := InstallDracutEtcOverlay(ctx, mountPoint, dryRun, progress); err != nil {
			return fmt.Errorf("failed to install dracut etc-overlay module: %w", err)
		}

		if err := RegenerateInitramfs(ctx, mountPoint, dryRun, verbose, progress); err != nil {
			progress.Warning("initramfs regeneration failed: %v", err)
			progress.Warning("Boot may fail if container's initramfs lacks etc-overlay support")
		}
	}

	// Setup system directories
	if err := SetupSystemDirectories(ctx, mountPoint, progress); err != nil {
		return fmt.Errorf("failed to setup directories: %w", err)
	}

	// Prepare /etc/machine-id for first boot on read-only root
	if err := PrepareMachineID(ctx, mountPoint, progress); err != nil {
		return fmt.Errorf("failed to prepare machine-id: %w", err)
	}

	// Populate /.etc.lower with container's /etc for overlay lower layer
	if err := PopulateEtcLower(ctx, mountPoint, dryRun, progress); err != nil {
		return fmt.Errorf("failed to populate .etc.lower: %w", err)
	}

	// Install tmpfiles.d config for /run/nbc-booted marker
	if err := InstallTmpfilesConfig(ctx, mountPoint, dryRun, progress); err != nil {
		return fmt.Errorf("failed to install tmpfiles config: %w", err)
	}

	return nil
}

// ExtractAndVerifyContainer creates and runs a container extractor, then verifies
// the extraction succeeded. Used by both install and update flows.
func ExtractAndVerifyContainer(ctx context.Context, imageRef, localLayoutPath, mountPoint string, verbose bool, progress Reporter) error {
	var extractor *ContainerExtractor
	if localLayoutPath != "" {
		extractor = NewContainerExtractorFromLocal(localLayoutPath, mountPoint)
	} else {
		extractor = NewContainerExtractor(imageRef, mountPoint)
	}
	extractor.SetVerbose(verbose)
	extractor.SetProgress(progress)

	if err := extractor.Extract(ctx); err != nil {
		return fmt.Errorf("failed to extract container: %w", err)
	}

	// Verify extraction succeeded
	progress.Message("Verifying extraction...")
	if err := VerifyExtraction(mountPoint); err != nil {
		return fmt.Errorf("container extraction verification failed: %w", err)
	}

	return nil
}
