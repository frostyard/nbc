package pkg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

func TestInstallConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  InstallConfig
		wantErr string
	}{
		{
			name: "valid config with image and device",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
			},
			wantErr: "",
		},
		{
			name: "valid config with local image and device",
			config: InstallConfig{
				LocalImage: &LocalImageSource{
					LayoutPath: "/tmp/layout",
				},
				Device: "/dev/sda",
			},
			wantErr: "",
		},
		{
			name: "valid config with image and loopback",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{
					ImagePath: "/tmp/disk.img",
					SizeGB:    40,
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with encryption",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
				Encryption: &EncryptionOptions{
					Passphrase: "secret",
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with encryption and TPM2",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
				Encryption: &EncryptionOptions{
					Passphrase: "secret",
					TPM2:       true,
				},
			},
			wantErr: "",
		},
		{
			name:    "missing image and local image",
			config:  InstallConfig{Device: "/dev/sda"},
			wantErr: "either ImageRef or LocalImage is required",
		},
		{
			name:    "missing device and loopback",
			config:  InstallConfig{ImageRef: "quay.io/example/image:latest"},
			wantErr: "either Device or Loopback is required",
		},
		{
			name: "image and local image both set",
			config: InstallConfig{
				ImageRef:   "quay.io/example/image:latest",
				LocalImage: &LocalImageSource{LayoutPath: "/tmp/layout"},
				Device:     "/dev/sda",
			},
			wantErr: "imageRef and localImage are mutually exclusive",
		},
		{
			name: "device and loopback both set",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
				Loopback: &LoopbackOptions{ImagePath: "/tmp/disk.img"},
			},
			wantErr: "device and loopback are mutually exclusive",
		},
		{
			name: "invalid filesystem type",
			config: InstallConfig{
				ImageRef:       "quay.io/example/image:latest",
				Device:         "/dev/sda",
				FilesystemType: "xfs",
			},
			wantErr: "unsupported filesystem type: xfs",
		},
		{
			name: "encryption without passphrase",
			config: InstallConfig{
				ImageRef:   "quay.io/example/image:latest",
				Device:     "/dev/sda",
				Encryption: &EncryptionOptions{},
			},
			wantErr: "encryption passphrase is required",
		},
		{
			name: "loopback without image path",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{},
			},
			wantErr: "loopback ImagePath is required",
		},
		{
			name: "loopback size too small",
			config: InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{
					ImagePath: "/tmp/disk.img",
					SizeGB:    10, // Below minimum
				},
			},
			wantErr: "loopback size must be at least",
		},
		{
			name: "local image without layout path",
			config: InstallConfig{
				LocalImage: &LocalImageSource{},
				Device:     "/dev/sda",
			},
			wantErr: "LocalImage.LayoutPath is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestNewInstaller(t *testing.T) {
	tests := []struct {
		name    string
		config  *InstallConfig
		wantErr bool
	}{
		{
			name:    "nil config returns error",
			config:  nil,
			wantErr: true,
		},
		{
			name: "valid config creates installer",
			config: &InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
			},
			wantErr: false,
		},
		{
			name: "applies default filesystem type",
			config: &InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Device:   "/dev/sda",
			},
			wantErr: false,
		},
		{
			name: "applies default loopback size",
			config: &InstallConfig{
				ImageRef: "quay.io/example/image:latest",
				Loopback: &LoopbackOptions{
					ImagePath: "/tmp/disk.img",
					// SizeGB: 0, // Should default to DefaultLoopbackSizeGB
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config returns error",
			config: &InstallConfig{
				// Missing both ImageRef and LocalImage
				Device: "/dev/sda",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer, err := NewInstaller(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Error("NewInstaller() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("NewInstaller() unexpected error: %v", err)
				}
				if installer == nil {
					t.Error("NewInstaller() returned nil installer")
				}
			}
		})
	}
}

