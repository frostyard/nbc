package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// PristineEtcPath is where we store the pristine /etc from installation
	PristineEtcPath = "/var/lib/nbc/etc.pristine"
	// VarEtcPath is DEPRECATED - we no longer use /var/etc for boot-time bind mount
	// Kept for documentation purposes
	VarEtcPath = "/var/etc.backup"
)

// SetupEtcPersistence ensures /etc is properly configured for persistence across A/B updates.
//
// IMPORTANT: We do NOT bind-mount /var/etc to /etc at boot time.
// The bind-mount approach causes critical boot failures because:
// 1. Services like dbus-broker, systemd-journald need /etc very early in boot
// 2. The etc.mount unit runs too late (after var.mount, before local-fs.target)
// 3. Early systemd generators and services fail trying to read unmounted /etc
//
// Instead, we keep /etc on the root filesystem where services expect it.
// For A/B updates, /etc contents are merged from the old root to the new root
// during the update process (see MergeEtcFromActive).
//
// We still backup /etc to /var/etc for disaster recovery purposes.
func SetupEtcPersistence(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would setup /etc persistence\n")
		return nil
	}

	fmt.Println("  Setting up /etc persistence...")

	// Verify /etc exists and has content
	etcSource := filepath.Join(targetDir, "etc")
	if _, err := os.Stat(etcSource); os.IsNotExist(err) {
		return fmt.Errorf("/etc does not exist at %s", etcSource)
	}

	// List contents of /etc for debugging
	entries, err := os.ReadDir(etcSource)
	if err != nil {
		return fmt.Errorf("failed to read /etc directory: %w", err)
	}
	fmt.Printf("  /etc contains %d entries\n", len(entries))
	if len(entries) == 0 {
		return fmt.Errorf("/etc is empty at %s", etcSource)
	}

	// Check for critical files in /etc
	criticalFiles := []string{"passwd", "group", "os-release"}
	for _, f := range criticalFiles {
		path := filepath.Join(etcSource, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("  Warning: critical file %s not found in /etc\n", f)
		} else {
			fmt.Printf("  ✓ Found %s in /etc\n", f)
		}
	}

	// Create backup of /etc in /var/etc for disaster recovery
	// This is NOT used for boot-time mounting, only as a backup
	varEtcDir := filepath.Join(targetDir, "var", "etc.backup")
	if err := os.MkdirAll(varEtcDir, 0755); err != nil {
		return fmt.Errorf("failed to create /var/etc.backup directory: %w", err)
	}

	// Backup /etc contents to /var/etc.backup
	cmd := exec.Command("rsync", "-al", etcSource+"/", varEtcDir+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("  Warning: failed to backup /etc to /var/etc.backup: %v\nOutput: %s\n", err, string(output))
		// Don't fail on backup error - it's not critical for boot
	} else {
		fmt.Println("  Created /etc backup in /var/etc.backup")
	}

	fmt.Println("  /etc persistence setup complete (/etc stays on root filesystem)")
	return nil
}

// InstallEtcMountUnit is DEPRECATED - do not use.
// The bind-mount approach causes boot failures because services need /etc before the mount happens.
// This function is kept for backwards compatibility but does nothing.
// Use SetupEtcPersistence instead.
func InstallEtcMountUnit(targetDir string, dryRun bool) error {
	// DEPRECATED: The bind-mount approach doesn't work because:
	// - dbus-broker and other early services need /etc before var.mount completes
	// - systemd generators run before etc.mount can activate
	// - This causes "Failed to read /etc/passwd" and similar errors
	//
	// Instead, we now keep /etc on the root filesystem and only use
	// /var/etc.backup for disaster recovery, not for boot-time mounting.
	fmt.Println("  Note: /etc bind-mount skipped (using root filesystem /etc for reliability)")
	return SetupEtcPersistence(targetDir, dryRun)
}

// SavePristineEtc saves a copy of the pristine /etc after installation
// This is used to detect user modifications during updates
func SavePristineEtc(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would save pristine /etc to %s\n", PristineEtcPath)
		return nil
	}

	fmt.Println("  Saving pristine /etc for future updates...")

	etcSource := filepath.Join(targetDir, "etc")
	pristineDest := filepath.Join(targetDir, "var", "lib", "nbc", "etc.pristine")

	// Create directory
	if err := os.MkdirAll(filepath.Dir(pristineDest), 0755); err != nil {
		return fmt.Errorf("failed to create pristine etc directory: %w", err)
	}

	// Use rsync to copy /etc
	cmd := exec.Command("rsync", "-a", "--delete", etcSource+"/", pristineDest+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to save pristine /etc: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("  Saved pristine /etc snapshot\n")
	return nil
}

