package pkg

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DiskInfo represents information about a physical disk
type DiskInfo struct {
	Device      string
	Size        uint64
	Model       string
	IsRemovable bool
	Partitions  []PartitionInfo
}

// PartitionInfo represents information about a disk partition
type PartitionInfo struct {
	Device     string
	Size       uint64
	MountPoint string
	FileSystem string
}

// ListDisks returns a list of available physical disks
func ListDisks() ([]DiskInfo, error) {
	disks := []DiskInfo{}

	// Find all block devices
	devices, err := filepath.Glob("/sys/block/sd*")
	if err != nil {
		return nil, fmt.Errorf("failed to list block devices: %w", err)
	}

	// Also check for nvme devices
	nvmeDevices, err := filepath.Glob("/sys/block/nvme*n*")
	if err == nil {
		devices = append(devices, nvmeDevices...)
	}

	// Also check for vd* (virtio) devices
	vdDevices, err := filepath.Glob("/sys/block/vd*")
	if err == nil {
		devices = append(devices, vdDevices...)
	}

	for _, device := range devices {
		deviceName := filepath.Base(device)
		diskInfo, err := getDiskInfo(deviceName)
		if err != nil {
			continue // Skip devices we can't read
		}
		disks = append(disks, diskInfo)
	}

	return disks, nil
}

// getDiskInfo retrieves detailed information about a disk
func getDiskInfo(device string) (DiskInfo, error) {
	info := DiskInfo{
		Device: "/dev/" + device,
	}

	// Get size from sysfs
	sizePath := filepath.Join("/sys/block", device, "size")
	sizeData, err := os.ReadFile(sizePath)
	if err != nil {
		return info, fmt.Errorf("failed to read disk size: %w", err)
	}
	sizeBlocks, err := strconv.ParseUint(strings.TrimSpace(string(sizeData)), 10, 64)
	if err != nil {
		return info, fmt.Errorf("failed to parse disk size: %w", err)
	}
	info.Size = sizeBlocks * 512 // Convert 512-byte blocks to bytes

	// Check if removable
	removablePath := filepath.Join("/sys/block", device, "removable")
	if removableData, err := os.ReadFile(removablePath); err == nil {
		info.IsRemovable = strings.TrimSpace(string(removableData)) == "1"
	}

	// Get model
	modelPath := filepath.Join("/sys/block", device, "device", "model")
	if modelData, err := os.ReadFile(modelPath); err == nil {
		info.Model = strings.TrimSpace(string(modelData))
	}

	// Get partitions
	partitions, err := getPartitions(device)
	if err == nil {
		info.Partitions = partitions
	}

	return info, nil
}

// getPartitions returns partition information for a disk
func getPartitions(device string) ([]PartitionInfo, error) {
	partitions := []PartitionInfo{}

	// List partition directories
	partPattern := filepath.Join("/sys/block", device, device+"*")
	partDirs, err := filepath.Glob(partPattern)
	if err != nil {
		return nil, err
	}

	for _, partDir := range partDirs {
		partName := filepath.Base(partDir)
		if partName == device {
			continue // Skip the device itself
		}

		partInfo := PartitionInfo{
			Device: "/dev/" + partName,
		}

		// Get partition size
		sizePath := filepath.Join(partDir, "size")
		if sizeData, err := os.ReadFile(sizePath); err == nil {
			sizeBlocks, _ := strconv.ParseUint(strings.TrimSpace(string(sizeData)), 10, 64)
			partInfo.Size = sizeBlocks * 512
		}

		// Try to get mount point and filesystem from /proc/mounts
		if mounts, err := os.ReadFile("/proc/mounts"); err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(mounts)))
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 3 && fields[0] == partInfo.Device {
					partInfo.MountPoint = fields[1]
					partInfo.FileSystem = fields[2]
					break
				}
			}
		}

		partitions = append(partitions, partInfo)
	}

	return partitions, nil
}

