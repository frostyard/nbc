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
//	installer.SetCallbacks(&pkg.InstallCallbacks{
//	    OnStep: func(step, total int, name string) {
//	        fmt.Printf("[%d/%d] %s\n", step, total, name)
//	    },
//	    OnMessage: func(msg string) {
//	        fmt.Printf("  %s\n", msg)
//	    },
//	})
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

// InstallCallbacks provides hooks for progress reporting during installation.
// All callbacks are optional; nil callbacks are safely ignored.
type InstallCallbacks struct {
	// OnStep is called when starting a new major step.
	// step: 1-based step number (1-6), totalSteps: 6, name: step description.
	OnStep func(step, totalSteps int, name string)

	// OnProgress is called for progress within a step (0-100%).
	OnProgress func(percent int, message string)

	// OnMessage is called for informational messages.
	OnMessage func(message string)

	// OnWarning is called for warning messages.
	OnWarning func(message string)

	// OnError is called before returning an error.
	// This allows logging before error handling.
	OnError func(err error, message string)
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
	config    *InstallConfig
	callbacks *InstallCallbacks

	// Internal state
	loopback  *LoopbackDevice
	startTime time.Time

	// TODO: Remove progressAdapter when ProgressReporter is deprecated
	progressAdapter *callbackProgressAdapter
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

	return &Installer{
		config: cfg,
	}, nil
}

// SetCallbacks sets the progress callbacks for the installation.
func (i *Installer) SetCallbacks(cb *InstallCallbacks) {
	i.callbacks = cb
	i.progressAdapter = newCallbackProgressAdapter(cb, 6)
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

	// Set up cleanup function to always be returned
	defer func() {
		if i.loopback != nil {
			result.LoopbackPath = i.loopback.ImagePath
			result.Cleanup = i.loopback.Cleanup
		} else {
			result.Cleanup = func() error { return nil }
		}
		result.Duration = time.Since(i.startTime)
	}()

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		i.callOnError(err, "Installation cancelled")
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
		i.callOnError(err, "Failed to acquire system lock")
		return result, err
	}
	defer func() { _ = lock.Release() }()

	// Check prerequisites
	i.callOnMessage("Checking prerequisites...")
	if err := CheckRequiredTools(); err != nil {
		err = fmt.Errorf("missing required tools: %w", err)
		i.callOnError(err, "Prerequisites check failed")
		return result, err
	}

	// Validate disk
	i.callOnMessage(fmt.Sprintf("Validating disk %s...", device))
	minSize := uint64(10 * 1024 * 1024 * 1024) // 10 GB minimum
	if err := ValidateDisk(device, minSize); err != nil {
		i.callOnError(err, "Disk validation failed")
		return result, err
	}

	// Check for cancellation
	if err := ctx.Err(); err != nil {
		i.callOnError(err, "Installation cancelled")
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
		i.callOnError(err, "Installation cancelled")
		return result, err
	}

	// Wipe disk (in non-dry-run mode, confirmation should be handled by CLI)
	i.callOnMessage(fmt.Sprintf("Wiping disk %s...", device))
	if err := WipeDisk(ctx, device, i.config.DryRun); err != nil {
		i.callOnError(err, "Failed to wipe disk")
		return result, err
	}

	// Perform installation using internal BootcInstaller
	// TODO: Migrate BootcInstaller internals directly into Installer
	bootcInstaller := i.createBootcInstaller(device)

	if err := bootcInstaller.Install(ctx); err != nil {
		i.callOnError(err, "Installation failed")
		return result, err
	}

	// Verify installation
	if err := bootcInstaller.Verify(ctx); err != nil {
		i.callOnWarning(fmt.Sprintf("Verification failed: %v", err))
	}

	// Update result with final values
	result.BootloaderType = DetectBootloader(i.config.MountPoint)

	// Report completion
	i.callOnMessage("Installation complete! You can now boot from this disk.")

	return result, nil
}

// setupDevice handles loopback setup or device path resolution.
func (i *Installer) setupDevice(ctx context.Context) (string, error) {
	if i.config.Loopback != nil {
		i.callOnMessage("Setting up loopback device...")

		loopback, err := SetupLoopbackInstall(
			i.config.Loopback.ImagePath,
			i.config.Loopback.SizeGB,
			i.config.Loopback.Force,
		)
		if err != nil {
			err = fmt.Errorf("failed to setup loopback: %w", err)
			i.callOnError(err, "Loopback setup failed")
			return "", err
		}
		i.loopback = loopback
		return loopback.Device, nil
	}

	// Resolve device path
	device, err := GetDiskByPath(i.config.Device)
	if err != nil {
		err = fmt.Errorf("invalid device: %w", err)
		i.callOnError(err, "Device resolution failed")
		return "", err
	}
	return device, nil
}

// pullImage validates and accesses the container image.
func (i *Installer) pullImage(ctx context.Context) error {
	if i.config.DryRun {
		i.callOnMessage(fmt.Sprintf("[DRY RUN] Would pull image: %s", i.config.ImageRef))
		return nil
	}

	i.callOnMessage(fmt.Sprintf("Validating image reference: %s", i.config.ImageRef))

	// Create a temporary BootcInstaller for image validation
	// TODO: Extract image validation into standalone function
	bootcInstaller := NewBootcInstaller(i.config.ImageRef, "")
	if err := bootcInstaller.PullImage(ctx); err != nil {
		i.callOnError(err, "Failed to access image")
		return err
	}

	i.callOnMessage("Image reference is valid and accessible")
	return nil
}

