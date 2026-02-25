// Package pkg provides the public API for nbc (bootc container installation).
//
// The primary entry point is the Installer type, which handles installation
// of bootc container images to physical disks or loopback devices.
//
// Example usage:
//
//	cfg := &pkg.InstallConfig{
//	    ImageRef:       "quay.io/example/myimage:latest",
//	    Device:         "/dev/sda",
//	    FilesystemType: "btrfs",
//	}
//
//	installer, err := pkg.NewInstaller(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	result, err := installer.Install(context.Background())
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if result != nil && result.Cleanup != nil {
//	    defer result.Cleanup()
//	}
package pkg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallConfig holds all configuration options for an installation.
// Either ImageRef or LocalImage must be provided.
// Either Device or Loopback must be provided, but not both.
type InstallConfig struct {
	// ImageRef is the container image reference (e.g., "quay.io/example/myimage:latest").
	// Required unless LocalImage is provided.
	ImageRef string

	// Device is the target disk device (e.g., "/dev/sda").
	// Required unless Loopback is provided.
	Device string

	// FilesystemType is the filesystem for root and var partitions.
	// Supported: "ext4", "btrfs". Default: "btrfs".
	FilesystemType string

	// KernelArgs are additional kernel command line arguments.
	KernelArgs []string

	// MountPoint is the temporary mount point for installation.
	// Default: "/tmp/nbc-install".
	MountPoint string

	// Encryption configures LUKS full disk encryption.
	// Optional; if nil, no encryption is used.
	Encryption *EncryptionOptions

	// LocalImage specifies a pre-staged local image to use instead of pulling.
	// Optional; mutually exclusive with ImageRef.
	LocalImage *LocalImageSource

	// Loopback configures installation to a loopback image file.
	// Optional; mutually exclusive with Device.
	Loopback *LoopbackOptions

	// RootPassword sets the root password during installation.
	// Optional; if empty, no password is set.
	RootPassword string

	// Verbose enables verbose output.
	Verbose bool

	// DryRun simulates the installation without making changes.
	DryRun bool

	// JSONOutput enables JSON Lines output format.
	JSONOutput bool

	// SkipPull skips pulling the image (assumes it's already available).
	SkipPull bool
}

// EncryptionOptions configures LUKS encryption for the installation.
type EncryptionOptions struct {
	// Passphrase is the LUKS encryption passphrase.
	// Required for encryption.
	Passphrase string

	// TPM2 enables automatic unlock via TPM2 (no PCR binding).
	TPM2 bool
}

// LocalImageSource specifies a pre-staged local image.
type LocalImageSource struct {
	// LayoutPath is the path to the OCI layout directory.
	LayoutPath string

	// Metadata contains information about the cached image.
	Metadata *CachedImageMetadata
}

// LoopbackOptions configures installation to a loopback image file.
type LoopbackOptions struct {
	// ImagePath is the path to create the loopback image file.
	ImagePath string

	// SizeGB is the size of the loopback image in gigabytes.
	// Minimum: 35GB.
	SizeGB int

	// Force overwrites an existing image file.
	Force bool
}

// InstallResult contains the results of a successful installation.
type InstallResult struct {
	// ImageRef is the container image that was installed.
	ImageRef string

	// ImageDigest is the digest of the installed image.
	ImageDigest string

	// Device is the device that was installed to.
	Device string

	// FilesystemType is the filesystem used for root and var partitions.
	FilesystemType string

	// BootloaderType is the type of bootloader installed ("grub2" or "systemd-boot").
	BootloaderType BootloaderType

	// LoopbackPath is set if loopback installation was used.
	LoopbackPath string

	// Duration is the total time taken for installation.
	Duration time.Duration

	// Cleanup releases resources (e.g., loopback device).
	// Always non-nil if loopback was used, even on error or cancellation.
	// Caller decides whether to call it based on error handling strategy.
	Cleanup func() error
}

// Installer performs bootc container installation.
type Installer struct {
	config *InstallConfig

	// Internal state
	loopback  *LoopbackDevice
	startTime time.Time
	progress  Reporter
}

// Validate checks the InstallConfig for errors.
func (c *InstallConfig) Validate() error {
	// Check required fields
	if c.ImageRef == "" && c.LocalImage == nil {
		return errors.New("either ImageRef or LocalImage is required")
	}
	if c.Device == "" && c.Loopback == nil {
		return errors.New("either Device or Loopback is required")
	}

	// Check mutual exclusivity
	if c.ImageRef != "" && c.LocalImage != nil {
		return errors.New("imageRef and localImage are mutually exclusive")
	}
	if c.Device != "" && c.Loopback != nil {
		return errors.New("device and loopback are mutually exclusive")
	}

	// Validate filesystem type
	if c.FilesystemType != "" && c.FilesystemType != "ext4" && c.FilesystemType != "btrfs" {
		return fmt.Errorf("unsupported filesystem type: %s (supported: ext4, btrfs)", c.FilesystemType)
	}

	// Validate encryption options
	if c.Encryption != nil {
		if c.Encryption.Passphrase == "" {
			return errors.New("encryption passphrase is required when encryption is enabled")
		}
	}

	// Validate loopback options
	if c.Loopback != nil {
		if c.Loopback.ImagePath == "" {
			return errors.New("loopback ImagePath is required")
		}
		if c.Loopback.SizeGB != 0 && c.Loopback.SizeGB < MinLoopbackSizeGB {
			return fmt.Errorf("loopback size must be at least %dGB", MinLoopbackSizeGB)
		}
	}

	// Validate local image
	if c.LocalImage != nil {
		if c.LocalImage.LayoutPath == "" {
			return errors.New("LocalImage.LayoutPath is required")
		}
	}

	return nil
}

// NewInstaller creates a new Installer with the given configuration.
// Returns an error if the configuration is invalid.
func NewInstaller(cfg *InstallConfig) (*Installer, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	// Apply defaults
	if cfg.FilesystemType == "" {
		cfg.FilesystemType = "btrfs"
	}
	if cfg.MountPoint == "" {
		cfg.MountPoint = "/tmp/nbc-install"
	}
	if cfg.Loopback != nil && cfg.Loopback.SizeGB == 0 {
		cfg.Loopback.SizeGB = DefaultLoopbackSizeGB
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	var reporter Reporter
	if cfg.JSONOutput {
		reporter = NewJSONReporter(os.Stdout)
	} else {
		reporter = NewTextReporter(os.Stdout)
	}

	return &Installer{
		config:   cfg,
		progress: reporter,
	}, nil
}

// Install performs the bootc installation.
// The context can be used to cancel the operation.
// Returns a result with cleanup function even on error/cancellation if resources were allocated.
func (i *Installer) Install(ctx context.Context) (*InstallResult, error) {
	i.startTime = time.Now()

	result := &InstallResult{
		ImageRef:       i.config.ImageRef,
		FilesystemType: i.config.FilesystemType,
	}

	// If using local image, use its image ref
	if i.config.LocalImage != nil && i.config.LocalImage.Metadata != nil {
		result.ImageRef = i.config.LocalImage.Metadata.ImageRef
		result.ImageDigest = i.config.LocalImage.Metadata.ImageDigest
	}

	// Track partition scheme for cleanup
	var scheme *PartitionScheme

	// Set up cleanup function to always be returned
	defer func() {
		if i.loopback != nil {
			result.LoopbackPath = i.loopback.ImagePath
			result.Cleanup = func() error {
				return i.loopback.Cleanup(i.progress)
			}
		} else {
			result.Cleanup = func() error { return nil }
		}
		result.Duration = time.Since(i.startTime)
	}()

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		i.progress.Error(err,"Installation cancelled")
		return result, err
	}

	// Setup loopback or resolve device
	device, err := i.setupDevice(ctx)
	if err != nil {
		return result, err
	}
	result.Device = device

	// Acquire exclusive lock for system operation
	lock, err := AcquireSystemLock()
	if err != nil {
		i.progress.Error(err,"Failed to acquire system lock")
		return result, err
	}
	defer func() { _ = lock.Release() }()

	// Check prerequisites (skip in dry-run mode since we won't actually use the tools)
	if !i.config.DryRun {
		i.progress.Message("Checking prerequisites...")
		if err := CheckRequiredTools(); err != nil {
			err = fmt.Errorf("missing required tools: %w", err)
			i.progress.Error(err,"Prerequisites check failed")
			return result, err
		}
	}

	// Validate disk
	i.progress.Message("Validating disk %s...", device)
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := ValidateDisk(device, minSize); err != nil {
		i.progress.Error(err,"Disk validation failed")
		return result, err
	}

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		i.progress.Error(err,"Installation cancelled")
		return result, err
	}

	// Pull image if not skipped and not using local image
	if !i.config.SkipPull && i.config.LocalImage == nil {
		if err := i.pullImage(ctx); err != nil {
			return result, err
		}
	}

	// Check for cancellation before wiping
	if err := ctx.Err(); err != nil {
		i.progress.Error(err,"Installation cancelled")
		return result, err
	}

	// Wipe disk (in non-dry-run mode, confirmation should be handled by CLI)
	i.progress.Message("Wiping disk %s...", device)
	if err := WipeDisk(ctx, device, i.config.DryRun, i.progress); err != nil {
		i.progress.Error(err,"Failed to wipe disk")
		return result, err
	}

	// Handle dry run mode
	if i.config.DryRun {
		i.progress.MessagePlain("[DRY RUN] Would install %s to %s", i.config.ImageRef, device)
		if len(i.config.KernelArgs) > 0 {
			i.progress.MessagePlain("[DRY RUN] With kernel arguments: %s", strings.Join(i.config.KernelArgs, " "))
		}
		i.progress.Message("Installation complete! You can now boot from this disk.")
		return result, nil
	}

	i.progress.Message("Installing bootc image to disk...")
	i.progress.Message("Image: %s", result.ImageRef)
	i.progress.Message("Device: %s", device)
	i.progress.Message("Filesystem: %s", i.config.FilesystemType)

	// Step 1: Create partitions
	i.progress.Step(1, 6, "Creating partitions")
	scheme, err = CreatePartitions(ctx, device, i.config.DryRun, i.progress)
	if err != nil {
		err = fmt.Errorf("failed to create partitions: %w", err)
		i.progress.Error(err,"Partitioning failed")
		return result, err
	}

	// Set filesystem type on partition scheme
	scheme.FilesystemType = i.config.FilesystemType

	// Setup LUKS encryption if enabled
	if i.config.Encryption != nil {
		i.progress.Message("Setting up LUKS encryption...")
		if err := SetupLUKS(ctx, scheme, i.config.Encryption.Passphrase, i.config.DryRun, i.progress); err != nil {
			err = fmt.Errorf("failed to setup LUKS encryption: %w", err)
			i.progress.Error(err,"Encryption setup failed")
			return result, err
		}
		// Ensure LUKS devices are always cleaned up
		defer scheme.CloseLUKSDevices(ctx)
	}

	// Step 2: Format partitions
	i.progress.Step(2, 6, "Formatting partitions")
	if err := FormatPartitions(ctx, scheme, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to format partitions: %w", err)
		i.progress.Error(err,"Formatting failed")
		return result, err
	}

	// Step 3: Mount partitions
	i.progress.Step(3, 6, "Mounting partitions")
	if err := MountPartitions(ctx, scheme, i.config.MountPoint, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to mount partitions: %w", err)
		i.progress.Error(err,"Mounting failed")
		return result, err
	}

	// Ensure cleanup on error
	defer func() {
		i.progress.Message("Cleaning up...")
		_ = UnmountPartitions(ctx, i.config.MountPoint, i.config.DryRun, i.progress)
		if scheme != nil && scheme.Encrypted {
			scheme.CloseLUKSDevices(ctx)
		}
		_ = os.RemoveAll(i.config.MountPoint)
	}()

	// Step 4: Extract container filesystem
	i.progress.Step(4, 6, "Extracting container filesystem")
	var extractor *ContainerExtractor
	if i.config.LocalImage != nil {
		extractor = NewContainerExtractorFromLocal(i.config.LocalImage.LayoutPath, i.config.MountPoint)
	} else {
		extractor = NewContainerExtractor(i.config.ImageRef, i.config.MountPoint)
	}
	extractor.SetVerbose(i.config.Verbose)
	extractor.SetProgress(i.progress)
	if err := extractor.Extract(ctx); err != nil {
		err = fmt.Errorf("failed to extract container: %w", err)
		i.progress.Error(err,"Container extraction failed")
		return result, err
	}

	// Verify extraction succeeded
	i.progress.Message("Verifying extraction...")
	if err := VerifyExtraction(i.config.MountPoint); err != nil {
		err = fmt.Errorf("container extraction verification failed: %w", err)
		i.progress.Error(err,"Extraction verification failed")
		return result, err
	}

	// Validate initramfs has LUKS/TPM2 support if encryption is enabled
	if i.config.Encryption != nil {
		warnings := ValidateInitramfsSupport(i.config.MountPoint, i.config.Encryption.TPM2)
		for _, warning := range warnings {
			i.progress.Warning("%s", warning)
		}
	}

	// Install the embedded dracut module for /etc overlay persistence
	if err := InstallDracutEtcOverlay(i.config.MountPoint, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to install dracut etc-overlay module: %w", err)
		i.progress.Error(err,"Dracut module installation failed")
		return result, err
	}

	// Regenerate initramfs to include the etc-overlay module
	if err := RegenerateInitramfs(ctx, i.config.MountPoint, i.config.DryRun, i.config.Verbose, i.progress); err != nil {
		i.progress.Warning("initramfs regeneration failed: %v", err)
		i.progress.Warning("Boot may fail if container's initramfs lacks etc-overlay support")
	}

	// Step 5: Configure system
	i.progress.Step(5, 6, "Configuring system")

	// Create fstab
	if err := CreateFstab(ctx, i.config.MountPoint, scheme, i.progress); err != nil {
		err = fmt.Errorf("failed to create fstab: %w", err)
		i.progress.Error(err,"Fstab creation failed")
		return result, err
	}

	// Generate /etc/crypttab if encryption is enabled
	if i.config.Encryption != nil && len(scheme.LUKSDevices) > 0 {
		if i.config.Verbose {
			i.progress.Message("Generating /etc/crypttab (TPM2=%v)", i.config.Encryption.TPM2)
		}
		crypttabContent := GenerateCrypttab(scheme.LUKSDevices, i.config.Encryption.TPM2)
		crypttabPath := filepath.Join(i.config.MountPoint, "etc", "crypttab")
		if err := os.WriteFile(crypttabPath, []byte(crypttabContent), 0600); err != nil {
			err = fmt.Errorf("failed to write /etc/crypttab: %w", err)
			i.progress.Error(err,"Crypttab creation failed")
			return result, err
		}
		if i.config.Verbose {
			i.progress.Message("Created /etc/crypttab with %d devices", len(scheme.LUKSDevices))
		}
	}

	// Setup system directories
	if err := SetupSystemDirectories(i.config.MountPoint, i.progress); err != nil {
		err = fmt.Errorf("failed to setup directories: %w", err)
		i.progress.Error(err,"Directory setup failed")
		return result, err
	}

	// Prepare /etc/machine-id for first boot
	if err := PrepareMachineID(i.config.MountPoint, i.progress); err != nil {
		err = fmt.Errorf("failed to prepare machine-id: %w", err)
		i.progress.Error(err,"Machine-id preparation failed")
		return result, err
	}

	// Populate /.etc.lower with container's /etc
	if err := PopulateEtcLower(i.config.MountPoint, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to populate .etc.lower: %w", err)
		i.progress.Error(err,"Etc lower population failed")
		return result, err
	}

	// Install tmpfiles.d config for /run/nbc-booted marker
	if err := InstallTmpfilesConfig(i.config.MountPoint, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to install tmpfiles config: %w", err)
		i.progress.Error(err,"Tmpfiles config installation failed")
		return result, err
	}

	// Setup /etc persistence
	if err := InstallEtcMountUnit(i.config.MountPoint, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to setup /etc persistence: %w", err)
		i.progress.Error(err,"Etc persistence setup failed")
		return result, err
	}

	// Save pristine /etc for future updates
	if err := SavePristineEtc(i.config.MountPoint, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to save pristine /etc: %w", err)
		i.progress.Error(err,"Pristine etc save failed")
		return result, err
	}

	// Set root password if provided
	if i.config.RootPassword != "" {
		if err := SetRootPasswordInTarget(i.config.MountPoint, i.config.RootPassword, i.config.DryRun, i.progress); err != nil {
			err = fmt.Errorf("failed to set root password: %w", err)
			i.progress.Error(err,"Root password setup failed")
			return result, err
		}
	}

	// Get image digest for tracking updates
	if result.ImageDigest == "" {
		// Fetch digest from remote if not already set from local metadata
		digest, err := GetRemoteImageDigest(i.config.ImageRef)
		if err != nil {
			i.progress.Warning("could not get image digest: %v", err)
		} else {
			result.ImageDigest = digest
			if i.config.Verbose {
				i.progress.Message("Image digest: %s", digest)
			}
		}
	}

	// Write system configuration
	sysConfig := &SystemConfig{
		ImageRef:       result.ImageRef,
		ImageDigest:    result.ImageDigest,
		Device:         device,
		InstallDate:    time.Now().Format(time.RFC3339),
		KernelArgs:     i.config.KernelArgs,
		BootloaderType: string(DetectBootloader(i.config.MountPoint)),
		FilesystemType: i.config.FilesystemType,
	}

	// Get stable disk ID
	if diskID, err := GetDiskID(device); err == nil {
		sysConfig.DiskID = diskID
		if i.config.Verbose {
			i.progress.Message("Disk ID: %s", diskID)
		}
	} else if i.config.Verbose {
		i.progress.Warning("could not determine disk ID: %v", err)
	}

	// Store encryption config if enabled
	if i.config.Encryption != nil && len(scheme.LUKSDevices) > 0 {
		sysConfig.Encryption = &EncryptionConfig{
			Enabled: true,
			TPM2:    i.config.Encryption.TPM2,
		}
		for _, dev := range scheme.LUKSDevices {
			switch dev.MapperName {
			case "root1":
				sysConfig.Encryption.Root1LUKSUUID = dev.LUKSUUID
			case "root2":
				sysConfig.Encryption.Root2LUKSUUID = dev.LUKSUUID
			case "var":
				sysConfig.Encryption.VarLUKSUUID = dev.LUKSUUID
			}
		}
	}

	// Write config to /var partition
	varMountPoint := filepath.Join(i.config.MountPoint, "var")
	if err := WriteSystemConfigToVar(varMountPoint, sysConfig, i.config.DryRun, i.progress); err != nil {
		err = fmt.Errorf("failed to write system config: %w", err)
		i.progress.Error(err,"System config write failed")
		return result, err
	}

	// Step 6: Install bootloader
	i.progress.Step(6, 6, "Installing bootloader")

	// Parse OS information
	osName := ParseOSRelease(i.config.MountPoint)
	if i.config.Verbose {
		i.progress.Message("Detected OS: %s", osName)
	}

	bootloader := NewBootloaderInstaller(i.config.MountPoint, device, scheme, osName)
	bootloader.SetVerbose(i.config.Verbose)
	bootloader.SetProgress(i.progress)

	// Set encryption config if enabled
	if i.config.Encryption != nil {
		luksConfig := &LUKSConfig{
			Enabled:    true,
			Passphrase: i.config.Encryption.Passphrase,
			TPM2:       i.config.Encryption.TPM2,
		}
		bootloader.SetEncryption(luksConfig)
	}

	// Add kernel arguments
	for _, arg := range i.config.KernelArgs {
		bootloader.AddKernelArg(arg)
	}

	// Detect and install bootloader
	bootloaderType := DetectBootloader(i.config.MountPoint)
	bootloader.SetType(bootloaderType)
	result.BootloaderType = bootloaderType

	if err := bootloader.Install(ctx); err != nil {
		err = fmt.Errorf("failed to install bootloader: %w", err)
		i.progress.Error(err,"Bootloader installation failed")
		return result, err
	}

	// Enroll TPM2 if encryption is enabled with TPM2
	if i.config.Encryption != nil && i.config.Encryption.TPM2 {
		luksConfig := &LUKSConfig{
			Enabled:    true,
			Passphrase: i.config.Encryption.Passphrase,
			TPM2:       true,
		}
		i.progress.Message("Enrolling TPM2 for automatic unlock (%d LUKS devices)...", len(scheme.LUKSDevices))
		for idx, luksDevice := range scheme.LUKSDevices {
			i.progress.MessagePlain("  [%d/%d] Enrolling TPM2 for %s (%s)...", idx+1, len(scheme.LUKSDevices), luksDevice.MapperName, luksDevice.Partition)
			if err := EnrollTPM2(ctx, luksDevice.Partition, luksConfig); err != nil {
				err = fmt.Errorf("failed to enroll TPM2 for %s: %w", luksDevice.Partition, err)
				i.progress.Error(err,"TPM2 enrollment failed")
				return result, err
			}
			i.progress.MessagePlain("  [%d/%d] Enrolled TPM2 for %s", idx+1, len(scheme.LUKSDevices), luksDevice.MapperName)
		}
	}

	// Verify installation
	if err := i.verify(ctx, device); err != nil {
		i.progress.Warning("Verification failed: %v", err)
	}

	// Report completion
	i.progress.Message("Installation complete! You can now boot from this disk.")

	return result, nil
}

