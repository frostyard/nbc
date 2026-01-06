package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	installImage            string
	installDevice           string
	installSkipPull         bool
	installKernelArgs       []string
	installFilesystem       string
	installEncrypt          bool
	installPassphrase       string
	installKeyfile          string
	installTPM2             bool
	installLocalImage       string
	installRootPasswordFile string
	installViaLoopback      string
	installImageSize        int
	installForce            bool
)

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

	installCmd.Flags().StringVarP(&installImage, "image", "i", "", "Container image reference (required unless --local-image or staged image exists)")
	installCmd.Flags().StringVarP(&installDevice, "device", "d", "", "Target disk device (required)")
	installCmd.Flags().BoolVar(&installSkipPull, "skip-pull", false, "Skip pulling the image (use already pulled image)")
	installCmd.Flags().StringArrayVarP(&installKernelArgs, "karg", "k", []string{}, "Kernel argument to pass (can be specified multiple times)")
	installCmd.Flags().StringVarP(&installFilesystem, "filesystem", "f", "btrfs", "Filesystem type for root and var partitions (ext4, btrfs)")
	installCmd.Flags().BoolVar(&installEncrypt, "encrypt", false, "Enable LUKS full disk encryption for root and var partitions")
	installCmd.Flags().StringVar(&installPassphrase, "passphrase", "", "LUKS passphrase (required when --encrypt is set, unless --keyfile is provided)")
	installCmd.Flags().StringVar(&installKeyfile, "keyfile", "", "Path to file containing LUKS passphrase (alternative to --passphrase)")
	installCmd.Flags().BoolVar(&installTPM2, "tpm2", false, "Enroll TPM2 for automatic LUKS unlock (no PCR binding)")
	installCmd.Flags().StringVar(&installLocalImage, "local-image", "", "Use staged local image by digest (auto-detects from /var/cache/nbc/staged-install/ if not specified)")
	installCmd.Flags().StringVar(&installRootPasswordFile, "root-password-file", "", "Path to file containing root password to set during installation")
	installCmd.Flags().StringVar(&installViaLoopback, "via-loopback", "", "Path to create a loopback disk image file for installation (instead of --device)")
	installCmd.Flags().IntVar(&installImageSize, "image-size", pkg.DefaultLoopbackSizeGB, "Size of loopback image in GB (minimum 35GB, default 35GB)")
	installCmd.Flags().BoolVar(&installForce, "force", false, "Overwrite existing loopback image file")

	// Don't mark device as required - can use --via-loopback instead
}

