package pkg

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
			break
		}
	}

	// Wait for the command to finish (we may have stopped reading early)
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
func VerifyDracutEtcOverlay(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would verify etc-overlay dracut module exists")
		return nil
	}

	fmt.Println("  Verifying etc-overlay dracut module...")

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

	fmt.Println("  ✓ Dracut etc-overlay module verified")
	return nil
}

// RegenerateInitramfs regenerates the initramfs using dracut in a chroot environment.
// This is necessary to include the etc-overlay module in the initramfs.
// If the initramfs already contains the etc-overlay module, regeneration is skipped.
func RegenerateInitramfs(targetDir string, dryRun bool, verbose bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would check/regenerate initramfs with dracut if needed")
		return nil
	}

	fmt.Println("  Checking initramfs for etc-overlay module...")

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
			fmt.Println("  Warning: dracut not found in target, skipping initramfs regeneration")
			fmt.Println("  The container image's initramfs will be used (may not have etc-overlay support)")
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
		hasModule, err := InitramfsHasEtcOverlay(initramfsPath)
		if err != nil {
			if verbose {
				fmt.Printf("    Warning: could not check initramfs for %s: %v\n", kernelVersion, err)
			}
			needsRegeneration = append(needsRegeneration, kernelVersion)
			continue
		}
		if hasModule {
			fmt.Printf("    ✓ Initramfs for %s already has etc-overlay module\n", kernelVersion)
		} else {
			needsRegeneration = append(needsRegeneration, kernelVersion)
		}
	}

	if len(needsRegeneration) == 0 {
		fmt.Println("  All initramfs images already have etc-overlay module, skipping regeneration")
		return nil
	}

	fmt.Printf("  Regenerating initramfs for %d kernel(s)...\n", len(needsRegeneration))

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

		fmt.Printf("    Regenerating initramfs for kernel %s...\n", kernelVersion)

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

		cmd := exec.Command("chroot", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to regenerate initramfs for kernel %s: %w", kernelVersion, err)
		}

		fmt.Printf("    ✓ Initramfs regenerated for %s\n", kernelVersion)
	}

	fmt.Println("  Initramfs regeneration complete")
	return nil
}
