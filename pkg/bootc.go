package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// BootcInstaller handles bootc container installation
type BootcInstaller struct {
	ImageRef       string
	Device         string
	Verbose        bool
	DryRun         bool
	JSONOutput     bool
	KernelArgs     []string
	MountPoint     string
	FilesystemType string // ext4 or btrfs
	Progress       *ProgressReporter
	Encryption     *LUKSConfig // Encryption configuration
}

// NewBootcInstaller creates a new BootcInstaller
func NewBootcInstaller(imageRef, device string) *BootcInstaller {
	return &BootcInstaller{
		ImageRef:       imageRef,
		Device:         device,
		KernelArgs:     []string{},
		MountPoint:     "/tmp/nbc-install",
		FilesystemType: "ext4", // Default to ext4
		Progress:       NewProgressReporter(false, 6),
	}
}

// SetVerbose enables verbose output
func (b *BootcInstaller) SetVerbose(verbose bool) {
	b.Verbose = verbose
}

// SetDryRun enables dry run mode
func (b *BootcInstaller) SetDryRun(dryRun bool) {
	b.DryRun = dryRun
}

// AddKernelArg adds a kernel argument
func (b *BootcInstaller) AddKernelArg(arg string) {
	b.KernelArgs = append(b.KernelArgs, arg)
}

// SetMountPoint sets the temporary mount point for installation
func (b *BootcInstaller) SetMountPoint(mountPoint string) {
	b.MountPoint = mountPoint
}

// SetFilesystemType sets the filesystem type for root and var partitions
func (b *BootcInstaller) SetFilesystemType(fsType string) {
	b.FilesystemType = fsType
}

// SetJSONOutput enables JSON output mode
func (b *BootcInstaller) SetJSONOutput(jsonOutput bool) {
	b.JSONOutput = jsonOutput
	b.Progress = NewProgressReporter(jsonOutput, 6)
}

// SetEncryption enables LUKS encryption with the given passphrase/keyfile and optional TPM2
func (b *BootcInstaller) SetEncryption(passphrase, keyfile string, tpm2 bool) {
	b.Encryption = &LUKSConfig{
		Enabled:    true,
		Passphrase: passphrase,
		Keyfile:    keyfile,
		TPM2:       tpm2,
	}
}

// CheckRequiredTools checks if required tools are available
func CheckRequiredTools() error {
	tools := []string{
		"sgdisk",
		"mkfs.vfat",
		"mkfs.ext4",
		"mount",
		"umount",
		"blkid",
		"partprobe",
	}

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found: %w", tool, err)
		}
	}

	return nil
}

// PullImage validates the image reference and checks if it's accessible
// The actual image pull happens during Extract() to avoid duplicate work
func (b *BootcInstaller) PullImage() error {
	p := b.Progress

	if b.DryRun {
		p.MessagePlain("[DRY RUN] Would pull image: %s", b.ImageRef)
		return nil
	}

	p.MessagePlain("Validating image reference: %s", b.ImageRef)

	// Parse and validate the image reference
	ref, err := name.ParseReference(b.ImageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference: %w", err)
	}

	if b.Verbose {
		p.Message("Image: %s", ref.String())
	}

	// Try to get image descriptor to verify it exists and is accessible
	// This is a lightweight check that doesn't download layers
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to access image: %w (check credentials if private registry)", err)
	}

	p.Message("Image reference is valid and accessible")
	return nil
}

