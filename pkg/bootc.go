package pkg

import (
	"fmt"
	"os"
	"os/exec"
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
	KernelArgs     []string
	MountPoint     string
	FilesystemType string // ext4 or btrfs
}

// NewBootcInstaller creates a new BootcInstaller
func NewBootcInstaller(imageRef, device string) *BootcInstaller {
	return &BootcInstaller{
		ImageRef:       imageRef,
		Device:         device,
		KernelArgs:     []string{},
		MountPoint:     "/tmp/nbc-install",
		FilesystemType: "ext4", // Default to ext4
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
	if b.DryRun {
		fmt.Printf("[DRY RUN] Would pull image: %s\n", b.ImageRef)
		return nil
	}

	fmt.Printf("Validating image reference: %s\n", b.ImageRef)

	// Parse and validate the image reference
	ref, err := name.ParseReference(b.ImageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference: %w", err)
	}

	if b.Verbose {
		fmt.Printf("  Image: %s\n", ref.String())
	}

	// Try to get image descriptor to verify it exists and is accessible
	// This is a lightweight check that doesn't download layers
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("failed to access image: %w (check credentials if private registry)", err)
	}

	fmt.Println("  Image reference is valid and accessible")
	return nil
}

// Install performs the bootc installation to the target disk
func (b *BootcInstaller) Install() error {
	if b.DryRun {
		fmt.Printf("[DRY RUN] Would install %s to %s\n", b.ImageRef, b.Device)
		if len(b.KernelArgs) > 0 {
			fmt.Printf("[DRY RUN] With kernel arguments: %s\n", strings.Join(b.KernelArgs, " "))
		}
		return nil
	}

	fmt.Printf("Installing bootc image to disk...\n")
	fmt.Printf("  Image:      %s\n", b.ImageRef)
	fmt.Printf("  Device:     %s\n", b.Device)
	fmt.Printf("  Filesystem: %s\n", b.FilesystemType)
	fmt.Println()

	// Step 1: Create partitions
	fmt.Println("Step 1/6: Creating partitions...")
	scheme, err := CreatePartitions(b.Device, b.DryRun)
	if err != nil {
		return fmt.Errorf("failed to create partitions: %w", err)
	}

	// Set filesystem type on partition scheme
	scheme.FilesystemType = b.FilesystemType

	// Step 2: Format partitions
	fmt.Println("\nStep 2/6: Formatting partitions...")
	if err := FormatPartitions(scheme, b.DryRun); err != nil {
		return fmt.Errorf("failed to format partitions: %w", err)
	}

	// Step 3: Mount partitions
	fmt.Println("\nStep 3/6: Mounting partitions...")
	if err := MountPartitions(scheme, b.MountPoint, b.DryRun); err != nil {
		return fmt.Errorf("failed to mount partitions: %w", err)
	}

	// Ensure cleanup on error
	defer func() {
		if !b.DryRun {
			fmt.Println("\nCleaning up...")
			_ = UnmountPartitions(b.MountPoint, b.DryRun)
			_ = os.RemoveAll(b.MountPoint)
		}
	}()

	// Step 4: Extract container filesystem
	fmt.Println("\nStep 4/6: Extracting container filesystem...")
	extractor := NewContainerExtractor(b.ImageRef, b.MountPoint)
	extractor.SetVerbose(b.Verbose)
	if err := extractor.Extract(); err != nil {
		return fmt.Errorf("failed to extract container: %w", err)
	}

	// Step 5: Configure system
	fmt.Println("\nStep 5/6: Configuring system...")

	// Create fstab
	if err := CreateFstab(b.MountPoint, scheme); err != nil {
		return fmt.Errorf("failed to create fstab: %w", err)
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
		fmt.Printf("  Warning: could not get image digest: %v\n", err)
		imageDigest = "" // Continue without digest
	} else if b.Verbose {
		fmt.Printf("  Image digest: %s\n", imageDigest)
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
	if err := WriteSystemConfigToTarget(b.MountPoint, config, b.DryRun); err != nil {
		return fmt.Errorf("failed to write system config: %w", err)
	}

	// Step 6: Install bootloader
	fmt.Println("\nStep 6/6: Installing bootloader...")

	// Parse OS information from the extracted container
	osName := ParseOSRelease(b.MountPoint)
	if b.Verbose {
		fmt.Printf("  Detected OS: %s\n", osName)
	}

	bootloader := NewBootloaderInstaller(b.MountPoint, b.Device, scheme, osName)
	bootloader.SetVerbose(b.Verbose)

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

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Installation completed successfully!")
	fmt.Println(strings.Repeat("=", 60))
	return nil
}

// Verify performs post-installation verification
func (b *BootcInstaller) Verify() error {
	if b.DryRun {
		fmt.Println("[DRY RUN] Would verify installation")
		return nil
	}

	fmt.Println("Verifying installation...")

	// Check if the device has partitions now
	deviceName := strings.TrimPrefix(b.Device, "/dev/")
	diskInfo, err := getDiskInfo(deviceName)
	if err != nil {
		return fmt.Errorf("failed to verify: %w", err)
	}

	if len(diskInfo.Partitions) == 0 {
		return fmt.Errorf("no partitions found on device after installation")
	}

	fmt.Printf("Found %d partition(s) on %s\n", len(diskInfo.Partitions), b.Device)
	for _, part := range diskInfo.Partitions {
		fmt.Printf("  - %s (%s)\n", part.Device, FormatSize(part.Size))
	}

	return nil
}

// InstallComplete performs the complete installation workflow
func (b *BootcInstaller) InstallComplete(skipPull bool) error {
	// Check prerequisites
	fmt.Println("Checking prerequisites...")
	if err := CheckRequiredTools(); err != nil {
		return fmt.Errorf("missing required tools: %w", err)
	}

	// Validate disk
	fmt.Printf("Validating disk %s...\n", b.Device)
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

	// Confirm before wiping
	if !b.DryRun {
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
	fmt.Printf("Wiping disk %s...\n", b.Device)
	if err := WipeDisk(b.Device, b.DryRun); err != nil {
		return err
	}
	fmt.Println()

	// Install
	if err := b.Install(); err != nil {
		return err
	}

	// Verify
	if err := b.Verify(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: verification failed: %v\n", err)
	}

	return nil
}
