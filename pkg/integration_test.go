package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

// TestIntegration_PartitionAndFormat tests the full partition and format flow
func TestIntegration_PartitionAndFormat(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "blkid")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create partitions
	t.Log("Creating partitions...")
	scheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	// Verify A/B partition scheme
	if scheme.Root1Partition == "" || scheme.Root2Partition == "" {
		t.Error("A/B root partitions not created")
	}
	if scheme.BootPartition == "" {
		t.Error("Boot partition not created")
	}
	if scheme.VarPartition == "" {
		t.Error("Var partition not created")
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	// Format partitions
	t.Log("Formatting partitions...")
	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	// Verify all partitions have UUIDs
	partitions := map[string]string{
		"boot":  scheme.BootPartition,
		"root1": scheme.Root1Partition,
		"root2": scheme.Root2Partition,
		"var":   scheme.VarPartition,
	}

	for name, part := range partitions {
		uuid, err := GetPartitionUUID(part)
		if err != nil {
			t.Errorf("Failed to get UUID for %s: %v", name, err)
		}
		if uuid == "" {
			t.Errorf("Empty UUID for %s", name)
		}
		t.Logf("  %s UUID: %s", name, uuid)
	}
}

// TestIntegration_MountUnmount tests mounting and unmounting partitions
func TestIntegration_MountUnmount(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "mount", "umount")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	// Create mount point
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	// Mount partitions
	t.Log("Mounting partitions...")
	if err := MountPartitions(scheme, mountPoint, false); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}

	// Verify mounts exist
	expectedMounts := []string{
		mountPoint,                        // root
		filepath.Join(mountPoint, "var"),  // var
		filepath.Join(mountPoint, "boot"), // boot
	}

	for _, mp := range expectedMounts {
		if _, err := os.Stat(mp); err != nil {
			t.Errorf("Mount point %s not accessible: %v", mp, err)
		}
	}

	// Test writing a file to verify mounts are writable
	testFile := filepath.Join(mountPoint, "test-file")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Errorf("Failed to write test file: %v", err)
	}

	// Unmount partitions
	t.Log("Unmounting partitions...")
	if err := UnmountPartitions(mountPoint, false); err != nil {
		t.Errorf("UnmountPartitions failed: %v", err)
	}
}

// TestIntegration_DetectPartitionScheme tests detection of existing partitions
func TestIntegration_DetectPartitionScheme(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create partitions
	originalScheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	// Detect the scheme
	t.Log("Detecting existing partition scheme...")
	detectedScheme, err := DetectExistingPartitionScheme(disk.GetDevice())
	if err != nil {
		t.Fatalf("DetectExistingPartitionScheme failed: %v", err)
	}

	// Verify detected partitions match original
	if detectedScheme.BootPartition != originalScheme.BootPartition {
		t.Errorf("Boot partition mismatch: got %s, want %s",
			detectedScheme.BootPartition, originalScheme.BootPartition)
	}
	if detectedScheme.Root1Partition != originalScheme.Root1Partition {
		t.Errorf("Root1 partition mismatch: got %s, want %s",
			detectedScheme.Root1Partition, originalScheme.Root1Partition)
	}
	if detectedScheme.Root2Partition != originalScheme.Root2Partition {
		t.Errorf("Root2 partition mismatch: got %s, want %s",
			detectedScheme.Root2Partition, originalScheme.Root2Partition)
	}
	if detectedScheme.VarPartition != originalScheme.VarPartition {
		t.Errorf("Var partition mismatch: got %s, want %s",
			detectedScheme.VarPartition, originalScheme.VarPartition)
	}
}

// TestIntegration_EtcOverlaySetup tests /etc overlay directory creation
func TestIntegration_EtcOverlaySetup(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "mount", "umount")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(scheme, mountPoint, false); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(mountPoint, false)
	}()

	// Create /etc with required content
	etcDir := filepath.Join(mountPoint, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create /etc: %v", err)
	}
	for _, f := range []string{"passwd", "group", "os-release"} {
		if err := os.WriteFile(filepath.Join(etcDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", f, err)
		}
	}

	// Setup /etc overlay
	t.Log("Setting up /etc overlay...")
	if err := SetupEtcOverlay(mountPoint, false); err != nil {
		t.Fatalf("SetupEtcOverlay failed: %v", err)
	}

	// Verify overlay directories were created
	overlayDirs := []string{
		filepath.Join(mountPoint, "var", "lib", "nbc", "etc-overlay", "upper"),
		filepath.Join(mountPoint, "var", "lib", "nbc", "etc-overlay", "work"),
	}

	for _, dir := range overlayDirs {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Overlay directory not created: %s", dir)
		}
	}
}

