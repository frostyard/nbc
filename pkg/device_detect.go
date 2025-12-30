package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Device Detection and Naming Stability
//
// This file provides device detection for the update command, which auto-detects
// the boot device from the running system. The install command requires an explicit
// --device argument since it's installing to a (potentially empty) disk.
//
// Design Notes on Device Naming Stability:
//
// - Device names like /dev/nvme0n1 or /dev/sda can change between boots
// - The RUNNING SYSTEM uses UUIDs everywhere (kernel cmdline, fstab, bootloader)
// - Device names are only used during install/update operations, not at boot time
// - Auto-detection reads the device from /etc/nbc/config.json (set during install)
// - Disk ID from /dev/disk/by-id is stored and verified to detect disk replacement
// - For encrypted systems, device names map to /dev/mapper/<name> at runtime
//
// This means the system boots reliably using UUIDs, while install/update commands
// use device names only during their execution (which is safe since you know the
// device at that moment).

// GetBootDeviceFromPartition extracts the parent disk device from a partition path
// Example: /dev/sda3 -> /dev/sda, /dev/nvme0n1p3 -> /dev/nvme0n1
func GetBootDeviceFromPartition(partition string) (string, error) {
	// Remove /dev/ prefix if present
	partition = strings.TrimPrefix(partition, "/dev/")

	// Handle NVMe and MMC devices (nvme0n1p3 -> nvme0n1, mmcblk0p3 -> mmcblk0)
	if strings.Contains(partition, "nvme") || strings.Contains(partition, "mmcblk") {
		// Find the 'p' separator
		idx := strings.LastIndex(partition, "p")
		if idx == -1 {
			return "", fmt.Errorf("invalid nvme/mmcblk partition format: %s", partition)
		}
		device := partition[:idx]
		return "/dev/" + device, nil
	}

	// Handle standard devices (sda3 -> sda, vda3 -> vda)
	// Strip trailing digits
	var deviceName string
	for i := len(partition) - 1; i >= 0; i-- {
		if partition[i] < '0' || partition[i] > '9' {
			deviceName = partition[:i+1]
			break
		}
	}

	if deviceName == "" {
		return "", fmt.Errorf("could not extract device name from partition: %s", partition)
	}

	return "/dev/" + deviceName, nil
}

// GetCurrentBootDevice determines the disk device that the system booted from
func GetCurrentBootDevice() (string, error) {
	// First, try to read the device from the system config
	// This is the most reliable method, especially for encrypted systems
	config, err := ReadSystemConfig()
	if err == nil && config.Device != "" {
		// Verify the device exists
		if _, err := os.Stat(config.Device); err == nil {
			// Verify disk ID if available
			if config.DiskID != "" {
				match, verifyErr := VerifyDiskID(config.Device, config.DiskID)
				if verifyErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to verify disk ID: %v\n", verifyErr)
				} else if !match {
					actualID, _ := GetDiskID(config.Device)
					fmt.Fprintf(os.Stderr, "Warning: disk ID mismatch!\n")
					fmt.Fprintf(os.Stderr, "  Device: %s\n", config.Device)
					fmt.Fprintf(os.Stderr, "  Expected disk ID: %s\n", config.DiskID)
					fmt.Fprintf(os.Stderr, "  Actual disk ID:   %s\n", actualID)
					fmt.Fprintf(os.Stderr, "  This may indicate the wrong disk or disk replacement.\n")
					fmt.Fprintf(os.Stderr, "  Proceeding with caution - verify before updating!\n")
				}
			}
			return config.Device, nil
		}
	}

	// Fall back to parsing kernel command line
	// Get the active root partition
	rootPartition, err := GetActiveRootPartition()
	if err != nil {
		return "", fmt.Errorf("failed to determine active root partition: %w", err)
	}

	// Handle device mapper paths (encrypted systems)
	// For /dev/mapper/root1 or /dev/mapper/root2, we need to find the underlying device
	if strings.HasPrefix(rootPartition, "/dev/mapper/") {
		// Try to get device from system config (already tried above, but let's be explicit)
		if config != nil && config.Device != "" {
			return config.Device, nil
		}
		// Cannot determine underlying device without config
		return "", fmt.Errorf("encrypted root detected (%s) but no system config found - use --device to specify", rootPartition)
	}

	// Extract the parent device
	device, err := GetBootDeviceFromPartition(rootPartition)
	if err != nil {
		return "", fmt.Errorf("failed to extract device from partition %s: %w", rootPartition, err)
	}

	// Verify the device exists
	if _, err := os.Stat(device); os.IsNotExist(err) {
		return "", fmt.Errorf("device %s does not exist", device)
	}

	return device, nil
}

// GetCurrentBootDeviceInfo returns detailed information about the boot device
func GetCurrentBootDeviceInfo(verbose bool) (string, error) {
	device, err := GetCurrentBootDevice()
	if err != nil {
		return "", err
	}

	if verbose {
		// Get disk info
		deviceName := filepath.Base(device)
		diskInfo, err := getDiskInfo(deviceName)
		if err != nil {
			fmt.Printf("Auto-detected boot device: %s\n", device)
		} else {
			fmt.Printf("Auto-detected boot device: %s (%s, %s)\n",
				device, diskInfo.Model, FormatSize(diskInfo.Size))
		}
	}

	return device, nil
}
