package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

func TestSystemUpdater_Update(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount", "rsync")

	// Step 1: Install initial system
	t.Log("Step 1: Installing initial system")
	disk, scheme, _ := installTestSystem(t, "v1")

	// Step 2: Modify /etc to simulate user changes
	t.Log("Step 2: Modifying /etc to simulate user changes")
	modifyEtcOnRoot1(t, scheme)

	// Step 3: Create updated container image
	t.Log("Step 3: Creating updated container image")
	updatedImageName := "localhost/nbc-test-update:v2"
	if err := createUpdatedMockContainer(t, updatedImageName); err != nil {
		t.Fatalf("Failed to create updated container: %v", err)
	}

	// Step 4: Perform update
	t.Log("Step 4: Performing system update")
	updater := NewSystemUpdater(disk.GetDevice(), updatedImageName)
	updater.SetVerbose(true)
	updater.SetDryRun(false)
	updater.SetForce(true)

	// Skip pull since we're using a local test image
	if err := updater.PerformUpdate(true); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Step 5: Verify update
	t.Log("Step 5: Verifying update")
	verifyUpdate(t, scheme, updatedImageName)

	// Step 6: Verify /etc persistence
	t.Log("Step 6: Verifying /etc persistence")
	verifyEtcPersistence(t, scheme)

	t.Log("Update test completed successfully")
}

func TestSystemUpdater_EtcPersistence(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount", "rsync")

	// Install initial system
	t.Log("Installing initial system")
	disk, scheme, _ := installTestSystem(t, "v1")

	// Mount root1 and modify /etc
	t.Log("Modifying /etc configuration")
	mountPoint := filepath.Join(t.TempDir(), "root1")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, mountPoint)

	if err := mountSinglePartition(scheme.Root1Partition, mountPoint); err != nil {
		t.Fatalf("Failed to mount root1: %v", err)
	}
	defer func() { _ = unmountSinglePartition(mountPoint) }()

	// Create a custom config file
	customConfigPath := filepath.Join(mountPoint, "etc", "custom.conf")
	customContent := "# Custom configuration\ntest=value\n"
	if err := os.WriteFile(customConfigPath, []byte(customContent), 0644); err != nil {
		t.Fatalf("Failed to write custom config: %v", err)
	}

	// Modify existing file
	hostnameOverride := "my-custom-hostname\n"
	hostnamePath := filepath.Join(mountPoint, "etc", "hostname")
	if err := os.WriteFile(hostnamePath, []byte(hostnameOverride), 0644); err != nil {
		t.Fatalf("Failed to modify hostname: %v", err)
	}

	_ = unmountSinglePartition(mountPoint)

	// Create new container image
	t.Log("Creating updated container")
	newImageName := "localhost/nbc-test-etc:v2"
	if err := createUpdatedMockContainer(t, newImageName); err != nil {
		t.Fatalf("Failed to create new container: %v", err)
	}

	// Perform update
	t.Log("Performing update")
	updater := NewSystemUpdater(disk.GetDevice(), newImageName)
	updater.SetVerbose(true)
	updater.SetDryRun(false)
	updater.SetForce(true)

	// Skip pull since we're using a local test image
	if err := updater.PerformUpdate(true); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify custom config persisted to root2
	t.Log("Verifying /etc persistence")
	verifyMount := filepath.Join(t.TempDir(), "root2")
	if err := os.MkdirAll(verifyMount, 0755); err != nil {
		t.Fatalf("Failed to create verify mount: %v", err)
	}
	defer testutil.CleanupMounts(t, verifyMount)

	if err := mountSinglePartition(scheme.Root2Partition, verifyMount); err != nil {
		t.Fatalf("Failed to mount root2: %v", err)
	}
	defer func() { _ = unmountSinglePartition(verifyMount) }()

	// Check custom config exists
	customConfigPath2 := filepath.Join(verifyMount, "etc", "custom.conf")
	content, err := os.ReadFile(customConfigPath2)
	if err != nil {
		t.Errorf("Custom config not found in root2: %v", err)
	} else if string(content) != customContent {
		t.Errorf("Custom config content mismatch: got %q, want %q", string(content), customContent)
	} else {
		t.Logf("✓ Custom config persisted: %s", customConfigPath2)
	}

	// Check hostname override
	hostnamePath2 := filepath.Join(verifyMount, "etc", "hostname")
	content, err = os.ReadFile(hostnamePath2)
	if err != nil {
		t.Errorf("Hostname not found in root2: %v", err)
	} else if string(content) != hostnameOverride {
		t.Errorf("Hostname content mismatch: got %q, want %q", string(content), hostnameOverride)
	} else {
		t.Logf("✓ Hostname override persisted")
	}

	t.Log("/etc persistence test completed successfully")
}

