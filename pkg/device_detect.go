package pkg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
// - Device names are only used during install/update operations, not at boot time. Auto-detection now works by:
//   1. Reading kernel cmdline to find active root partition (root=UUID=... or root=/dev/mapper/...)
//   2. For LUKS: Using cryptsetup status to find backing device
//   3. Extracting parent disk from partition path
//   4. Verifying against stored disk ID in /var/lib/nbc/state/config.json (set during install)
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

	// Handle NVMe, MMC, and loop devices (nvme0n1p3 -> nvme0n1, mmcblk0p3 -> mmcblk0, loop0p3 -> loop0)
	if strings.Contains(partition, "nvme") || strings.Contains(partition, "mmcblk") || strings.HasPrefix(partition, "loop") {
		// For these devices, the partition separator 'p' comes after the device number
		// We need to find 'p' followed by digits, but NOT the 'p' in "loop"
		// Strategy: find the last 'p' that is followed only by digits to the end

		// Start from the end and work backwards to find 'p' + digits pattern
		for i := len(partition) - 1; i >= 0; i-- {
			if partition[i] == 'p' && i < len(partition)-1 {
				// Check if everything after 'p' is digits
				suffix := partition[i+1:]
				isAllDigits := true
				for _, c := range suffix {
					if c < '0' || c > '9' {
						isAllDigits = false
						break
					}
				}
				if isAllDigits && len(suffix) > 0 {
					// For loop devices, ensure this isn't the 'p' in "loop"
					// The device part should end with a digit before 'p'
					if i > 0 && partition[i-1] >= '0' && partition[i-1] <= '9' {
						return "/dev/" + partition[:i], nil
					}
				}
			}
		}
		return "", fmt.Errorf("invalid nvme/mmcblk/loop partition format: %s", partition)
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

// getLUKSBackingDevice gets the underlying physical device for a LUKS mapper device
// For example: /dev/mapper/root1 -> /dev/nvme0n1p2
func getLUKSBackingDevice(mapperDevice string) (string, error) {
	// Extract mapper name (e.g., "root1" from "/dev/mapper/root1")
	mapperName := strings.TrimPrefix(mapperDevice, "/dev/mapper/")

	// Use cryptsetup status to get device info
	cmd := exec.Command("cryptsetup", "status", mapperName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get cryptsetup status for %s: %w", mapperName, err)
	}

	// Parse output to find "device:" line
	// Example line: "  device:  /dev/nvme0n1p3"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "device:") {
			// Extract device path after "device:"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}

	return "", fmt.Errorf("could not find device line in cryptsetup status output for %s", mapperName)
}

// GetCurrentBootDevice determines the disk device that the system booted from
func GetCurrentBootDevice(progress Reporter) (string, error) {
	// Get the active root partition from kernel command line
	rootPartition, err := GetActiveRootPartition()
	if err != nil {
		return "", fmt.Errorf("failed to determine active root partition: %w", err)
	}

	var physicalPartition string

	// Handle device mapper paths (encrypted systems)
	// For /dev/mapper/root1 or /dev/mapper/root2, we need to find the underlying device
	if strings.HasPrefix(rootPartition, "/dev/mapper/") {
		// Use cryptsetup to find the backing device
		backingDevice, err := getLUKSBackingDevice(rootPartition)
		if err != nil {
			// Fall back to config file if cryptsetup fails
			config, configErr := ReadSystemConfig()
			if configErr == nil && config.Device != "" {
				if _, statErr := os.Stat(config.Device); statErr == nil {
					progress.Warning("could not auto-detect device from LUKS (%v), using config: %s", err, config.Device)
					return config.Device, nil
				}
			}
			return "", fmt.Errorf("encrypted root detected (%s) but could not determine backing device: %w", rootPartition, err)
		}
		physicalPartition = backingDevice
	} else {
		// Non-encrypted system, use partition directly
		physicalPartition = rootPartition
	}

	// Extract the parent device from the partition
	device, err := GetBootDeviceFromPartition(physicalPartition)
	if err != nil {
		return "", fmt.Errorf("failed to extract device from partition %s: %w", physicalPartition, err)
	}

	// Verify the device exists
	if _, err := os.Stat(device); os.IsNotExist(err) {
		return "", fmt.Errorf("device %s does not exist", device)
	}

	// Verify against config if available (for disk replacement detection)
	config, err := ReadSystemConfig()
	if err == nil && config.DiskID != "" {
		match, verifyErr := VerifyDiskID(device, config.DiskID)
		if verifyErr != nil {
			progress.Warning("failed to verify disk ID: %v", verifyErr)
		} else if !match {
			actualID, _ := GetDiskID(device)
			progress.Warning("disk ID mismatch!")
			progress.Warning("  Auto-detected device: %s", device)
			progress.Warning("  Expected disk ID: %s", config.DiskID)
			progress.Warning("  Actual disk ID:   %s", actualID)
			progress.Warning("  This may indicate the wrong disk or disk replacement")
			progress.Warning("  Proceeding with caution - verify before updating!")
		}
	}

	return device, nil
}

// GetCurrentBootDeviceInfo returns detailed information about the boot device
func GetCurrentBootDeviceInfo(ctx context.Context, verbose bool, progress Reporter) (string, error) {
	device, err := GetCurrentBootDevice(progress)
	if err != nil {
		return "", err
	}

	if verbose && progress != nil {
		// Get disk info
		deviceName := filepath.Base(device)
		diskInfo, err := getDiskInfo(deviceName)
		if err != nil {
			progress.Message("Auto-detected boot device: %s", device)
		} else {
			progress.Message("Auto-detected boot device: %s (%s, %s)",
				device, diskInfo.Model, FormatSize(diskInfo.Size))
		}
	}

	return device, nil
}