// TestIntegration_TmpfilesConfig tests tmpfiles.d config installation
func TestIntegration_TmpfilesConfig(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "mount", "umount")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(scheme, mountPoint, false); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(mountPoint, false)
	}()

	// Install tmpfiles.d config
	t.Log("Installing tmpfiles.d config...")
	if err := InstallTmpfilesConfig(mountPoint, false); err != nil {
		t.Fatalf("InstallTmpfilesConfig failed: %v", err)
	}

	// Verify config was created
	tmpfilesPath := filepath.Join(mountPoint, "usr", "lib", "tmpfiles.d", "nbc.conf")
	content, err := os.ReadFile(tmpfilesPath)
	if err != nil {
		t.Fatalf("Failed to read tmpfiles.d config: %v", err)
	}

	if !containsString(string(content), "/run/nbc-booted") {
		t.Error("tmpfiles.d config missing /run/nbc-booted reference")
	}
}

// TestIntegration_SystemConfig tests system config read/write
func TestIntegration_SystemConfig(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "mount", "umount")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(scheme, mountPoint, false); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(mountPoint, false)
	}()

	// Write system config
	config := &SystemConfig{
		ImageRef:       "ghcr.io/test/bootc:latest",
		ImageDigest:    "sha256:abc123",
		Device:         disk.GetDevice(),
		InstallDate:    "2025-01-01T00:00:00Z",
		BootloaderType: "systemd-boot",
		FilesystemType: "ext4",
	}

	t.Log("Writing system config...")
	if err := WriteSystemConfigToTarget(mountPoint, config, false); err != nil {
		t.Fatalf("WriteSystemConfigToTarget failed: %v", err)
	}

	// Verify config file exists
	configPath := filepath.Join(mountPoint, "etc", "nbc", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Read and verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if !containsString(string(data), "ghcr.io/test/bootc:latest") {
		t.Error("Config missing image reference")
	}
	if !containsString(string(data), "systemd-boot") {
		t.Error("Config missing bootloader type")
	}
}

// Helper function
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestIntegration_ValidateLUKSInitramfs tests LUKS support detection
func TestIntegration_ValidateLUKSInitramfs(t *testing.T) {
	// This test doesn't require root - just filesystem operations
	targetDir := t.TempDir()

	t.Run("detects missing LUKS support", func(t *testing.T) {
		warnings := ValidateInitramfsSupport(targetDir, false)
		if len(warnings) == 0 {
			t.Error("Should warn about missing LUKS support")
		}
	})

	t.Run("detects dracut crypt module", func(t *testing.T) {
		cryptDir := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "90crypt")
		if err := os.MkdirAll(cryptDir, 0755); err != nil {
			t.Fatalf("Failed to create crypt dir: %v", err)
		}

		warnings := ValidateInitramfsSupport(targetDir, false)
		for _, w := range warnings {
			if containsString(w, "LUKS initramfs support") {
				t.Error("Should not warn when crypt module exists")
			}
		}
	})
}

// TestIntegration_Crypttab tests crypttab generation
func TestIntegration_Crypttab(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "mount", "umount")

	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(disk.GetDevice(), false)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(scheme, mountPoint, false); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(mountPoint, false)
	}()

	// Generate crypttab content
	devices := []*LUKSDevice{
		{MapperName: "root1", LUKSUUID: "test-uuid-root1"},
		{MapperName: "var", LUKSUUID: "test-uuid-var"},
	}

	crypttab := GenerateCrypttab(devices, true)

	// Write to target
	etcDir := filepath.Join(mountPoint, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create /etc: %v", err)
	}

	crypttabPath := filepath.Join(etcDir, "crypttab")
	if err := os.WriteFile(crypttabPath, []byte(crypttab), 0644); err != nil {
		t.Fatalf("Failed to write crypttab: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(crypttabPath)
	if err != nil {
		t.Fatalf("Failed to read crypttab: %v", err)
	}

	if !containsString(string(content), "tpm2-device=auto") {
		t.Error("crypttab missing TPM2 option")
	}
	if !containsString(string(content), "UUID=test-uuid-root1") {
		t.Error("crypttab missing root1 UUID")
	}
}

// RequireCommand checks if a command is available
func RequireCommand(t *testing.T, cmd string) {
	t.Helper()
	if _, err := exec.LookPath(cmd); err != nil {
		t.Skipf("Command %s not available", cmd)
	}
}