func TestNewInstaller_DefaultValues(t *testing.T) {
	t.Run("applies default filesystem type", func(t *testing.T) {
		cfg := &InstallConfig{
			ImageRef: "quay.io/example/image:latest",
			Device:   "/dev/sda",
		}
		installer, err := NewInstaller(cfg)
		if err != nil {
			t.Fatalf("NewInstaller() error: %v", err)
		}
		if installer.config.FilesystemType != "btrfs" {
			t.Errorf("FilesystemType = %q, want %q", installer.config.FilesystemType, "btrfs")
		}
	})

	t.Run("applies default mount point", func(t *testing.T) {
		cfg := &InstallConfig{
			ImageRef: "quay.io/example/image:latest",
			Device:   "/dev/sda",
		}
		installer, err := NewInstaller(cfg)
		if err != nil {
			t.Fatalf("NewInstaller() error: %v", err)
		}
		if installer.config.MountPoint != "/tmp/nbc-install" {
			t.Errorf("MountPoint = %q, want %q", installer.config.MountPoint, "/tmp/nbc-install")
		}
	})

	t.Run("applies default loopback size", func(t *testing.T) {
		cfg := &InstallConfig{
			ImageRef: "quay.io/example/image:latest",
			Loopback: &LoopbackOptions{
				ImagePath: "/tmp/disk.img",
			},
		}
		installer, err := NewInstaller(cfg)
		if err != nil {
			t.Fatalf("NewInstaller() error: %v", err)
		}
		if installer.config.Loopback.SizeGB != DefaultLoopbackSizeGB {
			t.Errorf("Loopback.SizeGB = %d, want %d", installer.config.Loopback.SizeGB, DefaultLoopbackSizeGB)
		}
	})
}

func TestInstaller_SetCallbacks(t *testing.T) {
	cfg := &InstallConfig{
		ImageRef: "quay.io/example/image:latest",
		Device:   "/dev/sda",
	}
	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	// Initially callbacks should be nil
	if installer.callbacks != nil {
		t.Error("callbacks should initially be nil")
	}

	// Set callbacks
	callbacks := &InstallCallbacks{
		OnStep: func(step, total int, name string) {
			// callback placeholder
		},
	}
	installer.SetCallbacks(callbacks)

	if installer.callbacks == nil {
		t.Error("callbacks should not be nil after SetCallbacks")
	}
	if installer.progressAdapter == nil {
		t.Error("progressAdapter should not be nil after SetCallbacks")
	}
}

func TestInstaller_CallOnError(t *testing.T) {
	cfg := &InstallConfig{
		ImageRef: "quay.io/example/image:latest",
		Device:   "/dev/sda",
	}
	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	t.Run("nil callbacks does not panic", func(t *testing.T) {
		// Should not panic with nil callbacks
		installer.callOnError(context.Canceled, "test")
	})

	t.Run("nil OnError does not panic", func(t *testing.T) {
		installer.SetCallbacks(&InstallCallbacks{})
		installer.callOnError(context.Canceled, "test")
	})

	t.Run("OnError is called", func(t *testing.T) {
		var calledWith error
		var calledMessage string
		installer.SetCallbacks(&InstallCallbacks{
			OnError: func(err error, msg string) {
				calledWith = err
				calledMessage = msg
			},
		})

		testErr := context.Canceled
		installer.callOnError(testErr, "test message")

		if calledWith != testErr {
			t.Errorf("OnError called with %v, want %v", calledWith, testErr)
		}
		if calledMessage != "test message" {
			t.Errorf("OnError called with message %q, want %q", calledMessage, "test message")
		}
	})
}

func TestCreateCLICallbacks(t *testing.T) {
	t.Run("text output callbacks", func(t *testing.T) {
		callbacks := CreateCLICallbacks(false)
		if callbacks == nil {
			t.Fatal("CreateCLICallbacks(false) returned nil")
			return
		}
		if callbacks.OnStep == nil {
			t.Error("OnStep should not be nil")
		}
		if callbacks.OnMessage == nil {
			t.Error("OnMessage should not be nil")
		}
		if callbacks.OnWarning == nil {
			t.Error("OnWarning should not be nil")
		}
		if callbacks.OnError == nil {
			t.Error("OnError should not be nil")
		}
	})

	t.Run("json output callbacks", func(t *testing.T) {
		callbacks := CreateCLICallbacks(true)
		if callbacks == nil {
			t.Fatal("CreateCLICallbacks(true) returned nil")
			return
		}
		if callbacks.OnStep == nil {
			t.Error("OnStep should not be nil")
		}
		if callbacks.OnMessage == nil {
			t.Error("OnMessage should not be nil")
		}
		if callbacks.OnWarning == nil {
			t.Error("OnWarning should not be nil")
		}
		if callbacks.OnError == nil {
			t.Error("OnError should not be nil")
		}
	})
}

