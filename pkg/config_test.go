package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		if !strings.Contains(string(content), "/run/nbc-booted") {
			t.Error("tmpfiles.d config should reference /run/nbc-booted")
		}
		if !strings.Contains(string(content), "f /run/nbc-booted") {
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
		if strings.Contains(string(data), "encryption") {
			t.Error("encryption field should be omitted when nil")
		}
	})
}

func TestWriteSystemConfigToVar(t *testing.T) {
	t.Run("dry run does not create files", func(t *testing.T) {
		varMountPoint := t.TempDir()
		config := &SystemConfig{ImageRef: "test:latest"}

		err := WriteSystemConfigToVar(varMountPoint, config, true)
		if err != nil {
			t.Fatalf("WriteSystemConfigToVar dry run failed: %v", err)
		}

		configPath := filepath.Join(varMountPoint, "lib", "nbc", "state", "config.json")
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Error("dry run should not create config file")
		}
	})

	t.Run("creates config file", func(t *testing.T) {
		varMountPoint := t.TempDir()
		config := &SystemConfig{
			ImageRef:       "ghcr.io/test/image:latest",
			Device:         "/dev/sda",
			BootloaderType: "grub2",
		}

		err := WriteSystemConfigToVar(varMountPoint, config, false)
		if err != nil {
			t.Fatalf("WriteSystemConfigToVar failed: %v", err)
		}

		configPath := filepath.Join(varMountPoint, "lib", "nbc", "state", "config.json")
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

	t.Run("sets correct permissions", func(t *testing.T) {
		varMountPoint := t.TempDir()
		config := &SystemConfig{ImageRef: "test:latest"}

		err := WriteSystemConfigToVar(varMountPoint, config, false)
		if err != nil {
			t.Fatalf("WriteSystemConfigToVar failed: %v", err)
		}

		// Check directory permissions (0755)
		dirPath := filepath.Join(varMountPoint, "lib", "nbc", "state")
		dirInfo, err := os.Stat(dirPath)
		if err != nil {
			t.Fatalf("failed to stat directory: %v", err)
		}
		if dirInfo.Mode().Perm() != 0755 {
			t.Errorf("directory permissions mismatch: got %o, want 0755", dirInfo.Mode().Perm())
		}

		// Check file permissions (0644)
		filePath := filepath.Join(dirPath, "config.json")
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("failed to stat file: %v", err)
		}
		if fileInfo.Mode().Perm() != 0644 {
			t.Errorf("file permissions mismatch: got %o, want 0644", fileInfo.Mode().Perm())
		}
	})
}

func TestVerifyConfigFile(t *testing.T) {
	t.Run("valid config file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")

		config := &SystemConfig{ImageRef: "test:latest"}
		data, _ := json.MarshalIndent(config, "", "  ")
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		err := verifyConfigFile(configPath)
		if err != nil {
			t.Errorf("verifyConfigFile should succeed for valid config: %v", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")

		if err := os.WriteFile(configPath, []byte("{invalid json}"), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		err := verifyConfigFile(configPath)
		if err == nil {
			t.Error("verifyConfigFile should fail for invalid JSON")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		err := verifyConfigFile("/nonexistent/config.json")
		if err == nil {
			t.Error("verifyConfigFile should fail for missing file")
		}
	})
}

func TestCleanupLegacyConfig(t *testing.T) {
	// This test verifies the cleanup logic works correctly
	// Note: cleanupLegacyConfig uses hardcoded paths, so we can't easily unit test it
	// The integration tests will verify the full migration path
	t.Run("function exists and is callable", func(t *testing.T) {
		// Just verify it doesn't panic when called
		// In a real system, this would clean up /etc/nbc and overlay paths
		cleanupLegacyConfig()
	})
}

func TestConfigConstants(t *testing.T) {
	t.Run("new config path is in /var", func(t *testing.T) {
		if !strings.HasPrefix(SystemConfigDir, "/var/lib/nbc") {
			t.Errorf("SystemConfigDir should be in /var/lib/nbc, got %s", SystemConfigDir)
		}
		if !strings.HasPrefix(SystemConfigFile, "/var/lib/nbc/state") {
			t.Errorf("SystemConfigFile should be in /var/lib/nbc/state, got %s", SystemConfigFile)
		}
	})

	t.Run("legacy paths are in /etc", func(t *testing.T) {
		if !strings.HasPrefix(LegacySystemConfigDir, "/etc/nbc") {
			t.Errorf("LegacySystemConfigDir should be /etc/nbc, got %s", LegacySystemConfigDir)
		}
		if !strings.HasPrefix(LegacySystemConfigFile, "/etc/nbc") {
			t.Errorf("LegacySystemConfigFile should be in /etc/nbc, got %s", LegacySystemConfigFile)
		}
	})
}
