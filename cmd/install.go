package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type installFlags struct {
	image            string
	device           string
	skipPull         bool
	kernelArgs       []string
	filesystem       string
	encrypt          bool
	passphrase       string
	keyfile          string
	tpm2             bool
	localImage       string
	rootPasswordFile string
	viaLoopback      string
	imageSize        int
	force            bool
}

var instFlags installFlags

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"inst"},
	Short:   "Install a bootc container to a physical disk",
	Long: `Install a bootc compatible container image to a physical disk.

This command will:
  1. Validate the target disk
  2. Pull the container image (unless --skip-pull is specified)
  3. Wipe the disk (after confirmation)
  4. Create partitions (EFI: 2GB, boot: 1GB, root1: 12GB, root2: 12GB, var: remaining)
  5. Extract container filesystem
  6. Configure system and install bootloader
  7. Verify the installation

The dual root partitions enable A/B updates for system resilience.

Supported filesystems: btrfs (default), ext4

With --json flag, outputs streaming JSON Lines for progress updates.

Loopback Installation:
  Use --via-loopback to install to a disk image file instead of a physical disk.
  This creates a sparse image file that can be booted with QEMU or converted to
  other virtual disk formats. Minimum size is 35GB (default).

Example:
  nbc install --image quay.io/example/myimage:latest --device /dev/sda
  nbc install --image localhost/myimage --device /dev/nvme0n1 --filesystem ext4
  nbc install --image localhost/myimage --device /dev/nvme0n1 --karg console=ttyS0
  nbc install --image localhost/myimage --device /dev/sda --json
  nbc install --local-image sha256:abc123 --device /dev/sda  # Use staged image
  nbc install --device /dev/sda  # Auto-detect staged image on ISO

  # Loopback installation
  nbc install --image quay.io/example/myimage:latest --via-loopback ./disk.img
  nbc install --image localhost/myimage --via-loopback ./disk.img --image-size 50
  nbc install --image localhost/myimage --via-loopback ./disk.img --force  # Overwrite existing`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringVarP(&instFlags.image, "image", "i", "", "Container image reference (required unless --local-image or staged image exists)")
	installCmd.Flags().StringVarP(&instFlags.device, "device", "d", "", "Target disk device (required)")
	installCmd.Flags().BoolVar(&instFlags.skipPull, "skip-pull", false, "Skip pulling the image (use already pulled image)")
	installCmd.Flags().StringArrayVarP(&instFlags.kernelArgs, "karg", "k", []string{}, "Kernel argument to pass (can be specified multiple times)")
	installCmd.Flags().StringVarP(&instFlags.filesystem, "filesystem", "f", "btrfs", "Filesystem type for root and var partitions (ext4, btrfs)")
	installCmd.Flags().BoolVar(&instFlags.encrypt, "encrypt", false, "Enable LUKS full disk encryption for root and var partitions")
	installCmd.Flags().StringVar(&instFlags.passphrase, "passphrase", "", "LUKS passphrase (required when --encrypt is set, unless --keyfile is provided)")
	installCmd.Flags().StringVar(&instFlags.keyfile, "keyfile", "", "Path to file containing LUKS passphrase (alternative to --passphrase)")
	installCmd.Flags().BoolVar(&instFlags.tpm2, "tpm2", false, "Enroll TPM2 for automatic LUKS unlock (no PCR binding)")
	installCmd.Flags().StringVar(&instFlags.localImage, "local-image", "", "Use staged local image by digest (auto-detects from /var/cache/nbc/staged-install/ if not specified)")
	installCmd.Flags().StringVar(&instFlags.rootPasswordFile, "root-password-file", "", "Path to file containing root password to set during installation")
	installCmd.Flags().StringVar(&instFlags.viaLoopback, "via-loopback", "", "Path to create a loopback disk image file for installation (instead of --device)")
	installCmd.Flags().IntVar(&instFlags.imageSize, "image-size", pkg.DefaultLoopbackSizeGB, "Size of loopback image in GB (minimum 35GB, default 35GB)")
	installCmd.Flags().BoolVar(&instFlags.force, "force", false, "Overwrite existing loopback image file")
	installCmd.Flags().BoolVar(&instFlags.force, "yes", false, "Overwrite existing loopback image file (alias for --force)")
	installCmd.Flags().Lookup("yes").Hidden = true

	// Don't mark device as required - can use --via-loopback instead
}

