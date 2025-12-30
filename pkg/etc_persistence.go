package pkg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	// PristineEtcPath is where we store the pristine /etc from installation
	PristineEtcPath = "/var/lib/nbc/etc.pristine"
	// EtcOverlayPath is where we store the overlay upper/work directories
	EtcOverlayPath = "/var/lib/nbc/etc-overlay"
	// VarEtcPath is DEPRECATED - we no longer use /var/etc for boot-time bind mount
	// Kept for documentation purposes
	VarEtcPath = "/var/etc.backup"
)

// SetupEtcOverlay creates the overlay directories for /etc persistence.
//
// The overlay approach works as follows:
// 1. The root filesystem's /etc is the read-only lower layer (from container image)
// 2. User modifications persist in /var/lib/nbc/etc-overlay/upper (writable layer)
// 3. A dracut module (95etc-overlay) mounts the overlay during early boot
//
// This allows user changes to /etc to persist across A/B updates while
// keeping the base /etc from the container image.
func SetupEtcOverlay(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would setup /etc overlay directories\n")
		return nil
	}

	fmt.Println("  Setting up /etc overlay persistence...")

	// Create overlay directories
	overlayBase := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay")
	upperDir := filepath.Join(overlayBase, "upper")
	workDir := filepath.Join(overlayBase, "work")

	if err := os.MkdirAll(upperDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay upper directory: %w", err)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create overlay work directory: %w", err)
	}

	fmt.Printf("  Created overlay directories at %s\n", overlayBase)

	// Verify /etc exists and has content (will be the lower layer)
	etcSource := filepath.Join(targetDir, "etc")
	if _, err := os.Stat(etcSource); os.IsNotExist(err) {
		return fmt.Errorf("/etc does not exist at %s", etcSource)
	}

	entries, err := os.ReadDir(etcSource)
	if err != nil {
		return fmt.Errorf("failed to read /etc directory: %w", err)
	}
	fmt.Printf("  /etc (lower layer) contains %d entries\n", len(entries))
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

	fmt.Println("  /etc overlay persistence setup complete")
	return nil
}

// SetupEtcPersistence ensures /etc is properly configured for persistence across A/B updates.
//
// IMPORTANT: This function now sets up overlay-based persistence.
// A dracut module mounts an overlayfs for /etc during early boot:
// - lowerdir: /etc from root filesystem (read-only, from container image)
// - upperdir: /var/lib/nbc/etc-overlay/upper (writable, user modifications)
// - workdir: /var/lib/nbc/etc-overlay/work (required by overlayfs)
//
// This approach solves the timing issues that plagued bind-mount approaches,
// because the dracut hook runs before pivot_root when /etc is not yet in use.
func SetupEtcPersistence(targetDir string, dryRun bool) error {
	return SetupEtcOverlay(targetDir, dryRun)
}

// InstallEtcMountUnit is DEPRECATED - use SetupEtcPersistence instead.
// This function now just calls SetupEtcPersistence for backwards compatibility.
func InstallEtcMountUnit(targetDir string, dryRun bool) error {
	return SetupEtcPersistence(targetDir, dryRun)
}