// setupDevice handles loopback setup or device path resolution.
func (i *Installer) setupDevice(ctx context.Context) (string, error) {
	if i.config.Loopback != nil {
		i.progress.Message("Setting up loopback device...")

		loopback, err := SetupLoopbackInstall(
			i.config.Loopback.ImagePath,
			i.config.Loopback.SizeGB,
			i.config.Loopback.Force,
			i.progress,
		)
		if err != nil {
			err = fmt.Errorf("failed to setup loopback: %w", err)
			i.progress.Error(err,"Loopback setup failed")
			return "", err
		}
		i.loopback = loopback
		return loopback.Device, nil
	}

	// Resolve device path
	device, err := GetDiskByPath(i.config.Device)
	if err != nil {
		err = fmt.Errorf("invalid device: %w", err)
		i.progress.Error(err,"Device resolution failed")
		return "", err
	}
	return device, nil
}

// pullImage validates and accesses the container image.
func (i *Installer) pullImage(ctx context.Context) error {
	if i.config.DryRun {
		i.progress.MessagePlain("[DRY RUN] Would pull image: %s", i.config.ImageRef)
		return nil
	}

	i.progress.Message("Validating image reference: %s", i.config.ImageRef)

	// Validate and check image accessibility using PullImage helper
	if err := PullImage(ctx, i.config.ImageRef, i.config.Verbose, i.progress); err != nil {
		i.progress.Error(err,"Failed to access image")
		return err
	}

	i.progress.Message("Image reference is valid and accessible")
	return nil
}

// verify performs post-installation verification.
func (i *Installer) verify(ctx context.Context, device string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if i.config.DryRun {
		i.progress.MessagePlain("[DRY RUN] Would verify installation")
		return nil
	}

	i.progress.Message("Verifying installation...")

	// Check if the device has partitions now
	deviceName := strings.TrimPrefix(device, "/dev/")
	diskInfo, err := getDiskInfo(deviceName)
	if err != nil {
		return fmt.Errorf("failed to verify: %w", err)
	}

	if len(diskInfo.Partitions) == 0 {
		return fmt.Errorf("no partitions found on device after installation")
	}

	i.progress.Message("Found %d partition(s) on %s", len(diskInfo.Partitions), device)
	for _, part := range diskInfo.Partitions {
		i.progress.MessagePlain("- %s (%s)", part.Device, FormatSize(part.Size))
	}

	return nil
}