func TestCallbackProgressAdapter(t *testing.T) {
	t.Run("Step calls OnStep", func(t *testing.T) {
		var stepNum, totalSteps int
		var stepName string

		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnStep: func(step, total int, name string) {
				stepNum = step
				totalSteps = total
				stepName = name
			},
		}, 6)

		adapter.Step(3, "Testing")

		if stepNum != 3 {
			t.Errorf("step = %d, want 3", stepNum)
		}
		if totalSteps != 6 {
			t.Errorf("totalSteps = %d, want 6", totalSteps)
		}
		if stepName != "Testing" {
			t.Errorf("stepName = %q, want %q", stepName, "Testing")
		}
	})

	t.Run("Message calls OnMessage", func(t *testing.T) {
		var message string
		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnMessage: func(msg string) {
				message = msg
			},
		}, 6)

		adapter.Message("Hello %s", "World")

		if message != "Hello World" {
			t.Errorf("message = %q, want %q", message, "Hello World")
		}
	})

	t.Run("Warning calls OnWarning", func(t *testing.T) {
		var warning string
		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnWarning: func(msg string) {
				warning = msg
			},
		}, 6)

		adapter.Warning("Warning: %d issues", 5)

		if warning != "Warning: 5 issues" {
			t.Errorf("warning = %q, want %q", warning, "Warning: 5 issues")
		}
	})

	t.Run("Progress calls OnProgress", func(t *testing.T) {
		var percent int
		var message string
		adapter := newCallbackProgressAdapter(&InstallCallbacks{
			OnProgress: func(p int, msg string) {
				percent = p
				message = msg
			},
		}, 6)

		adapter.Progress(75, "Processing...")

		if percent != 75 {
			t.Errorf("percent = %d, want 75", percent)
		}
		if message != "Processing..." {
			t.Errorf("message = %q, want %q", message, "Processing...")
		}
	})

	t.Run("nil callbacks do not panic", func(t *testing.T) {
		adapter := newCallbackProgressAdapter(nil, 6)
		// These should not panic
		adapter.Step(1, "Test")
		adapter.Message("Test")
		adapter.Warning("Test")
		adapter.Progress(50, "Test")
		adapter.Error(context.Canceled, "Test")
	})
}

// TestIntegration_Installer_Install tests the full installation flow using a loopback device
func TestIntegration_Installer_Install(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount")

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container image
	imageName := "localhost/nbc-test-installer:latest"
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Create installer using new API
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	cfg := &InstallConfig{
		ImageRef:       imageName,
		Device:         disk.GetDevice(),
		MountPoint:     mountPoint,
		FilesystemType: "ext4",
		Verbose:        true,
	}

	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	// Track callback invocations
	var steps []string
	var messages []string
	installer.SetCallbacks(&InstallCallbacks{
		OnStep: func(step, total int, name string) {
			steps = append(steps, name)
			t.Logf("Step %d/%d: %s", step, total, name)
		},
		OnMessage: func(msg string) {
			messages = append(messages, msg)
		},
		OnWarning: func(msg string) {
			t.Logf("Warning: %s", msg)
		},
		OnError: func(err error, msg string) {
			t.Logf("Error: %s: %v", msg, err)
		},
	})

	// Perform installation
	t.Log("Starting Installer.Install() test")
	result, err := installer.Install(context.Background())
	if err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	// Ensure cleanup is called
	if result.Cleanup != nil {
		defer func() { _ = result.Cleanup() }()
	}

	// Verify result fields
	if result.Device == "" {
		t.Error("result.Device is empty")
	}
	if result.ImageRef == "" {
		t.Error("result.ImageRef is empty")
	}
	t.Logf("Device: %s, ImageRef: %s", result.Device, result.ImageRef)

	// Verify callbacks were invoked
	if len(steps) == 0 {
		t.Error("OnStep callback was never invoked")
	} else {
		t.Logf("Steps executed: %v", steps)
	}

	// Verify partitions were created
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
		"etc", "var", "boot", "usr",
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

	// Check for os-release (ensures container extraction worked)
	osReleasePath := filepath.Join(verifyMount, "etc", "os-release")
	if _, err := os.Stat(osReleasePath); err != nil {
		// Also check usr/lib/os-release (common location)
		osReleasePath = filepath.Join(verifyMount, "usr", "lib", "os-release")
		if _, err := os.Stat(osReleasePath); err != nil {
			t.Logf("Warning: os-release not found (mock container may not have it)")
		} else {
			t.Logf("✓ os-release exists at %s", osReleasePath)
		}
	} else {
		t.Logf("✓ os-release exists at %s", osReleasePath)
	}

	t.Log("Installer.Install() test completed successfully")
}

