package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SystemConfigDir is the directory for nbc system configuration
	// Stored in /var/lib/nbc/state/ to avoid /etc overlay complications
	SystemConfigDir = "/var/lib/nbc/state"
	// SystemConfigFile is the main configuration file
	SystemConfigFile = "/var/lib/nbc/state/config.json"
	// LegacySystemConfigDir is the old location (for migration)
	LegacySystemConfigDir = "/etc/nbc"
	// LegacySystemConfigFile is the old config file location (for migration)
	LegacySystemConfigFile = "/etc/nbc/config.json"
	// LegacyOverlayUpperNbc is the old config location in overlay upper layer
	LegacyOverlayUpperNbc = "/var/lib/nbc/etc-overlay/upper/nbc"
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

// SystemConfig represents the system configuration stored in /var/lib/nbc/state/
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

// WriteSystemConfig writes system configuration to /var/lib/nbc/state/config.json
// If legacy config exists at /etc/nbc/config.json, it will be cleaned up after
// successful write and verification.
func WriteSystemConfig(config *SystemConfig, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write config to %s\n", SystemConfigFile)
		return nil
	}

	// Create directory if it doesn't exist (0755 = rwxr-xr-x)
	if err := os.MkdirAll(SystemConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file (0644 = rw-r--r-- = world-readable, root-writable)
	if err := os.WriteFile(SystemConfigFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Verify the file can be read back and parsed
	if err := verifyConfigFile(SystemConfigFile); err != nil {
		return fmt.Errorf("config verification failed: %w", err)
	}

	// Clean up legacy locations after successful write and verification
	cleanupLegacyConfig()

	fmt.Printf("  Wrote system configuration to %s\n", SystemConfigFile)
	return nil
}

// verifyConfigFile reads back a config file and verifies it can be parsed
func verifyConfigFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read back config: %w", err)
	}

	var config SystemConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	return nil
}

// cleanupLegacyConfig removes config from legacy locations after migration
// Errors are intentionally ignored as this is best-effort cleanup
func cleanupLegacyConfig() {
	// Remove legacy /etc/nbc/config.json and directory
	if _, err := os.Stat(LegacySystemConfigFile); err == nil {
		_ = os.Remove(LegacySystemConfigFile)
		// Try to remove the directory (will fail if not empty, which is fine)
		_ = os.Remove(LegacySystemConfigDir)
	}

	// Remove config from overlay upper layer (legacy location)
	_ = os.RemoveAll(LegacyOverlayUpperNbc)
}

// ReadSystemConfig reads system configuration from /var/lib/nbc/state/config.json
// Falls back to legacy location /etc/nbc/config.json for older installations
func ReadSystemConfig() (*SystemConfig, error) {
	// Try new location first
	data, err := os.ReadFile(SystemConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Try legacy location for older installations
			data, err = os.ReadFile(LegacySystemConfigFile)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf("system configuration not found at %s or %s (system may not be installed with nbc)", SystemConfigFile, LegacySystemConfigFile)
				}
				return nil, fmt.Errorf("failed to read legacy config file: %w", err)
			}
			// Successfully read from legacy location - config will be migrated on next write
		} else {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var config SystemConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// WriteSystemConfigToVar writes system configuration to the mounted /var partition
// varMountPoint is the path where the var partition is mounted (e.g., /mnt/var or /mnt/root/var)
func WriteSystemConfigToVar(varMountPoint string, config *SystemConfig, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would write config to %s/lib/nbc/state/config.json\n", varMountPoint)
		return nil
	}

	// Config goes to {varMountPoint}/lib/nbc/state/config.json
	// which corresponds to /var/lib/nbc/state/config.json on the running system
	configDir := filepath.Join(varMountPoint, "lib", "nbc", "state")
	configFile := filepath.Join(configDir, "config.json")

	// Create directory if it doesn't exist (0755 = rwxr-xr-x)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory in var: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file (0644 = rw-r--r-- = world-readable, root-writable)
	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Verify the file can be read back and parsed
	if err := verifyConfigFile(configFile); err != nil {
		return fmt.Errorf("config verification failed: %w", err)
	}

	fmt.Printf("  Wrote system configuration to var partition\n")
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

	// Write back using WriteSystemConfig (handles migration)
	return WriteSystemConfig(config, false)
}