// ValidateDisk checks if a disk is suitable for installation
func ValidateDisk(device string, minSize uint64) error {
	// Check if device exists
	if _, err := os.Stat(device); os.IsNotExist(err) {
		return fmt.Errorf("device %s does not exist", device)
	}

	// Get disk info
	deviceName := strings.TrimPrefix(device, "/dev/")
	diskInfo, err := getDiskInfo(deviceName)
	if err != nil {
		return fmt.Errorf("failed to get disk info: %w", err)
	}

	// Check minimum size
	if diskInfo.Size < minSize {
		return fmt.Errorf("disk is too small: %d bytes (minimum: %d bytes)", diskInfo.Size, minSize)
	}

	// Check if any partitions are mounted
	for _, part := range diskInfo.Partitions {
		if part.MountPoint != "" {
			return fmt.Errorf("partition %s is mounted at %s (please unmount first)", part.Device, part.MountPoint)
		}
	}

	return nil
}

// WipeDisk securely wipes a disk's partition table
func WipeDisk(device string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would wipe disk: %s\n", device)
		return nil
	}

	// Use wipefs to remove filesystem signatures
	cmd := exec.Command("wipefs", "--all", device)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to wipe disk: %w\nOutput: %s", err, string(output))
	}

	// Use sgdisk to zap GPT structures
	cmd = exec.Command("sgdisk", "--zap-all", device)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to zap GPT: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// FormatSize formats a byte size as human-readable string
func FormatSize(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// IsBlockDevice checks if a path is a block device
func IsBlockDevice(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0
}

// GetDiskByPath resolves a disk path (handles by-id, by-uuid, etc.)
func GetDiskByPath(path string) (string, error) {
	// If it's already a device node, check if it's valid
	if strings.HasPrefix(path, "/dev/") && IsBlockDevice(path) {
		return path, nil
	}

	// Try to resolve symlink
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	if IsBlockDevice(resolved) {
		return resolved, nil
	}

	return "", fmt.Errorf("path %s is not a block device", path)
}

// ParseDeviceName extracts the base device name without partition number
func ParseDeviceName(device string) (string, error) {
	// Remove /dev/ prefix
	device = strings.TrimPrefix(device, "/dev/")

	// Handle different device naming schemes
	patterns := []string{
		`^(sd[a-z]+)`,    // sda, sdb, etc.
		`^(nvme\d+n\d+)`, // nvme0n1, etc.
		`^(vd[a-z]+)`,    // vda, vdb, etc. (virtio)
		`^(mmcblk\d+)`,   // mmcblk0, etc. (SD/MMC)
		`^(loop\d+)`,     // loop0, loop1, etc. (loopback)
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(device); len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("unrecognized device name format: %s", device)
}

// GetDiskID returns the stable disk identifier from /dev/disk/by-id for a given device
// Returns the by-id path (e.g., "nvme-Samsung_SSD_980_PRO_2TB_S1234567890") or empty string if not found
func GetDiskID(device string) (string, error) {
	// Normalize device path
	device = strings.TrimPrefix(device, "/dev/")

	// Read /dev/disk/by-id directory
	byIDDir := "/dev/disk/by-id"
	entries, err := os.ReadDir(byIDDir)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", byIDDir, err)
	}

	// Find entries that point to our device
	var candidates []string
	for _, entry := range entries {
		// Skip partition entries (contain -part)
		if strings.Contains(entry.Name(), "-part") {
			continue
		}

		// Resolve the symlink
		linkPath := filepath.Join(byIDDir, entry.Name())
		target, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}

		// Check if this points to our device
		targetBase := filepath.Base(target)
		if targetBase == device {
			candidates = append(candidates, entry.Name())
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no disk ID found for device %s", device)
	}

	// Prefer specific IDs over generic ones
	// Priority: nvme-* > ata-* > scsi-* > wwn-*
	for _, prefix := range []string{"nvme-", "ata-", "scsi-"} {
		for _, candidate := range candidates {
			if strings.HasPrefix(candidate, prefix) && !strings.HasPrefix(candidate, prefix+"eui.") {
				return candidate, nil
			}
		}
	}

	// Return the first candidate if no preferred match
	return candidates[0], nil
}

// VerifyDiskID checks if a device matches the expected disk ID
// Returns true if they match, false otherwise
func VerifyDiskID(device, expectedDiskID string) (bool, error) {
	if expectedDiskID == "" {
		// No disk ID to verify against
		return true, nil
	}

	actualDiskID, err := GetDiskID(device)
	if err != nil {
		// Can't get disk ID, but we have one stored - this is suspicious
		return false, fmt.Errorf("failed to get disk ID for %s: %w", device, err)
	}

	return actualDiskID == expectedDiskID, nil
}
