package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PartitionScheme defines the disk partitioning layout
type PartitionScheme struct {
	BootPartition  string // Boot partition (EFI System Partition, FAT32, 2GB) - holds EFI binaries + kernel/initramfs
	Root1Partition string // First root filesystem partition (12GB)
	Root2Partition string // Second root filesystem partition (12GB)
	VarPartition   string // /var partition (remaining space)
	FilesystemType string // Filesystem type for root/var partitions (ext4, btrfs)

	// LUKS encryption (optional)
	Encrypted   bool          // Whether partitions are LUKS encrypted
	LUKSDevices []*LUKSDevice // Opened LUKS devices (for cleanup)
}

// CreatePartitions creates a GPT partition table with EFI, boot, and root partitions
func CreatePartitions(device string, dryRun bool) (*PartitionScheme, error) {
	if dryRun {
		fmt.Printf("[DRY RUN] Would create partitions on %s\n", device)
		deviceBase := filepath.Base(device)
		return &PartitionScheme{
			BootPartition:  "/dev/" + deviceBase + "1",
			Root1Partition: "/dev/" + deviceBase + "2",
			Root2Partition: "/dev/" + deviceBase + "3",
			VarPartition:   "/dev/" + deviceBase + "4",
		}, nil
	}

	fmt.Println("Creating GPT partition table...")

	// Use sgdisk to create partitions
	// Partition 1: Boot/EFI System Partition (2GB, FAT32) - holds EFI binaries + kernel/initramfs
	// Partition 2: First root filesystem (12GB)
	// Partition 3: Second root filesystem (12GB)
	// Partition 4: /var partition (remaining space)

	commands := [][]string{
		// Create GPT partition table
		{"sgdisk", "--clear", device},
		// Create boot/EFI partition (2GB, type EF00 = EFI System Partition)
		// This single partition serves as both ESP and boot - holds EFI binaries + kernel/initramfs
		{"sgdisk", "--new=1:0:+2G", "--typecode=1:EF00", "--change-name=1:boot", device},
		// Create first root partition (12GB, type 8300 = generic Linux data)
		// NOT using discoverable root partition type - root specified via kernel cmdline
		{"sgdisk", "--new=2:0:+12G", "--typecode=2:8300", "--change-name=2:root1", device},
		// Create second root partition (12GB, type 8300 = generic Linux data)
		// NOT using discoverable root partition type - allows A/B updates with explicit control
		{"sgdisk", "--new=3:0:+12G", "--typecode=3:8300", "--change-name=3:root2", device},
		// Create /var partition (remaining space, type 8300 = generic Linux data)
		// NOT using auto-discoverable var type (4d21b016...) - would require machine-id binding
		{"sgdisk", "--new=4:0:0", "--typecode=4:8300", "--change-name=4:var", device},
	}

	for _, cmdArgs := range commands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to run %s: %w\nOutput: %s", cmdArgs[0], err, string(output))
		}
	}

	// Inform kernel of partition changes
	deviceBase := filepath.Base(device)
	if strings.HasPrefix(deviceBase, "loop") {
		// For loop devices, use losetup --partscan to force partition re-read
		if err := exec.Command("losetup", "--partscan", device).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: losetup --partscan failed: %v\n", err)
		}
	}
	if err := exec.Command("partprobe", device).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: partprobe failed: %v\n", err)
	}

	// Wait for device nodes to appear
	if err := exec.Command("udevadm", "settle").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: udevadm settle failed: %v\n", err)
	}

	// Determine partition device names
	// Handle different device naming conventions
	// nvme, mmcblk, and loop devices use "p" prefix for partitions
	var part1, part2, part3, part4 string
	if strings.HasPrefix(deviceBase, "nvme") || strings.HasPrefix(deviceBase, "mmcblk") || strings.HasPrefix(deviceBase, "loop") {
		part1 = device + "p1"
		part2 = device + "p2"
		part3 = device + "p3"
		part4 = device + "p4"
	} else {
		part1 = device + "1"
		part2 = device + "2"
		part3 = device + "3"
		part4 = device + "4"
	}

	scheme := &PartitionScheme{
		BootPartition:  part1,
		Root1Partition: part2,
		Root2Partition: part3,
		VarPartition:   part4,
	}

	fmt.Printf("Created partitions:\n")
	fmt.Printf("  Boot:  %s\n", scheme.BootPartition)
	fmt.Printf("  Root1: %s\n", scheme.Root1Partition)
	fmt.Printf("  Root2: %s\n", scheme.Root2Partition)
	fmt.Printf("  Var:   %s\n", scheme.VarPartition)

	return scheme, nil
}

