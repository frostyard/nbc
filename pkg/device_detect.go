package pkg

import (
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
//   4. Verifying against stored disk IDg.json (set during install)
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
func GetCurrentBootDevice() (string, error) {
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
					fmt.Fprintf(os.Stderr, "Warning: could not auto-detect device from LUKS (%v), using config: %s\n", err, config.Device)
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
			fmt.Fprintf(os.Stderr, "Warning: failed to verify disk ID: %v\n", verifyErr)
		} else if !match {
			actualID, _ := GetDiskID(device)
			fmt.Fprintf(os.Stderr, "Warning: disk ID mismatch!\n")
			fmt.Fprintf(os.Stderr, "  Auto-detected device: %s\n", device)
			fmt.Fprintf(os.Stderr, "  Expected disk ID: %s\n", config.DiskID)
			fmt.Fprintf(os.Stderr, "  Actual disk ID:   %s\n", actualID)
			fmt.Fprintf(os.Stderr, "  This may indicate the wrong disk or disk replacement.\n")
			fmt.Fprintf(os.Stderr, "  Proceeding with caution - verify before updating!\n")
		}
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