func TestSystemUpdater_DeviceFieldPersistence(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount")

	// Install initial system
	t.Log("Installing initial system")
	disk, scheme, _ := installTestSystem(t, "v1")

	// Verify initial config has correct device
	t.Log("Verifying initial config device")
	mountPoint := filepath.Join(t.TempDir(), "root1")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, mountPoint)

	if err := mountSinglePartition(scheme.Root1Partition, mountPoint); err != nil {
		t.Fatalf("Failed to mount root1: %v", err)
	}

	configPath := filepath.Join(mountPoint, "etc", "nbc", "config.json")
	initialConfig, err := readConfigFromFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read initial config: %v", err)
	}

	if initialConfig.Device != disk.GetDevice() {
		t.Errorf("Initial config device mismatch: got %q, want %q", initialConfig.Device, disk.GetDevice())
	}
	t.Logf("✓ Initial config has correct device: %s", initialConfig.Device)

	_ = unmountSinglePartition(mountPoint)

	// Create updated container image
	t.Log("Creating updated container")
	updatedImageName := "localhost/nbc-test-device:v2"
	if err := createUpdatedMockContainer(t, updatedImageName); err != nil {
		t.Fatalf("Failed to create updated container: %v", err)
	}

	// Perform update with the same device
	t.Log("Performing update with same device")
	updater := NewSystemUpdater(disk.GetDevice(), updatedImageName)
	updater.SetVerbose(true)
	updater.SetDryRun(false)
	updater.SetForce(true)

	if err := updater.PerformUpdate(true); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify config on root2 has correct device
	t.Log("Verifying updated config device on root2")
	verifyMount := filepath.Join(t.TempDir(), "root2")
	if err := os.MkdirAll(verifyMount, 0755); err != nil {
		t.Fatalf("Failed to create verify mount: %v", err)
	}
	defer testutil.CleanupMounts(t, verifyMount)

	if err := mountSinglePartition(scheme.Root2Partition, verifyMount); err != nil {
		t.Fatalf("Failed to mount root2: %v", err)
	}
	defer func() { _ = unmountSinglePartition(verifyMount) }()

	updatedConfigPath := filepath.Join(verifyMount, "etc", "nbc", "config.json")
	updatedConfig, err := readConfigFromFile(updatedConfigPath)
	if err != nil {
		t.Fatalf("Failed to read updated config: %v", err)
	}

	// Verify device field was correctly updated
	if updatedConfig.Device != disk.GetDevice() {
		t.Errorf("Updated config device mismatch: got %q, want %q", updatedConfig.Device, disk.GetDevice())
	} else {
		t.Logf("✓ Updated config has correct device: %s", updatedConfig.Device)
	}

	// Verify image reference was updated
	if updatedConfig.ImageRef != updatedImageName {
		t.Errorf("Updated config image mismatch: got %q, want %q", updatedConfig.ImageRef, updatedImageName)
	} else {
		t.Logf("✓ Updated config has correct image: %s", updatedConfig.ImageRef)
	}

	// Verify image digest was updated
	switch updatedConfig.ImageDigest {
	case "":
		t.Errorf("Updated config has empty digest")
	case initialConfig.ImageDigest:
		t.Errorf("Updated config digest unchanged: both are %q", updatedConfig.ImageDigest)
	default:
		t.Logf("✓ Updated config has new digest: %s", updatedConfig.ImageDigest)
	}

	t.Log("Device field persistence test completed successfully")
}

// Helper functions

func installTestSystem(t *testing.T, version string) (*testutil.TestDisk, *PartitionScheme, string) {
	t.Helper()

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container
	imageName := "localhost/nbc-test:" + version
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Install
	mountPoint := filepath.Join(t.TempDir(), "install")
	installer := NewBootcInstaller(imageName, disk.GetDevice())
	installer.SetMountPoint(mountPoint)
	installer.SetVerbose(true)
	installer.SetDryRun(false)

	defer testutil.CleanupMounts(t, mountPoint)

	if err := installer.Install(); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())
	scheme, err := DetectExistingPartitionScheme(disk.GetDevice())
	if err != nil {
		t.Fatalf("Failed to detect partition scheme: %v", err)
	}

	return disk, scheme, imageName
}