func runInstall(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")
	jsonOutput := viper.GetBool("json")

	// Build configuration from flags
	cfg, err := buildInstallConfig(cmd.Context(), verbose, dryRun, jsonOutput)
	if err != nil {
		return err
	}

	// Create installer
	installer, err := pkg.NewInstaller(cfg)
	if err != nil {
		return err
	}

	// Run installation
	result, err := installer.Install(cmd.Context())

	// Always call cleanup if available (handles both success and error cases)
	if result != nil && result.Cleanup != nil {
		defer func() {
			if cleanupErr := result.Cleanup(); cleanupErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanup: %v\n", cleanupErr)
			}
		}()
	}

	if err != nil {
		return err
	}

	// Print loopback usage instructions
	if result.LoopbackPath != "" && !jsonOutput {
		fmt.Println()
		fmt.Println("Loopback image created successfully!")
		fmt.Println()
		fmt.Println("To boot the image with QEMU:")
		fmt.Printf("  qemu-system-x86_64 -enable-kvm -m 2048 -drive file=%s,format=raw -bios /usr/share/ovmf/OVMF.fd\n", result.LoopbackPath)
		fmt.Println()
		fmt.Println("To convert to other formats:")
		fmt.Printf("  qemu-img convert -f raw -O qcow2 %s disk.qcow2\n", result.LoopbackPath)
		fmt.Printf("  qemu-img convert -f raw -O vmdk %s disk.vmdk\n", result.LoopbackPath)
	}

	return nil
}

