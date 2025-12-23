package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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
func RegenerateInitramfs(targetDir string, dryRun bool, verbose bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would regenerate initramfs with dracut")
		return nil
	}

	fmt.Println("  Regenerating initramfs to include etc-overlay module...")

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

	// Regenerate initramfs for each kernel version
	for _, kernelVersion := range kernelVersions {
		fmt.Printf("    Regenerating initramfs for kernel %s...\n", kernelVersion)

		initramfsPath := filepath.Join("/usr/lib/modules", kernelVersion, "initramfs.img")

		// Run dracut in chroot
		args := []string{
			targetDir,
			dracutChrootPath,
			"--force",
			"--add", "etc-overlay",
			initramfsPath,
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
