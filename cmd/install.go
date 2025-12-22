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
	installImage      string
	installDevice     string
	installSkipPull   bool
	installKernelArgs []string
	installFilesystem string
	installEncrypt    bool
	installPassphrase string
	installKeyfile    string
	installTPM2       bool
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a bootc container to a physical disk",
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

Supported filesystems: ext4 (default), btrfs

With --json flag, outputs streaming JSON Lines for progress updates.

Example:
  nbc install --image quay.io/example/myimage:latest --device /dev/sda
  nbc install --image localhost/myimage --device /dev/nvme0n1 --filesystem btrfs
  nbc install --image localhost/myimage --device /dev/nvme0n1 --karg console=ttyS0
  nbc install --image localhost/myimage --device /dev/sda --json`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().StringVarP(&installImage, "image", "i", "", "Container image reference (required)")
	installCmd.Flags().StringVarP(&installDevice, "device", "d", "", "Target disk device (required)")
	installCmd.Flags().BoolVar(&installSkipPull, "skip-pull", false, "Skip pulling the image (use already pulled image)")
	installCmd.Flags().StringArrayVarP(&installKernelArgs, "karg", "k", []string{}, "Kernel argument to pass (can be specified multiple times)")
	installCmd.Flags().StringVarP(&installFilesystem, "filesystem", "f", "btrfs", "Filesystem type for root and var partitions (ext4, btrfs)")
	installCmd.Flags().BoolVar(&installEncrypt, "encrypt", false, "Enable LUKS full disk encryption for root and var partitions")
	installCmd.Flags().StringVar(&installPassphrase, "passphrase", "", "LUKS passphrase (required when --encrypt is set, unless --keyfile is provided)")
	installCmd.Flags().StringVar(&installKeyfile, "keyfile", "", "Path to file containing LUKS passphrase (alternative to --passphrase)")
	installCmd.Flags().BoolVar(&installTPM2, "tpm2", false, "Enroll TPM2 for automatic LUKS unlock (no PCR binding)")

	_ = installCmd.MarkFlagRequired("image")
	_ = installCmd.MarkFlagRequired("device")
}

func runInstall(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")
	jsonOutput := viper.GetBool("json")

	// Create progress reporter for early error output
	progress := pkg.NewProgressReporter(jsonOutput, 6)

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

	// Resolve device path
	device, err := pkg.GetDiskByPath(installDevice)
	if err != nil {
		if jsonOutput {
			progress.Error(err, "Invalid device")
		}
		return fmt.Errorf("invalid device: %w", err)
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

	// Add kernel arguments
	for _, arg := range installKernelArgs {
		installer.AddKernelArg(arg)
	}

	// Set encryption options
	if installEncrypt {
		installer.SetEncryption(installPassphrase, installKeyfile, installTPM2)
	}

	// Run installation
	if err := installer.InstallComplete(installSkipPull); err != nil {
		if jsonOutput {
			progress.Error(err, "Installation failed")
		}
		return err
	}

	return nil
}
