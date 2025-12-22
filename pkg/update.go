package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// GetRemoteImageDigest fetches the digest of a remote container image without downloading layers.
// Returns the digest in the format "sha256:..."
func GetRemoteImageDigest(imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("invalid image reference: %w", err)
	}

	// Get the image descriptor (manifest digest) without downloading layers
	desc, err := remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", fmt.Errorf("failed to get image descriptor: %w", err)
	}

	return desc.Digest.String(), nil
}

// CheckUpdateNeeded compares the installed image digest with the remote image digest
// Returns true if an update is needed (digests differ), false otherwise
func CheckUpdateNeeded(installedDigest, remoteDigest string) bool {
	if installedDigest == "" {
		// No digest stored (old installation), assume update needed
		return true
	}
	return installedDigest != remoteDigest
}

// GetActiveRootPartition determines which root partition is currently active
func GetActiveRootPartition() (string, error) {
	// Read /proc/cmdline to see which root is being used
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "", fmt.Errorf("failed to read /proc/cmdline: %w", err)
	}

	cmdlineStr := string(cmdline)

	// Look for root=UUID=XXX or root=/dev/XXX
	fields := strings.Fields(cmdlineStr)
	for _, field := range fields {
		if strings.HasPrefix(field, "root=UUID=") {
			uuid := strings.TrimPrefix(field, "root=UUID=")
			// Find which partition has this UUID
			return findPartitionByUUID(uuid)
		} else if strings.HasPrefix(field, "root=/dev/") {
			return strings.TrimPrefix(field, "root="), nil
		}
	}

	return "", fmt.Errorf("could not determine active root partition from kernel command line")
}