// SetupLUKS creates LUKS containers on root and var partitions
// Returns the opened LUKS devices (must be closed during cleanup)
func SetupLUKS(scheme *PartitionScheme, passphrase string, dryRun bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would create LUKS containers on root1, root2, var")
		return nil
	}

	fmt.Println("Setting up LUKS encryption...")

	// Create and open LUKS containers for each partition
	partitions := []struct {
		partition  string
		mapperName string
	}{
		{scheme.Root1Partition, "root1"},
		{scheme.Root2Partition, "root2"},
		{scheme.VarPartition, "var"},
	}

	var luksDevices []*LUKSDevice

	for _, p := range partitions {
		// Create LUKS container
		if err := CreateLUKSContainer(p.partition, passphrase); err != nil {
			// Close any already-opened devices on error
			for _, dev := range luksDevices {
				_ = CloseLUKS(dev.MapperName)
			}
			return fmt.Errorf("failed to create LUKS on %s: %w", p.partition, err)
		}

		// Open LUKS container
		dev, err := OpenLUKS(p.partition, p.mapperName, passphrase)
		if err != nil {
			// Close any already-opened devices on error
			for _, d := range luksDevices {
				_ = CloseLUKS(d.MapperName)
			}
			return fmt.Errorf("failed to open LUKS on %s: %w", p.partition, err)
		}

		luksDevices = append(luksDevices, dev)
		fmt.Printf("  LUKS container %s ready at %s (UUID: %s)\n", p.mapperName, dev.MapperPath, dev.LUKSUUID)
	}

	scheme.Encrypted = true
	scheme.LUKSDevices = luksDevices

	fmt.Println("LUKS encryption setup complete")
	return nil
}

// GetRoot1Device returns the device path to use for root1 (mapper or raw partition)
func (s *PartitionScheme) GetRoot1Device() string {
	if s.Encrypted && len(s.LUKSDevices) > 0 {
		for _, dev := range s.LUKSDevices {
			if dev.MapperName == "root1" {
				return dev.MapperPath
			}
		}
	}
	return s.Root1Partition
}

// GetRoot2Device returns the device path to use for root2 (mapper or raw partition)
func (s *PartitionScheme) GetRoot2Device() string {
	if s.Encrypted && len(s.LUKSDevices) > 0 {
		for _, dev := range s.LUKSDevices {
			if dev.MapperName == "root2" {
				return dev.MapperPath
			}
		}
	}
	return s.Root2Partition
}

// GetVarDevice returns the device path to use for var (mapper or raw partition)
func (s *PartitionScheme) GetVarDevice() string {
	if s.Encrypted && len(s.LUKSDevices) > 0 {
		for _, dev := range s.LUKSDevices {
			if dev.MapperName == "var" {
				return dev.MapperPath
			}
		}
	}
	return s.VarPartition
}

// GetLUKSDevice returns the LUKS device for a given mapper name
func (s *PartitionScheme) GetLUKSDevice(mapperName string) *LUKSDevice {
	for _, dev := range s.LUKSDevices {
		if dev.MapperName == mapperName {
			return dev
		}
	}
	return nil
}

// CloseLUKSDevices closes all open LUKS containers
func (s *PartitionScheme) CloseLUKSDevices() {
	if !s.Encrypted {
		return
	}
	for _, dev := range s.LUKSDevices {
		_ = CloseLUKS(dev.MapperName)
	}
	s.LUKSDevices = nil
}

