package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/frostyard/nbc/pkg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// interactiveInstallOptions holds all the options collected from the interactive form
type interactiveInstallOptions struct {
	imageSource      string // "remote", "staged"
	image            string
	stagedImage      string
	installTarget    string // "physical", "loopback"
	device           string
	loopbackPath     string
	loopbackSizeStr  string
	filesystem       string
	encrypt          bool
	passphrase       string
	passphraseConf   string
	tpm2             bool
	kernelArgs       string
	rootPassword     string
	rootPasswordConf string
}

var interactiveInstallCmd = &cobra.Command{
	Use:   "interactive-install",
	Short: "Interactively install a bootc container to a physical disk",
	Long: `Interactively install a bootc compatible container image to a physical disk.

This command provides a user-friendly interactive form to configure the installation.
It prompts for all the options that the regular 'install' command accepts via flags.

Example:
  nbc interactive-install`,
	RunE: runInteractiveInstall,
}

func init() {
	rootCmd.AddCommand(interactiveInstallCmd)
}

func runInteractiveInstall(cmd *cobra.Command, args []string) error {
	verbose := viper.GetBool("verbose")
	dryRun := viper.GetBool("dry-run")

	opts := &interactiveInstallOptions{
		filesystem: "btrfs",
	}

	// Get available disks
	disks, err := pkg.ListDisks()
	if err != nil {
		return fmt.Errorf("failed to list disks: %w", err)
	}

	if len(disks) == 0 {
		return fmt.Errorf("no disks found")
	}

	// Build disk options
	diskOptions := make([]huh.Option[string], len(disks))
	for i, disk := range disks {
		label := fmt.Sprintf("%s - %s (%s)", disk.Device, disk.Model, pkg.FormatSize(disk.Size))
		if disk.IsRemovable {
			label += " [removable]"
		}
		diskOptions[i] = huh.NewOption(label, disk.Device)
	}

	// Check for staged images
	stagedCache := pkg.NewStagedInstallCache()
	stagedImages, _ := stagedCache.List()
	hasStagedImages := len(stagedImages) > 0

	// Build staged image options
	var stagedImageOptions []huh.Option[string]
	if hasStagedImages {
		stagedImageOptions = make([]huh.Option[string], len(stagedImages))
		for i, img := range stagedImages {
			shortDigest := img.ImageDigest
			if len(shortDigest) > 19 {
				shortDigest = shortDigest[:19] + "..."
			}
			label := fmt.Sprintf("%s (%s)", img.ImageRef, shortDigest)
			stagedImageOptions[i] = huh.NewOption(label, img.ImageDigest)
		}
	}

	// Build image source options
	imageSourceOptions := []huh.Option[string]{
		huh.NewOption("Pull from remote registry", "remote"),
	}
	if hasStagedImages {
		imageSourceOptions = append(imageSourceOptions,
			huh.NewOption(fmt.Sprintf("Use staged image (%d available)", len(stagedImages)), "staged"),
		)
	}

	// First form: Image source selection
	imageSourceForm := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("nbc Interactive Install").
				Description("This wizard will guide you through installing a bootc container to disk.\n\n"+
					"⚠️  WARNING: This will DESTROY all data on the selected disk!"),

			huh.NewSelect[string]().
				Title("Image Source").
				Description("Where should the container image come from?").
				Options(imageSourceOptions...).
				Value(&opts.imageSource),
		),
	)

	if err := imageSourceForm.Run(); err != nil {
		return err
	}

	// Second form: Based on image source
	var imageForm *huh.Form
	if opts.imageSource == "remote" {
		imageForm = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Container Image").
					Description("Full image reference (e.g., quay.io/example/myimage:latest)").
					Placeholder("quay.io/example/myimage:latest").
					Value(&opts.image).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("image reference is required")
						}
						return nil
					}),
			),
		)
	} else {
		imageForm = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Staged Image").
					Description("Select a previously staged image").
					Options(stagedImageOptions...).
					Value(&opts.stagedImage),
			),
		)
	}

	if err := imageForm.Run(); err != nil {
		return err
	}

	// Third form: Install target selection (physical disk vs loopback)
	installTargetForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Install Target").
				Description("Where should the system be installed?").
				Options(
					huh.NewOption("Physical disk", "physical"),
					huh.NewOption("Loopback image file", "loopback"),
				).
				Value(&opts.installTarget),
		),
	)

	if err := installTargetForm.Run(); err != nil {
		return err
	}

	// Fourth form: Device or loopback selection based on target
	if opts.installTarget == "loopback" {
		// Loopback image configuration
		loopbackForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Image File Path").
					Description("Path where the disk image will be created").
					Placeholder("./disk.img").
					Value(&opts.loopbackPath).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("image path is required")
						}
						// Check if file exists
						if _, err := os.Stat(s); err == nil {
							return fmt.Errorf("file already exists: %s", s)
						}
						return nil
					}),

				huh.NewInput().
					Title("Image Size (GB)").
					Description(fmt.Sprintf("Size of the disk image (minimum %dGB)", pkg.MinLoopbackSizeGB)).
					Placeholder(fmt.Sprintf("%d", pkg.DefaultLoopbackSizeGB)).
					Value(&opts.loopbackSizeStr).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return nil // Will use default
						}
						size, err := strconv.Atoi(s)
						if err != nil {
							return fmt.Errorf("invalid number")
						}
						if size < pkg.MinLoopbackSizeGB {
							return fmt.Errorf("minimum size is %dGB", pkg.MinLoopbackSizeGB)
						}
						return nil
					}),

				huh.NewSelect[string]().
					Title("Filesystem").
					Description("Filesystem type for root and var partitions").
					Options(
						huh.NewOption("btrfs (recommended)", "btrfs"),
						huh.NewOption("ext4", "ext4"),
					).
					Value(&opts.filesystem),
			),
		)

		if err := loopbackForm.Run(); err != nil {
			return err
		}
	} else {
		// Physical disk selection
		deviceForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Target Disk").
					Description("Select the disk to install to (ALL DATA WILL BE ERASED)").
					Options(diskOptions...).
					Value(&opts.device),

				huh.NewSelect[string]().
					Title("Filesystem").
					Description("Filesystem type for root and var partitions").
					Options(
						huh.NewOption("btrfs (recommended)", "btrfs"),
						huh.NewOption("ext4", "ext4"),
					).
					Value(&opts.filesystem),
			),
		)

		if err := deviceForm.Run(); err != nil {
			return err
		}
	}

	// Fifth form: Encryption options
	encryptForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Encryption?").
				Description("Enable LUKS full disk encryption for root and var partitions").
				Value(&opts.encrypt),
		),
	)

	if err := encryptForm.Run(); err != nil {
		return err
	}

	// If encryption is enabled, get passphrase
	if opts.encrypt {
		// Build passphrase form fields
		passphraseFields := []huh.Field{
			huh.NewInput().
				Title("LUKS Passphrase").
				Description("Enter the passphrase for disk encryption").
				EchoMode(huh.EchoModePassword).
				Value(&opts.passphrase).
				Validate(func(s string) error {
					if len(s) < 8 {
						return fmt.Errorf("passphrase must be at least 8 characters")
					}
					return nil
				}),

			huh.NewInput().
				Title("Confirm Passphrase").
				Description("Re-enter the passphrase to confirm").
				EchoMode(huh.EchoModePassword).
				Value(&opts.passphraseConf).
				Validate(func(s string) error {
					if s != opts.passphrase {
						return fmt.Errorf("passphrases do not match")
					}
					return nil
				}),
		}

		// Only add TPM2 prompt if TPM is available
		if pkg.IsTPMAvailable() {
			passphraseFields = append(passphraseFields,
				huh.NewConfirm().
					Title("Enable TPM2?").
					Description("Enroll TPM2 for automatic unlock").
					Value(&opts.tpm2),
			)
		}

		passphraseForm := huh.NewForm(
			huh.NewGroup(passphraseFields...),
		)

		if err := passphraseForm.Run(); err != nil {
			return err
		}
	}

	// Fifth form: Advanced options
	advancedForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Additional Kernel Arguments").
				Description("Space-separated kernel arguments (optional)").
				Placeholder("console=ttyS0,115200").
				Value(&opts.kernelArgs),

			huh.NewInput().
				Title("Root Password").
				Description("Set a root password (optional, leave empty to skip)").
				EchoMode(huh.EchoModePassword).
				Value(&opts.rootPassword),
		),
	)

	if err := advancedForm.Run(); err != nil {
		return err
	}

	// Confirm root password if provided
	if opts.rootPassword != "" {
		confirmPassForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Confirm Root Password").
					EchoMode(huh.EchoModePassword).
					Value(&opts.rootPasswordConf).
					Validate(func(s string) error {
						if s != opts.rootPassword {
							return fmt.Errorf("passwords do not match")
						}
						return nil
					}),
			),
		)

		if err := confirmPassForm.Run(); err != nil {
			return err
		}
	}

	// Final confirmation
	var confirm bool
	summaryLines := []string{
		"Installation Summary:",
		"",
	}

	if opts.imageSource == "remote" {
		summaryLines = append(summaryLines, fmt.Sprintf("  Image: %s", opts.image))
	} else {
		summaryLines = append(summaryLines, fmt.Sprintf("  Staged Image: %s", opts.stagedImage))
	}

	// Add target info based on install type
	if opts.installTarget == "loopback" {
		loopbackSize := pkg.DefaultLoopbackSizeGB
		if opts.loopbackSizeStr != "" {
			loopbackSize, _ = strconv.Atoi(opts.loopbackSizeStr)
		}
		summaryLines = append(summaryLines,
			"  Target: Loopback image",
			fmt.Sprintf("  Image Path: %s", opts.loopbackPath),
			fmt.Sprintf("  Image Size: %dGB", loopbackSize),
		)
	} else {
		summaryLines = append(summaryLines, fmt.Sprintf("  Target Disk: %s", opts.device))
	}

	summaryLines = append(summaryLines,
		fmt.Sprintf("  Filesystem: %s", opts.filesystem),
		fmt.Sprintf("  Encryption: %v", opts.encrypt),
	)
	if opts.encrypt {
		summaryLines = append(summaryLines, fmt.Sprintf("  TPM2: %v", opts.tpm2))
	}
	if opts.kernelArgs != "" {
		summaryLines = append(summaryLines, fmt.Sprintf("  Kernel Args: %s", opts.kernelArgs))
	}
	if opts.rootPassword != "" {
		summaryLines = append(summaryLines, "  Root Password: [set]")
	}

	// Add appropriate warning based on install target
	if opts.installTarget == "loopback" {
		summaryLines = append(summaryLines, "", "A new disk image will be created at the specified path.")
	} else {
		summaryLines = append(summaryLines, "", "⚠️  This will DESTROY all data on the selected disk!")
	}

	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Confirm Installation").
				Description(strings.Join(summaryLines, "\n")),

			huh.NewConfirm().
				Title("Proceed with installation?").
				Affirmative("Yes, install").
				Negative("Cancel").
				Value(&confirm),
		),
	)

	if err := confirmForm.Run(); err != nil {
		return err
	}

	if !confirm {
		fmt.Println("Installation cancelled.")
		return nil
	}

	// Now perform the installation
	fmt.Println()
	fmt.Println("Starting installation...")
	fmt.Println()

	// Determine image and local image settings
	var imageRef string
	var localLayoutPath string
	var localMetadata *pkg.CachedImageMetadata
	skipPull := false

	if opts.imageSource == "staged" {
		cache := pkg.NewStagedInstallCache()
		_, metadata, err := cache.GetImage(opts.stagedImage)
		if err != nil {
			return fmt.Errorf("failed to load staged image: %w", err)
		}
		localLayoutPath = cache.GetLayoutPath(metadata.ImageDigest)
		localMetadata = metadata
		imageRef = metadata.ImageRef
		skipPull = true
		fmt.Printf("Using staged image: %s\n", metadata.ImageRef)
		fmt.Printf("  Digest: %s\n", metadata.ImageDigest)
	} else {
		imageRef = opts.image
	}

	// Handle loopback or physical device
	var device string
	var loopbackDevice *pkg.LoopbackDevice

	if opts.installTarget == "loopback" {
		// Parse loopback size
		loopbackSize := pkg.DefaultLoopbackSizeGB
		if opts.loopbackSizeStr != "" {
			loopbackSize, _ = strconv.Atoi(opts.loopbackSizeStr)
		}

		// Setup loopback device
		fmt.Println("Setting up loopback device...")
		loopbackDevice, err = pkg.SetupLoopbackInstall(opts.loopbackPath, loopbackSize, false)
		if err != nil {
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
		device, err = pkg.GetDiskByPath(opts.device)
		if err != nil {
			return fmt.Errorf("invalid device: %w", err)
		}
	}

	// Create installer
	installer := pkg.NewBootcInstaller(imageRef, device)
	installer.SetVerbose(verbose)
	installer.SetDryRun(dryRun)
	installer.SetFilesystemType(opts.filesystem)

	// Set local image if using staged image
	if localLayoutPath != "" {
		installer.SetLocalImage(localLayoutPath, localMetadata)
	}

	// Add kernel arguments
	if opts.kernelArgs != "" {
		for _, arg := range strings.Fields(opts.kernelArgs) {
			installer.AddKernelArg(arg)
		}
	}

	// Set encryption options
	if opts.encrypt {
		installer.SetEncryption(opts.passphrase, "", opts.tpm2)
	}

	// Set root password
	if opts.rootPassword != "" {
		installer.SetRootPassword(opts.rootPassword)
	}

	// Run installation
	if err := installer.InstallComplete(skipPull); err != nil {
		return err
	}

	// Print loopback usage instructions
	if opts.installTarget == "loopback" {
		fmt.Println()
		fmt.Println("Loopback image created successfully!")
		fmt.Println()
		fmt.Println("To boot the image with QEMU:")
		fmt.Printf("  qemu-system-x86_64 -enable-kvm -m 2048 -drive file=%s,format=raw -bios /usr/share/ovmf/OVMF.fd\n", opts.loopbackPath)
		fmt.Println()
		fmt.Println("To convert to other formats:")
		fmt.Printf("  qemu-img convert -f raw -O qcow2 %s disk.qcow2\n", opts.loopbackPath)
		fmt.Printf("  qemu-img convert -f raw -O vmdk %s disk.vmdk\n", opts.loopbackPath)
	}

	return nil
}
