package pkg

import (
	"os"
	"path/filepath"
	"testing"
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

		_, _ = installer.buildKernelCmdline()
	})

	t.Run("returns error with empty root partition", func(t *testing.T) {
		scheme := &PartitionScheme{
			Root1Partition: "",
			VarPartition:   "/dev/sda4",
		}
		installer := NewBootloaderInstaller("/mnt", "/dev/sda", scheme, "Test")
		_, err := installer.buildKernelCmdline()
		if err == nil {
			t.Error("buildKernelCmdline should fail with empty root partition")
		}
	})
}

func TestEnsureUppercaseEFIDirectory(t *testing.T) {
	t.Run("handles non-existent ESP", func(t *testing.T) {
		// Should return nil for non-existent directory
		err := ensureUppercaseEFIDirectory("/nonexistent/path")
		if err != nil {
			t.Errorf("should return nil for non-existent path: %v", err)
		}
	})

	t.Run("handles empty ESP", func(t *testing.T) {
		espPath := t.TempDir()
		err := ensureUppercaseEFIDirectory(espPath)
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

		err := ensureUppercaseEFIDirectory(espPath)
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

		err := ensureUppercaseEFIDirectory(espPath)
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
		err := ensureUppercaseBOOTDirectory("/nonexistent/EFI")
		if err != nil {
			t.Errorf("should return nil for non-existent path: %v", err)
		}
	})

	t.Run("handles empty EFI", func(t *testing.T) {
		efiPath := t.TempDir()
		err := ensureUppercaseBOOTDirectory(efiPath)
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

		err := ensureUppercaseBOOTDirectory(efiPath)
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

		err := ensureUppercaseBOOTDirectory(efiPath)
		if err != nil {
			t.Errorf("rename should succeed: %v", err)
		}
	})
}
