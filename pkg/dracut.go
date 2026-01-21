package pkg

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Embed the dracut module files directly into the nbc binary.
// This ensures nbc always installs its own version of the dracut module,
// regardless of what's in the container image.
//
//go:embed dracut/95etc-overlay/module-setup.sh dracut/95etc-overlay/etc-overlay-mount.sh
var dracutModuleFS embed.FS

// InstallDracutEtcOverlay installs the embedded etc-overlay dracut module to the target filesystem.
// This overwrites any existing module from the container image to ensure the nbc binary's
// version is used (which may have fixes not yet in the published container image).
func InstallDracutEtcOverlay(targetDir string, dryRun bool, progress *ProgressReporter) error {
	if dryRun {
		progress.Message("[DRY RUN] Would install etc-overlay dracut module")
		return nil
	}

	progress.Message("Installing etc-overlay dracut module...")

	moduleDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "95etc-overlay")

	// Create the module directory
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		return fmt.Errorf("failed to create dracut module directory: %w", err)
	}

	// Files to install from embedded FS
	files := []string{
		"dracut/95etc-overlay/module-setup.sh",
		"dracut/95etc-overlay/etc-overlay-mount.sh",
	}

	for _, srcPath := range files {
		content, err := dracutModuleFS.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", srcPath, err)
		}

		// Extract just the filename for the destination
		filename := filepath.Base(srcPath)
		dstPath := filepath.Join(moduleDir, filename)

		// Write the file with executable permissions
		if err := os.WriteFile(dstPath, content, 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", dstPath, err)
		}
	}

	progress.Message("✓ Dracut etc-overlay module installed")
	return nil
}

// InitramfsHasEtcOverlay checks if the initramfs at the given path contains
// the etc-overlay dracut module. This is used to skip regenerating the
// initramfs if it already has the module.
//
// The function uses lsinitrd (Fedora/RHEL) or lsinitramfs (Debian/Ubuntu) to
// list the initramfs contents and searches for the etc-overlay hook script.
func InitramfsHasEtcOverlay(initramfsPath string) (bool, error) {
	// Determine which tool to use
	var listCmd *exec.Cmd
	if _, err := exec.LookPath("lsinitrd"); err == nil {
		// Fedora/RHEL style
		listCmd = exec.Command("lsinitrd", "--list", initramfsPath)
	} else if _, err := exec.LookPath("lsinitramfs"); err == nil {
		// Debian/Ubuntu style
		listCmd = exec.Command("lsinitramfs", initramfsPath)
	} else {
		// No tool available, assume we need to regenerate
		return false, nil
	}

	stdout, err := listCmd.StdoutPipe()
	if err != nil {
		return false, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := listCmd.Start(); err != nil {
		return false, fmt.Errorf("failed to list initramfs contents: %w", err)
	}

	// Search for the etc-overlay hook script
	// Dracut installs hooks as: usr/lib/dracut/hooks/<hook-type>/<priority><script-name>.sh
	// Our module installs: inst_hook pre-pivot 50 "$moddir/etc-overlay-mount.sh"
	// This becomes something like: usr/lib/dracut/hooks/pre-pivot/50etc-overlay-mount.sh
	scanner := bufio.NewScanner(stdout)
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "etc-overlay-mount.sh") {
			found = true
		}
	}

	// Wait for the command to finish after fully reading its output
	if err := listCmd.Wait(); err != nil {
		if found {
			// We already found the hook; log a warning but still return success.
			fmt.Printf("warning: listing initramfs contents failed after finding etc-overlay hook: %v\n", err)
		} else {
			// Listing failed before we found the hook; propagate error so callers can regenerate.
			return false, fmt.Errorf("failed to list initramfs contents: %w", err)
		}
	}

	return found, scanner.Err()
}

// VerifyDracutEtcOverlay verifies that the etc-overlay dracut module exists in the target filesystem.
// The module is installed via the nbc deb/rpm package to /usr/lib/dracut/modules.d/95etc-overlay/.
// This function checks that the container/host has nbc installed with the dracut module.
func VerifyDracutEtcOverlay(targetDir string, dryRun bool, progress *ProgressReporter) error {
	if dryRun {
		progress.Message("[DRY RUN] Would verify etc-overlay dracut module exists")
		return nil
	}

	progress.Message("Verifying etc-overlay dracut module...")

	moduleDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "95etc-overlay")

	// Check for required files
	requiredFiles := []string{
		"module-setup.sh",
		"etc-overlay-mount.sh",
	}

	for _, filename := range requiredFiles {
		filePath := filepath.Join(moduleDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return fmt.Errorf("etc-overlay dracut module not found at %s - ensure nbc is installed in the container image", moduleDir)
		}
	}

	progress.Message("✓ Dracut etc-overlay module verified")
	return nil
}