// MergeEtcFromActive merges /etc configuration from the active root during A/B updates.
//
// This function is called during the update process to preserve user modifications
// to /etc when switching to a new root partition. The approach is:
//
// 1. Mount the currently active root partition (contains user's modified /etc)
// 2. Mount the new root partition (contains fresh /etc from container image)
// 3. Merge user modifications from active /etc into new /etc, preserving:
//   - User-added files (not in container image)
//   - User-modified files (changed from container defaults)
//
// 4. System identity files (like /etc/os-release) always come from the new container
//
// Parameters:
//   - targetDir: mount point of the NEW root partition (e.g., /tmp/nbc-update)
//   - activeRootPartition: the CURRENT root partition device (contains user's /etc)
//   - dryRun: if true, don't make changes
func MergeEtcFromActive(targetDir string, activeRootPartition string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would merge /etc from active system\n")
		return nil
	}

	fmt.Println("  Merging /etc configuration from active system...")

	var activeEtc string
	var needsUnmount bool

	// Check if the active root is already mounted (we're running on the live system)
	// by checking if we can access /etc directly and it matches the active partition
	currentRoot, _ := GetActiveRootPartition()
	if currentRoot == activeRootPartition {
		// We're running on the active system, use /etc directly
		activeEtc = "/etc"
		needsUnmount = false
		fmt.Println("  Using live /etc from running system")
	} else {
		// Mount the active root partition to access user's /etc
		activeMountPoint := "/tmp/nbc-active-root"
		if err := os.MkdirAll(activeMountPoint, 0755); err != nil {
			return fmt.Errorf("failed to create active root mount point: %w", err)
		}
		defer func() { _ = os.RemoveAll(activeMountPoint) }()

		mountCmd := exec.Command("mount", "-o", "ro", activeRootPartition, activeMountPoint)
		if err := mountCmd.Run(); err != nil {
			return fmt.Errorf("failed to mount active root partition %s: %w", activeRootPartition, err)
		}
		activeEtc = filepath.Join(activeMountPoint, "etc")
		needsUnmount = true
		defer func() {
			if needsUnmount {
				_ = exec.Command("umount", activeMountPoint).Run()
			}
		}()
	}

	newEtc := filepath.Join(targetDir, "etc")

	// Check if active /etc exists
	if _, err := os.Stat(activeEtc); os.IsNotExist(err) {
		fmt.Println("  No /etc found on active root, using container defaults")
		return SetupEtcPersistence(targetDir, dryRun)
	}

	// Files that should always come from the NEW container (system identity files)
	// These should NOT be preserved from the old system
	systemFilesFromContainer := map[string]bool{
		"os-release": true,
	}

	// Files/directories that should be preserved from the active system
	// (user modifications that should persist across updates)
	fmt.Println("  Merging user modifications from active /etc...")

	err := filepath.Walk(activeEtc, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // Skip files we can't access
		}

		relPath, _ := filepath.Rel(activeEtc, path)
		if relPath == "." {
			return nil
		}

		destPath := filepath.Join(newEtc, relPath)

		// Check if this is a symlink
		linfo, err := os.Lstat(path)
		if err != nil {
			return nil // Skip files we can't lstat
		}

		isSymlink := linfo.Mode()&os.ModeSymlink != 0

		// Skip system identity files - these come from the new container
		if systemFilesFromContainer[filepath.Base(relPath)] {
			return nil
		}

		// Check if this file/directory exists in the new /etc
		newInfo, newErr := os.Lstat(destPath)
		fileExistsInNew := newErr == nil

		if linfo.IsDir() {
			// Create directory if it doesn't exist in new /etc
			if !fileExistsInNew {
				_ = os.MkdirAll(destPath, linfo.Mode())
				fmt.Printf("    + Added directory: %s\n", relPath)
			}
			return nil
		}

		// For files, decide whether to copy based on whether it exists in new /etc
		if !fileExistsInNew {
			// File doesn't exist in new container - this is a user-added file, preserve it
			_ = os.MkdirAll(filepath.Dir(destPath), 0755)
			if isSymlink {
				if err := copySymlink(path, destPath); err != nil {
					fmt.Printf("    Warning: failed to copy user symlink %s: %v\n", relPath, err)
				} else {
					fmt.Printf("    + Preserved user symlink: %s\n", relPath)
				}
			} else {
				if err := copyFile(path, destPath); err != nil {
					fmt.Printf("    Warning: failed to copy user file %s: %v\n", relPath, err)
				} else {
					fmt.Printf("    + Preserved user file: %s\n", relPath)
				}
			}
		} else if !newInfo.IsDir() && !linfo.IsDir() {
			// Both exist as files - check if user modified the file
			// For now, we preserve user's version for known config files
			// This is a simple heuristic - a more sophisticated approach would
			// compare against pristine /etc to detect actual modifications
			preserveUserModifications := []string{
				"passwd", "group", "shadow", "gshadow",
				"hostname", "hosts", "resolv.conf",
				"fstab", "crypttab",
				"machine-id",
				"localtime", "locale.conf", // Timezone and locale
				"adjtime", // Hardware clock settings
			}

			// Patterns for files that should be preserved (matched against relPath)
			preservePatterns := []string{
				"ssh/ssh_host_",                      // SSH host keys (identity of the system)
				"NetworkManager/system-connections/", // WiFi passwords, VPN configs
				"ssl/private/",                       // SSL/TLS private keys
				"pki/tls/private/",                   // PKI private keys (RHEL-style)
				"sudoers.d/",                         // Custom sudo rules
			}

			shouldPreserve := false
			// Check exact filename matches
			for _, preserve := range preserveUserModifications {
				if filepath.Base(relPath) == preserve {
					shouldPreserve = true
					break
				}
			}
			// Check pattern matches (prefix matching)
			if !shouldPreserve {
				for _, pattern := range preservePatterns {
					if len(relPath) >= len(pattern) && relPath[:len(pattern)] == pattern {
						shouldPreserve = true
						break
					}
				}
			}

			if shouldPreserve {
				if isSymlink {
					_ = copySymlink(path, destPath)
				} else {
					_ = copyFile(path, destPath)
				}
				fmt.Printf("    = Preserved user config: %s\n", relPath)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to merge /etc: %w", err)
	}

	// Setup persistence (creates backup in /var/etc.backup)
	if err := SetupEtcPersistence(targetDir, dryRun); err != nil {
		return fmt.Errorf("failed to setup etc persistence: %w", err)
	}

	fmt.Println("  /etc configuration merged successfully")
	return nil
}

// copyFile copies a single file preserving permissions
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := srcFile.WriteTo(dstFile); err != nil {
		return err
	}

	return nil
}

// copySymlink copies a symlink preserving its target
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}

	// Remove existing file/symlink if present
	_ = os.Remove(dst)

	return os.Symlink(target, dst)
}
