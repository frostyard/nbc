package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
	// Get the active root partition
	rootPartition, err := GetActiveRootPartition()
	if err != nil {
		return "", fmt.Errorf("failed to determine active root partition: %w", err)
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