// RegenerateInitramfs regenerates the initramfs using dracut in a chroot environment.
// This is necessary to include the etc-overlay module in the initramfs.
// If the initramfs already contains the etc-overlay module, regeneration is skipped.
func RegenerateInitramfs(ctx context.Context, targetDir string, dryRun bool, verbose bool, progress *ProgressReporter) error {
	if dryRun {
		progress.Message("[DRY RUN] Would check/regenerate initramfs with dracut if needed")
		return nil
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	progress.Message("Checking initramfs for etc-overlay module...")

	// Check if dracut is available in the target
	// We need to track the path relative to the chroot (for use inside chroot)
	var dracutChrootPath string
	dracutPath := filepath.Join(targetDir, "usr", "bin", "dracut")
	if _, err := os.Stat(dracutPath); err == nil {
		dracutChrootPath = "/usr/bin/dracut"
	} else {
		// Try /sbin/dracut
		dracutPath = filepath.Join(targetDir, "sbin", "dracut")
		if _, err := os.Stat(dracutPath); err == nil {
			dracutChrootPath = "/sbin/dracut"
		} else {
			progress.Warning("dracut not found in target, skipping initramfs regeneration")
			progress.Message("The container image's initramfs will be used (may not have etc-overlay support)")
			return nil
		}
	}

	// Find kernel version(s) from /usr/lib/modules
	modulesDir := filepath.Join(targetDir, "usr", "lib", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return fmt.Errorf("failed to read /usr/lib/modules: %w", err)
	}

	var kernelVersions []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "." && entry.Name() != ".." {
			kernelVersions = append(kernelVersions, entry.Name())
		}
	}

	if len(kernelVersions) == 0 {
		return fmt.Errorf("no kernel versions found in /usr/lib/modules")
	}

	// Check which kernels need initramfs regeneration
	var needsRegeneration []string
	for _, kernelVersion := range kernelVersions {
		initramfsPath := filepath.Join(targetDir, "usr", "lib", "modules", kernelVersion, "initramfs.img")

		// First check if the initramfs file exists before inspecting its contents
		if _, err := os.Stat(initramfsPath); err != nil {
			if os.IsNotExist(err) {
				if verbose {
					progress.Message("Info: initramfs for %s not found at %s, will regenerate", kernelVersion, initramfsPath)
				}
			} else {
				if verbose {
					progress.Warning("could not stat initramfs for %s at %s: %v", kernelVersion, initramfsPath, err)
				}
			}
			needsRegeneration = append(needsRegeneration, kernelVersion)
			continue
		}
		hasModule, err := InitramfsHasEtcOverlay(initramfsPath)
		if err != nil {
			if verbose {
				progress.Warning("could not check initramfs for %s: %v", kernelVersion, err)
			}
			needsRegeneration = append(needsRegeneration, kernelVersion)
			continue
		}
		if hasModule {
			progress.Message("✓ Initramfs for %s already has etc-overlay module", kernelVersion)
		} else {
			needsRegeneration = append(needsRegeneration, kernelVersion)
		}
	}

	if len(needsRegeneration) == 0 {
		progress.Message("All initramfs images already have etc-overlay module, skipping regeneration")
		return nil
	}

	progress.Message("Regenerating initramfs for %d kernel(s)...", len(needsRegeneration))

	// Setup bind mounts for chroot
	// Track successful mounts for cleanup
	bindMounts := []string{"/dev", "/proc", "/sys"}
	var mountedPaths []string

	// Ensure cleanup of bind mounts - defer before mounting so it runs even on partial failure
	defer func() {
		for i := len(mountedPaths) - 1; i >= 0; i-- {
			_ = exec.Command("umount", mountedPaths[i]).Run()
		}
	}()

	for _, mount := range bindMounts {
		targetMount := filepath.Join(targetDir, mount)
		if err := os.MkdirAll(targetMount, 0755); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", targetMount, err)
		}
		cmd := exec.Command("mount", "--bind", mount, targetMount)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to bind mount %s: %w", mount, err)
		}
		mountedPaths = append(mountedPaths, targetMount)
	}

	// Regenerate initramfs for each kernel version that needs it
	for _, kernelVersion := range needsRegeneration {
		chrootInitramfsPath := filepath.Join("/usr/lib/modules", kernelVersion, "initramfs.img")

		progress.Message("Regenerating initramfs for kernel %s...", kernelVersion)

		// Run dracut in chroot
		args := []string{
			targetDir,
			dracutChrootPath,
			"--force",
			"--add", "etc-overlay",
			chrootInitramfsPath,
			kernelVersion,
		}

		if verbose {
			args = append(args[:3], append([]string{"--verbose"}, args[3:]...)...)
		}

		cmd := exec.CommandContext(ctx, "chroot", args...)

		// Capture output to avoid cluttering JSON output
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			// Include captured output in error reporting
			combinedOutput := strings.TrimSpace(stderr.String() + stdout.String())
			if combinedOutput != "" {
				if progress.IsJSON() {
					progress.Error(fmt.Errorf("dracut failed: %s", combinedOutput), "initramfs regeneration")
				} else {
					fmt.Printf("    dracut output:\n%s\n", combinedOutput)
				}
			}
			return fmt.Errorf("failed to regenerate initramfs for kernel %s: %w", kernelVersion, err)
		}

		// In verbose non-JSON mode, show the output
		if verbose && !progress.IsJSON() {
			if out := strings.TrimSpace(stdout.String()); out != "" {
				fmt.Printf("%s\n", out)
			}
		}

		progress.Message("✓ Initramfs regenerated for %s", kernelVersion)
	}

	progress.Message("Initramfs regeneration complete")
	return nil
}