// Install performs the bootc installation to the target disk
func (b *BootcInstaller) Install() error {
	p := b.Progress

	if b.DryRun {
		p.MessagePlain("[DRY RUN] Would install %s to %s", b.ImageRef, b.Device)
		if len(b.KernelArgs) > 0 {
			p.MessagePlain("[DRY RUN] With kernel arguments: %s", strings.Join(b.KernelArgs, " "))
		}
		return nil
	}

	p.MessagePlain("Installing bootc image to disk...")
	p.Message("Image: %s", b.ImageRef)
	p.Message("Device: %s", b.Device)
	p.Message("Filesystem: %s", b.FilesystemType)

	// Step 1: Create partitions
	p.Step(1, "Creating partitions")
	scheme, err := CreatePartitions(b.Device, b.DryRun)
	if err != nil {
		return fmt.Errorf("failed to create partitions: %w", err)
	}

	// Set filesystem type on partition scheme
	scheme.FilesystemType = b.FilesystemType

	// Step 1.5: Setup LUKS encryption if enabled
	if b.Encryption != nil && b.Encryption.Enabled {
		p.Message("Setting up LUKS encryption...")
		if err := SetupLUKS(scheme, b.Encryption.Passphrase, b.DryRun); err != nil {
			return fmt.Errorf("failed to setup LUKS encryption: %w", err)
		}

		// Ensure LUKS devices are always cleaned up, even if later steps fail
		defer scheme.CloseLUKSDevices()

	}

	// Step 2: Format partitions
	p.Step(2, "Formatting partitions")
	if err := FormatPartitions(scheme, b.DryRun); err != nil {
		return fmt.Errorf("failed to format partitions: %w", err)
	}

	// Step 3: Mount partitions
	p.Step(3, "Mounting partitions")
	if err := MountPartitions(scheme, b.MountPoint, b.DryRun); err != nil {
		return fmt.Errorf("failed to mount partitions: %w", err)
	}

	// Ensure cleanup on error
	defer func() {
		if !b.DryRun {
			p.Message("Cleaning up...")
			_ = UnmountPartitions(b.MountPoint, b.DryRun)
			// Close LUKS devices if encrypted
			if scheme.Encrypted {
				scheme.CloseLUKSDevices()
			}
			_ = os.RemoveAll(b.MountPoint)
		}
	}()

	// Step 4: Extract container filesystem
	p.Step(4, "Extracting container filesystem")
	extractor := NewContainerExtractor(b.ImageRef, b.MountPoint)
	extractor.SetVerbose(b.Verbose)
	if err := extractor.Extract(); err != nil {
		return fmt.Errorf("failed to extract container: %w", err)
	}

	// Validate initramfs has LUKS/TPM2 support if encryption is enabled
	if b.Encryption != nil && b.Encryption.Enabled {
		warnings := ValidateInitramfsSupport(b.MountPoint, b.Encryption.TPM2)
		for _, warning := range warnings {
			p.Warning("%s", warning)
		}
	}

	// Step 5: Configure system
	p.Step(5, "Configuring system")

	// Create fstab
	if err := CreateFstab(b.MountPoint, scheme); err != nil {
		return fmt.Errorf("failed to create fstab: %w", err)
	}

	// Generate /etc/crypttab if encryption is enabled
	if b.Encryption != nil && b.Encryption.Enabled && len(scheme.LUKSDevices) > 0 {
		if b.Verbose {
			p.Message("Generating /etc/crypttab (TPM2=%v)", b.Encryption.TPM2)
		}
		crypttabContent := GenerateCrypttab(scheme.LUKSDevices, b.Encryption.TPM2)
		crypttabPath := filepath.Join(b.MountPoint, "etc", "crypttab")
		if err := os.WriteFile(crypttabPath, []byte(crypttabContent), 0600); err != nil {
			return fmt.Errorf("failed to write /etc/crypttab: %w", err)
		}
		if b.Verbose {
			p.Message("Created /etc/crypttab with %d devices", len(scheme.LUKSDevices))
		}
	}

	// Setup system directories
	if err := SetupSystemDirectories(b.MountPoint); err != nil {
		return fmt.Errorf("failed to setup directories: %w", err)
	}

	// Setup /etc persistence (verifies /etc and creates backup in /var/etc.backup)
	// Note: /etc stays on the root filesystem for reliable boot
	if err := InstallEtcMountUnit(b.MountPoint, b.DryRun); err != nil {
		return fmt.Errorf("failed to setup /etc persistence: %w", err)
	}

	// Save pristine /etc for future updates
	if err := SavePristineEtc(b.MountPoint, b.DryRun); err != nil {
		return fmt.Errorf("failed to save pristine /etc: %w", err)
	}

	// Get image digest for tracking updates
	imageDigest, err := GetRemoteImageDigest(b.ImageRef)
	if err != nil {
		p.Warning("could not get image digest: %v", err)
		imageDigest = "" // Continue without digest
	} else if b.Verbose {
		p.Message("Image digest: %s", imageDigest)
	}

	// Write system configuration
	config := &SystemConfig{
		ImageRef:       b.ImageRef,
		ImageDigest:    imageDigest,
		Device:         b.Device,
		InstallDate:    time.Now().Format(time.RFC3339),
		KernelArgs:     b.KernelArgs,
		BootloaderType: string(DetectBootloader(b.MountPoint)),
		FilesystemType: b.FilesystemType,
	}

	// Store encryption config if enabled (needed for A/B updates)
	if b.Encryption != nil && b.Encryption.Enabled && len(scheme.LUKSDevices) > 0 {
		config.Encryption = &EncryptionConfig{
			Enabled: true,
			TPM2:    b.Encryption.TPM2,
		}
		for _, dev := range scheme.LUKSDevices {
			switch dev.MapperName {
			case "root1":
				config.Encryption.Root1LUKSUUID = dev.LUKSUUID
			case "root2":
				config.Encryption.Root2LUKSUUID = dev.LUKSUUID
			case "var":
				config.Encryption.VarLUKSUUID = dev.LUKSUUID
			}
		}
	}

	if err := WriteSystemConfigToTarget(b.MountPoint, config, b.DryRun); err != nil {
		return fmt.Errorf("failed to write system config: %w", err)
	}

	// Step 6: Install bootloader
	p.Step(6, "Installing bootloader")

	// Parse OS information from the extracted container
	osName := ParseOSRelease(b.MountPoint)
	if b.Verbose {
		p.Message("Detected OS: %s", osName)
	}

	bootloader := NewBootloaderInstaller(b.MountPoint, b.Device, scheme, osName)
	bootloader.SetVerbose(b.Verbose)

	// Set encryption config if enabled
	if b.Encryption != nil && b.Encryption.Enabled {
		bootloader.SetEncryption(b.Encryption)
	}

	// Add kernel arguments
	for _, arg := range b.KernelArgs {
		bootloader.AddKernelArg(arg)
	}

	// Detect and install appropriate bootloader
	bootloaderType := DetectBootloader(b.MountPoint)
	bootloader.SetType(bootloaderType)

	if err := bootloader.Install(); err != nil {
		return fmt.Errorf("failed to install bootloader: %w", err)
	}

	// Enroll TPM2 if encryption is enabled with TPM2
	if b.Encryption != nil && b.Encryption.Enabled && b.Encryption.TPM2 {
		p.Message("Enrolling TPM2 for automatic unlock (%d LUKS devices)...", len(scheme.LUKSDevices))
		for i, luksDevice := range scheme.LUKSDevices {
			p.Message("  [%d/%d] Enrolling TPM2 for %s (%s)...", i+1, len(scheme.LUKSDevices), luksDevice.MapperName, luksDevice.Partition)
			if err := EnrollTPM2(luksDevice.Partition, b.Encryption); err != nil {
				return fmt.Errorf("failed to enroll TPM2 for %s: %w", luksDevice.Partition, err)
			}
			p.Message("  [%d/%d] Enrolled TPM2 for %s", i+1, len(scheme.LUKSDevices), luksDevice.MapperName)
		}
	}

	return nil
}