func modifyEtcOnRoot1(t *testing.T, scheme *PartitionScheme) {
	t.Helper()

	mountPoint := filepath.Join(t.TempDir(), "modify")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, mountPoint)

	if err := mountSinglePartition(scheme.Root1Partition, mountPoint); err != nil {
		t.Fatalf("Failed to mount root1: %v", err)
	}
	defer func() { _ = unmountSinglePartition(mountPoint) }()

	// Modify a file
	testFile := filepath.Join(mountPoint, "etc", "test-modified.conf")
	if err := os.WriteFile(testFile, []byte("modified=true\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	t.Logf("Modified /etc on root1")
}

func createUpdatedMockContainer(t *testing.T, imageName string) error {
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
			return err
		}
	}

	// Create /etc files with updated content
	etcFiles := map[string]string{
		"etc/hostname":     "updated-container\n",
		"etc/os-release":   "ID=test\nNAME=\"Updated Test OS\"\nVERSION_ID=2.0\nPRETTY_NAME=\"Updated Test OS 2.0\"\n",
		"etc/passwd":       "root:x:0:0:root:/root:/bin/sh\n",
		"etc/group":        "root:x:0:\n",
		"etc/shells":       "/bin/sh\n/bin/bash\n",
		"etc/updated.conf": "# This file is new in the update\nupdated=true\n",
	}

	for path, content := range etcFiles {
		fullPath := filepath.Join(rootDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return err
		}
	}

	// Create mock kernel and initramfs in /usr/lib/modules (new bootc layout)
	kernelFiles := map[string]string{
		"usr/lib/modules/6.6.0-test/vmlinuz":       "MOCK_KERNEL_IMAGE_V2\n",
		"usr/lib/modules/6.6.0-test/initramfs.img": "MOCK_INITRAMFS_V2\n",
	}

	for path, content := range kernelFiles {
		fullPath := filepath.Join(rootDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return err
		}
	}

	// Create mock systemd-boot EFI binary
	efiPath := filepath.Join(rootDir, "usr/lib/systemd/boot/efi/systemd-bootx64.efi")
	if err := os.WriteFile(efiPath, []byte("MOCK_SYSTEMD_BOOT_EFI_V2\n"), 0644); err != nil {
		return err
	}

	// Build container
	return testutil.BuildContainerFromDir(t, rootDir, imageName)
}

func verifyUpdate(t *testing.T, scheme *PartitionScheme, expectedImage string) {
	t.Helper()

	mountPoint := filepath.Join(t.TempDir(), "verify-update")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, mountPoint)

	// Mount root2 (target of update)
	if err := mountSinglePartition(scheme.Root2Partition, mountPoint); err != nil {
		t.Fatalf("Failed to mount root2: %v", err)
	}
	defer func() { _ = unmountSinglePartition(mountPoint) }()

	// Check for updated file
	updatedFile := filepath.Join(mountPoint, "etc", "updated.conf")
	if _, err := os.Stat(updatedFile); os.IsNotExist(err) {
		t.Errorf("Updated file not found: %s", updatedFile)
	} else {
		t.Logf("✓ Updated file exists: %s", updatedFile)
	}
}

func verifyEtcPersistence(t *testing.T, scheme *PartitionScheme) {
	t.Helper()

	mountPoint := filepath.Join(t.TempDir(), "verify-etc")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}
	defer testutil.CleanupMounts(t, mountPoint)

	// Mount root2
	if err := mountSinglePartition(scheme.Root2Partition, mountPoint); err != nil {
		t.Fatalf("Failed to mount root2: %v", err)
	}
	defer func() { _ = unmountSinglePartition(mountPoint) }()

	// Check that modified file persisted
	modifiedFile := filepath.Join(mountPoint, "etc", "test-modified.conf")
	if content, err := os.ReadFile(modifiedFile); os.IsNotExist(err) {
		t.Errorf("Modified file not found in root2: %s", modifiedFile)
	} else if err != nil {
		t.Errorf("Error reading modified file: %v", err)
	} else if !strings.Contains(string(content), "modified=true") {
		t.Errorf("Modified file content incorrect: %s", string(content))
	} else {
		t.Logf("✓ User-modified /etc file persisted")
	}
}