// FormatPartitions formats the partitions with appropriate filesystems
func FormatPartitions(scheme *PartitionScheme, dryRun bool) error {
	if dryRun {
		fmt.Println("[DRY RUN] Would format partitions")
		return nil
	}

	// Default to ext4 if not specified
	fsType := scheme.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	fmt.Printf("Formatting partitions (filesystem: %s)...\n", fsType)

	// Format boot partition as FAT32 (EFI System Partition)
	// Boot partition is never encrypted
	fmt.Printf("  Formatting %s as FAT32 (boot/EFI)...\n", scheme.BootPartition)
	cmd := exec.Command("mkfs.vfat", "-F", "32", "-n", "UEFI", scheme.BootPartition)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to format boot partition: %w\nOutput: %s", err, string(output))
	}

	// Get device paths (use mapper devices if encrypted)
	root1Dev := scheme.GetRoot1Device()
	root2Dev := scheme.GetRoot2Device()
	varDev := scheme.GetVarDevice()

	// Format first root partition (or LUKS mapper device)
	fmt.Printf("  Formatting %s as %s...\n", root1Dev, fsType)
	if err := formatPartition(root1Dev, fsType, "root1"); err != nil {
		return fmt.Errorf("failed to format root1 partition: %w", err)
	}

	// Format second root partition (or LUKS mapper device)
	fmt.Printf("  Formatting %s as %s...\n", root2Dev, fsType)
	if err := formatPartition(root2Dev, fsType, "root2"); err != nil {
		return fmt.Errorf("failed to format root2 partition: %w", err)
	}

	// Format /var partition (or LUKS mapper device)
	fmt.Printf("  Formatting %s as %s...\n", varDev, fsType)
	if err := formatPartition(varDev, fsType, "var"); err != nil {
		return fmt.Errorf("failed to format var partition: %w", err)
	}

	fmt.Println("Formatting complete")
	return nil
}

// formatPartition formats a single partition with the specified filesystem type
func formatPartition(partition, fsType, label string) error {
	var cmd *exec.Cmd

	switch fsType {
	case "ext4":
		cmd = exec.Command("mkfs.ext4", "-F", "-L", label, partition)
	case "btrfs":
		// Check if mkfs.btrfs is available
		if _, err := exec.LookPath("mkfs.btrfs"); err != nil {
			return fmt.Errorf("mkfs.btrfs not found - install btrfs-progs package")
		}
		cmd = exec.Command("mkfs.btrfs", "-f", "-L", label, partition)
	default:
		return fmt.Errorf("unsupported filesystem type: %s (supported: ext4, btrfs)", fsType)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// MountPartitions mounts the partitions to a temporary directory
func MountPartitions(scheme *PartitionScheme, mountPoint string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would mount partitions at %s\n", mountPoint)
		return nil
	}

	fmt.Printf("Mounting partitions at %s...\n", mountPoint)

	// Create mount point if it doesn't exist
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	// Get device paths (use mapper devices if encrypted)
	root1Dev := scheme.GetRoot1Device()
	varDev := scheme.GetVarDevice()

	// Mount first root partition (or LUKS mapper device)
	cmd := exec.Command("mount", root1Dev, mountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount root1 partition: %w\nOutput: %s", err, string(output))
	}

	// Create boot and var subdirectories
	bootDir := filepath.Join(mountPoint, "boot")
	varDir := filepath.Join(mountPoint, "var")
	if err := os.MkdirAll(bootDir, 0755); err != nil {
		return fmt.Errorf("failed to create boot directory: %w", err)
	}
	if err := os.MkdirAll(varDir, 0755); err != nil {
		return fmt.Errorf("failed to create var directory: %w", err)
	}

	// Mount boot partition (FAT32 EFI System Partition - never encrypted)
	cmd = exec.Command("mount", scheme.BootPartition, bootDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}

	// Mount /var partition (or LUKS mapper device)
	cmd = exec.Command("mount", varDev, varDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount var partition: %w\nOutput: %s", err, string(output))
	}

	fmt.Println("Partitions mounted successfully")
	return nil
}

// UnmountPartitions unmounts all partitions
func UnmountPartitions(mountPoint string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY RUN] Would unmount partitions at %s\n", mountPoint)
		return nil
	}

	fmt.Println("Unmounting partitions...")

	// Unmount in reverse order
	bootDir := filepath.Join(mountPoint, "boot")
	varDir := filepath.Join(mountPoint, "var")

	// Unmount boot
	if err := exec.Command("umount", bootDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount boot: %v\n", err)
	}

	// Unmount /var
	if err := exec.Command("umount", varDir).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount var: %v\n", err)
	}

	// Unmount root
	if err := exec.Command("umount", mountPoint).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to unmount root: %v\n", err)
	}

	return nil
}

// GetPartitionUUID returns the UUID of a partition
func GetPartitionUUID(partition string) (string, error) {
	cmd := exec.Command("blkid", "-s", "UUID", "-o", "value", partition)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get UUID: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