// createBootcInstaller creates a BootcInstaller from the current config.
func (i *Installer) createBootcInstaller(device string) *BootcInstaller {
	bootc := NewBootcInstaller(i.config.ImageRef, device)
	bootc.SetVerbose(i.config.Verbose)
	bootc.SetDryRun(i.config.DryRun)
	bootc.SetFilesystemType(i.config.FilesystemType)
	bootc.SetMountPoint(i.config.MountPoint)
	// Note: bootc.Progress is already initialized by NewBootcInstaller()
	// The progressAdapter is used by Installer's own callbacks, not BootcInstaller's

	// Set local image if provided
	if i.config.LocalImage != nil {
		bootc.SetLocalImage(i.config.LocalImage.LayoutPath, i.config.LocalImage.Metadata)
	}

	// Add kernel arguments
	for _, arg := range i.config.KernelArgs {
		bootc.AddKernelArg(arg)
	}

	// Set encryption if provided
	if i.config.Encryption != nil {
		bootc.SetEncryption(i.config.Encryption.Passphrase, "", i.config.Encryption.TPM2)
	}

	// Set root password if provided
	if i.config.RootPassword != "" {
		bootc.SetRootPassword(i.config.RootPassword)
	}

	return bootc
}

// Callback helper methods (nil-safe)
// These are used by the Install() implementation which is currently delegated to bootc.

//nolint:unused // Will be used when Install() is fully implemented
func (i *Installer) callOnStep(step, total int, name string) {
	if i.callbacks != nil && i.callbacks.OnStep != nil {
		i.callbacks.OnStep(step, total, name)
	}
}

//nolint:unused // Will be used when Install() is fully implemented
func (i *Installer) callOnProgress(percent int, message string) {
	if i.callbacks != nil && i.callbacks.OnProgress != nil {
		i.callbacks.OnProgress(percent, message)
	}
}

func (i *Installer) callOnMessage(message string) {
	if i.callbacks != nil && i.callbacks.OnMessage != nil {
		i.callbacks.OnMessage(message)
	}
}

func (i *Installer) callOnWarning(message string) {
	if i.callbacks != nil && i.callbacks.OnWarning != nil {
		i.callbacks.OnWarning(message)
	}
}

func (i *Installer) callOnError(err error, message string) {
	if i.callbacks != nil && i.callbacks.OnError != nil {
		i.callbacks.OnError(err, message)
	}
}

// callbackProgressAdapter adapts InstallCallbacks to work with code
// that still uses ProgressReporter-style calls.
// TODO: Remove this adapter when ProgressReporter usage is fully migrated.
type callbackProgressAdapter struct {
	callbacks  *InstallCallbacks
	totalSteps int
}

func newCallbackProgressAdapter(cb *InstallCallbacks, totalSteps int) *callbackProgressAdapter {
	return &callbackProgressAdapter{
		callbacks:  cb,
		totalSteps: totalSteps,
	}
}

func (a *callbackProgressAdapter) Step(step int, name string) {
	if a.callbacks != nil && a.callbacks.OnStep != nil {
		a.callbacks.OnStep(step, a.totalSteps, name)
	}
}

func (a *callbackProgressAdapter) Message(format string, args ...any) {
	if a.callbacks != nil && a.callbacks.OnMessage != nil {
		a.callbacks.OnMessage(fmt.Sprintf(format, args...))
	}
}

func (a *callbackProgressAdapter) MessagePlain(format string, args ...any) {
	if a.callbacks != nil && a.callbacks.OnMessage != nil {
		a.callbacks.OnMessage(fmt.Sprintf(format, args...))
	}
}

func (a *callbackProgressAdapter) Warning(format string, args ...any) {
	if a.callbacks != nil && a.callbacks.OnWarning != nil {
		a.callbacks.OnWarning(fmt.Sprintf(format, args...))
	}
}

func (a *callbackProgressAdapter) Error(err error, message string) {
	if a.callbacks != nil && a.callbacks.OnError != nil {
		a.callbacks.OnError(err, message)
	}
}

func (a *callbackProgressAdapter) Progress(percent int, message string) {
	if a.callbacks != nil && a.callbacks.OnProgress != nil {
		a.callbacks.OnProgress(percent, message)
	}
}

// CreateCLICallbacks creates InstallCallbacks suitable for CLI usage.
// If jsonOutput is true, emits JSON Lines format; otherwise prints to stdout.
func CreateCLICallbacks(jsonOutput bool) *InstallCallbacks {
	if jsonOutput {
		// Use ProgressReporter for JSON output (maintains compatibility)
		reporter := NewProgressReporter(true, 6)
		return &InstallCallbacks{
			OnStep: func(step, total int, name string) {
				reporter.Step(step, name)
			},
			OnProgress: func(percent int, message string) {
				reporter.Progress(percent, message)
			},
			OnMessage: func(message string) {
				reporter.MessagePlain("%s", message)
			},
			OnWarning: func(message string) {
				reporter.Warning("%s", message)
			},
			OnError: func(err error, message string) {
				reporter.Error(err, message)
			},
		}
	}

	return &InstallCallbacks{
		OnStep: func(step, total int, name string) {
			if step > 1 {
				fmt.Println()
			}
			fmt.Printf("Step %d/%d: %s...\n", step, total, name)
		},
		OnProgress: func(percent int, message string) {
			if message != "" {
				fmt.Printf("  %s\n", message)
			}
		},
		OnMessage: func(message string) {
			// Check if message already has indentation
			if strings.HasPrefix(message, " ") || strings.HasPrefix(message, "[") {
				fmt.Println(message)
			} else {
				fmt.Printf("  %s\n", message)
			}
		},
		OnWarning: func(message string) {
			fmt.Printf("Warning: %s\n", message)
		},
		OnError: func(err error, message string) {
			fmt.Printf("Error: %s: %v\n", message, err)
		},
	}
}