// findPartitionByUUID finds a partition device path by its UUID
func findPartitionByUUID(uuid string) (string, error) {
	cmd := exec.Command("blkid", "-U", uuid)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find partition with UUID %s: %w", uuid, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetInactiveRootPartition returns the inactive root partition given a partition scheme
func GetInactiveRootPartition(scheme *PartitionScheme) (string, bool, error) {
	active, err := GetActiveRootPartition()
	if err != nil {
		// If we can't determine active, default to root1 as active
		fmt.Fprintf(os.Stderr, "Warning: could not determine active partition: %v\n", err)
		fmt.Fprintf(os.Stderr, "Defaulting to root2 as target\n")
		return scheme.Root2Partition, true, nil
	}

	// Handle encrypted systems - mapper paths like /dev/mapper/root1 or /dev/mapper/root2
	if strings.HasPrefix(active, "/dev/mapper/") {
		mapperName := filepath.Base(active)
		switch mapperName {
		case "root1":
			return scheme.Root2Partition, true, nil
		case "root2":
			return scheme.Root1Partition, false, nil
		}
	}

	// Normalize paths for comparison (non-encrypted systems)
	activeBase := filepath.Base(active)
	root1Base := filepath.Base(scheme.Root1Partition)
	root2Base := filepath.Base(scheme.Root2Partition)

	switch activeBase {
	case root1Base:
		return scheme.Root2Partition, true, nil
	case root2Base:
		return scheme.Root1Partition, false, nil
	}

	// Active partition doesn't match either root partition
	// This can happen in test scenarios where we're not booted from the target disk
	// Default to root1 as active, root2 as target
	fmt.Fprintf(os.Stderr, "Warning: active partition %s does not match either root partition (%s or %s)\n",
		active, scheme.Root1Partition, scheme.Root2Partition)
	fmt.Fprintf(os.Stderr, "Defaulting to root2 as target\n")
	return scheme.Root2Partition, true, nil
}

// DetectExistingPartitionScheme detects the partition scheme of an existing installation
func DetectExistingPartitionScheme(device string) (*PartitionScheme, error) {
	deviceBase := filepath.Base(device)
	var part1, part2, part3, part4 string

	// Handle different device naming conventions
	// nvme, mmcblk, and loop devices use "p" prefix for partitions
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

	// Verify partitions exist
	for _, part := range []string{part1, part2, part3, part4} {
		if _, err := os.Stat(part); os.IsNotExist(err) {
			return nil, fmt.Errorf("partition %s does not exist", part)
		}
	}

	scheme := &PartitionScheme{
		BootPartition:  part1,
		Root1Partition: part2,
		Root2Partition: part3,
		VarPartition:   part4,
	}

	return scheme, nil
}

// UpdaterConfig holds configuration for system updates
type UpdaterConfig struct {
	Device         string
	ImageRef       string
	ImageDigest    string // Digest of the remote image (set by IsUpdateNeeded)
	FilesystemType string // Filesystem type (ext4, btrfs)
	Verbose        bool
	DryRun         bool
	Force          bool // Skip interactive confirmation
	JSONOutput     bool
	KernelArgs     []string
	MountPoint     string
	BootMountPoint string
}

// SystemUpdater handles A/B system updates
type SystemUpdater struct {
	Config           UpdaterConfig
	Scheme           *PartitionScheme
	Active           bool // true if root1 is active, false if root2 is active
	Target           string
	TargetMapperName string // For encrypted systems: "root1" or "root2"
	TargetMapperPath string // For encrypted systems: "/dev/mapper/root1" or "/dev/mapper/root2"
	Progress         *ProgressReporter
	Encryption       *EncryptionConfig // Encryption configuration (loaded from system config)
}

// NewSystemUpdater creates a new SystemUpdater
func NewSystemUpdater(device, imageRef string) *SystemUpdater {
	return &SystemUpdater{
		Config: UpdaterConfig{
			Device:         device,
			ImageRef:       imageRef,
			MountPoint:     "/tmp/nbc-update",
			BootMountPoint: "/tmp/nbc-boot",
		},
		Progress: NewProgressReporter(false, 7),
	}
}

// SetVerbose enables verbose output
func (u *SystemUpdater) SetVerbose(verbose bool) {
	u.Config.Verbose = verbose
}

// SetDryRun enables dry run mode
func (u *SystemUpdater) SetDryRun(dryRun bool) {
	u.Config.DryRun = dryRun
}

// SetForce enables non-interactive mode (skips confirmation)
func (u *SystemUpdater) SetForce(force bool) {
	u.Config.Force = force
}

// SetJSONOutput enables JSON output mode
func (u *SystemUpdater) SetJSONOutput(jsonOutput bool) {
	u.Config.JSONOutput = jsonOutput
	u.Progress = NewProgressReporter(jsonOutput, 7)
}

// AddKernelArg adds a kernel argument
func (u *SystemUpdater) AddKernelArg(arg string) {
	u.Config.KernelArgs = append(u.Config.KernelArgs, arg)
}

// buildKernelCmdline builds the kernel command line with proper LUKS support if encrypted
// isTarget indicates if this is for the target (new) root or the active (previous) root
func (u *SystemUpdater) buildKernelCmdline(rootUUID, varUUID, fsType string, isTarget bool) []string {
	var kernelCmdline []string

	if u.Encryption != nil && u.Encryption.Enabled {
		// LUKS encrypted system - determine which root LUKS UUID to use
		var rootLUKSUUID string
		var rootMapperName string

		if isTarget {
			// Building cmdline for the target (new) root
			if u.Active {
				// root1 is active, so target is root2
				rootLUKSUUID = u.Encryption.Root2LUKSUUID
				rootMapperName = "root2"
			} else {
				// root2 is active, so target is root1
				rootLUKSUUID = u.Encryption.Root1LUKSUUID
				rootMapperName = "root1"
			}
		} else {
			// Building cmdline for the active (previous) root
			if u.Active {
				// root1 is active
				rootLUKSUUID = u.Encryption.Root1LUKSUUID
				rootMapperName = "root1"
			} else {
				// root2 is active
				rootLUKSUUID = u.Encryption.Root2LUKSUUID
				rootMapperName = "root2"
			}
		}

		// Root via device mapper
		kernelCmdline = append(kernelCmdline, "root=/dev/mapper/"+rootMapperName)
		kernelCmdline = append(kernelCmdline, "rw")

		// LUKS UUIDs for initramfs to discover and unlock
		kernelCmdline = append(kernelCmdline, "rd.luks.uuid="+rootLUKSUUID)
		kernelCmdline = append(kernelCmdline, "rd.luks.name="+rootLUKSUUID+"="+rootMapperName)

		// Var partition via device mapper
		kernelCmdline = append(kernelCmdline, "rd.luks.uuid="+u.Encryption.VarLUKSUUID)
		kernelCmdline = append(kernelCmdline, "rd.luks.name="+u.Encryption.VarLUKSUUID+"=var")

		// TPM2 auto-unlock if enabled
		if u.Encryption.TPM2 {
			kernelCmdline = append(kernelCmdline, "rd.luks.options="+rootLUKSUUID+"=tpm2-device=auto")
			kernelCmdline = append(kernelCmdline, "rd.luks.options="+u.Encryption.VarLUKSUUID+"=tpm2-device=auto")
		}

		// Mount /var via systemd.mount-extra using mapper device
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=/dev/mapper/var:/var:"+fsType+":defaults")
	} else {
		// Non-encrypted system - use UUID
		kernelCmdline = append(kernelCmdline, "root=UUID="+rootUUID)
		kernelCmdline = append(kernelCmdline, "rw")
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+varUUID+":/var:"+fsType+":defaults")
	}

	// Add user-specified kernel arguments
	kernelCmdline = append(kernelCmdline, u.Config.KernelArgs...)

	return kernelCmdline
}

