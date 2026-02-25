package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

func TestBootloaderType(t *testing.T) {
	t.Run("constants are correct", func(t *testing.T) {
		if BootloaderGRUB2 != "grub2" {
			t.Errorf("BootloaderGRUB2 = %q, want %q", BootloaderGRUB2, "grub2")
		}
		if BootloaderSystemdBoot != "systemd-boot" {
			t.Errorf("BootloaderSystemdBoot = %q, want %q", BootloaderSystemdBoot, "systemd-boot")
		}
	})
}

func TestNewBootloaderInstaller(t *testing.T) {
	t.Run("creates installer with defaults", func(t *testing.T) {
		installer := NewBootloaderInstaller("/mnt", "/dev/sda", nil, "Test Linux")

		if installer.Type != BootloaderGRUB2 {
			t.Errorf("default type = %q, want %q", installer.Type, BootloaderGRUB2)
		}
		if installer.TargetDir != "/mnt" {
			t.Errorf("TargetDir = %q, want %q", installer.TargetDir, "/mnt")
		}
		if installer.Device != "/dev/sda" {
			t.Errorf("Device = %q, want %q", installer.Device, "/dev/sda")
		}
		if installer.OSName != "Test Linux" {
			t.Errorf("OSName = %q, want %q", installer.OSName, "Test Linux")
		}
		if len(installer.KernelArgs) != 0 {
			t.Errorf("KernelArgs should be empty, got %v", installer.KernelArgs)
		}
	})
}

func TestBootloaderInstaller_SetType(t *testing.T) {
	installer := NewBootloaderInstaller("/mnt", "/dev/sda", nil, "Test")

	installer.SetType(BootloaderSystemdBoot)
	if installer.Type != BootloaderSystemdBoot {
		t.Errorf("SetType failed: got %q, want %q", installer.Type, BootloaderSystemdBoot)
	}

	installer.SetType(BootloaderGRUB2)
	if installer.Type != BootloaderGRUB2 {
		t.Errorf("SetType failed: got %q, want %q", installer.Type, BootloaderGRUB2)
	}
}

func TestBootloaderInstaller_AddKernelArg(t *testing.T) {
	installer := NewBootloaderInstaller("/mnt", "/dev/sda", nil, "Test")

	installer.AddKernelArg("quiet")
	installer.AddKernelArg("splash")

	if len(installer.KernelArgs) != 2 {
		t.Fatalf("expected 2 kernel args, got %d", len(installer.KernelArgs))
	}
	if installer.KernelArgs[0] != "quiet" {
		t.Errorf("first arg = %q, want %q", installer.KernelArgs[0], "quiet")
	}
	if installer.KernelArgs[1] != "splash" {
		t.Errorf("second arg = %q, want %q", installer.KernelArgs[1], "splash")
	}
}

func TestBootloaderInstaller_SetVerbose(t *testing.T) {
	installer := NewBootloaderInstaller("/mnt", "/dev/sda", nil, "Test")

	if installer.Verbose {
		t.Error("Verbose should be false by default")
	}

	installer.SetVerbose(true)
	if !installer.Verbose {
		t.Error("SetVerbose(true) should set Verbose to true")
	}

	installer.SetVerbose(false)
	if installer.Verbose {
		t.Error("SetVerbose(false) should set Verbose to false")
	}
}

func TestBootloaderInstaller_SetEncryption(t *testing.T) {
	installer := NewBootloaderInstaller("/mnt", "/dev/sda", nil, "Test")

	if installer.Encryption != nil {
		t.Error("Encryption should be nil by default")
	}

	config := &LUKSConfig{
		Enabled: true,
		TPM2:    true,
	}
	installer.SetEncryption(config)

	if installer.Encryption == nil {
		t.Fatal("SetEncryption should set Encryption")
	}
	if !installer.Encryption.Enabled {
		t.Error("Encryption.Enabled should be true")
	}
	if !installer.Encryption.TPM2 {
		t.Error("Encryption.TPM2 should be true")
	}
}

