package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDisk represents a test disk image with loop device
type TestDisk struct {
	ImagePath  string
	LoopDevice string
	Size       int64 // Size in bytes
	Mounted    bool
	t          *testing.T
}

// CreateTestDisk creates a disk image file and attaches it to a loop device
func CreateTestDisk(t *testing.T, sizeGB int) (*TestDisk, error) {
	t.Helper()

	// Create temporary file for disk image
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test-disk.img")

	sizeBytes := int64(sizeGB) * 1024 * 1024 * 1024

	// Create sparse file
	t.Logf("Creating %dGB test disk image: %s", sizeGB, imagePath)
	f, err := os.Create(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create image file: %w", err)
	}
	if err := f.Truncate(sizeBytes); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to truncate image file: %w", err)
	}
	_ = f.Close()

	// Attach to loop device with partition scanning enabled
	cmd := exec.Command("losetup", "--find", "--show", "--partscan", imagePath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to attach loop device (are you root?): %w", err)
	}

	loopDevice := strings.TrimSpace(string(output))
	t.Logf("Attached loop device: %s", loopDevice)

	disk := &TestDisk{
		ImagePath:  imagePath,
		LoopDevice: loopDevice,
		Size:       sizeBytes,
		t:          t,
	}

	// Register cleanup
	t.Cleanup(func() {
		disk.Cleanup()
	})

	return disk, nil
}

// Cleanup detaches the loop device and removes the image file
func (d *TestDisk) Cleanup() {
	if d.LoopDevice != "" {
		d.t.Logf("Detaching loop device: %s", d.LoopDevice)
		cmd := exec.Command("losetup", "-d", d.LoopDevice)
		if err := cmd.Run(); err != nil {
			d.t.Logf("Warning: failed to detach loop device %s: %v", d.LoopDevice, err)
		}
		d.LoopDevice = ""
	}
}

// GetDevice returns the loop device path (e.g., /dev/loop0)
func (d *TestDisk) GetDevice() string {
	return d.LoopDevice
}

// RequireRoot skips the test if not running as root
func RequireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges (sudo)")
	}
}

// CheckToolExists checks if a required tool is available
func CheckToolExists(t *testing.T, tool string) {
	t.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		t.Skipf("Required tool not found: %s", tool)
	}
}

// RequireTools checks for required tools and skips if any are missing
func RequireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		CheckToolExists(t, tool)
	}
}

// CreateMockContainer creates a minimal container image for testing
func CreateMockContainer(t *testing.T, imageName string) error {
	t.Helper()

	// Create a temporary directory with minimal root filesystem
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "rootfs")

	// Create basic directory structure
	dirs := []string{
		"etc", "var", "boot", "usr/bin", "usr/lib", "usr/share",
		"usr/lib/modules/6.6.0-test",
		"usr/lib/systemd/boot/efi",
		"dev", "proc", "sys", "tmp", "run", "home", "root",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(rootDir, dir), 0755); err != nil {
			return fmt.Errorf("failed to create dir %s: %w", dir, err)
		}
	}

	// Create minimal /etc files
	etcFiles := map[string]string{
		"etc/hostname":   "test-container\n",
		"etc/os-release": "ID=test\nNAME=\"Test OS\"\nVERSION_ID=1.0\nPRETTY_NAME=\"Test OS 1.0\"\n",
		"etc/passwd":     "root:x:0:0:root:/root:/bin/sh\n",
		"etc/group":      "root:x:0:\n",
		"etc/shells":     "/bin/sh\n/bin/bash\n",
	}

	for path, content := range etcFiles {
		fullPath := filepath.Join(rootDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	// Create mock kernel and initramfs in /usr/lib/modules (new bootc layout)
	kernelFiles := map[string]string{
		"usr/lib/modules/6.6.0-test/vmlinuz":       "MOCK_KERNEL_IMAGE\n",
		"usr/lib/modules/6.6.0-test/initramfs.img": "MOCK_INITRAMFS\n",
	}

	for path, content := range kernelFiles {
		fullPath := filepath.Join(rootDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	// Create mock systemd-boot EFI binary (needed for bootloader installation)
	efiPath := filepath.Join(rootDir, "usr/lib/systemd/boot/efi/systemd-bootx64.efi")
	if err := os.WriteFile(efiPath, []byte("MOCK_SYSTEMD_BOOT_EFI\n"), 0644); err != nil {
		return fmt.Errorf("failed to write systemd-boot EFI: %w", err)
	}

	// Create a Dockerfile
	dockerfile := filepath.Join(tmpDir, "Dockerfile")
	dockerfileContent := `FROM scratch
COPY rootfs/ /
CMD ["/bin/sh"]
`
	if err := os.WriteFile(dockerfile, []byte(dockerfileContent), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Build container with podman
	t.Logf("Building test container image: %s", imageName)
	cmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfile, tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build container: %w\nOutput: %s", err, string(output))
	}

	// Register cleanup to remove image
	t.Cleanup(func() {
		t.Logf("Removing test container image: %s", imageName)
		_ = exec.Command("podman", "rmi", "-f", imageName).Run()
	})

	return nil
}

// WaitForDevice waits for a device to appear (useful after partitioning)
func WaitForDevice(device string) error {
	// For loop devices, force partition rescan using partx
	// Note: losetup --partscan only works during initial setup, not on existing devices
	if strings.HasPrefix(filepath.Base(device), "loop") {
		cmd := exec.Command("partx", "-u", device)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("partx -u failed: %w", err)
		}
	}

	// Also run partprobe to inform kernel of partition changes
	// partprobe may fail but device could still work, so we ignore errors
	cmd := exec.Command("partprobe", device)
	_ = cmd.Run()

	// Wait for udev to settle
	cmd = exec.Command("udevadm", "settle")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("udevadm settle failed: %w", err)
	}

	return nil
}

// BuildContainerFromDir builds a container image from a directory
func BuildContainerFromDir(t *testing.T, rootDir, imageName string) error {
	t.Helper()

	tmpDir := filepath.Dir(rootDir)

	// Create a Dockerfile
	dockerfile := filepath.Join(tmpDir, "Dockerfile")
	dockerfileContent := `FROM scratch
COPY rootfs/ /
CMD ["/bin/sh"]
`
	if err := os.WriteFile(dockerfile, []byte(dockerfileContent), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Build container with podman
	t.Logf("Building container image: %s", imageName)
	cmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfile, tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build container: %w\nOutput: %s", err, string(output))
	}

	// Register cleanup to remove image
	t.Cleanup(func() {
		t.Logf("Removing container image: %s", imageName)
		_ = exec.Command("podman", "rmi", "-f", imageName).Run()
	})

	return nil
}

// CleanupMounts unmounts any mounts under a directory
func CleanupMounts(t *testing.T, mountPoint string) {
	t.Helper()

	// Find all mounts under mountPoint
	cmd := exec.Command("mount")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Warning: failed to list mounts: %v", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, mountPoint) {
			// Extract mount point (format: "device on /path type fstype")
			parts := strings.Split(line, " on ")
			if len(parts) < 2 {
				continue
			}
			mountParts := strings.Split(parts[1], " type ")
			if len(mountParts) < 1 {
				continue
			}
			mount := mountParts[0]

			t.Logf("Unmounting: %s", mount)
			cmd := exec.Command("umount", "-f", mount)
			if err := cmd.Run(); err != nil {
				t.Logf("Warning: failed to unmount %s: %v", mount, err)
			}
		}
	}
}