func mountSinglePartition(partition, mountPoint string) error {
	cmd := exec.Command("mount", partition, mountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mount failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func unmountSinglePartition(mountPoint string) error {
	_ = exec.Command("umount", mountPoint).Run()
	return nil
}

func TestBuildKernelCmdline_UpdaterWithBootMount(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4")

	// Create a test disk with actual partitions
	disk, err := testutil.CreateTestDisk(t, 30) // 30GB disk
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create partitions
	scheme, err := CreatePartitions(disk.GetDevice(), false, NewProgressReporter(false, 1))
	if err != nil {
		t.Fatalf("Failed to create partitions: %v", err)
	}

	// Format partitions so they have UUIDs
	if err := FormatPartitions(scheme, false); err != nil {
		t.Fatalf("Failed to format partitions: %v", err)
	}

	// Wait for devices to settle
	_ = testutil.WaitForDevice(scheme.BootPartition)
	_ = testutil.WaitForDevice(scheme.Root1Partition)
	_ = testutil.WaitForDevice(scheme.Root2Partition)
	_ = testutil.WaitForDevice(scheme.VarPartition)

	// Get actual UUIDs
	bootUUID, err := GetPartitionUUID(scheme.BootPartition)
	if err != nil {
		t.Fatalf("Failed to get boot UUID: %v", err)
	}
	root1UUID, err := GetPartitionUUID(scheme.Root1Partition)
	if err != nil {
		t.Fatalf("Failed to get root1 UUID: %v", err)
	}
	root2UUID, err := GetPartitionUUID(scheme.Root2Partition)
	if err != nil {
		t.Fatalf("Failed to get root2 UUID: %v", err)
	}
	varUUID, err := GetPartitionUUID(scheme.VarPartition)
	if err != nil {
		t.Fatalf("Failed to get var UUID: %v", err)
	}

	t.Run("non-encrypted includes boot mount for target root", func(t *testing.T) {
		updater := &SystemUpdater{
			Config: UpdaterConfig{
				Device: disk.GetDevice(),
			},
			Scheme: scheme,
			Active: true, // root1 is active, so target is root2
		}

		cmdline, err := updater.buildKernelCmdline(root2UUID, varUUID, "ext4", true)
		if err != nil {
			t.Fatalf("buildKernelCmdline failed: %v", err)
		}
		cmdlineStr := strings.Join(cmdline, " ")
		t.Logf("Generated target cmdline: %s", cmdlineStr)

		// Check for boot mount parameter
		expectedBootMount := fmt.Sprintf("systemd.mount-extra=UUID=%s:/boot:vfat:defaults", bootUUID)
		if !strings.Contains(cmdlineStr, expectedBootMount) {
			t.Errorf("Missing boot mount parameter.\nExpected: %s\nGot cmdline: %s", expectedBootMount, cmdlineStr)
		}

		// Check for var mount parameter
		expectedVarMount := fmt.Sprintf("systemd.mount-extra=UUID=%s:/var:", varUUID)
		if !strings.Contains(cmdlineStr, expectedVarMount) {
			t.Errorf("Missing var mount parameter.\nExpected substring: %s\nGot cmdline: %s", expectedVarMount, cmdlineStr)
		}

		// Check for root parameter (should be root2)
		expectedRoot := fmt.Sprintf("root=UUID=%s", root2UUID)
		if !strings.Contains(cmdlineStr, expectedRoot) {
			t.Errorf("Missing root parameter.\nExpected: %s\nGot cmdline: %s", expectedRoot, cmdlineStr)
		}
	})

	t.Run("non-encrypted includes boot mount for active root", func(t *testing.T) {
		updater := &SystemUpdater{
			Config: UpdaterConfig{
				Device: disk.GetDevice(),
			},
			Scheme: scheme,
			Active: true, // root1 is active
		}

		cmdline, err := updater.buildKernelCmdline(root1UUID, varUUID, "ext4", false)
		if err != nil {
			t.Fatalf("buildKernelCmdline failed: %v", err)
		}
		cmdlineStr := strings.Join(cmdline, " ")
		t.Logf("Generated active cmdline: %s", cmdlineStr)

		// Check for boot mount parameter
		expectedBootMount := fmt.Sprintf("systemd.mount-extra=UUID=%s:/boot:vfat:defaults", bootUUID)
		if !strings.Contains(cmdlineStr, expectedBootMount) {
			t.Errorf("Missing boot mount parameter.\nExpected: %s\nGot cmdline: %s", expectedBootMount, cmdlineStr)
		}

		// Check for root parameter (should be root1)
		expectedRoot := fmt.Sprintf("root=UUID=%s", root1UUID)
		if !strings.Contains(cmdlineStr, expectedRoot) {
			t.Errorf("Missing root parameter.\nExpected: %s\nGot cmdline: %s", expectedRoot, cmdlineStr)
		}
	})

	t.Run("encrypted includes boot mount", func(t *testing.T) {
		// Setup LUKS encryption
		passphrase := "test-passphrase"
		if err := SetupLUKS(scheme, passphrase, false, NewProgressReporter(false, 1)); err != nil {
			t.Fatalf("Failed to setup LUKS: %v", err)
		}
		defer scheme.CloseLUKSDevices()

		root1Dev := scheme.GetLUKSDevice("root1")
		root2Dev := scheme.GetLUKSDevice("root2")
		varDev := scheme.GetLUKSDevice("var")

		updater := &SystemUpdater{
			Config: UpdaterConfig{
				Device: disk.GetDevice(),
			},
			Scheme: scheme,
			Active: true, // root1 is active, target is root2
			Encryption: &EncryptionConfig{
				Enabled:       true,
				TPM2:          false,
				Root1LUKSUUID: root1Dev.LUKSUUID,
				Root2LUKSUUID: root2Dev.LUKSUUID,
				VarLUKSUUID:   varDev.LUKSUUID,
			},
		}

		cmdline, err := updater.buildKernelCmdline(root2UUID, varUUID, "ext4", true)
		if err != nil {
			t.Fatalf("buildKernelCmdline failed: %v", err)
		}
		cmdlineStr := strings.Join(cmdline, " ")
		t.Logf("Generated encrypted cmdline: %s", cmdlineStr)

		// Check for boot mount parameter (boot is never encrypted)
		expectedBootMount := fmt.Sprintf("systemd.mount-extra=UUID=%s:/boot:vfat:defaults", bootUUID)
		if !strings.Contains(cmdlineStr, expectedBootMount) {
			t.Errorf("Missing boot mount parameter in encrypted setup.\nExpected: %s\nGot cmdline: %s", expectedBootMount, cmdlineStr)
		}

		// Check for encrypted var mount parameter
		if !strings.Contains(cmdlineStr, "systemd.mount-extra=/dev/mapper/var:/var:") {
			t.Errorf("Missing encrypted var mount parameter.\nGot cmdline: %s", cmdlineStr)
		}

		// Check for LUKS parameters
		if !strings.Contains(cmdlineStr, "rd.luks.uuid=") {
			t.Errorf("Missing LUKS parameters.\nGot cmdline: %s", cmdlineStr)
		}

		// Verify boot is referenced by UUID, not mapper device
		if strings.Contains(cmdlineStr, "/dev/mapper/boot") {
			t.Errorf("Boot partition should not be referenced as mapper device")
		}
	})

	t.Run("boot mount parameter appears before var mount", func(t *testing.T) {
		updater := &SystemUpdater{
			Config: UpdaterConfig{
				Device: disk.GetDevice(),
			},
			Scheme: scheme,
			Active: true,
		}

		cmdline, err := updater.buildKernelCmdline(root2UUID, varUUID, "ext4", true)
		if err != nil {
			t.Fatalf("buildKernelCmdline failed: %v", err)
		}
		cmdlineStr := strings.Join(cmdline, " ")

		// Find the positions of boot and var mount parameters
		bootMount := fmt.Sprintf("systemd.mount-extra=UUID=%s:/boot:vfat:defaults", bootUUID)
		varMount := fmt.Sprintf("systemd.mount-extra=UUID=%s:/var:", varUUID)

		bootIdx := strings.Index(cmdlineStr, bootMount)
		varIdx := strings.Index(cmdlineStr, varMount)

		if bootIdx == -1 {
			t.Errorf("Boot mount parameter not found")
		}
		if varIdx == -1 {
			t.Errorf("Var mount parameter not found")
		}
		if bootIdx > varIdx {
			t.Errorf("Boot mount should appear before var mount.\nBoot index: %d, Var index: %d\nCmdline: %s",
				bootIdx, varIdx, cmdlineStr)
		}
	})
}