// PopulateEtcLower copies the container's /etc to /.etc.lower for use as the overlay lower layer.
// This must be called after container extraction to ensure the dracut etc-overlay module
// finds a populated /.etc.lower directory on first boot.
//
// The dracut module checks if /.etc.lower exists and has content:
// - If it exists with content: uses it as the overlay lowerdir
// - If it's empty or missing: moves /etc to /.etc.lower and uses it
//
// By populating /.etc.lower during install/update, we ensure consistent behavior
// and the container's /etc is preserved as the read-only base layer.
func PopulateEtcLower(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would populate /.etc.lower with container /etc\n")
		return nil
	}

	fmt.Println("  Populating /.etc.lower with container /etc...")

	etcSource := filepath.Join(targetDir, "etc")
	etcLowerDest := filepath.Join(targetDir, ".etc.lower")

	// Verify source /etc exists and has content
	if _, err := os.Stat(etcSource); os.IsNotExist(err) {
		return fmt.Errorf("/etc does not exist at %s", etcSource)
	}

	entries, err := os.ReadDir(etcSource)
	if err != nil {
		return fmt.Errorf("failed to read /etc directory: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("/etc is empty at %s", etcSource)
	}

	// Create /.etc.lower if it doesn't exist
	if err := os.MkdirAll(etcLowerDest, 0755); err != nil {
		return fmt.Errorf("failed to create .etc.lower directory: %w", err)
	}

	// Use rsync to copy /etc to /.etc.lower
	cmd := exec.Command("rsync", "-a", "--delete", etcSource+"/", etcLowerDest+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to populate .etc.lower: %w\nOutput: %s", err, string(output))
	}

	// Verify /.etc.lower has content
	lowerEntries, err := os.ReadDir(etcLowerDest)
	if err != nil {
		return fmt.Errorf("failed to verify .etc.lower: %w", err)
	}
	if len(lowerEntries) == 0 {
		return fmt.Errorf(".etc.lower is empty after copy")
	}

	fmt.Printf("  Populated /.etc.lower with %d entries from container /etc\n", len(lowerEntries))
	return nil
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

// MergeEtcFromActive handles /etc configuration during A/B updates with overlay persistence.
//
// With the overlay-based persistence model, user modifications to /etc are stored in
// /var/lib/nbc/etc-overlay/upper and automatically apply to whichever root is active.
// This function no longer needs to copy files between roots.
//
// The main task now is to:
// 1. Ensure the overlay directories exist on the new root
// 2. Optionally detect conflicts where both the container and user modified the same file
//
// Parameters:
//   - targetDir: mount point of the NEW root partition (e.g., /tmp/nbc-update)
//   - activeRootPartition: the CURRENT root partition device (not used with overlay)
//   - dryRun: if true, don't make changes
func MergeEtcFromActive(targetDir string, activeRootPartition string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would setup /etc overlay for updated root\n")
		return nil
	}

	fmt.Println("  Setting up /etc overlay for updated root...")

	// With overlay persistence, user modifications are stored in /var/lib/nbc/etc-overlay/upper
	// and automatically apply to the new root's /etc when the overlay is mounted at boot.
	// We just need to ensure the overlay directories exist.

	overlayBase := filepath.Join(targetDir, "var", "lib", "nbc", "etc-overlay")
	upperDir := filepath.Join(overlayBase, "upper")
	workDir := filepath.Join(overlayBase, "work")

	// Check if overlay directories already exist (they should, on /var)
	if _, err := os.Stat(upperDir); os.IsNotExist(err) {
		fmt.Println("  Creating overlay directories...")
		if err := os.MkdirAll(upperDir, 0755); err != nil {
			return fmt.Errorf("failed to create overlay upper directory: %w", err)
		}
		if err := os.MkdirAll(workDir, 0755); err != nil {
			return fmt.Errorf("failed to create overlay work directory: %w", err)
		}
	} else {
		fmt.Println("  Overlay directories already exist (user modifications preserved)")
	}

	// Optionally check for conflicts: files modified by both user AND new container
	// This happens when the overlay upper has a file that also changed in the new container
	newEtc := filepath.Join(targetDir, "etc")
	pristineEtc := filepath.Join(targetDir, "var", "lib", "nbc", "etc.pristine")

	// Only check for conflicts if we have a pristine snapshot to compare against
	if _, err := os.Stat(pristineEtc); err == nil {
		conflicts := detectEtcConflicts(upperDir, newEtc, pristineEtc)
		if len(conflicts) > 0 {
			fmt.Println("  Warning: Potential conflicts detected (files modified by both user and update):")
			for _, conflict := range conflicts {
				fmt.Printf("    ! %s\n", conflict)
			}
			fmt.Println("  User modifications in overlay will take precedence over container changes.")
		}
	}

	fmt.Println("  /etc overlay configuration complete")
	return nil
}

