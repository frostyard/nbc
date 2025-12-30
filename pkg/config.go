package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SystemConfigDir is the directory for nbc system configuration
	SystemConfigDir = "/etc/nbc"
	// SystemConfigFile is the main configuration file
	SystemConfigFile = "/etc/nbc/config.json"
	// NBCBootedMarker is the runtime marker file indicating nbc-managed boot
	// Created by tmpfiles.d during boot, similar to /run/ostree-booted
	NBCBootedMarker = "/run/nbc-booted"
	// NBCTmpfilesConfig is the tmpfiles.d config that creates the marker
	NBCTmpfilesConfig = "/usr/lib/tmpfiles.d/nbc.conf"
)

// IsNBCBooted checks if the current system was booted via nbc.
// Returns true if /run/nbc-booted exists (created by tmpfiles.d during boot).
func IsNBCBooted() bool {
	_, err := os.Stat(NBCBootedMarker)
	return err == nil
}

// InstallTmpfilesConfig installs a tmpfiles.d config to create /run/nbc-booted marker.
// This marker is created by systemd-tmpfiles during boot, after /run is mounted.
// Unlike the dracut approach, this ensures the marker exists after switch_root
// when systemd mounts a fresh tmpfs on /run.
func InstallTmpfilesConfig(targetDir string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would install tmpfiles.d config for nbc-booted marker\n")
		return nil
	}

	// Content for the tmpfiles.d config
	// Format: type path mode user group age argument
	// 'f' = create file if it doesn't exist
	tmpfilesContent := `# nbc boot marker - indicates system was installed by nbc
# Similar to /run/ostree-booted for ostree-managed systems
f /run/nbc-booted 0644 root root - nbc
`

	tmpfilesDir := filepath.Join(targetDir, "usr", "lib", "tmpfiles.d")
	if err := os.MkdirAll(tmpfilesDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmpfiles.d directory: %w", err)
	}

	tmpfilesPath := filepath.Join(tmpfilesDir, "nbc.conf")
	if err := os.WriteFile(tmpfilesPath, []byte(tmpfilesContent), 0644); err != nil {
		return fmt.Errorf("failed to write tmpfiles.d config: %w", err)
	}

	fmt.Println("  Installed tmpfiles.d config for /run/nbc-booted marker")
	return nil
}

// FilesystemType represents the supported filesystem types
type FilesystemType string

const (
	FilesystemExt4  FilesystemType = "ext4"
	FilesystemBtrfs FilesystemType = "btrfs"
)

// EncryptionConfig stores LUKS encryption configuration for A/B updates
type EncryptionConfig struct {
	Enabled       bool   `json:"enabled"`         // Whether LUKS encryption is enabled
	TPM2          bool   `json:"tpm2"`            // Whether TPM2 auto-unlock is enabled
	Root1LUKSUUID string `json:"root1_luks_uuid"` // LUKS UUID for root1 partition
	Root2LUKSUUID string `json:"root2_luks_uuid"` // LUKS UUID for root2 partition
	VarLUKSUUID   string `json:"var_luks_uuid"`   // LUKS UUID for var partition
}

// SystemConfig represents the system configuration stored in /etc/nbc/
type SystemConfig struct {
	ImageRef       string            `json:"image_ref"`            // Container image reference
	ImageDigest    string            `json:"image_digest"`         // Container image digest (sha256:...)
	Device         string            `json:"device"`               // Installation device (e.g. /dev/sda, /dev/nvme0n1)
	DiskID         string            `json:"disk_id,omitempty"`    // Stable disk identifier from /dev/disk/by-id
	InstallDate    string            `json:"install_date"`         // Installation timestamp
	KernelArgs     []string          `json:"kernel_args"`          // Custom kernel arguments
	BootloaderType string            `json:"bootloader_type"`      // Bootloader type (grub2, systemd-boot)
	FilesystemType string            `json:"filesystem_type"`      // Filesystem type (ext4, btrfs)
	Encryption     *EncryptionConfig `json:"encryption,omitempty"` // Encryption configuration (nil if not encrypted)
}

// WriteSystemConfig writes system configuration to /etc/nbc/config.json
func WriteSystemConfig(config *SystemConfig, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write config to %s\n", SystemConfigFile)
		return nil
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(SystemConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(SystemConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("  Wrote system configuration to %s\n", SystemConfigFile)
	return nil
}

// ReadSystemConfig reads system configuration from /etc/nbc/config.json
func ReadSystemConfig() (*SystemConfig, error) {
	data, err := os.ReadFile(SystemConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("system configuration not found at %s (system may not be installed with nbc)", SystemConfigFile)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config SystemConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// WriteSystemConfigToTarget writes system configuration to the target root filesystem
func WriteSystemConfigToTarget(targetDir string, config *SystemConfig, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write config to %s/etc/nbc/config.json\n", targetDir)
		return nil
	}

	configDir := filepath.Join(targetDir, "etc", "nbc")
	configFile := filepath.Join(configDir, "config.json")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory in target: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("  Wrote system configuration to target filesystem\n")
	return nil
}

// UpdateSystemConfigImageRef updates the image reference and digest in the system config
func UpdateSystemConfigImageRef(imageRef, imageDigest string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would update config with image: %s (digest: %s)\n", imageRef, imageDigest)
		return nil
	}

	// Read existing config
	config, err := ReadSystemConfig()
	if err != nil {
		return err
	}

	// Update image reference and digest
	config.ImageRef = imageRef
	config.ImageDigest = imageDigest

	// Write back
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(SystemConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("  Updated system configuration with new image: %s\n", imageRef)
	if imageDigest != "" {
		fmt.Printf("  Digest: %s\n", imageDigest)
	}
	return nil
}
