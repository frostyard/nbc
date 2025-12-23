package pkg

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed dracut/95etc-overlay/*
var dracutModules embed.FS

// InstallDracutEtcOverlay installs the etc-overlay dracut module into the target filesystem.
// This module sets up an overlayfs for /etc at boot time, with the root filesystem's /etc
// as the read-only lower layer and /var/lib/nbc/etc-overlay as the writable upper layer.
func InstallDracutEtcOverlay(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would install etc-overlay dracut module")
		return nil
	}

	fmt.Println("  Installing etc-overlay dracut module...")

	moduleDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "95etc-overlay")

	// Create module directory
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		return fmt.Errorf("failed to create dracut module directory: %w", err)
	}

	// List of files to install
	files := []string{
		"module-setup.sh",
		"etc-overlay-mount.sh",
	}

	for _, filename := range files {
		srcPath := filepath.Join("dracut", "95etc-overlay", filename)
		dstPath := filepath.Join(moduleDir, filename)

		content, err := dracutModules.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", srcPath, err)
		}

		// Determine permissions (scripts need to be executable)
		perm := os.FileMode(0755)

		if err := os.WriteFile(dstPath, content, perm); err != nil {
			return fmt.Errorf("failed to write %s: %w", dstPath, err)
		}
		fmt.Printf("    Installed %s\n", filename)
	}

	fmt.Println("  Dracut module installed successfully")
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
	bindMounts := []string{"/dev", "/proc", "/sys"}
	for _, mount := range bindMounts {
		targetMount := filepath.Join(targetDir, mount)
		if err := os.MkdirAll(targetMount, 0755); err != nil {
			return fmt.Errorf("failed to create mount point %s: %w", targetMount, err)
		}
		cmd := exec.Command("mount", "--bind", mount, targetMount)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to bind mount %s: %w", mount, err)
		}
	}

	// Ensure cleanup of bind mounts
	defer func() {
		for i := len(bindMounts) - 1; i >= 0; i-- {
			targetMount := filepath.Join(targetDir, bindMounts[i])
			_ = exec.Command("umount", targetMount).Run()
		}
	}()

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

// ValidateDracutModule checks if the target filesystem has the etc-overlay dracut module.
func ValidateDracutModule(targetDir string) bool {
	moduleSetup := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "95etc-overlay", "module-setup.sh")
	_, err := os.Stat(moduleSetup)
	return err == nil
}
