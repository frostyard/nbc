package pkg

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

func TestCreatePartitions(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4")

	// Create a test disk
	disk, err := testutil.CreateTestDisk(t, 50) // 50GB test disk
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create partitions
	t.Log("Creating partitions on test disk")
	scheme, err := CreatePartitions(context.Background(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	// Wait for devices to appear
	if err := testutil.WaitForDevice(disk.GetDevice()); err != nil {
		t.Logf("Warning: WaitForDevice failed: %v", err)
	}

	// Verify partition scheme
	if scheme == nil {
		t.Fatal("Partition scheme is nil")
		return
	}

	if scheme.BootPartition == "" {
		t.Error("Boot partition path is empty")
	}
	if scheme.Root1Partition == "" {
		t.Error("Root1 partition path is empty")
	}
	if scheme.Root2Partition == "" {
		t.Error("Root2 partition path is empty")
	}
	if scheme.VarPartition == "" {
		t.Error("Var partition path is empty")
	}

	t.Logf("Partition scheme created:")
	t.Logf("  Boot:  %s", scheme.BootPartition)
	t.Logf("  Root1: %s", scheme.Root1Partition)
	t.Logf("  Root2: %s", scheme.Root2Partition)
	t.Logf("  Var:   %s", scheme.VarPartition)
}

func TestFormatPartitions(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "blkid")

	// Create and partition test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(context.Background(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	// Format partitions
	t.Log("Formatting partitions")
	if err := FormatPartitions(context.Background(), scheme, false, NoopReporter{}); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	// Verify UUIDs can be retrieved (means partitions are formatted)
	for name, part := range map[string]string{
		"Boot":  scheme.BootPartition,
		"Root1": scheme.Root1Partition,
		"Root2": scheme.Root2Partition,
		"Var":   scheme.VarPartition,
	} {
		uuid, err := GetPartitionUUID(context.Background(), part)
		if err != nil {
			t.Errorf("Failed to get UUID for %s partition %s: %v", name, part, err)
		} else if uuid == "" {
			t.Errorf("UUID is empty for %s partition %s", name, part)
		} else {
			t.Logf("  %s UUID: %s", name, uuid)
		}
	}
}

func TestMountPartitions(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "mount", "umount")

	// Create and partition test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	scheme, err := CreatePartitions(context.Background(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	if err := FormatPartitions(context.Background(), scheme, false, NoopReporter{}); err != nil {
		t.Fatalf("FormatPartitions failed: %v", err)
	}

	// Create mount point
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatalf("Failed to create mount point: %v", err)
	}

	// Register cleanup for mounts
	defer testutil.CleanupMounts(t, mountPoint)

	// Mount partitions
	t.Log("Mounting partitions")
	if err := MountPartitions(context.Background(), scheme, mountPoint, false, NoopReporter{}); err != nil {
		t.Fatalf("MountPartitions failed: %v", err)
	}

	// Verify mount points exist
	// New 4-partition scheme: boot (ESP), root1, root2, var
	// /boot is the combined ESP partition (no separate /boot/efi)
	expectedDirs := []string{
		mountPoint,
		filepath.Join(mountPoint, "boot"),
		filepath.Join(mountPoint, "var"),
	}

	for _, dir := range expectedDirs {
		if info, err := os.Stat(dir); err != nil {
			t.Errorf("Mount point does not exist: %s: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("Mount point is not a directory: %s", dir)
		} else {
			t.Logf("  Verified mount: %s", dir)
		}
	}

	// Cleanup
	t.Log("Unmounting partitions")
	if err := UnmountPartitions(context.Background(), mountPoint, false, NoopReporter{}); err != nil {
		t.Errorf("UnmountPartitions failed: %v", err)
	}
}

func TestDetectExistingPartitionScheme(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4")

	// Create and partition test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	originalScheme, err := CreatePartitions(context.Background(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("CreatePartitions failed: %v", err)
	}

	_ = testutil.WaitForDevice(disk.GetDevice())

	// Detect the scheme
	t.Log("Detecting existing partition scheme")
	detectedScheme, err := DetectExistingPartitionScheme(disk.GetDevice())
	if err != nil {
		t.Fatalf("DetectExistingPartitionScheme failed: %v", err)
	}

	// Compare schemes
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

	t.Log("Partition scheme detection successful")
}