// PrepareUpdate prepares for an update by detecting partitions and determining target
func (u *SystemUpdater) PrepareUpdate() error {
	p := u.Progress
	p.MessagePlain("Preparing for system update...")

	// Read system config to get encryption settings and other config
	sysConfig, err := ReadSystemConfig()
	if err != nil {
		p.Warning("could not read system config: %v", err)
	} else {
		// Load encryption config if present
		if sysConfig.Encryption != nil && sysConfig.Encryption.Enabled {
			u.Encryption = sysConfig.Encryption
			p.Message("Detected LUKS encryption configuration")
		}
		// Load filesystem type if not already set
		if u.Config.FilesystemType == "" && sysConfig.FilesystemType != "" {
			u.Config.FilesystemType = sysConfig.FilesystemType
		}
	}

	// Detect existing partition scheme
	scheme, err := DetectExistingPartitionScheme(u.Config.Device)
	if err != nil {
		return fmt.Errorf("failed to detect partition scheme: %w", err)
	}
	u.Scheme = scheme

	// Determine inactive partition
	target, active, err := GetInactiveRootPartition(scheme)
	if err != nil {
		return fmt.Errorf("failed to determine target partition: %w", err)
	}
	u.Target = target
	u.Active = active

	// Set mapper info for encrypted systems
	if u.Encryption != nil && u.Encryption.Enabled {
		if u.Active {
			// root1 is active, target is root2
			u.TargetMapperName = "root2"
		} else {
			// root2 is active, target is root1
			u.TargetMapperName = "root1"
		}
		u.TargetMapperPath = "/dev/mapper/" + u.TargetMapperName
	}

	if u.Active {
		p.Message("Currently booted from: %s (root1)", scheme.Root1Partition)
		p.Message("Update target: %s (root2)", u.Target)
	} else {
		p.Message("Currently booted from: %s (root2)", scheme.Root2Partition)
		p.Message("Update target: %s (root1)", u.Target)
	}

	return nil
}

// PullImage validates the image reference and checks if it's accessible
// The actual image pull happens during Extract() to avoid duplicate work
func (u *SystemUpdater) PullImage() error {
	p := u.Progress

	if u.Config.DryRun {
		p.MessagePlain("[DRY RUN] Would pull image: %s", u.Config.ImageRef)
		return nil
	}

	p.MessagePlain("Validating image reference: %s", u.Config.ImageRef)

	// Parse and validate the image reference
	ref, err := name.ParseReference(u.Config.ImageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference: %w", err)
	}

	if u.Config.Verbose {
		p.Message("Image: %s", ref.String())
	}

	// Try to get image descriptor to verify it exists and is accessible
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to access image: %w (check credentials if private registry)", err)
	}

	p.Message("Image reference is valid and accessible")
	return nil
}