// Verify performs post-installation verification
func (b *BootcInstaller) Verify() error {
	p := b.Progress

	if b.DryRun {
		p.MessagePlain("[DRY RUN] Would verify installation")
		return nil
	}

	p.MessagePlain("Verifying installation...")

	// Check if the device has partitions now
	deviceName := strings.TrimPrefix(b.Device, "/dev/")
	diskInfo, err := getDiskInfo(deviceName)
	if err != nil {
		return fmt.Errorf("failed to verify: %w", err)
	}

	if len(diskInfo.Partitions) == 0 {
		return fmt.Errorf("no partitions found on device after installation")
	}

	p.Message("Found %d partition(s) on %s", len(diskInfo.Partitions), b.Device)
	for _, part := range diskInfo.Partitions {
		p.Message("- %s (%s)", part.Device, FormatSize(part.Size))
	}

	return nil
}

// InstallComplete performs the complete installation workflow
func (b *BootcInstaller) InstallComplete(skipPull bool) error {
	p := b.Progress

	// Check prerequisites
	p.MessagePlain("Checking prerequisites...")
	if err := CheckRequiredTools(); err != nil {
		return fmt.Errorf("missing required tools: %w", err)
	}

	// Validate disk
	p.MessagePlain("Validating disk %s...", b.Device)
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := ValidateDisk(b.Device, minSize); err != nil {
		return err
	}

	// Pull image if not skipped
	if !skipPull {
		if err := b.PullImage(); err != nil {
			return err
		}
	}

	// Confirm before wiping (only in non-JSON mode, JSON mode should use --force or be non-interactive)
	if !b.DryRun && !b.JSONOutput {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("WARNING: This will DESTROY ALL DATA on %s!\n", b.Device)
		fmt.Printf("%s\n", strings.Repeat("=", 60))
		fmt.Print("Type 'yes' to continue: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "yes" {
			return fmt.Errorf("installation cancelled by user")
		}
		fmt.Println()
	}

	// Wipe disk
	p.MessagePlain("Wiping disk %s...", b.Device)
	if err := WipeDisk(b.Device, b.DryRun); err != nil {
		return err
	}

	// Install
	if err := b.Install(); err != nil {
		return err
	}

	// Verify
	if err := b.Verify(); err != nil {
		p.Warning("verification failed: %v", err)
	}

	// Report completion
	p.Complete("Installation complete! You can now boot from this disk.", map[string]interface{}{
		"image":      b.ImageRef,
		"device":     b.Device,
		"filesystem": b.FilesystemType,
	})

	return nil
}
