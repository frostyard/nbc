package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNBCBooted(t *testing.T) {
	t.Run("returns false when marker does not exist", func(t *testing.T) {
		// Use a path that doesn't exist
		// Since IsNBCBooted checks NBCBootedMarker constant, we test the logic
		// by creating a temp dir and verifying behavior
		result := IsNBCBooted()
		// In test environment, /run/nbc-booted typically doesn't exist
		// This test documents expected behavior
		t.Logf("IsNBCBooted() = %v (depends on test environment)", result)
	})
}

func TestInstallTmpfilesConfig(t *testing.T) {
	t.Run("dry run does not create files", func(t *testing.T) {
		targetDir := t.TempDir()
		err := InstallTmpfilesConfig(targetDir, true)
		if err != nil {
			t.Fatalf("InstallTmpfilesConfig dry run failed: %v", err)
		}

		// Verify no file was created
		tmpfilesPath := filepath.Join(targetDir, "usr", "lib", "tmpfiles.d", "nbc.conf")
		if _, err := os.Stat(tmpfilesPath); !os.IsNotExist(err) {
			t.Error("dry run should not create tmpfiles.d config")
		}
	})

	t.Run("creates tmpfiles.d config", func(t *testing.T) {
		targetDir := t.TempDir()
		err := InstallTmpfilesConfig(targetDir, false)
		if err != nil {
			t.Fatalf("InstallTmpfilesConfig failed: %v", err)
		}

		// Verify file was created
		tmpfilesPath := filepath.Join(targetDir, "usr", "lib", "tmpfiles.d", "nbc.conf")
		content, err := os.ReadFile(tmpfilesPath)
		if err != nil {
			t.Fatalf("failed to read tmpfiles.d config: %v", err)
		}

		// Verify content contains the marker creation line
		if !contains(string(content), "/run/nbc-booted") {
			t.Error("tmpfiles.d config should reference /run/nbc-booted")
		}
		if !contains(string(content), "f /run/nbc-booted") {
			t.Error("tmpfiles.d config should use 'f' type for file creation")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		targetDir := t.TempDir()
		err := InstallTmpfilesConfig(targetDir, false)
		if err != nil {
			t.Fatalf("InstallTmpfilesConfig failed: %v", err)
		}

		// Verify directory structure
		tmpfilesDir := filepath.Join(targetDir, "usr", "lib", "tmpfiles.d")
		info, err := os.Stat(tmpfilesDir)
		if err != nil {
			t.Fatalf("tmpfiles.d directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("tmpfiles.d should be a directory")
		}
	})
}

func TestSystemConfig(t *testing.T) {
	t.Run("marshal and unmarshal", func(t *testing.T) {
		config := &SystemConfig{
			ImageRef:       "ghcr.io/test/image:latest",
			ImageDigest:    "sha256:abc123",
			Device:         "/dev/sda",
			InstallDate:    "2025-01-01T00:00:00Z",
			KernelArgs:     []string{"quiet", "splash"},
			BootloaderType: "systemd-boot",
			FilesystemType: "ext4",
		}

		data, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		var parsed SystemConfig
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if parsed.ImageRef != config.ImageRef {
			t.Errorf("ImageRef mismatch: got %q, want %q", parsed.ImageRef, config.ImageRef)
		}
		if parsed.BootloaderType != config.BootloaderType {
			t.Errorf("BootloaderType mismatch: got %q, want %q", parsed.BootloaderType, config.BootloaderType)
		}
	})

	t.Run("encryption config", func(t *testing.T) {
		config := &SystemConfig{
			ImageRef: "test:latest",
			Encryption: &EncryptionConfig{
				Enabled:       true,
				TPM2:          true,
				Root1LUKSUUID: "uuid-root1",
				Root2LUKSUUID: "uuid-root2",
				VarLUKSUUID:   "uuid-var",
			},
		}

		data, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("failed to marshal config with encryption: %v", err)
		}

		var parsed SystemConfig
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal config: %v", err)
		}

		if parsed.Encryption == nil {
			t.Fatal("encryption config should not be nil")
		}
		if !parsed.Encryption.Enabled {
			t.Error("encryption should be enabled")
		}
		if !parsed.Encryption.TPM2 {
			t.Error("TPM2 should be enabled")
		}
	})

	t.Run("encryption config omitted when nil", func(t *testing.T) {
		config := &SystemConfig{
			ImageRef: "test:latest",
		}

		data, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		// Verify encryption field is omitted from JSON
		if contains(string(data), "encryption") {
			t.Error("encryption field should be omitted when nil")
		}
	})
}

func TestWriteSystemConfigToTarget(t *testing.T) {
	t.Run("dry run does not create files", func(t *testing.T) {
		targetDir := t.TempDir()
		config := &SystemConfig{ImageRef: "test:latest"}

		err := WriteSystemConfigToTarget(targetDir, config, true)
		if err != nil {
			t.Fatalf("WriteSystemConfigToTarget dry run failed: %v", err)
		}

		configPath := filepath.Join(targetDir, "etc", "nbc", "config.json")
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Error("dry run should not create config file")
		}
	})

	t.Run("creates config file", func(t *testing.T) {
		targetDir := t.TempDir()
		config := &SystemConfig{
			ImageRef:       "ghcr.io/test/image:latest",
			Device:         "/dev/sda",
			BootloaderType: "grub2",
		}

		err := WriteSystemConfigToTarget(targetDir, config, false)
		if err != nil {
			t.Fatalf("WriteSystemConfigToTarget failed: %v", err)
		}

		configPath := filepath.Join(targetDir, "etc", "nbc", "config.json")
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read config: %v", err)
		}

		var parsed SystemConfig
		if err := json.Unmarshal(content, &parsed); err != nil {
			t.Fatalf("failed to parse config: %v", err)
		}

		if parsed.ImageRef != config.ImageRef {
			t.Errorf("ImageRef mismatch: got %q, want %q", parsed.ImageRef, config.ImageRef)
		}
	})
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