// IsUpdateNeeded checks if the remote image differs from the currently installed image.
// Returns true if an update is needed, false if the system is already up-to-date.
// Also returns the remote digest for use during the update process.
func (u *SystemUpdater) IsUpdateNeeded() (bool, string, error) {
	p := u.Progress
	p.MessagePlain("Checking if update is needed...")

	// Get the remote image digest
	remoteDigest, err := GetRemoteImageDigest(u.Config.ImageRef)
	if err != nil {
		return false, "", fmt.Errorf("failed to get remote image digest: %w", err)
	}

	if u.Config.Verbose {
		p.Message("Remote image digest: %s", remoteDigest)
	}

	// Read the current system config to get installed digest
	config, err := ReadSystemConfig()
	if err != nil {
		// If we can't read config, assume update is needed
		p.Message("Could not read system config: %v", err)
		p.Message("Assuming update is needed")
		return true, remoteDigest, nil
	}

	// Store filesystem type from config for use during update
	if config.FilesystemType != "" {
		u.Config.FilesystemType = config.FilesystemType
	} else {
		u.Config.FilesystemType = "ext4" // Default for older installations
	}

	if u.Config.Verbose {
		p.Message("Installed image: %s", config.ImageRef)
		p.Message("Installed digest: %s", config.ImageDigest)
	}

	if config.ImageDigest == "" {
		p.Message("No digest stored (older installation), update needed")
		return true, remoteDigest, nil
	}

	if config.ImageDigest == remoteDigest {
		p.Message("✓ System is already up-to-date")
		p.Message("Installed: %s", config.ImageDigest)
		return false, remoteDigest, nil
	}

	p.Message("Update available:")
	p.Message("Installed: %s", config.ImageDigest)
	p.Message("Available: %s", remoteDigest)
	return true, remoteDigest, nil
}

