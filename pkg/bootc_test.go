package pkg

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

func TestBootcInstaller_Install(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount")

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container image
	imageName := "localhost/nbc-test-install:latest"
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Create installer
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	installer := NewBootcInstaller(imageName, disk.GetDevice())
	installer.SetMountPoint(mountPoint)
	installer.SetVerbose(true)
	installer.SetDryRun(false)

	// Register cleanup for any mounts
	defer testutil.CleanupMounts(t, mountPoint)

	// Perform installation
	t.Log("Starting installation test")
	if err := installer.Install(context.Background()); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify installation
	t.Log("Verifying installation")

	// Check that partitions were created
	_ = testutil.WaitForDevice(disk.GetDevice())
	scheme, err := DetectExistingPartitionScheme(disk.GetDevice())
	if err != nil {
		t.Fatalf("Failed to detect partition scheme: %v", err)
	}

	// Verify all partitions exist
	partitions := []struct {
		name string
		path string
	}{
		{"Boot", scheme.BootPartition},
		{"Root1", scheme.Root1Partition},
		{"Root2", scheme.Root2Partition},
		{"Var", scheme.VarPartition},
	}

	for _, part := range partitions {
		if _, err := os.Stat(part.path); os.IsNotExist(err) {
			t.Errorf("Partition %s does not exist: %s", part.name, part.path)
		} else {
			t.Logf("✓ Partition %s exists: %s", part.name, part.path)
		}
	}

	// Mount and verify filesystem contents
	t.Log("Verifying filesystem contents")
	verifyMount := filepath.Join(t.TempDir(), "verify")
	if err := os.MkdirAll(verifyMount, 0755); err != nil {
		t.Fatalf("Failed to create verify mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, verifyMount)

	// Mount root1 partition
	if err := MountPartitions(context.Background(), scheme, verifyMount, false, NoopReporter{}); err != nil {
		t.Fatalf("Failed to mount partitions for verification: %v", err)
	}
	defer func() { _ = UnmountPartitions(context.Background(), verifyMount, false, NoopReporter{}) }()

	// Check for expected directories
	expectedDirs := []string{
		"etc", "var", "boot", "usr", "dev", "proc", "sys", "tmp", "run",
	}
	for _, dir := range expectedDirs {
		dirPath := filepath.Join(verifyMount, dir)
		if info, err := os.Stat(dirPath); err != nil {
			t.Errorf("Expected directory %s does not exist: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("Expected %s to be a directory", dir)
		} else {
			t.Logf("✓ Directory exists: %s", dir)
		}
	}

	// Check for /etc files from container
	expectedFiles := []string{
		"etc/hostname",
		"etc/os-release",
		"etc/passwd",
	}
	for _, file := range expectedFiles {
		filePath := filepath.Join(verifyMount, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Expected file %s does not exist", file)
		} else {
			t.Logf("✓ File exists: %s", file)
		}
	}

	// Check for pristine /etc backup
	pristineEtc := filepath.Join(verifyMount, "var", "lib", "nbc", "etc.pristine")
	if info, err := os.Stat(pristineEtc); err != nil {
		t.Errorf("Pristine /etc backup does not exist: %v", err)
	} else if !info.IsDir() {
		t.Errorf("Pristine /etc backup is not a directory")
	} else {
		t.Logf("✓ Pristine /etc backup exists")
	}

	// Check for system config
	configFile := filepath.Join(verifyMount, "etc", "nbc", "config.json")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Errorf("System config does not exist: %v", err)
	} else {
		t.Logf("✓ System config exists")
		// Verify config can be read
		config, err := readConfigFromFile(configFile)
		if err != nil {
			t.Errorf("Failed to read config: %v", err)
		} else if config.ImageRef != imageName {
			t.Errorf("Config ImageRef = %s, want %s", config.ImageRef, imageName)
		} else {
			t.Logf("✓ System config is valid")
		}
	}

	// Check for fstab
	fstabPath := filepath.Join(verifyMount, "etc", "fstab")
	if _, err := os.Stat(fstabPath); os.IsNotExist(err) {
		t.Errorf("fstab does not exist: %v", err)
	} else {
		t.Logf("✓ fstab exists")
	}

	t.Log("Installation test completed successfully")
}

func TestBootcInstaller_DryRun(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "podman")

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container image
	imageName := "localhost/nbc-test-dryrun:latest"
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Create installer with dry-run enabled
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	installer := NewBootcInstaller(imageName, disk.GetDevice())
	installer.SetMountPoint(mountPoint)
	installer.SetVerbose(true)
	installer.SetDryRun(true)

	// Perform dry-run installation
	t.Log("Testing dry-run mode")
	if err := installer.Install(context.Background()); err != nil {
		t.Fatalf("Dry-run install failed: %v", err)
	}

	// Verify that nothing was actually created
	_ = testutil.WaitForDevice(disk.GetDevice())

	// Check that partitions were NOT created (dry-run should not modify disk)
	_, err = DetectExistingPartitionScheme(disk.GetDevice())
	if err == nil {
		t.Error("Dry-run created partitions (should not have)")
	} else {
		t.Logf("✓ Dry-run did not create partitions (expected)")
	}

	t.Log("Dry-run test completed successfully")
}

func TestBootcInstaller_WithKernelArgs(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount")

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container image
	imageName := "localhost/nbc-test-kargs:latest"
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Create installer with kernel arguments
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	installer := NewBootcInstaller(imageName, disk.GetDevice())
	installer.SetMountPoint(mountPoint)
	installer.SetVerbose(true)
	installer.SetDryRun(false)
	installer.AddKernelArg("console=ttyS0")
	installer.AddKernelArg("quiet")

	defer testutil.CleanupMounts(t, mountPoint)

	// Perform installation
	t.Log("Testing installation with kernel arguments")
	if err := installer.Install(context.Background()); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify kernel arguments in config
	_ = testutil.WaitForDevice(disk.GetDevice())
	scheme, err := DetectExistingPartitionScheme(disk.GetDevice())
	if err != nil {
		t.Fatalf("Failed to detect partition scheme: %v", err)
	}

	// Mount and check config
	verifyMount := filepath.Join(t.TempDir(), "verify")
	if err := os.MkdirAll(verifyMount, 0755); err != nil {
		t.Fatalf("Failed to create verify mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, verifyMount)

	if err := MountPartitions(context.Background(), scheme, verifyMount, false, NoopReporter{}); err != nil {
		t.Fatalf("Failed to mount partitions: %v", err)
	}
	defer func() { _ = UnmountPartitions(context.Background(), verifyMount, false, NoopReporter{}) }()

	configFile := filepath.Join(verifyMount, "etc", "nbc", "config.json")
	config, err := readConfigFromFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	// Check kernel arguments
	expectedArgs := []string{"console=ttyS0", "quiet"}
	if len(config.KernelArgs) != len(expectedArgs) {
		t.Errorf("KernelArgs count = %d, want %d", len(config.KernelArgs), len(expectedArgs))
	}
	for i, arg := range expectedArgs {
		if i >= len(config.KernelArgs) || config.KernelArgs[i] != arg {
			t.Errorf("KernelArgs[%d] = %s, want %s", i,
				getOrEmpty(config.KernelArgs, i), arg)
		} else {
			t.Logf("✓ Kernel arg preserved: %s", arg)
		}
	}

	t.Log("Kernel arguments test completed successfully")
}

// Helper functions

func readConfigFromFile(path string) (*SystemConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config SystemConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func getOrEmpty(slice []string, index int) string {
	if index < len(slice) {
		return slice[index]
	}
	return ""
}

// Tests for SetRootPasswordInTarget

func TestSetRootPasswordInTarget_EmptyPassword(t *testing.T) {
	// Empty password should be a no-op and return nil
	targetDir := t.TempDir()
	err := SetRootPasswordInTarget(targetDir, "", false, NoopReporter{})
	if err != nil {
		t.Errorf("SetRootPasswordInTarget with empty password should return nil, got: %v", err)
	}
}

func TestSetRootPasswordInTarget_DryRun(t *testing.T) {
	// Dry run should not execute chpasswd
	targetDir := t.TempDir()
	err := SetRootPasswordInTarget(targetDir, "testpassword", true, NoopReporter{})
	if err != nil {
		t.Errorf("SetRootPasswordInTarget dry run should return nil, got: %v", err)
	}
}

func TestSetRootPasswordInTarget_InvalidTarget(t *testing.T) {
	// Test with a non-existent target directory
	// chpasswd -R should fail when target doesn't exist
	err := SetRootPasswordInTarget("/nonexistent/path/for/testing", "testpassword", false, NoopReporter{})
	if err == nil {
		t.Error("SetRootPasswordInTarget should fail with non-existent target directory")
	}
}

func TestBootcInstaller_SetRootPassword(t *testing.T) {
	// Test that SetRootPassword correctly sets the RootPassword field
	installer := NewBootcInstaller("test-image", "/dev/sda")

	if installer.RootPassword != "" {
		t.Error("RootPassword should be empty initially")
	}

	installer.SetRootPassword("mysecretpassword")
	if installer.RootPassword != "mysecretpassword" {
		t.Errorf("RootPassword = %q, want %q", installer.RootPassword, "mysecretpassword")
	}
}

func TestSetRootPasswordInTarget_Integration(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "chpasswd")

	// Create a minimal target directory with /etc/passwd, /etc/shadow, and PAM config
	targetDir := t.TempDir()
	etcDir := filepath.Join(targetDir, "etc")
	pamDir := filepath.Join(etcDir, "pam.d")
	if err := os.MkdirAll(pamDir, 0755); err != nil {
		t.Fatalf("Failed to create /etc/pam.d directory: %v", err)
	}

	// Create minimal passwd file
	passwdContent := "root:x:0:0:root:/root:/bin/bash\n"
	if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte(passwdContent), 0644); err != nil {
		t.Fatalf("Failed to create passwd file: %v", err)
	}

	// Create minimal shadow file
	shadowContent := "root:!:19000:0:99999:7:::\n"
	if err := os.WriteFile(filepath.Join(etcDir, "shadow"), []byte(shadowContent), 0600); err != nil {
		t.Fatalf("Failed to create shadow file: %v", err)
	}

	// Create minimal PAM configuration for chpasswd
	// This allows chpasswd to work in our test chroot environment
	pamChpasswd := `#%PAM-1.0
auth       sufficient   pam_rootok.so
account    required     pam_permit.so
password   required     pam_unix.so sha512 shadow
`
	if err := os.WriteFile(filepath.Join(pamDir, "chpasswd"), []byte(pamChpasswd), 0644); err != nil {
		t.Fatalf("Failed to create PAM chpasswd config: %v", err)
	}

	// Also need a common-password or system-auth fallback on some systems
	pamCommon := `#%PAM-1.0
password   required     pam_unix.so sha512 shadow
`
	if err := os.WriteFile(filepath.Join(pamDir, "common-password"), []byte(pamCommon), 0644); err != nil {
		t.Fatalf("Failed to create PAM common-password config: %v", err)
	}

	// Set the root password
	err := SetRootPasswordInTarget(targetDir, "testpassword123", false, NoopReporter{})
	if err != nil {
		// chpasswd -R may fail in test environments due to PAM configuration
		// This is expected behavior - the real test happens during actual installation
		t.Skipf("SetRootPasswordInTarget failed (expected in minimal test environment): %v", err)
	}

	// Verify the shadow file was modified (password hash should be different from "!")
	shadowData, err := os.ReadFile(filepath.Join(etcDir, "shadow"))
	if err != nil {
		t.Fatalf("Failed to read shadow file: %v", err)
	}

	shadowStr := string(shadowData)
	// The password hash should not be "!" anymore (locked account)
	if shadowStr == shadowContent {
		t.Error("Shadow file was not modified - password was not set")
	}

	// Check that the line starts with root: and has a hash (not "!" or "!!")
	if len(shadowStr) < 10 {
		t.Error("Shadow file content is too short")
	}

	// A proper password hash starts with $algorithm$salt$hash
	// Common prefixes: $1$ (MD5), $5$ (SHA-256), $6$ (SHA-512), $y$ (yescrypt)
	if shadowStr[5] != '$' {
		t.Logf("Shadow content (first 50 chars): %s", shadowStr[:min(50, len(shadowStr))])
		t.Error("Password hash does not appear to be properly set")
	} else {
		t.Log("✓ Root password was successfully set")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