// detectEtcConflicts finds files that exist in the overlay upper (user modified)
// AND have changed between the pristine snapshot and the new container's /etc.
func detectEtcConflicts(upperDir, newEtc, pristineEtc string) []string {
	var conflicts []string

	_ = filepath.Walk(upperDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(upperDir, path)

		// Check if file exists in new container's /etc
		newPath := filepath.Join(newEtc, relPath)
		pristinePath := filepath.Join(pristineEtc, relPath)

		_, newErr := os.Stat(newPath)
		_, pristineErr := os.Stat(pristinePath)

		// File exists in both new and pristine - check if container changed it
		if newErr == nil && pristineErr == nil {
			// Compare SHA256 hashes to detect changes
			newHash, newHashErr := hashFile(newPath)
			pristineHash, pristineHashErr := hashFile(pristinePath)
			if newHashErr == nil && pristineHashErr == nil && newHash != pristineHash {
				conflicts = append(conflicts, relPath)
			}
		} else if newErr == nil && pristineErr != nil {
			// File is new in container but user also added it
			conflicts = append(conflicts, relPath+" (new in container)")
		}

		return nil
	})

	return conflicts
}

// hashFile computes the SHA256 hash of a file and returns it as a hex string
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// EnsureCriticalFilesInOverlay ensures critical files that should persist across updates
// are captured in the overlay upper layer. This is necessary because some files
// (like SSH host keys) may exist in the container image from build time, meaning
// they never get written to the overlay upper layer during normal operation.
//
// When an A/B update happens with a new container image, the lower layer changes
// to the new container's /etc, and any files not in the overlay upper will show
// the new container's version instead of the running system's version.
//
// This function copies critical files from the running system's /etc to the overlay
// upper layer if they don't already exist there.
func EnsureCriticalFilesInOverlay(dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would ensure critical files are in overlay upper layer\n")
		return nil
	}

	fmt.Println("  Ensuring critical files are preserved in overlay...")

	// Critical files/directories that should persist across updates
	criticalPaths := []string{
		"ssh/ssh_host_ecdsa_key",
		"ssh/ssh_host_ecdsa_key.pub",
		"ssh/ssh_host_ed25519_key",
		"ssh/ssh_host_ed25519_key.pub",
		"ssh/ssh_host_rsa_key",
		"ssh/ssh_host_rsa_key.pub",
		"machine-id",
	}

	overlayUpper := "/var/lib/nbc/etc-overlay/upper"

	// Check if overlay is active
	if _, err := os.Stat(overlayUpper); os.IsNotExist(err) {
		return fmt.Errorf("overlay upper directory does not exist: %s", overlayUpper)
	}

	for _, relPath := range criticalPaths {
		srcPath := filepath.Join("/etc", relPath)
		dstPath := filepath.Join(overlayUpper, relPath)

		// Check if source exists
		srcInfo, err := os.Stat(srcPath)
		if os.IsNotExist(err) {
			continue // Source doesn't exist, skip
		}
		if err != nil {
			fmt.Printf("  Warning: could not stat %s: %v\n", srcPath, err)
			continue
		}

		// Check if already in overlay
		if _, err := os.Stat(dstPath); err == nil {
			continue // Already in overlay, skip
		}

		// Create parent directory if needed
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dstDir, err)
		}

		// Copy the file preserving permissions
		if err := copyFileWithOwnership(srcPath, dstPath, srcInfo); err != nil {
			return fmt.Errorf("failed to copy %s to overlay: %w", relPath, err)
		}

		fmt.Printf("  Preserved %s in overlay\n", relPath)
	}

	return nil
}

// copyFileWithOwnership copies a file preserving permissions and ownership
func copyFileWithOwnership(src, dst string, srcInfo os.FileInfo) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Try to preserve ownership (requires root)
	if stat, ok := srcInfo.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(dst, int(stat.Uid), int(stat.Gid))
	}

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