// Update performs the system update
func (u *SystemUpdater) Update() error {
	p := u.Progress

	if u.Config.DryRun {
		p.MessagePlain("[DRY RUN] Would update to partition: %s", u.Target)
		return nil
	}

	p.MessagePlain("Starting system update...")

	// Step 1: Mount target partition
	p.Step(1, "Mounting target partition")
	if err := os.MkdirAll(u.Config.MountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	// Determine which device to mount
	mountDevice := u.Target

	// For encrypted systems, open the LUKS container first
	if u.Encryption != nil && u.Encryption.Enabled {
		p.Message("Opening LUKS container for %s...", u.TargetMapperName)

		// Check if already open (from a previous failed attempt)
		if _, err := os.Stat(u.TargetMapperPath); os.IsNotExist(err) {
			// Need to open the LUKS container - prompt for passphrase
			fmt.Print("Enter LUKS passphrase: ")
			var passphrase string
			_, err := fmt.Scanln(&passphrase)
			if err != nil {
				return fmt.Errorf("failed to read passphrase: %w", err)
			}

			_, err = OpenLUKS(u.Target, u.TargetMapperName, passphrase)
			if err != nil {
				return fmt.Errorf("failed to open LUKS container: %w", err)
			}
		} else {
			p.Message("LUKS container already open at %s", u.TargetMapperPath)
		}

		mountDevice = u.TargetMapperPath

		// Ensure we close LUKS on cleanup
		defer func() {
			if u.TargetMapperName != "" {
				_ = CloseLUKS(u.TargetMapperName)
			}
		}()
	}

	cmd := exec.Command("mount", mountDevice, u.Config.MountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount target partition: %w\nOutput: %s", err, string(output))
	}
	defer func() {
		p.Message("Cleaning up...")
		_ = exec.Command("umount", u.Config.MountPoint).Run()
		_ = os.RemoveAll(u.Config.MountPoint)
	}()

	// Step 2: Clear existing content
	p.Step(2, "Clearing old content from target partition")
	entries, err := os.ReadDir(u.Config.MountPoint)
	if err != nil {
		return fmt.Errorf("failed to read target directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(u.Config.MountPoint, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}

	// Step 3: Extract new container filesystem
	p.Step(3, "Extracting new container filesystem")
	extractor := NewContainerExtractor(u.Config.ImageRef, u.Config.MountPoint)
	extractor.SetVerbose(u.Config.Verbose)
	if err := extractor.Extract(); err != nil {
		return fmt.Errorf("failed to extract container: %w", err)
	}

	// Step 4: Merge /etc configuration from active system
	p.Step(4, "Preserving user configuration")
	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}
	// For encrypted systems, use the mapper path since that's what GetActiveRootPartition returns
	if u.Encryption != nil && u.Encryption.Enabled {
		if u.Active {
			activeRoot = "/dev/mapper/root1"
		} else {
			activeRoot = "/dev/mapper/root2"
		}
	}
	if err := MergeEtcFromActive(u.Config.MountPoint, activeRoot, u.Config.DryRun); err != nil {
		return fmt.Errorf("failed to merge /etc: %w", err)
	}

	// Step 5: Setup system directories
	p.Step(5, "Setting up system directories")
	if err := SetupSystemDirectories(u.Config.MountPoint); err != nil {
		return fmt.Errorf("failed to setup directories: %w", err)
	}

	// Step 6: Install new kernel and initramfs if present
	p.Step(6, "Checking for new kernel and initramfs")
	if err := u.InstallKernelAndInitramfs(); err != nil {
		return fmt.Errorf("failed to install kernel/initramfs: %w", err)
	}

	// Step 7: Update bootloader configuration
	p.Step(7, "Updating bootloader configuration")
	if err := u.UpdateBootloader(); err != nil {
		return fmt.Errorf("failed to update bootloader: %w", err)
	}

	return nil
}

// InstallKernelAndInitramfs checks for new kernel and initramfs in the updated root
// and copies them to the boot partition (which is the combined EFI/boot partition)
func (u *SystemUpdater) InstallKernelAndInitramfs() error {
	p := u.Progress
	// Look for kernel modules in the new root's /usr/lib/modules directory
	modulesDir := filepath.Join(u.Config.MountPoint, "usr", "lib", "modules")

	// Find kernel version directories
	entries, err := os.ReadDir(modulesDir)
	if err != nil || len(entries) == 0 {
		p.Message("No kernel modules found in updated image")
		return nil
	}

	// Mount boot partition
	bootMountPoint := filepath.Join(os.TempDir(), "nbc-boot-mount")
	if err := os.MkdirAll(bootMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create boot mount point: %w", err)
	}
	defer func() { _ = os.RemoveAll(bootMountPoint) }()

	cmd := exec.Command("mount", u.Scheme.BootPartition, bootMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}
	defer func() { _ = exec.Command("umount", bootMountPoint).Run() }()

	// Detect bootloader type to determine where to copy kernels
	bootloaderType := u.detectBootloaderTypeFromMount(bootMountPoint)
	p.Message("Detected bootloader: %s", bootloaderType)

	// All kernels go to boot partition (combined EFI/boot)
	kernelDestDir := bootMountPoint

	// Get existing kernels for comparison
	existingKernels, _ := filepath.Glob(filepath.Join(kernelDestDir, "vmlinuz-*"))
	existingKernelMap := make(map[string]bool)
	for _, k := range existingKernels {
		existingKernelMap[filepath.Base(k)] = true
	}

	copiedKernel := false
	copiedInitramfs := false

	// Process each kernel version directory
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		kernelVersion := entry.Name()
		kernelModuleDir := filepath.Join(modulesDir, kernelVersion)

		// Look for kernel in /usr/lib/modules/$KERNEL_VERSION/
		kernelPatterns := []string{
			filepath.Join(kernelModuleDir, "vmlinuz"),
			filepath.Join(kernelModuleDir, "vmlinuz-"+kernelVersion),
		}

		var srcKernel string
		for _, pattern := range kernelPatterns {
			if _, err := os.Stat(pattern); err == nil {
				srcKernel = pattern
				break
			}
		}

		if srcKernel == "" {
			continue // No kernel found for this version
		}

		// Destination kernel name
		kernelName := "vmlinuz-" + kernelVersion
		destKernel := filepath.Join(kernelDestDir, kernelName)

		// Check if kernel needs to be copied
		needsCopy := false
		if !existingKernelMap[kernelName] {
			needsCopy = true
			p.Message("Found new kernel: %s", kernelName)
		} else {
			// Compare file sizes to detect changes
			srcInfo, _ := os.Stat(srcKernel)
			dstInfo, _ := os.Stat(destKernel)
			if srcInfo.Size() != dstInfo.Size() {
				needsCopy = true
				p.Message("Kernel %s has changed, updating", kernelName)
			}
		}

		if needsCopy {
			if err := copyFile(srcKernel, destKernel); err != nil {
				return fmt.Errorf("failed to copy kernel %s: %w", kernelName, err)
			}
			p.Message("Installed kernel: %s", kernelName)
			copiedKernel = true
		}

		// Look for initramfs in /usr/lib/modules/$KERNEL_VERSION/
		initrdPatterns := []string{
			filepath.Join(kernelModuleDir, "initramfs.img"),
			filepath.Join(kernelModuleDir, "initrd.img"),
			filepath.Join(kernelModuleDir, "initramfs-"+kernelVersion+".img"),
			filepath.Join(kernelModuleDir, "initrd.img-"+kernelVersion),
		}

		for _, pattern := range initrdPatterns {
			if srcInitrd, err := os.Stat(pattern); err == nil && !srcInitrd.IsDir() {
				initrdName := "initramfs-" + kernelVersion + ".img"
				destInitrd := filepath.Join(kernelDestDir, initrdName)

				// Check if initramfs needs to be copied
				needsCopy := false
				if dstInitrd, err := os.Stat(destInitrd); os.IsNotExist(err) {
					needsCopy = true
					p.Message("Found new initramfs: %s", initrdName)
				} else if err == nil && srcInitrd.Size() != dstInitrd.Size() {
					needsCopy = true
					p.Message("Initramfs %s has changed, updating", initrdName)
				}

				if needsCopy {
					if err := copyFile(pattern, destInitrd); err != nil {
						return fmt.Errorf("failed to copy initramfs %s: %w", initrdName, err)
					}
					p.Message("Installed initramfs: %s", initrdName)
					copiedInitramfs = true
				}
				break // Only copy the first matching initramfs
			}
		}
	}

	if !copiedKernel && !copiedInitramfs {
		p.Message("Kernel and initramfs are up to date")
	}

	return nil
}

// detectBootloaderTypeFromMount detects bootloader from already-mounted boot partition
func (u *SystemUpdater) detectBootloaderTypeFromMount(bootMount string) BootloaderType {
	// Check for systemd-boot loader directory
	loaderDir := filepath.Join(bootMount, "loader")
	if _, err := os.Stat(loaderDir); err == nil {
		return BootloaderSystemdBoot
	}

	// Check for GRUB on boot partition
	grubDirs := []string{
		filepath.Join(bootMount, "grub"),
		filepath.Join(bootMount, "grub2"),
	}
	for _, dir := range grubDirs {
		if _, err := os.Stat(dir); err == nil {
			return BootloaderGRUB2
		}
	}

	// Default to GRUB2
	return BootloaderGRUB2
}

// UpdateBootloader updates the bootloader to boot from the new partition
func (u *SystemUpdater) UpdateBootloader() error {
	// Mount boot partition
	if err := os.MkdirAll(u.Config.BootMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create boot mount point: %w", err)
	}

	cmd := exec.Command("mount", u.Scheme.BootPartition, u.Config.BootMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount boot partition: %w\nOutput: %s", err, string(output))
	}
	defer func() { _ = exec.Command("umount", u.Config.BootMountPoint).Run() }()

	// Detect bootloader type
	bootloaderType := u.detectBootloaderType()
	fmt.Printf("  Detected bootloader: %s\n", bootloaderType)

	// Update based on bootloader type
	switch bootloaderType {
	case BootloaderGRUB2:
		return u.updateGRUBBootloader()
	case BootloaderSystemdBoot:
		return u.updateSystemdBootBootloader()
	default:
		return fmt.Errorf("unsupported bootloader type: %s", bootloaderType)
	}
}

// detectBootloaderType detects which bootloader is installed
func (u *SystemUpdater) detectBootloaderType() BootloaderType {
	// Check for systemd-boot loader directory
	loaderDir := filepath.Join(u.Config.BootMountPoint, "loader")
	if _, err := os.Stat(loaderDir); err == nil {
		return BootloaderSystemdBoot
	}

	// Check for GRUB
	grubDirs := []string{
		filepath.Join(u.Config.BootMountPoint, "grub"),
		filepath.Join(u.Config.BootMountPoint, "grub2"),
	}
	for _, dir := range grubDirs {
		if _, err := os.Stat(dir); err == nil {
			return BootloaderGRUB2
		}
	}

	// Default to GRUB2
	return BootloaderGRUB2
}

// updateGRUBBootloader updates GRUB configuration
func (u *SystemUpdater) updateGRUBBootloader() error {
	// Get UUID of new root partition
	targetUUID, err := GetPartitionUUID(u.Target)
	if err != nil {
		return fmt.Errorf("failed to get target UUID: %w", err)
	}

	// Get /var UUID for kernel command line mount
	varUUID, err := GetPartitionUUID(u.Scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

	// Find kernel and initramfs
	kernels, err := filepath.Glob(filepath.Join(u.Config.BootMountPoint, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs
	var initrd string
	initrdPatterns := []string{
		filepath.Join(u.Config.BootMountPoint, "initramfs-"+kernelVersion+".img"),
		filepath.Join(u.Config.BootMountPoint, "initrd.img-"+kernelVersion),
		filepath.Join(u.Config.BootMountPoint, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Get filesystem type (default to ext4 for backward compatibility)
	fsType := u.Config.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	// Build kernel command line
	kernelCmdline := u.buildKernelCmdline(targetUUID, varUUID, fsType, true)

	// Get OS name from the updated system
	osName := ParseOSRelease(u.Config.MountPoint)

	// Find GRUB directory
	grubDirs := []string{
		filepath.Join(u.Config.BootMountPoint, "grub"),
		filepath.Join(u.Config.BootMountPoint, "grub2"),
	}

	var grubDir string
	for _, dir := range grubDirs {
		if _, err := os.Stat(dir); err == nil {
			grubDir = dir
			break
		}
	}

	if grubDir == "" {
		return fmt.Errorf("could not find grub directory")
	}

	// Create new GRUB config with both boot options
	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}

	activeUUID, _ := GetPartitionUUID(activeRoot)

	// Build previous kernel command line (for the currently active root)
	previousCmdline := u.buildKernelCmdline(activeUUID, varUUID, fsType, false)

	grubCfg := fmt.Sprintf(`set timeout=5
set default=0

menuentry '%s' {
    linux /vmlinuz-%s %s
    initrd /%s
}

menuentry '%s (Previous)' {
    linux /vmlinuz-%s %s
    initrd /%s
}
`, osName, kernelVersion, strings.Join(kernelCmdline, " "), initrd,
		osName, kernelVersion, strings.Join(previousCmdline, " "), initrd)

	grubCfgPath := filepath.Join(grubDir, "grub.cfg")
	if err := os.WriteFile(grubCfgPath, []byte(grubCfg), 0644); err != nil {
		return fmt.Errorf("failed to write grub.cfg: %w", err)
	}

	fmt.Printf("  Updated GRUB to boot from %s\n", u.Target)
	return nil
}

// updateSystemdBootBootloader updates systemd-boot configuration
func (u *SystemUpdater) updateSystemdBootBootloader() error {
	// Get UUIDs
	targetUUID, err := GetPartitionUUID(u.Target)
	if err != nil {
		return fmt.Errorf("failed to get target UUID: %w", err)
	}

	varUUID, err := GetPartitionUUID(u.Scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}
	activeUUID, _ := GetPartitionUUID(activeRoot)

	// Find kernel and initramfs on boot partition
	kernels, err := filepath.Glob(filepath.Join(u.Config.BootMountPoint, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found on boot partition")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs on boot partition
	var initrd string
	initrdPatterns := []string{
		filepath.Join(u.Config.BootMountPoint, "initramfs-"+kernelVersion+".img"),
		filepath.Join(u.Config.BootMountPoint, "initrd.img-"+kernelVersion),
		filepath.Join(u.Config.BootMountPoint, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Get filesystem type (default to ext4 for backward compatibility)
	fsType := u.Config.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	// Build kernel command line
	kernelCmdline := u.buildKernelCmdline(targetUUID, varUUID, fsType, true)

	// Get OS name from the updated system
	osName := ParseOSRelease(u.Config.MountPoint)

	// Update loader.conf to default to bootc entry
	loaderDir := filepath.Join(u.Config.BootMountPoint, "loader")

	// Create/update main boot entry (always points to newest system)
	entriesDir := filepath.Join(loaderDir, "entries")
	if err := os.MkdirAll(entriesDir, 0755); err != nil {
		return fmt.Errorf("failed to create entries directory: %w", err)
	}

	mainEntry := fmt.Sprintf(`title   %s
linux   /vmlinuz-%s
initrd  /%s
options %s
`, osName, kernelVersion, initrd, strings.Join(kernelCmdline, " "))

	mainEntryPath := filepath.Join(entriesDir, "bootc.conf")
	if err := os.WriteFile(mainEntryPath, []byte(mainEntry), 0644); err != nil {
		return fmt.Errorf("failed to write main boot entry: %w", err)
	}

	// Build previous kernel command line (for the currently active root)
	previousCmdline := u.buildKernelCmdline(activeUUID, varUUID, fsType, false)

	// Create/update rollback boot entry (points to previous system)
	previousEntry := fmt.Sprintf(`title   %s (Previous)
linux   /vmlinuz-%s
initrd  /%s
options %s
`, osName, kernelVersion, initrd, strings.Join(previousCmdline, " "))

	previousEntryPath := filepath.Join(entriesDir, "bootc-previous.conf")
	if err := os.WriteFile(previousEntryPath, []byte(previousEntry), 0644); err != nil {
		return fmt.Errorf("failed to write rollback boot entry: %w", err)
	}

	fmt.Printf("  Updated systemd-boot to boot from %s\n", u.Target)
	return nil
}

// PerformUpdate performs the complete update workflow
func (u *SystemUpdater) PerformUpdate(skipPull bool) error {
	p := u.Progress

	// Prepare update
	if err := u.PrepareUpdate(); err != nil {
		return err
	}

	// Pull image if not skipped
	if !skipPull {
		if err := u.PullImage(); err != nil {
			return err
		}
	}

	// Check if update is actually needed (compare digests)
	needed, digest, err := u.IsUpdateNeeded()
	if err != nil {
		p.Warning("could not check if update needed: %v", err)
		// Continue with update anyway
	} else if !needed && !u.Config.Force {
		p.MessagePlain("No update needed - system is already running the latest version.")
		p.Message("Use --force to reinstall anyway.")
		return nil
	} else if !needed && u.Config.Force {
		p.MessagePlain("System is up-to-date, but --force was specified. Proceeding with reinstall...")
	}

	// Store digest for later use
	u.Config.ImageDigest = digest

	// Confirm update (only in non-JSON mode)
	if !u.Config.DryRun && !u.Config.Force && !u.Config.JSONOutput {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("This will update the system to a new root filesystem.\n")
		fmt.Printf("Target partition: %s\n", u.Target)
		fmt.Printf("%s\n", strings.Repeat("=", 60))
		fmt.Print("Type 'yes' to continue: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "yes" {
			return fmt.Errorf("update cancelled by user")
		}
		fmt.Println()
	}

	// Perform update
	if err := u.Update(); err != nil {
		return err
	}

	// Update system config with new image reference and digest
	if !u.Config.DryRun {
		if err := UpdateSystemConfigImageRef(u.Config.ImageRef, u.Config.ImageDigest, u.Config.DryRun); err != nil {
			p.Warning("failed to update system config: %v", err)
		}
	}

	// Report completion
	p.Complete("System update complete! Reboot to activate the new version.", map[string]interface{}{
		"image":        u.Config.ImageRef,
		"device":       u.Config.Device,
		"target":       u.Target,
		"image_digest": u.Config.ImageDigest,
	})

	return nil
}
