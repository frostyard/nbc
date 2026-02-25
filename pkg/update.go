package pkg

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/frostyard/nbc/pkg/types"
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

// IsRootMountedReadOnly checks if the root partition is mounted read-only
// Returns "ro" for read-only, "rw" for read-write, or empty string if unable to determine
func IsRootMountedReadOnly() string {
	// Read /proc/mounts to check mount options
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(string(mounts)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 && fields[1] == "/" {
			// fields[3] contains mount options
			options := strings.Split(fields[3], ",")
			for _, opt := range options {
				switch opt {
				case "ro":
					return "ro"
				case "rw":
					return "rw"
				}
			}
		}
	}

	return ""
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

// findPartitionByLUKSUUID finds a partition device path by its LUKS UUID
func findPartitionByLUKSUUID(luksUUID string) (string, error) {
	// blkid can search for LUKS UUID using TYPE and UUID together
	cmd := exec.Command("blkid", "-t", "UUID="+luksUUID, "-o", "device")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find partition with LUKS UUID %s: %w", luksUUID, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetInactiveRootPartition returns the inactive root partition given a partition scheme
func GetInactiveRootPartition(scheme *PartitionScheme, progress Reporter) (string, bool, error) {
	active, err := GetActiveRootPartition()
	if err != nil {
		// If we can't determine active, default to root1 as active
		progress.Warning("could not determine active partition: %v", err)
		progress.Warning("Defaulting to root2 as target")
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
	progress.Warning("active partition %s does not match either root partition (%s or %s)",
		active, scheme.Root1Partition, scheme.Root2Partition)
	progress.Warning("Defaulting to root2 as target")
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
	Progress         Reporter
	Encryption       *EncryptionConfig    // Encryption configuration (loaded from system config)
	LocalLayoutPath  string               // Path to OCI layout directory for local image
	LocalMetadata    *CachedImageMetadata // Metadata from cached image
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
		Progress: NewTextReporter(os.Stdout),
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
	if jsonOutput {
		u.Progress = NewJSONReporter(os.Stdout)
	} else {
		u.Progress = NewTextReporter(os.Stdout)
	}
}

// AddKernelArg adds a kernel argument
func (u *SystemUpdater) AddKernelArg(arg string) {
	u.Config.KernelArgs = append(u.Config.KernelArgs, arg)
}

// SetLocalImage sets the local OCI layout path and metadata for offline updates
func (u *SystemUpdater) SetLocalImage(layoutPath string, metadata *CachedImageMetadata) {
	u.LocalLayoutPath = layoutPath
	u.LocalMetadata = metadata
	if metadata != nil {
		u.Config.ImageRef = metadata.ImageRef
		u.Config.ImageDigest = metadata.ImageDigest
	}
}

// buildKernelCmdline builds the kernel command line with proper LUKS support if encrypted
// isTarget indicates if this is for the target (new) root or the active (previous) root
func (u *SystemUpdater) buildKernelCmdline(rootUUID, varUUID, fsType string, isTarget bool) ([]string, error) {
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
		kernelCmdline = append(kernelCmdline, "ro")

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

		// Get boot partition UUID (always FAT32, never encrypted)
		bootUUID, err := GetPartitionUUID(context.Background(), u.Scheme.BootPartition)
		if err != nil {
			return nil, fmt.Errorf("failed to get boot UUID: %w", err)
		}
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+bootUUID+":/boot:vfat:defaults")

		// Mount /var via systemd.mount-extra using mapper device
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=/dev/mapper/var:/var:"+fsType+":defaults")

		// Enable /etc overlay persistence
		// The dracut module 95etc-overlay will mount an overlayfs for /etc
		kernelCmdline = append(kernelCmdline, "rd.etc.overlay=1")
		kernelCmdline = append(kernelCmdline, "rd.etc.overlay.var=/dev/mapper/var")
	} else {
		// Non-encrypted system - use UUID
		// Get boot partition UUID (always FAT32, never encrypted)
		bootUUID, err := GetPartitionUUID(context.Background(), u.Scheme.BootPartition)
		if err != nil {
			return nil, fmt.Errorf("failed to get boot UUID: %w", err)
		}
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+bootUUID+":/boot:vfat:defaults")

		kernelCmdline = append(kernelCmdline, "root=UUID="+rootUUID)
		kernelCmdline = append(kernelCmdline, "ro")
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+varUUID+":/var:"+fsType+":defaults")

		// Enable /etc overlay persistence
		// The dracut module 95etc-overlay will mount an overlayfs for /etc
		kernelCmdline = append(kernelCmdline, "rd.etc.overlay=1")
		kernelCmdline = append(kernelCmdline, "rd.etc.overlay.var=UUID="+varUUID)
	}

	// HACK: Disable NVMe multipath to ensure stable device naming across reboots
	// Modern kernels have CONFIG_NVME_MULTIPATH=y by default, causing nvme0/nvme1
	// to swap between boots due to non-deterministic controller enumeration order.
	// This is a workaround - ideally we should always use /dev/disk/by-id/* or UUIDs
	// everywhere and not rely on /dev/nvmeXnY naming at all.
	// TODO: Audit all code for hardcoded /dev/nvme* paths and migrate to stable identifiers
	kernelCmdline = append(kernelCmdline, "nvme_core.multipath=N")

	// Add user-specified kernel arguments
	kernelCmdline = append(kernelCmdline, u.Config.KernelArgs...)

	return kernelCmdline, nil
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
		// Load stored kernel args, prepending them to any CLI-specified args
		// This allows CLI args to add to stored args
		if len(sysConfig.KernelArgs) > 0 {
			u.Config.KernelArgs = append(sysConfig.KernelArgs, u.Config.KernelArgs...)
			if u.Config.Verbose {
				p.Message("Loaded kernel args from config: %v", sysConfig.KernelArgs)
			}
		}
	}

	// Detect existing partition scheme
	scheme, err := DetectExistingPartitionScheme(u.Config.Device)
	if err != nil {
		return fmt.Errorf("failed to detect partition scheme: %w", err)
	}
	u.Scheme = scheme

	// Determine inactive partition
	target, active, err := GetInactiveRootPartition(scheme, p)
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
func (u *SystemUpdater) IsUpdateNeeded(short bool) (bool, string, error) {
	p := u.Progress
	if short {
		u.Config.Verbose = false
	}
	if !short {
		p.MessagePlain("Checking if update is needed...")
	}

	// Get the image digest - from local cache or remote
	var remoteDigest string
	var err error
	if u.LocalMetadata != nil {
		remoteDigest = u.LocalMetadata.ImageDigest
		if u.Config.Verbose {
			p.Message("Image digest (from cache): %s", remoteDigest)
		}
	} else {
		remoteDigest, err = GetRemoteImageDigest(u.Config.ImageRef)
		if err != nil {
			return false, "", fmt.Errorf("failed to get remote image digest: %w", err)
		}
		if u.Config.Verbose {
			p.Message("Remote image digest: %s", remoteDigest)
		}
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
		if !short {
			p.Message("✓ System is already up-to-date")
			p.Message("Installed: %s", config.ImageDigest)
		}
		return false, remoteDigest, nil
	}

	if !short {
		p.Message("Update available:")
		p.Message("Installed: %s", config.ImageDigest)
		p.Message("Available: %s", remoteDigest)
	}
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

	// Ensure critical files (SSH host keys, machine-id) are in overlay upper layer
	// This must happen before we extract the new container image, so that these
	// files persist even if the new container image has different versions
	if err := EnsureCriticalFilesInOverlay(context.Background(), u.Config.DryRun, p); err != nil {
		p.Warning("Failed to preserve critical files in overlay: %v", err)
		// Continue anyway - this is a best-effort operation
	}

	// Step 1: Mount target partition
	p.Step(1, 7, "Mounting target partition")
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
			var opened bool

			// Try TPM2 auto-unlock first if enabled
			if u.Encryption.TPM2 {
				_, tpmErr := TryTPM2Unlock(context.Background(), u.Target, u.TargetMapperName, p)
				if tpmErr == nil {
					p.Message("LUKS container unlocked via TPM2")
					opened = true
				} else {
					p.Warning("TPM2 unlock failed, falling back to passphrase: %v", tpmErr)
				}
			}

			// Fall back to passphrase if TPM2 not enabled or failed
			if !opened {
				fmt.Print("Enter LUKS passphrase: ")
				var passphrase string
				_, err := fmt.Scanln(&passphrase)
				if err != nil {
					return fmt.Errorf("failed to read passphrase: %w", err)
				}

				_, err = OpenLUKS(context.Background(), u.Target, u.TargetMapperName, passphrase, p)
				if err != nil {
					return fmt.Errorf("failed to open LUKS container: %w", err)
				}
			}
		} else {
			p.Message("LUKS container already open at %s", u.TargetMapperPath)
		}

		mountDevice = u.TargetMapperPath

		// Ensure we close LUKS on cleanup
		defer func() {
			if u.TargetMapperName != "" {
				_ = CloseLUKS(context.Background(), u.TargetMapperName, nil)
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
	p.Step(2, 7, "Clearing old content from target partition")
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
	p.Step(3, 7, "Extracting new container filesystem")
	if err := ExtractAndVerifyContainer(context.Background(), u.Config.ImageRef, u.LocalLayoutPath, u.Config.MountPoint, u.Config.Verbose, p); err != nil {
		return fmt.Errorf("%w\n\nThe target partition may be in an inconsistent state.\nThe previous installation is still bootable - do NOT reboot.\nRe-run the update to try again", err)
	}

	// Step 4: Merge /etc configuration from active system
	p.Step(4, 7, "Preserving user configuration")
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
	if err := MergeEtcFromActive(context.Background(), u.Config.MountPoint, activeRoot, u.Config.DryRun, p); err != nil {
		return fmt.Errorf("failed to merge /etc: %w", err)
	}

	// Mount the /var partition at {MountPoint}/var
	// This is needed to write config to /var/lib/nbc/state/
	// The var partition is shared between A/B roots - same partition mounted at /var on running system
	varMountPoint := filepath.Join(u.Config.MountPoint, "var")
	varDevice := u.Scheme.VarPartition
	varLUKSOpened := false // Track if we opened var LUKS (need to close it on cleanup)

	// For encrypted systems, open var LUKS container if not already open
	if u.Encryption != nil && u.Encryption.Enabled {
		varMapperPath := "/dev/mapper/var"

		// Check if var LUKS is already open (running system uses it)
		if _, err := os.Stat(varMapperPath); os.IsNotExist(err) {
			p.Message("Opening LUKS container for var partition...")

			// Need the raw var partition device (before LUKS)
			// Find it using VarLUKSUUID from config
			varLUKSDevice, err := findPartitionByLUKSUUID(u.Encryption.VarLUKSUUID)
			if err != nil {
				return fmt.Errorf("failed to find var LUKS partition: %w", err)
			}

			var opened bool

			// Try TPM2 auto-unlock first if enabled
			if u.Encryption.TPM2 {
				_, tpmErr := TryTPM2Unlock(context.Background(), varLUKSDevice, "var", p)
				if tpmErr == nil {
					p.Message("Var LUKS container unlocked via TPM2")
					opened = true
				} else {
					p.Warning("TPM2 unlock for var failed, falling back to passphrase: %v", tpmErr)
				}
			}

			// Fall back to passphrase if TPM2 not enabled or failed
			if !opened {
				fmt.Print("Enter LUKS passphrase for var partition: ")
				var passphrase string
				_, err := fmt.Scanln(&passphrase)
				if err != nil {
					return fmt.Errorf("failed to read passphrase: %w", err)
				}

				_, err = OpenLUKS(context.Background(), varLUKSDevice, "var", passphrase, p)
				if err != nil {
					return fmt.Errorf("failed to open var LUKS container: %w", err)
				}
			}
			varLUKSOpened = true
		} else {
			p.Message("Var LUKS container already open at %s", varMapperPath)
		}

		varDevice = varMapperPath
	}

	cmd = exec.Command("mount", varDevice, varMountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount var partition: %w\nOutput: %s", err, string(output))
	}
	defer func() {
		_ = exec.Command("umount", varMountPoint).Run()
		// Close var LUKS if we opened it
		if varLUKSOpened {
			_ = CloseLUKS(context.Background(), "var", nil)
		}
	}()

	// Step 5: Setup target system (dracut, directories, machine-id, etc.lower, tmpfiles)
	p.Step(5, 7, "Setting up target system")
	if err := SetupTargetSystem(context.Background(), u.Config.MountPoint, u.Config.DryRun, u.Config.Verbose, p); err != nil {
		return err
	}

	// Write updated system config to /var (which persists across updates)
	if !u.Config.DryRun {
		// Read current config (from new or legacy location)
		existingConfig, err := ReadSystemConfig()
		if err != nil {
			p.Warning("failed to read existing config: %v", err)
		} else {
			// Update the image reference and digest
			existingConfig.ImageRef = u.Config.ImageRef
			existingConfig.ImageDigest = u.Config.ImageDigest
			// NOTE: Do NOT update Device field - device names can change between boots
			// due to enumeration order. The disk_id field provides stable identification.

			// Update or add disk ID (migration path for older installations)
			if diskID, err := GetDiskID(u.Config.Device); err == nil {
				if existingConfig.DiskID == "" {
					p.Message("Adding disk ID to config: %s", diskID)
					existingConfig.DiskID = diskID
				} else if existingConfig.DiskID != diskID {
					p.Warning("updating disk ID in config (was: %s, now: %s)", existingConfig.DiskID, diskID)
					existingConfig.DiskID = diskID
				}
			} else if u.Config.Verbose {
				p.Warning("could not determine disk ID: %v", err)
			}

			// Write to target's /var partition (already mounted at varMountPoint)
			// This also handles migration from legacy /etc/nbc location
			if err := WriteSystemConfigToVar(context.Background(), varMountPoint, existingConfig, false, p); err != nil {
				p.Warning("failed to write config to target var: %v", err)
			}

			// Update running system's config (migrates from /etc/nbc if needed)
			if err := WriteSystemConfig(context.Background(), existingConfig, false, p); err != nil {
				p.Warning("failed to update running system config: %v", err)
			} else {
				p.Message("Updated running system config")
			}
		}
	}

	// Step 6: Install new kernel and initramfs if present
	p.Step(6, 7, "Checking for new kernel and initramfs")
	if err := u.InstallKernelAndInitramfs(); err != nil {
		return fmt.Errorf("failed to install kernel/initramfs: %w", err)
	}

	// Step 7: Update bootloader configuration
	p.Step(7, 7, "Updating bootloader configuration")
	if err := u.UpdateBootloader(); err != nil {
		return fmt.Errorf("failed to update bootloader: %w", err)
	}

	// Write reboot-required marker to /run (automatically cleared on reboot)
	if !u.Config.DryRun {
		rebootInfo := &types.RebootPendingInfo{
			PendingImageRef:    u.Config.ImageRef,
			PendingImageDigest: u.Config.ImageDigest,
			UpdateTime:         time.Now().UTC().Format(time.RFC3339),
			TargetPartition:    u.Target,
		}
		if err := WriteRebootRequiredMarker(rebootInfo); err != nil {
			p.Warning("failed to write reboot-required marker: %v", err)
			// Non-fatal - update still succeeded
		}
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
	u.Progress.Message("Detected bootloader: %s", bootloaderType)

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

// getUpdatedRootKernelVersion finds the kernel version from the updated root's modules directory.
// This ensures we use the kernel version from the newly extracted image, not a random kernel
// from the boot partition which might be a different version.
func (u *SystemUpdater) getUpdatedRootKernelVersion() (string, error) {
	modulesDir := filepath.Join(u.Config.MountPoint, "usr", "lib", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return "", fmt.Errorf("failed to read modules directory: %w", err)
	}

	// Find kernel version directories (should typically be just one)
	var kernelVersions []string
	for _, entry := range entries {
		if entry.IsDir() {
			// Verify this directory has a kernel/vmlinuz
			kernelPath := filepath.Join(modulesDir, entry.Name(), "vmlinuz")
			if _, err := os.Stat(kernelPath); err == nil {
				kernelVersions = append(kernelVersions, entry.Name())
			}
		}
	}

	if len(kernelVersions) == 0 {
		return "", fmt.Errorf("no kernel found in updated root at %s", modulesDir)
	}

	// If there are multiple kernel versions (unusual), use the latest one based on string sorting
	// This ensures deterministic behavior
	if len(kernelVersions) > 1 {
		// Sort in reverse order to get the newest version first
		sort.Sort(sort.Reverse(sort.StringSlice(kernelVersions)))
	}

	return kernelVersions[0], nil
}

// updateGRUBBootloader updates GRUB configuration
func (u *SystemUpdater) updateGRUBBootloader() error {
	// Get UUID of new root partition
	targetUUID, err := GetPartitionUUID(context.Background(), u.Target)
	if err != nil {
		return fmt.Errorf("failed to get target UUID: %w", err)
	}

	// Get /var UUID for kernel command line mount
	varUUID, err := GetPartitionUUID(context.Background(), u.Scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

	// Get kernel version from the updated root's modules directory
	// This ensures we use the kernel from the newly extracted image, not a stale kernel
	// that might exist on the boot partition from a previous version
	kernelVersion, err := u.getUpdatedRootKernelVersion()
	if err != nil {
		return fmt.Errorf("failed to get kernel version from updated root: %w", err)
	}

	// Verify the kernel exists on the boot partition (it should have been copied by InstallKernelAndInitramfs)
	kernelPath := filepath.Join(u.Config.BootMountPoint, "vmlinuz-"+kernelVersion)
	if _, err := os.Stat(kernelPath); err != nil {
		return fmt.Errorf("kernel vmlinuz-%s not found on boot partition (should have been copied earlier): %w", kernelVersion, err)
	}

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
	kernelCmdline, err := u.buildKernelCmdline(targetUUID, varUUID, fsType, true)
	if err != nil {
		return fmt.Errorf("failed to build kernel cmdline: %w", err)
	}

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

	activeUUID, _ := GetPartitionUUID(context.Background(), activeRoot)

	// Build previous kernel command line (for the currently active root)
	previousCmdline, err := u.buildKernelCmdline(activeUUID, varUUID, fsType, false)
	if err != nil {
		return fmt.Errorf("failed to build previous kernel cmdline: %w", err)
	}

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

	u.Progress.Message("  Updated GRUB to boot from %s", u.Target)
	return nil
}

// updateSystemdBootBootloader updates systemd-boot configuration
func (u *SystemUpdater) updateSystemdBootBootloader() error {
	// Get UUIDs
	targetUUID, err := GetPartitionUUID(context.Background(), u.Target)
	if err != nil {
		return fmt.Errorf("failed to get target UUID: %w", err)
	}

	varUUID, err := GetPartitionUUID(context.Background(), u.Scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

	activeRoot := u.Scheme.Root1Partition
	if !u.Active {
		activeRoot = u.Scheme.Root2Partition
	}
	activeUUID, _ := GetPartitionUUID(context.Background(), activeRoot)

	// Get kernel version from the updated root's modules directory
	// This ensures we use the kernel from the newly extracted image, not a stale kernel
	// that might exist on the boot partition from a previous version
	kernelVersion, err := u.getUpdatedRootKernelVersion()
	if err != nil {
		return fmt.Errorf("failed to get kernel version from updated root: %w", err)
	}

	// Verify the kernel exists on the boot partition (it should have been copied by InstallKernelAndInitramfs)
	kernelPath := filepath.Join(u.Config.BootMountPoint, "vmlinuz-"+kernelVersion)
	if _, err := os.Stat(kernelPath); err != nil {
		return fmt.Errorf("kernel vmlinuz-%s not found on boot partition (should have been copied earlier): %w", kernelVersion, err)
	}

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
	kernelCmdline, err := u.buildKernelCmdline(targetUUID, varUUID, fsType, true)
	if err != nil {
		return fmt.Errorf("failed to build kernel cmdline: %w", err)
	}

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
	previousCmdline, err := u.buildKernelCmdline(activeUUID, varUUID, fsType, false)
	if err != nil {
		return fmt.Errorf("failed to build previous kernel cmdline: %w", err)
	}

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

	u.Progress.Message("Updated systemd-boot to boot from %s", u.Target)
	return nil
}

// PerformUpdate performs the complete update workflow
func (u *SystemUpdater) PerformUpdate(skipPull bool) error {
	// Acquire exclusive lock for system operation
	lock, err := AcquireSystemLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

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
	needed, digest, err := u.IsUpdateNeeded(false)
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

	// Report completion
	p.Complete("System update complete! Reboot to activate the new version.", map[string]interface{}{
		"image":        u.Config.ImageRef,
		"device":       u.Config.Device,
		"target":       u.Target,
		"image_digest": u.Config.ImageDigest,
	})

	return nil
}