func runInstall(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")
	jsonOutput := viper.GetBool("json")

	// Create progress reporter for early error output
	progress := pkg.NewProgressReporter(jsonOutput, 6)

	// Variables for local image handling
	var localLayoutPath string
	var localMetadata *pkg.CachedImageMetadata

	// Resolve image source: --image, --local-image, or auto-detect from staged-install
	if installImage != "" && installLocalImage != "" {
		err := fmt.Errorf("--image and --local-image are mutually exclusive")
		if jsonOutput {
			progress.Error(err, "Invalid options")
		}
		return err
	}

	if installLocalImage != "" {
		// User specified a local image digest
		cache := pkg.NewStagedInstallCache()
		_, metadata, err := cache.GetImage(installLocalImage)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to load local image")
			}
			return fmt.Errorf("failed to load local image: %w", err)
		}
		localLayoutPath = cache.GetLayoutPath(metadata.ImageDigest)
		localMetadata = metadata
		installImage = metadata.ImageRef
		if !jsonOutput {
			fmt.Printf("Using staged image: %s\n", metadata.ImageRef)
			fmt.Printf("  Digest: %s\n", metadata.ImageDigest)
		}
	} else if installImage == "" {
		// Try to auto-detect from staged-install cache
		cache := pkg.NewStagedInstallCache()
		images, err := cache.List()
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to check staged images")
			}
			return fmt.Errorf("failed to check staged images: %w", err)
		}

		if len(images) == 0 {
			err := fmt.Errorf("no --image specified and no staged images found in %s", pkg.StagedInstallDir)
			if jsonOutput {
				progress.Error(err, "No image specified")
			}
			return err
		}

		if len(images) == 1 {
			// Auto-select the only staged image
			localMetadata = &images[0]
			localLayoutPath = cache.GetLayoutPath(localMetadata.ImageDigest)
			installImage = localMetadata.ImageRef
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
			if jsonOutput {
				progress.Error(err, "Multiple staged images found")
			}
			return err
		}
	}

	// Validate filesystem type
	if installFilesystem != "ext4" && installFilesystem != "btrfs" {
		err := fmt.Errorf("unsupported filesystem type: %s (supported: ext4, btrfs)", installFilesystem)
		if jsonOutput {
			progress.Error(err, "Invalid filesystem type")
		}
		return err
	}

	// Validate encryption options
	if installEncrypt {
		if installPassphrase == "" && installKeyfile == "" {
			err := fmt.Errorf("--passphrase or --keyfile is required when --encrypt is set")
			if jsonOutput {
				progress.Error(err, "Missing passphrase")
			}
			return err
		}
		if installPassphrase != "" && installKeyfile != "" {
			err := fmt.Errorf("--passphrase and --keyfile are mutually exclusive")
			if jsonOutput {
				progress.Error(err, "Invalid encryption options")
			}
			return err
		}
		// Read passphrase from keyfile if provided
		if installKeyfile != "" {
			keyData, err := os.ReadFile(installKeyfile)
			if err != nil {
				err = fmt.Errorf("failed to read keyfile: %w", err)
				if jsonOutput {
					progress.Error(err, "Failed to read keyfile")
				}
				return err
			}
			installPassphrase = strings.TrimRight(string(keyData), "\n\r")
		}
	}

	if installTPM2 && !installEncrypt {
		err := fmt.Errorf("--tpm2 requires --encrypt to be set")
		if jsonOutput {
			progress.Error(err, "Invalid encryption options")
		}
		return err
	}

	// Validate device/loopback options
	if installDevice != "" && installViaLoopback != "" {
		err := fmt.Errorf("--device and --via-loopback are mutually exclusive")
		if jsonOutput {
			progress.Error(err, "Invalid options")
		}
		return err
	}

	if installDevice == "" && installViaLoopback == "" {
		err := fmt.Errorf("either --device or --via-loopback is required")
		if jsonOutput {
			progress.Error(err, "Missing target")
		}
		return err
	}

	// Validate loopback image size
	if installViaLoopback != "" && installImageSize < pkg.MinLoopbackSizeGB {
		err := fmt.Errorf("--image-size must be at least %dGB", pkg.MinLoopbackSizeGB)
		if jsonOutput {
			progress.Error(err, "Invalid image size")
		}
		return err
	}

	// Handle loopback setup
	var loopbackDevice *pkg.LoopbackDevice
	var device string
	var err error

	if installViaLoopback != "" {
		// Setup loopback device
		if !jsonOutput {
			fmt.Println("Setting up loopback device...")
		}
		loopbackDevice, err = pkg.SetupLoopbackInstall(installViaLoopback, installImageSize, installForce)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Failed to setup loopback")
			}
			return fmt.Errorf("failed to setup loopback: %w", err)
		}
		device = loopbackDevice.Device

		// Ensure cleanup on exit
		defer func() {
			if loopbackDevice != nil {
				if cleanupErr := loopbackDevice.Cleanup(); cleanupErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to cleanup loopback device: %v\n", cleanupErr)
				}
			}
		}()
	} else {
		// Resolve device path
		device, err = pkg.GetDiskByPath(installDevice)
		if err != nil {
			if jsonOutput {
				progress.Error(err, "Invalid device")
			}
			return fmt.Errorf("invalid device: %w", err)
		}
	}

	if verbose && !jsonOutput {
		fmt.Printf("Resolved device: %s\n", device)
	}

	// Create installer
	installer := pkg.NewBootcInstaller(installImage, device)
	installer.SetVerbose(verbose)
	installer.SetDryRun(dryRun)
	installer.SetFilesystemType(installFilesystem)
	installer.SetJSONOutput(jsonOutput)

	// Set local image if using staged image
	if localLayoutPath != "" {
		installer.SetLocalImage(localLayoutPath, localMetadata)
	}

	// Add kernel arguments
	for _, arg := range installKernelArgs {
		installer.AddKernelArg(arg)
	}

	// Set encryption options
	if installEncrypt {
		installer.SetEncryption(installPassphrase, installKeyfile, installTPM2)
	}

	// Set root password if provided
	if installRootPasswordFile != "" {
		passwordData, err := os.ReadFile(installRootPasswordFile)
		if err != nil {
			err = fmt.Errorf("failed to read root password file: %w", err)
			if jsonOutput {
				progress.Error(err, "Failed to read root password file")
			}
			return err
		}
		installer.SetRootPassword(strings.TrimRight(string(passwordData), "\n\r"))
	}

	// Run installation (skip pull if using local image)
	skipPull := installSkipPull || localLayoutPath != ""
	if err := installer.InstallComplete(skipPull); err != nil {
		if jsonOutput {
			progress.Error(err, "Installation failed")
		}
		return err
	}

	// Print loopback usage instructions
	if installViaLoopback != "" && !jsonOutput {
		fmt.Println()
		fmt.Println("Loopback image created successfully!")
		fmt.Println()
		fmt.Println("To boot the image with QEMU:")
		fmt.Printf("  qemu-system-x86_64 -enable-kvm -m 2048 -drive file=%s,format=raw -bios /usr/share/ovmf/OVMF.fd\n", installViaLoopback)
		fmt.Println()
		fmt.Println("To convert to other formats:")
		fmt.Printf("  qemu-img convert -f raw -O qcow2 %s disk.qcow2\n", installViaLoopback)
		fmt.Printf("  qemu-img convert -f raw -O vmdk %s disk.vmdk\n", installViaLoopback)
	}

	return nil
}