// buildInstallConfig constructs an InstallConfig from command-line flags.
// It handles local image resolution and validation of flag combinations.
func buildInstallConfig(ctx context.Context, verbose, dryRun, jsonOutput bool) (*pkg.InstallConfig, error) {
	// Create reporter for early error output
	var reporter pkg.Reporter
	if jsonOutput {
		reporter = pkg.NewJSONReporter(os.Stdout)
	} else {
		reporter = pkg.NewTextReporter(os.Stdout)
	}
	reportError := func(err error, msg string) error {
		reporter.Error(err, msg)
		return err
	}

	cfg := &pkg.InstallConfig{
		ImageRef:       instFlags.image,
		Device:         instFlags.device,
		FilesystemType: instFlags.filesystem,
		KernelArgs:     instFlags.kernelArgs,
		RootPassword:   "",
		Verbose:        verbose,
		DryRun:         dryRun,
		JSONOutput:     jsonOutput,
		SkipPull:       instFlags.skipPull,
	}

	// Resolve image source: --image, --local-image, or auto-detect from staged-install
	if instFlags.image != "" && instFlags.localImage != "" {
		err := fmt.Errorf("--image and --local-image are mutually exclusive")
		return nil, reportError(err, "Invalid options")
	}

	if instFlags.localImage != "" {
		// User specified a local image digest
		cache := pkg.NewStagedInstallCache()
		_, metadata, err := cache.GetImage(instFlags.localImage)
		if err != nil {
			return nil, reportError(fmt.Errorf("failed to load local image: %w", err), "Failed to load local image")
		}
		cfg.LocalImage = &pkg.LocalImageSource{
			LayoutPath: cache.GetLayoutPath(metadata.ImageDigest),
			Metadata:   metadata,
		}
		cfg.ImageRef = "" // Clear image ref since we're using local image
		cfg.SkipPull = true
		if !jsonOutput {
			fmt.Printf("Using staged image: %s\n", metadata.ImageRef)
			fmt.Printf("  Digest: %s\n", metadata.ImageDigest)
		}
	} else if instFlags.image == "" {
		// Try to auto-detect from staged-install cache
		cache := pkg.NewStagedInstallCache()
		images, err := cache.List()
		if err != nil {
			return nil, reportError(fmt.Errorf("failed to check staged images: %w", err), "Failed to check staged images")
		}

		if len(images) == 0 {
			err := fmt.Errorf("no --image specified and no staged images found in %s", pkg.StagedInstallDir)
			return nil, reportError(err, "No image specified")
		}

		if len(images) == 1 {
			// Auto-select the only staged image
			localMetadata := &images[0]
			cfg.LocalImage = &pkg.LocalImageSource{
				LayoutPath: cache.GetLayoutPath(localMetadata.ImageDigest),
				Metadata:   localMetadata,
			}
			cfg.SkipPull = true
			if !jsonOutput {
				fmt.Printf("Auto-detected staged image: %s\n", localMetadata.ImageRef)
				fmt.Printf("  Digest: %s\n", localMetadata.ImageDigest)
			}
		} else {
			// Multiple staged images - user must choose
			var b strings.Builder
			b.WriteString("multiple staged images found, use --local-image to select one:\n")
			for _, img := range images {
				fmt.Fprintf(&b, "  %s (%s)\n", img.ImageDigest, img.ImageRef)
			}
			err := fmt.Errorf("%s", b.String())
			return nil, reportError(err, "Multiple staged images found")
		}
	}

	// Handle encryption options
	if instFlags.encrypt {
		if instFlags.passphrase == "" && instFlags.keyfile == "" {
			err := fmt.Errorf("--passphrase or --keyfile is required when --encrypt is set")
			return nil, reportError(err, "Missing passphrase")
		}
		if instFlags.passphrase != "" && instFlags.keyfile != "" {
			err := fmt.Errorf("--passphrase and --keyfile are mutually exclusive")
			return nil, reportError(err, "Invalid encryption options")
		}

		passphrase := instFlags.passphrase
		// Read passphrase from keyfile if provided
		if instFlags.keyfile != "" {
			// Check for cancellation before file I/O
			if err := ctx.Err(); err != nil {
				return nil, reportError(err, "Operation cancelled")
			}
			keyData, err := os.ReadFile(instFlags.keyfile)
			if err != nil {
				return nil, reportError(fmt.Errorf("failed to read keyfile: %w", err), "Failed to read keyfile")
			}
			passphrase = strings.TrimRight(string(keyData), "\n\r")
		}

		cfg.Encryption = &pkg.EncryptionOptions{
			Passphrase: passphrase,
			TPM2:       instFlags.tpm2,
		}
	}

	if instFlags.tpm2 && !instFlags.encrypt {
		err := fmt.Errorf("--tpm2 requires --encrypt to be set")
		return nil, reportError(err, "Invalid encryption options")
	}

	// Handle device/loopback options
	if instFlags.device != "" && instFlags.viaLoopback != "" {
		err := fmt.Errorf("--device and --via-loopback are mutually exclusive")
		return nil, reportError(err, "Invalid options")
	}

	if instFlags.device == "" && instFlags.viaLoopback == "" {
		err := fmt.Errorf("either --device or --via-loopback is required")
		return nil, reportError(err, "Missing target")
	}

	if instFlags.viaLoopback != "" {
		cfg.Loopback = &pkg.LoopbackOptions{
			ImagePath: instFlags.viaLoopback,
			SizeGB:    instFlags.imageSize,
			Force:     instFlags.force,
		}
		cfg.Device = "" // Clear device since we're using loopback
	}

	// Read root password if provided
	if instFlags.rootPasswordFile != "" {
		// Check for cancellation before file I/O
		if err := ctx.Err(); err != nil {
			return nil, reportError(err, "Operation cancelled")
		}
		passwordData, err := os.ReadFile(instFlags.rootPasswordFile)
		if err != nil {
			return nil, reportError(fmt.Errorf("failed to read root password file: %w", err), "Failed to read root password file")
		}
		cfg.RootPassword = strings.TrimRight(string(passwordData), "\n\r")
	}

	return cfg, nil
}