func TestBuildKernelCmdline_NonEncrypted(t *testing.T) {
	// This test requires partition UUIDs which need actual partitions
	// We test the structure and error handling instead

	t.Run("panics with nil partition scheme", func(t *testing.T) {
		installer := NewBootloaderInstaller("/mnt", "/dev/sda", nil, "Test")

		defer func() {
			if r := recover(); r == nil {
				t.Error("buildKernelCmdline should panic with nil scheme")
			}
		}()

		_, _ = installer.buildKernelCmdline(context.Background())
	})

	t.Run("returns error with empty root partition", func(t *testing.T) {
		scheme := &PartitionScheme{
			Root1Partition: "",
			VarPartition:   "/dev/sda4",
		}
		installer := NewBootloaderInstaller("/mnt", "/dev/sda", scheme, "Test")
		_, err := installer.buildKernelCmdline(context.Background())
		if err == nil {
			t.Error("buildKernelCmdline should fail with empty root partition")
		}
	})
}

func TestBuildKernelCmdline_BootloaderWithBootMount(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4")

	// Create a test disk with actual partitions
	disk, err := testutil.CreateTestDisk(t, 30) // 30GB disk
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create partitions
	scheme, err := CreatePartitions(context.Background(), disk.GetDevice(), false, NoopReporter{})
	if err != nil {
		t.Fatalf("Failed to create partitions: %v", err)
	}

	// Format boot and var partitions so they have UUIDs
	if err := FormatPartitions(context.Background(), scheme, false, NoopReporter{}); err != nil {
		t.Fatalf("Failed to format partitions: %v", err)
	}

	// Wait for devices to settle
	_ = testutil.WaitForDevice(scheme.BootPartition)
	_ = testutil.WaitForDevice(scheme.Root1Partition)
	_ = testutil.WaitForDevice(scheme.VarPartition)

	// Get actual UUIDs
	bootUUID, err := GetPartitionUUID(context.Background(), scheme.BootPartition)
	if err != nil {
		t.Fatalf("Failed to get boot UUID: %v", err)
	}
	rootUUID, err := GetPartitionUUID(context.Background(), scheme.Root1Partition)
	if err != nil {
		t.Fatalf("Failed to get root UUID: %v", err)
	}
	varUUID, err := GetPartitionUUID(context.Background(), scheme.VarPartition)
	if err != nil {
		t.Fatalf("Failed to get var UUID: %v", err)
	}

	t.Run("non-encrypted includes boot mount", func(t *testing.T) {
		installer := NewBootloaderInstaller("/mnt", disk.GetDevice(), scheme, "Test OS")
		cmdline, err := installer.buildKernelCmdline(context.Background())
		if err != nil {
			t.Fatalf("buildKernelCmdline failed: %v", err)
		}

		// Convert to string for easier checking
		cmdlineStr := strings.Join(cmdline, " ")
		t.Logf("Generated cmdline: %s", cmdlineStr)

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

		// Check for root parameter
		expectedRoot := fmt.Sprintf("root=UUID=%s", rootUUID)
		if !strings.Contains(cmdlineStr, expectedRoot) {
			t.Errorf("Missing root parameter.\nExpected: %s\nGot cmdline: %s", expectedRoot, cmdlineStr)
		}

		// Verify boot mount comes before var mount (order matters for systemd)
		bootIdx := strings.Index(cmdlineStr, expectedBootMount)
		varIdx := strings.Index(cmdlineStr, "systemd.mount-extra=UUID="+varUUID)
		if bootIdx > varIdx {
			t.Errorf("Boot mount should come before var mount in cmdline")
		}
	})

	t.Run("encrypted includes boot mount", func(t *testing.T) {
		// Setup LUKS encryption for testing
		passphrase := "test-passphrase"
		if err := SetupLUKS(context.Background(), scheme, passphrase, false, NoopReporter{}); err != nil {
			t.Fatalf("Failed to setup LUKS: %v", err)
		}
		defer scheme.CloseLUKSDevices(context.Background())

		installer := NewBootloaderInstaller("/mnt", disk.GetDevice(), scheme, "Test OS")

		// Set encryption config
		installer.SetEncryption(&LUKSConfig{
			Enabled: true,
			TPM2:    false,
		})

		cmdline, err := installer.buildKernelCmdline(context.Background())
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
}

func TestEnsureUppercaseEFIDirectory(t *testing.T) {
	t.Run("handles non-existent ESP", func(t *testing.T) {
		// Should return nil for non-existent directory
		err := ensureUppercaseEFIDirectory("/nonexistent/path", NoopReporter{})
		if err != nil {
			t.Errorf("should return nil for non-existent path: %v", err)
		}
	})

	t.Run("handles empty ESP", func(t *testing.T) {
		espPath := t.TempDir()
		err := ensureUppercaseEFIDirectory(espPath, NoopReporter{})
		if err != nil {
			t.Errorf("should return nil for empty ESP: %v", err)
		}
	})

	t.Run("keeps uppercase EFI", func(t *testing.T) {
		espPath := t.TempDir()
		efiDir := filepath.Join(espPath, "EFI")
		if err := os.MkdirAll(efiDir, 0755); err != nil {
			t.Fatalf("failed to create EFI dir: %v", err)
		}

		err := ensureUppercaseEFIDirectory(espPath, NoopReporter{})
		if err != nil {
			t.Errorf("should succeed with uppercase EFI: %v", err)
		}

		// Verify EFI still exists
		if _, err := os.Stat(efiDir); err != nil {
			t.Error("EFI directory should still exist")
		}
	})

	t.Run("renames lowercase efi to EFI", func(t *testing.T) {
		espPath := t.TempDir()
		efiDir := filepath.Join(espPath, "efi")
		if err := os.MkdirAll(efiDir, 0755); err != nil {
			t.Fatalf("failed to create efi dir: %v", err)
		}

		// Create a test file inside
		testFile := filepath.Join(efiDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := ensureUppercaseEFIDirectory(espPath, NoopReporter{})
		if err != nil {
			t.Errorf("rename should succeed: %v", err)
		}

		// Verify uppercase EFI exists (case-insensitive FS may still match)
		entries, err := os.ReadDir(espPath)
		if err != nil {
			t.Fatalf("failed to read ESP: %v", err)
		}

		var foundEFI bool
		for _, e := range entries {
			// On case-sensitive FS, should be "EFI"; on case-insensitive, could be either
			if e.Name() == "EFI" || e.Name() == "efi" {
				foundEFI = true
				break
			}
		}
		if !foundEFI {
			t.Error("EFI directory not found after rename")
		}

		// Verify content was preserved
		upperTestFile := filepath.Join(espPath, "EFI", "test.txt")
		lowerTestFile := filepath.Join(espPath, "efi", "test.txt")
		_, upperErr := os.Stat(upperTestFile)
		_, lowerErr := os.Stat(lowerTestFile)
		if upperErr != nil && lowerErr != nil {
			t.Error("test file should exist after rename")
		}
	})
}

func TestEnsureUppercaseBOOTDirectory(t *testing.T) {
	t.Run("handles non-existent EFI", func(t *testing.T) {
		err := ensureUppercaseBOOTDirectory("/nonexistent/EFI", NoopReporter{})
		if err != nil {
			t.Errorf("should return nil for non-existent path: %v", err)
		}
	})

	t.Run("handles empty EFI", func(t *testing.T) {
		efiPath := t.TempDir()
		err := ensureUppercaseBOOTDirectory(efiPath, NoopReporter{})
		if err != nil {
			t.Errorf("should return nil for empty EFI: %v", err)
		}
	})

	t.Run("keeps uppercase BOOT", func(t *testing.T) {
		efiPath := t.TempDir()
		bootDir := filepath.Join(efiPath, "BOOT")
		if err := os.MkdirAll(bootDir, 0755); err != nil {
			t.Fatalf("failed to create BOOT dir: %v", err)
		}

		err := ensureUppercaseBOOTDirectory(efiPath, NoopReporter{})
		if err != nil {
			t.Errorf("should succeed with uppercase BOOT: %v", err)
		}
	})

	t.Run("renames lowercase boot to BOOT", func(t *testing.T) {
		efiPath := t.TempDir()
		bootDir := filepath.Join(efiPath, "boot")
		if err := os.MkdirAll(bootDir, 0755); err != nil {
			t.Fatalf("failed to create boot dir: %v", err)
		}

		err := ensureUppercaseBOOTDirectory(efiPath, NoopReporter{})
		if err != nil {
			t.Errorf("rename should succeed: %v", err)
		}
	})
}
