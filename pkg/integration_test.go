package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	scheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, NoopReporter{})
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
	if err := FormatPartitions(t.Context(), scheme, false, NoopReporter{}); err != nil {
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
		uuid, err := GetPartitionUUID(t.Context(), part)
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

	scheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(t.Context(), scheme, false, NoopReporter{}); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	// Create mount point
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	// Mount partitions
	t.Log("Mounting partitions...")
	if err := MountPartitions(t.Context(), scheme, mountPoint, false, NoopReporter{}); err != nil {
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
	if err := UnmountPartitions(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
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
	originalScheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, NoopReporter{})
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

	scheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	progress := NoopReporter{}
	if err := FormatPartitions(t.Context(), scheme, false, progress); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(t.Context(), scheme, mountPoint, false, progress); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(t.Context(), mountPoint, false, progress)
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
	if err := SetupEtcOverlay(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
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

	scheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	progress := NoopReporter{}
	if err := FormatPartitions(t.Context(), scheme, false, progress); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(t.Context(), scheme, mountPoint, false, progress); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(t.Context(), mountPoint, false, progress)
	}()

	// Install tmpfiles.d config
	t.Log("Installing tmpfiles.d config...")
	if err := InstallTmpfilesConfig(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
		t.Fatalf("InstallTmpfilesConfig failed: %v", err)
	}

	// Verify config was created
	tmpfilesPath := filepath.Join(mountPoint, "usr", "lib", "tmpfiles.d", "nbc.conf")
	content, err := os.ReadFile(tmpfilesPath)
	if err != nil {
		t.Fatalf("Failed to read tmpfiles.d config: %v", err)
	}

	if !strings.Contains(string(content), "/run/nbc-booted") {
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

	scheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	progress := NoopReporter{}
	if err := FormatPartitions(t.Context(), scheme, false, progress); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(t.Context(), scheme, mountPoint, false, progress); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(t.Context(), mountPoint, false, progress)
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

	t.Log("Writing system config to var partition...")
	varMountPoint := filepath.Join(mountPoint, "var")
	if err := WriteSystemConfigToVar(t.Context(), varMountPoint, config, false, NoopReporter{}); err != nil {
		t.Fatalf("WriteSystemConfigToVar failed: %v", err)
	}

	// Verify config file exists in /var/lib/nbc/state/
	configPath := filepath.Join(varMountPoint, "lib", "nbc", "state", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Read and verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if !strings.Contains(string(data), "ghcr.io/test/bootc:latest") {
		t.Error("Config missing image reference")
	}
	if !strings.Contains(string(data), "systemd-boot") {
		t.Error("Config missing bootloader type")
	}
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
			if strings.Contains(w, "LUKS initramfs support") {
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

	progress := NoopReporter{}
	scheme, err := CreatePartitions(t.Context(), disk.GetDevice(), false, progress)
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(t.Context(), scheme, false, progress); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	if err := MountPartitions(t.Context(), scheme, mountPoint, false, progress); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}
	defer func() {
		_ = UnmountPartitions(t.Context(), mountPoint, false, progress)
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

	if !strings.Contains(string(content), "tpm2-device=auto") {
		t.Error("crypttab missing TPM2 option")
	}
	if !strings.Contains(string(content), "UUID=test-uuid-root1") {
		t.Error("crypttab missing root1 UUID")
	}
}

// TestIntegration_PopulateEtcLower tests that .etc.lower is populated during install
func TestIntegration_PopulateEtcLower_Install(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "rsync")

	// Create temporary mount point
	mountPoint := t.TempDir()

	// Create /etc directory with some files
	etcDir := filepath.Join(mountPoint, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create /etc: %v", err)
	}

	// Create test files in /etc
	testFiles := map[string]string{
		"passwd":      "root:x:0:0:root:/root:/bin/bash\n",
		"group":       "root:x:0:\n",
		"os-release":  "NAME=TestOS\nVERSION=1.0\n",
		"hostname":    "test-host\n",
		"resolv.conf": "nameserver 8.8.8.8\n",
	}

	for name, content := range testFiles {
		path := filepath.Join(etcDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	// Create subdirectory with file
	sshDir := filepath.Join(etcDir, "ssh")
	if err := os.MkdirAll(sshDir, 0755); err != nil {
		t.Fatalf("Failed to create ssh dir: %v", err)
	}
	keyPath := filepath.Join(sshDir, "ssh_host_rsa_key")
	if err := os.WriteFile(keyPath, []byte("fake-private-key"), 0600); err != nil {
		t.Fatalf("Failed to create ssh key: %v", err)
	}

	// Run PopulateEtcLower
	if err := PopulateEtcLower(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
		t.Fatalf("PopulateEtcLower failed: %v", err)
	}

	// Verify .etc.lower exists and contains all files
	etcLower := filepath.Join(mountPoint, ".etc.lower")
	info, err := os.Stat(etcLower)
	if err != nil {
		t.Fatalf(".etc.lower does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".etc.lower is not a directory")
	}

	// Check all test files were copied
	for name, expectedContent := range testFiles {
		path := filepath.Join(etcLower, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("File %s missing in .etc.lower: %v", name, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("File %s content mismatch in .etc.lower:\ngot: %q\nwant: %q", name, string(content), expectedContent)
		}
	}

	// Check subdirectory and file
	lowerKeyPath := filepath.Join(etcLower, "ssh", "ssh_host_rsa_key")
	keyContent, err := os.ReadFile(lowerKeyPath)
	if err != nil {
		t.Errorf("SSH key missing in .etc.lower: %v", err)
	} else if string(keyContent) != "fake-private-key" {
		t.Error("SSH key content mismatch in .etc.lower")
	}

	// Verify permissions preserved on private key
	keyInfo, err := os.Stat(lowerKeyPath)
	if err != nil {
		t.Fatalf("Cannot stat key in .etc.lower: %v", err)
	}
	if keyInfo.Mode().Perm() != 0600 {
		t.Errorf("Key permissions not preserved: got %o, want 0600", keyInfo.Mode().Perm())
	}

	// Count total files in .etc.lower
	var fileCount int
	err = filepath.Walk(etcLower, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			fileCount++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk .etc.lower: %v", err)
	}

	expectedFileCount := len(testFiles) + 1 // +1 for ssh_host_rsa_key
	if fileCount != expectedFileCount {
		t.Errorf("File count mismatch in .etc.lower: got %d, want %d", fileCount, expectedFileCount)
	}

	t.Logf("✓ .etc.lower populated with %d files", fileCount)
}

// TestIntegration_PopulateEtcLower_Update tests that .etc.lower is repopulated during update
func TestIntegration_PopulateEtcLower_Update(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "rsync")

	mountPoint := t.TempDir()

	// Simulate first installation with original /etc
	etcDir := filepath.Join(mountPoint, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("Failed to create /etc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("original-passwd"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "file1"), []byte("original-file1"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create old .etc.lower
	if err := PopulateEtcLower(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
		t.Fatalf("Initial PopulateEtcLower failed: %v", err)
	}

	etcLower := filepath.Join(mountPoint, ".etc.lower")

	// Verify old content exists
	oldContent, err := os.ReadFile(filepath.Join(etcLower, "passwd"))
	if err != nil {
		t.Fatal("Old passwd missing")
	}
	if string(oldContent) != "original-passwd" {
		t.Error("Old passwd content wrong")
	}

	// Simulate update: change /etc content (new container image)
	if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("updated-passwd"), 0644); err != nil {
		t.Fatal(err)
	}
	// Remove file1 from new image
	if err := os.Remove(filepath.Join(etcDir, "file1")); err != nil {
		t.Fatal(err)
	}
	// Add new file2 in new image
	if err := os.WriteFile(filepath.Join(etcDir, "file2"), []byte("new-file2"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run PopulateEtcLower again (simulating update)
	if err := PopulateEtcLower(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
		t.Fatalf("Update PopulateEtcLower failed: %v", err)
	}

	// Verify .etc.lower now has updated content
	newContent, err := os.ReadFile(filepath.Join(etcLower, "passwd"))
	if err != nil {
		t.Fatal("Updated passwd missing")
	}
	if string(newContent) != "updated-passwd" {
		t.Errorf("passwd not updated in .etc.lower: got %q, want %q", string(newContent), "updated-passwd")
	}

	// Verify file1 was removed (rsync --delete)
	if _, err := os.Stat(filepath.Join(etcLower, "file1")); !os.IsNotExist(err) {
		t.Error("file1 should have been deleted from .etc.lower")
	}

	// Verify file2 was added
	file2Content, err := os.ReadFile(filepath.Join(etcLower, "file2"))
	if err != nil {
		t.Error("file2 missing in updated .etc.lower")
	} else if string(file2Content) != "new-file2" {
		t.Error("file2 content wrong in .etc.lower")
	}

	t.Log("✓ .etc.lower successfully updated with new container /etc")
}

// TestIntegration_EtcLowerWithSymlinks tests symlink handling in .etc.lower
func TestIntegration_EtcLowerWithSymlinks(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "rsync")

	mountPoint := t.TempDir()

	// Create /etc with symlinks
	etcDir := filepath.Join(mountPoint, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create target file
	targetFile := filepath.Join(etcDir, "timezone.real")
	if err := os.WriteFile(targetFile, []byte("America/New_York"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink
	symlink := filepath.Join(etcDir, "localtime")
	if err := os.Symlink("timezone.real", symlink); err != nil {
		t.Fatal(err)
	}

	// Create absolute symlink
	absSymlink := filepath.Join(etcDir, "resolv.conf")
	if err := os.Symlink("/run/systemd/resolve/stub-resolv.conf", absSymlink); err != nil {
		t.Fatal(err)
	}

	// Populate .etc.lower
	if err := PopulateEtcLower(t.Context(), mountPoint, false, NoopReporter{}); err != nil {
		t.Fatalf("PopulateEtcLower failed: %v", err)
	}

	etcLower := filepath.Join(mountPoint, ".etc.lower")

	// Verify relative symlink was copied
	lowerSymlink := filepath.Join(etcLower, "localtime")
	info, err := os.Lstat(lowerSymlink)
	if err != nil {
		t.Fatalf("localtime symlink missing in .etc.lower: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("localtime is not a symlink in .etc.lower")
	}
	target, err := os.Readlink(lowerSymlink)
	if err != nil {
		t.Fatalf("Cannot read localtime symlink: %v", err)
	}
	if target != "timezone.real" {
		t.Errorf("Symlink target wrong: got %q, want %q", target, "timezone.real")
	}

	// Verify absolute symlink was copied
	lowerAbsSymlink := filepath.Join(etcLower, "resolv.conf")
	absTarget, err := os.Readlink(lowerAbsSymlink)
	if err != nil {
		t.Fatalf("Cannot read resolv.conf symlink: %v", err)
	}
	if absTarget != "/run/systemd/resolve/stub-resolv.conf" {
		t.Errorf("Absolute symlink target wrong: got %q", absTarget)
	}

	t.Log("✓ Symlinks correctly preserved in .etc.lower")
}

// RequireCommand checks if a command is available
func RequireCommand(t *testing.T, cmd string) {
	t.Helper()
	if _, err := exec.LookPath(cmd); err != nil {
		t.Skipf("Command %s not available", cmd)
	}
}
