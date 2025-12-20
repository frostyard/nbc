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
)

// FilesystemType represents the supported filesystem types
type FilesystemType string

const (
	FilesystemExt4  FilesystemType = "ext4"
	FilesystemBtrfs FilesystemType = "btrfs"
)

// SystemConfig represents the system configuration stored in /etc/nbc/
type SystemConfig struct {
	ImageRef       string   `json:"image_ref"`       // Container image reference
	ImageDigest    string   `json:"image_digest"`    // Container image digest (sha256:...)
	Device         string   `json:"device"`          // Installation device
	InstallDate    string   `json:"install_date"`    // Installation timestamp
	KernelArgs     []string `json:"kernel_args"`     // Custom kernel arguments
	BootloaderType string   `json:"bootloader_type"` // Bootloader type (grub2, systemd-boot)
	FilesystemType string   `json:"filesystem_type"` // Filesystem type (ext4, btrfs)
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