// TestIntegration_Installer_DryRun tests that dry-run mode doesn't modify the disk
func TestIntegration_Installer_DryRun(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "podman")

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container image
	imageName := "localhost/nbc-test-dryrun-installer:latest"
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Create installer with dry-run enabled
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	cfg := &InstallConfig{
		ImageRef:       imageName,
		Device:         disk.GetDevice(),
		MountPoint:     mountPoint,
		FilesystemType: "ext4",
		Verbose:        true,
		DryRun:         true,
	}

	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	// Track callback invocations
	var messages []string
	installer.SetCallbacks(&InstallCallbacks{
		OnStep: func(step, total int, name string) {
			t.Logf("[DRY RUN] Step %d/%d: %s", step, total, name)
		},
		OnMessage: func(msg string) {
			messages = append(messages, msg)
			t.Logf("[DRY RUN] %s", msg)
		},
	})

	// Perform dry-run installation
	t.Log("Testing Installer.Install() in dry-run mode")
	result, err := installer.Install(context.Background())
	if err != nil {
		t.Fatalf("Dry-run Install() failed: %v", err)
	}

	// Cleanup should still be provided (even if no-op)
	if result.Cleanup != nil {
		_ = result.Cleanup()
	}

	// In dry-run mode, OnStep is not called (no actual steps are performed)
	// but OnMessage should be called
	if len(messages) == 0 {
		t.Error("OnMessage callback was never invoked during dry-run")
	} else {
		t.Logf("Dry-run messages: %d messages logged", len(messages))
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

	t.Log("Installer.Install() dry-run test completed successfully")
}

// TestIntegration_Installer_WithEncryption tests installation with LUKS encryption
func TestIntegration_Installer_WithEncryption(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman", "mount", "umount", "cryptsetup")

	// Skip if the system already has LUKS devices with the names we'd use
	// This happens when running on an encrypted system (root1, root2, var are in use)
	for _, name := range []string{"root1", "root2", "var"} {
		mapperPath := filepath.Join("/dev/mapper", name)
		if _, err := os.Stat(mapperPath); err == nil {
			t.Skipf("Skipping encryption test: system LUKS device %s already exists (running on encrypted system)", mapperPath)
		}
	}

	// Create test disk
	disk, err := testutil.CreateTestDisk(t, 50)
	if err != nil {
		t.Fatalf("Failed to create test disk: %v", err)
	}

	// Create mock container image
	imageName := "localhost/nbc-test-encrypted:latest"
	if err := testutil.CreateMockContainer(t, imageName); err != nil {
		t.Fatalf("Failed to create mock container: %v", err)
	}

	// Create installer with encryption
	mountPoint := filepath.Join(t.TempDir(), "mnt")
	cfg := &InstallConfig{
		ImageRef:       imageName,
		Device:         disk.GetDevice(),
		MountPoint:     mountPoint,
		FilesystemType: "ext4",
		Verbose:        true,
		Encryption: &EncryptionOptions{
			Passphrase: "test-passphrase-123",
		},
	}

	installer, err := NewInstaller(cfg)
	if err != nil {
		t.Fatalf("NewInstaller() error: %v", err)
	}

	// Track steps
	var steps []string
	installer.SetCallbacks(&InstallCallbacks{
		OnStep: func(step, total int, name string) {
			steps = append(steps, name)
			t.Logf("Step %d/%d: %s", step, total, name)
		},
		OnMessage: func(msg string) {
			t.Logf("  %s", msg)
		},
		OnWarning: func(msg string) {
			t.Logf("Warning: %s", msg)
		},
	})

	// Perform installation
	t.Log("Starting encrypted installation test")
	result, err := installer.Install(context.Background())
	if err != nil {
		t.Fatalf("Encrypted Install() failed: %v", err)
	}

	// Ensure cleanup
	if result.Cleanup != nil {
		defer func() { _ = result.Cleanup() }()
	}

	// Verify result fields
	if result.Device == "" {
		t.Fatal("result.Device is empty")
	}

	// Verify that LUKS mapper devices exist by checking /dev/mapper/
	mapperDir := "/dev/mapper"
	entries, err := os.ReadDir(mapperDir)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", mapperDir, err)
	}

	// Look for our LUKS devices (root1, root2, var)
	var luksDevices []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "luks-") {
			luksDevices = append(luksDevices, name)
			t.Logf("Found LUKS device: %s", name)
		}
	}

	if len(luksDevices) == 0 {
		t.Error("Expected LUKS mapper devices but none found")
	} else {
		t.Logf("✓ LUKS encryption verified (%d mapper devices found)", len(luksDevices))
	}

	t.Log("Encrypted installation test completed successfully")
}
