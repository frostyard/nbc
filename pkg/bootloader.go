package pkg

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BootloaderType represents the type of bootloader to install
type BootloaderType string

const (
	BootloaderGRUB2       BootloaderType = "grub2"
	BootloaderSystemdBoot BootloaderType = "systemd-boot"
)

// BootloaderInstaller handles bootloader installation
type BootloaderInstaller struct {
	Type       BootloaderType
	TargetDir  string
	Device     string
	Scheme     *PartitionScheme
	KernelArgs []string
	OSName     string
	Verbose    bool
	Encryption *LUKSConfig // Encryption configuration
}

// NewBootloaderInstaller creates a new BootloaderInstaller
func NewBootloaderInstaller(targetDir, device string, scheme *PartitionScheme, osName string) *BootloaderInstaller {
	return &BootloaderInstaller{
		Type:       BootloaderGRUB2, // Default to GRUB2
		TargetDir:  targetDir,
		Device:     device,
		Scheme:     scheme,
		KernelArgs: []string{},
		OSName:     osName,
	}
}

// SetType sets the bootloader type
func (b *BootloaderInstaller) SetType(t BootloaderType) {
	b.Type = t
}

// AddKernelArg adds a kernel argument
func (b *BootloaderInstaller) AddKernelArg(arg string) {
	b.KernelArgs = append(b.KernelArgs, arg)
}

// SetVerbose enables verbose output
func (b *BootloaderInstaller) SetVerbose(verbose bool) {
	b.Verbose = verbose
}

// SetEncryption sets the encryption configuration
func (b *BootloaderInstaller) SetEncryption(config *LUKSConfig) {
	b.Encryption = config
}

// buildKernelCmdline builds the kernel command line with LUKS support if encrypted
func (b *BootloaderInstaller) buildKernelCmdline() ([]string, error) {
	fsType := b.Scheme.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	var kernelCmdline []string
	var varSpec string // /var device specification for overlay

	if b.Scheme.Encrypted {
		// LUKS encrypted root - use device mapper path
		rootDev := b.Scheme.GetLUKSDevice("root1")
		varDev := b.Scheme.GetLUKSDevice("var")

		if rootDev == nil || varDev == nil {
			return nil, fmt.Errorf("LUKS devices not found for encrypted scheme")
		}

		// Root via device mapper
		kernelCmdline = append(kernelCmdline, "root=/dev/mapper/root1")
		kernelCmdline = append(kernelCmdline, "ro")

		// LUKS UUIDs for initramfs to discover and unlock
		kernelCmdline = append(kernelCmdline, "rd.luks.uuid="+rootDev.LUKSUUID)
		kernelCmdline = append(kernelCmdline, "rd.luks.name="+rootDev.LUKSUUID+"=root1")

		// Var partition via device mapper
		kernelCmdline = append(kernelCmdline, "rd.luks.uuid="+varDev.LUKSUUID)
		kernelCmdline = append(kernelCmdline, "rd.luks.name="+varDev.LUKSUUID+"=var")

		// TPM2 auto-unlock if enabled
		if b.Encryption != nil && b.Encryption.TPM2 {
			if b.Verbose {
				fmt.Println("  Adding TPM2 unlock options to kernel cmdline")
			}
			kernelCmdline = append(kernelCmdline, "rd.luks.options="+rootDev.LUKSUUID+"=tpm2-device=auto")
			kernelCmdline = append(kernelCmdline, "rd.luks.options="+varDev.LUKSUUID+"=tpm2-device=auto")
		} else if b.Verbose {
			fmt.Printf("  TPM2 options not added: Encryption=%v, TPM2=%v\n", b.Encryption != nil, b.Encryption != nil && b.Encryption.TPM2)
		}

		// Mount boot partition via systemd.mount-extra (always FAT32, never encrypted)
		bootUUID, err := GetPartitionUUID(b.Scheme.BootPartition)
		if err != nil {
			return nil, fmt.Errorf("failed to get boot UUID: %w", err)
		}
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+bootUUID+":/boot:vfat:defaults")

		// Mount /var via systemd.mount-extra using mapper device
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=/dev/mapper/var:/var:"+fsType+":defaults")
		varSpec = "/dev/mapper/var"
	} else {
		// Non-encrypted root - use UUID
		rootUUID, err := GetPartitionUUID(b.Scheme.Root1Partition)
		if err != nil {
			return nil, fmt.Errorf("failed to get root UUID: %w", err)
		}

		varUUID, err := GetPartitionUUID(b.Scheme.VarPartition)
		if err != nil {
			return nil, fmt.Errorf("failed to get var UUID: %w", err)
		}

		// Get boot partition UUID (always FAT32, never encrypted)
		bootUUID, err := GetPartitionUUID(b.Scheme.BootPartition)
		if err != nil {
			return nil, fmt.Errorf("failed to get boot UUID: %w", err)
		}

		kernelCmdline = append(kernelCmdline, "root=UUID="+rootUUID)
		kernelCmdline = append(kernelCmdline, "ro")
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+bootUUID+":/boot:vfat:defaults")
		kernelCmdline = append(kernelCmdline, "systemd.mount-extra=UUID="+varUUID+":/var:"+fsType+":defaults")
		varSpec = "UUID=" + varUUID
	}

	// Enable /etc overlay persistence
	// The dracut module 95etc-overlay will mount an overlayfs for /etc
	// with the root filesystem as lowerdir and /var/lib/nbc/etc-overlay as upperdir
	kernelCmdline = append(kernelCmdline, "rd.etc.overlay=1")
	kernelCmdline = append(kernelCmdline, "rd.etc.overlay.var="+varSpec)

	// HACK: Disable NVMe multipath to ensure stable device naming across reboots
	// Modern kernels have CONFIG_NVME_MULTIPATH=y by default, causing nvme0/nvme1
	// to swap between boots due to non-deterministic controller enumeration order.
	// This is a workaround - ideally we should always use /dev/disk/by-id/* or UUIDs
	// everywhere and not rely on /dev/nvmeXnY naming at all.
	// TODO: Audit all code for hardcoded /dev/nvme* paths and migrate to stable identifiers
	kernelCmdline = append(kernelCmdline, "nvme_core.multipath=N")

	// Add user-specified kernel arguments
	kernelCmdline = append(kernelCmdline, b.KernelArgs...)

	return kernelCmdline, nil
}

// ensureUppercaseEFIDirectory ensures the EFI directory structure uses proper uppercase naming
// This is important because FAT32 is case-insensitive but case-preserving. If the container
// image was extracted with a lowercase "efi" directory, we need to rename it to "EFI".
// On FAT32, we must use a two-step rename (efi → efi_tmp → EFI) to actually change the
// stored case, since direct rename is a no-op on case-insensitive filesystems.
func ensureUppercaseEFIDirectory(espPath string) error {
	// Check for lowercase "efi" directory by listing the parent directory
	// and looking for the actual case used
	entries, err := os.ReadDir(espPath)
	if err != nil {
		return nil // ESP might not exist yet, that's fine
	}

	var efiDirName string
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), "efi") {
			efiDirName = entry.Name()
			break
		}
	}

	if efiDirName == "" {
		return nil // No EFI directory exists yet
	}

	// If it's already uppercase, we're done
	if efiDirName == "EFI" {
		// But still check for lowercase "boot" inside
		return ensureUppercaseBOOTDirectory(filepath.Join(espPath, "EFI"))
	}

	// Need to rename to uppercase using two-step rename for FAT32
	lowercaseEFI := filepath.Join(espPath, efiDirName)
	tempEFI := filepath.Join(espPath, "efi_rename_tmp")
	uppercaseEFI := filepath.Join(espPath, "EFI")

	// Step 1: Rename to temp name
	if err := os.Rename(lowercaseEFI, tempEFI); err != nil {
		return fmt.Errorf("failed to rename %s to temp: %w", efiDirName, err)
	}

	// Step 2: Rename to uppercase
	if err := os.Rename(tempEFI, uppercaseEFI); err != nil {
		// Try to restore original name
		_ = os.Rename(tempEFI, lowercaseEFI)
		return fmt.Errorf("failed to rename temp to EFI: %w", err)
	}

	fmt.Printf("  Renamed %s/ to EFI/ for UEFI compatibility\n", efiDirName)

	// Also fix BOOT subdirectory
	return ensureUppercaseBOOTDirectory(uppercaseEFI)
}

// ensureUppercaseBOOTDirectory ensures the BOOT subdirectory inside EFI uses uppercase
func ensureUppercaseBOOTDirectory(efiPath string) error {
	entries, err := os.ReadDir(efiPath)
	if err != nil {
		return nil
	}

	var bootDirName string
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), "boot") {
			bootDirName = entry.Name()
			break
		}
	}

	if bootDirName == "" || bootDirName == "BOOT" {
		return nil // No BOOT directory or already uppercase
	}

	// Two-step rename for FAT32
	lowercaseBoot := filepath.Join(efiPath, bootDirName)
	tempBoot := filepath.Join(efiPath, "boot_rename_tmp")
	uppercaseBoot := filepath.Join(efiPath, "BOOT")

	if err := os.Rename(lowercaseBoot, tempBoot); err != nil {
		return fmt.Errorf("failed to rename %s to temp: %w", bootDirName, err)
	}

	if err := os.Rename(tempBoot, uppercaseBoot); err != nil {
		_ = os.Rename(tempBoot, lowercaseBoot)
		return fmt.Errorf("failed to rename temp to BOOT: %w", err)
	}

	fmt.Printf("  Renamed EFI/%s/ to EFI/BOOT/ for UEFI compatibility\n", bootDirName)
	return nil
}

// copyKernelFromModules copies kernel and initramfs from /usr/lib/modules/$KERNEL_VERSION/ to /boot
// Since boot partition is now a combined EFI/boot partition, all files go to /boot
func (b *BootloaderInstaller) copyKernelFromModules() error {
	modulesDir := filepath.Join(b.TargetDir, "usr", "lib", "modules")

	// All bootloaders now use /boot (which is the EFI System Partition)
	bootDir := filepath.Join(b.TargetDir, "boot")

	// Remove any existing boot entries from the container image
	// These may have wrong OS names (e.g., "Fedora" when we're installing "Snow Linux")
	entriesDir := filepath.Join(bootDir, "loader", "entries")
	if entries, err := filepath.Glob(filepath.Join(entriesDir, "*.conf")); err == nil {
		for _, entry := range entries {
			_ = os.Remove(entry)
		}
	}

	// Find kernel version directories
	entries, err := os.ReadDir(modulesDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("no kernel modules found in /usr/lib/modules")
	}

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

		// Copy kernel to appropriate boot directory
		kernelName := "vmlinuz-" + kernelVersion
		destKernel := filepath.Join(bootDir, kernelName)
		if err := copyFile(srcKernel, destKernel); err != nil {
			return fmt.Errorf("failed to copy kernel %s: %w", kernelName, err)
		}
		fmt.Printf("  Copied kernel to boot partition: %s\n", kernelName)

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
				destInitrd := filepath.Join(bootDir, initrdName)

				if err := copyFile(pattern, destInitrd); err != nil {
					return fmt.Errorf("failed to copy initramfs %s: %w", initrdName, err)
				}
				fmt.Printf("  Copied initramfs to boot partition: %s\n", initrdName)
				break // Only copy the first matching initramfs
			}
		}
	}

	return nil
}

// Install installs the bootloader
func (b *BootloaderInstaller) Install() error {
	fmt.Printf("Installing %s bootloader...\n", b.Type)

	// Ensure EFI directory structure uses proper uppercase naming (UEFI spec requirement)
	espPath := filepath.Join(b.TargetDir, "boot")
	if err := ensureUppercaseEFIDirectory(espPath); err != nil {
		fmt.Printf("  Warning: %v\n", err)
	}

	// Copy kernel and initramfs from /usr/lib/modules to /boot
	if err := b.copyKernelFromModules(); err != nil {
		return fmt.Errorf("failed to copy kernel from modules: %w", err)
	}

	var err error
	switch b.Type {
	case BootloaderGRUB2:
		err = b.installGRUB2()
	case BootloaderSystemdBoot:
		err = b.installSystemdBoot()
	default:
		return fmt.Errorf("unsupported bootloader type: %s", b.Type)
	}

	if err != nil {
		return err
	}

	// Register EFI boot entry using efibootmgr if available
	if regErr := b.registerEFIBootEntry(); regErr != nil {
		// Not fatal - the removable media fallback path should still work
		fmt.Printf("  Warning: failed to register EFI boot entry: %v\n", regErr)
	}

	return nil
}

// installGRUB2 installs GRUB2 bootloader
func (b *BootloaderInstaller) installGRUB2() error {
	fmt.Println("  Installing GRUB2...")

	// Check if grub-install is available
	grubInstallCmd := "grub-install"
	if _, err := exec.LookPath("grub2-install"); err == nil {
		grubInstallCmd = "grub2-install"
	}

	espPath := filepath.Join(b.TargetDir, "boot")
	efiBootDir := filepath.Join(espPath, "EFI", "BOOT")

	// Install GRUB to the disk
	args := []string{
		"--target=x86_64-efi",
		"--efi-directory=" + espPath,
		"--boot-directory=" + espPath,
		"--bootloader-id=BOOT",
		"--removable", // Install to removable media path for compatibility
	}

	if b.Verbose {
		args = append(args, "--verbose")
	}

	cmd := exec.Command(grubInstallCmd, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install GRUB: %w", err)
	}

	// Find the GRUB EFI that was just installed
	grubEFI := filepath.Join(efiBootDir, "BOOTX64.EFI")
	if _, err := os.Stat(grubEFI); os.IsNotExist(err) {
		// Try alternate names
		alternates := []string{
			filepath.Join(efiBootDir, "grubx64.efi"),
			filepath.Join(espPath, "EFI", "BOOT", "grubx64.efi"),
		}
		for _, alt := range alternates {
			if _, err := os.Stat(alt); err == nil {
				grubEFI = alt
				break
			}
		}
	}

	// Try to set up Secure Boot chain with shim
	if _, err := os.Stat(grubEFI); err == nil {
		secureBootEnabled, err := b.setupSecureBootChain(grubEFI)
		if err != nil {
			return fmt.Errorf("failed to setup Secure Boot chain: %w", err)
		}
		if secureBootEnabled {
			fmt.Println("  Configured GRUB2 with Secure Boot support")
		}
	}

	// Generate GRUB configuration
	if err := b.generateGRUBConfig(); err != nil {
		return fmt.Errorf("failed to generate GRUB config: %w", err)
	}

	fmt.Println("  GRUB2 installation complete")
	return nil
}

// generateGRUBConfig generates GRUB configuration
func (b *BootloaderInstaller) generateGRUBConfig() error {
	fmt.Println("  Generating GRUB configuration...")

	// Find kernel and initramfs
	bootDir := filepath.Join(b.TargetDir, "boot")
	kernels, err := filepath.Glob(filepath.Join(bootDir, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs/initrd
	var initrd string
	initrdPatterns := []string{
		filepath.Join(bootDir, "initramfs-"+kernelVersion+".img"),
		filepath.Join(bootDir, "initrd.img-"+kernelVersion),
		filepath.Join(bootDir, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Build kernel command line (with LUKS support if encrypted)
	kernelCmdline, err := b.buildKernelCmdline()
	if err != nil {
		return fmt.Errorf("failed to build kernel command line: %w", err)
	}

	// Build final command line: root=..., ro, console=tty0, then rest of args
	var finalCmdline []string
	finalCmdline = append(finalCmdline, kernelCmdline[0]) // root=...
	finalCmdline = append(finalCmdline, "ro", "console=tty0")
	for _, arg := range kernelCmdline[1:] {
		if arg != "rw" { // Skip rw since we use ro for GRUB
			finalCmdline = append(finalCmdline, arg)
		}
	}

	// Create GRUB config
	grubCfg := fmt.Sprintf(`set timeout=5
set default=0

menuentry '%s' {
    linux /vmlinuz-%s %s
    initrd /%s
}
`, b.OSName, kernelVersion, strings.Join(finalCmdline, " "), initrd)

	// Write GRUB config
	grubDir := filepath.Join(b.TargetDir, "boot", "grub")
	if _, err := os.Stat(grubDir); os.IsNotExist(err) {
		grubDir = filepath.Join(b.TargetDir, "boot", "grub2")
	}

	if err := os.MkdirAll(grubDir, 0755); err != nil {
		return fmt.Errorf("failed to create grub directory: %w", err)
	}

	grubCfgPath := filepath.Join(grubDir, "grub.cfg")
	if err := os.WriteFile(grubCfgPath, []byte(grubCfg), 0644); err != nil {
		return fmt.Errorf("failed to write grub.cfg: %w", err)
	}

	fmt.Printf("  Created GRUB configuration at %s\n", grubCfgPath)
	return nil
}

// installSystemdBoot installs systemd-boot bootloader
func (b *BootloaderInstaller) installSystemdBoot() error {
	fmt.Println("  Installing systemd-boot...")

	espPath := filepath.Join(b.TargetDir, "boot")

	// Create EFI directory structure
	efiSystemdDir := filepath.Join(espPath, "EFI", "systemd")
	efiBootDir := filepath.Join(espPath, "EFI", "BOOT")
	if err := os.MkdirAll(efiSystemdDir, 0755); err != nil {
		return fmt.Errorf("failed to create EFI/systemd directory: %w", err)
	}
	if err := os.MkdirAll(efiBootDir, 0755); err != nil {
		return fmt.Errorf("failed to create EFI/BOOT directory: %w", err)
	}

	// Find systemd-boot EFI binary in the container image
	// Check both signed and unsigned variants
	efiSourcePaths := []string{
		filepath.Join(b.TargetDir, "usr", "lib", "systemd", "boot", "efi", "systemd-bootx64.efi.signed"),
		filepath.Join(b.TargetDir, "usr", "lib", "systemd", "boot", "efi", "systemd-bootx64.efi"),
		filepath.Join(b.TargetDir, "usr", "lib64", "systemd", "boot", "efi", "systemd-bootx64.efi.signed"),
		filepath.Join(b.TargetDir, "usr", "lib64", "systemd", "boot", "efi", "systemd-bootx64.efi"),
	}

	var efiSource string
	for _, path := range efiSourcePaths {
		if _, err := os.Stat(path); err == nil {
			efiSource = path
			break
		}
	}

	if efiSource == "" {
		return fmt.Errorf("systemd-boot EFI binary not found in container image")
	}

	// Copy to EFI/systemd/systemd-bootx64.efi
	if err := copyEFIFile(efiSource, filepath.Join(efiSystemdDir, "systemd-bootx64.efi")); err != nil {
		return fmt.Errorf("failed to copy systemd-boot EFI: %w", err)
	}

	// Try to set up Secure Boot chain with shim
	secureBootEnabled, err := b.setupSecureBootChain(efiSource)
	if err != nil {
		return fmt.Errorf("failed to setup Secure Boot chain: %w", err)
	}

	if !secureBootEnabled {
		// No shim available, copy directly to EFI/BOOT/BOOTX64.EFI for removable media boot
		if err := copyEFIFile(efiSource, filepath.Join(efiBootDir, "BOOTX64.EFI")); err != nil {
			return fmt.Errorf("failed to copy fallback EFI: %w", err)
		}
		fmt.Println("  Installed systemd-boot EFI binaries (no Secure Boot shim found)")
	} else {
		fmt.Println("  Installed systemd-boot with Secure Boot support")
	}

	// Generate loader configuration
	if err := b.generateSystemdBootConfig(); err != nil {
		return fmt.Errorf("failed to generate systemd-boot config: %w", err)
	}

	fmt.Println("  systemd-boot installation complete")
	return nil
}

// copyEFIFile copies a file from src to dst, ensuring data is synced to disk
func copyEFIFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	// Get source file info for size validation
	srcInfo, err := source.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}

	written, err := io.Copy(dest, source)
	if err != nil {
		_ = dest.Close()
		return fmt.Errorf("failed to copy data: %w", err)
	}

	// Verify we copied the expected amount
	if written != srcInfo.Size() {
		_ = dest.Close()
		return fmt.Errorf("incomplete copy: wrote %d bytes, expected %d", written, srcInfo.Size())
	}

	// Sync to ensure data is on disk
	if err := dest.Sync(); err != nil {
		_ = dest.Close()
		return fmt.Errorf("failed to sync: %w", err)
	}

	if err := dest.Close(); err != nil {
		return fmt.Errorf("failed to close: %w", err)
	}

	return nil
}

// generateSystemdBootConfig generates systemd-boot configuration
func (b *BootloaderInstaller) generateSystemdBootConfig() error {
	fmt.Println("  Generating systemd-boot configuration...")

	// Find kernel on boot partition (combined EFI/boot partition)
	bootDir := filepath.Join(b.TargetDir, "boot")
	kernels, err := filepath.Glob(filepath.Join(bootDir, "vmlinuz-*"))
	if err != nil || len(kernels) == 0 {
		return fmt.Errorf("no kernel found in /boot")
	}
	kernel := filepath.Base(kernels[0])
	kernelVersion := strings.TrimPrefix(kernel, "vmlinuz-")

	// Look for initramfs on boot partition
	var initrd string
	initrdPatterns := []string{
		filepath.Join(bootDir, "initramfs-"+kernelVersion+".img"),
		filepath.Join(bootDir, "initrd.img-"+kernelVersion),
		filepath.Join(bootDir, "initramfs-"+kernelVersion),
	}
	for _, pattern := range initrdPatterns {
		if _, err := os.Stat(pattern); err == nil {
			initrd = filepath.Base(pattern)
			break
		}
	}

	// Build kernel command line (with LUKS support if encrypted)
	kernelCmdline, err := b.buildKernelCmdline()
	if err != nil {
		return fmt.Errorf("failed to build kernel command line: %w", err)
	}

	// Create loader configuration (in /boot/loader since /boot is the ESP)
	loaderDir := filepath.Join(b.TargetDir, "boot", "loader")
	if err := os.MkdirAll(loaderDir, 0755); err != nil {
		return fmt.Errorf("failed to create loader directory: %w", err)
	}

	loaderConf := `default bootc
timeout 5
console-mode max
editor yes
`
	loaderConfPath := filepath.Join(loaderDir, "loader.conf")
	if err := os.WriteFile(loaderConfPath, []byte(loaderConf), 0644); err != nil {
		return fmt.Errorf("failed to write loader.conf: %w", err)
	}

	// Remove any existing boot entries (from container image or bootctl install)
	entriesDir := filepath.Join(loaderDir, "entries")
	if entries, err := filepath.Glob(filepath.Join(entriesDir, "*.conf")); err == nil {
		for _, entry := range entries {
			_ = os.Remove(entry)
		}
	}
	if err := os.MkdirAll(entriesDir, 0755); err != nil {
		return fmt.Errorf("failed to create entries directory: %w", err)
	}

	entry := fmt.Sprintf(`title   %s
linux   /vmlinuz-%s
initrd  /%s
options %s
`, b.OSName, kernelVersion, initrd, strings.Join(kernelCmdline, " "))

	entryPath := filepath.Join(entriesDir, "bootc.conf")
	if err := os.WriteFile(entryPath, []byte(entry), 0644); err != nil {
		return fmt.Errorf("failed to write boot entry: %w", err)
	}

	fmt.Printf("  Created boot entry: %s\n", b.OSName)
	return nil
}

// findShimEFI looks for shim EFI binary in the container image for Secure Boot support
// Returns the path to the shim if found, empty string otherwise
func findShimEFI(targetDir string) string {
	// Common locations for shim EFI binary
	shimPaths := []string{
		// Fedora/RHEL/CentOS locations
		filepath.Join(targetDir, "boot", "efi", "EFI", "fedora", "shimx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "centos", "shimx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "redhat", "shimx64.efi"),
		// Debian/Ubuntu locations
		filepath.Join(targetDir, "boot", "efi", "EFI", "debian", "shimx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "ubuntu", "shimx64.efi"),
		// Signed shim from shim-signed package
		filepath.Join(targetDir, "usr", "lib", "shim", "shimx64.efi.signed"),
		filepath.Join(targetDir, "usr", "lib64", "shim", "shimx64.efi.signed"),
		filepath.Join(targetDir, "usr", "share", "shim", "shimx64.efi.signed"),
		// Unsigned shim (less common)
		filepath.Join(targetDir, "usr", "lib", "shim", "shimx64.efi"),
		filepath.Join(targetDir, "usr", "lib64", "shim", "shimx64.efi"),
	}

	for _, path := range shimPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// findMokManager looks for the MOK (Machine Owner Key) manager EFI binary
// This is needed for Secure Boot key enrollment
func findMokManager(targetDir string) string {
	mokPaths := []string{
		// Fedora/RHEL/CentOS locations
		filepath.Join(targetDir, "boot", "efi", "EFI", "fedora", "mmx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "centos", "mmx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "redhat", "mmx64.efi"),
		// Debian/Ubuntu locations
		filepath.Join(targetDir, "boot", "efi", "EFI", "debian", "mmx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "ubuntu", "mmx64.efi"),
		// From shim package
		filepath.Join(targetDir, "usr", "lib", "shim", "mmx64.efi.signed"),
		filepath.Join(targetDir, "usr", "lib64", "shim", "mmx64.efi.signed"),
		filepath.Join(targetDir, "usr", "share", "shim", "mmx64.efi.signed"),
		filepath.Join(targetDir, "usr", "lib", "shim", "mmx64.efi"),
		filepath.Join(targetDir, "usr", "lib64", "shim", "mmx64.efi"),
	}

	for _, path := range mokPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// findSignedGrubEFI looks for the signed grubx64.efi binary in the container image
// This is essential for Secure Boot - shim will only chain-load a properly signed GRUB
func findSignedGrubEFI(targetDir string) string {
	// Common locations for signed GRUB EFI binary
	grubPaths := []string{
		// Fedora/RHEL/CentOS locations
		filepath.Join(targetDir, "boot", "efi", "EFI", "fedora", "grubx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "centos", "grubx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "redhat", "grubx64.efi"),
		// From grub2-efi-x64 package
		filepath.Join(targetDir, "usr", "lib", "grub", "x86_64-efi-signed", "grubx64.efi.signed"),
		filepath.Join(targetDir, "usr", "lib64", "grub", "x86_64-efi-signed", "grubx64.efi.signed"),
		// Debian/Ubuntu locations
		filepath.Join(targetDir, "usr", "lib", "grub", "x86_64-efi-signed", "grubx64.efi"),
		filepath.Join(targetDir, "usr", "share", "grub", "x86_64-efi-signed", "grubx64.efi"),
	}

	for _, path := range grubPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// findSignedSystemdBootEFI looks for a signed systemd-boot binary in the container
// On Debian/Ubuntu, systemd-boot is signed and can be chain-loaded via shim's fallback
func findSignedSystemdBootEFI(targetDir string) string {
	paths := []string{
		// Debian/Ubuntu signed systemd-boot
		filepath.Join(targetDir, "usr", "lib", "systemd", "boot", "efi", "systemd-bootx64.efi.signed"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "systemd", "systemd-bootx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "debian", "systemd-bootx64.efi"),
		filepath.Join(targetDir, "boot", "efi", "EFI", "ubuntu", "systemd-bootx64.efi"),
		// Fedora locations (though Fedora typically uses GRUB)
		filepath.Join(targetDir, "usr", "lib64", "systemd", "boot", "efi", "systemd-bootx64.efi.signed"),
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// setupSecureBootChain sets up the Secure Boot chain with shim
// Returns true if secure boot chain was set up, false if shim not available
//
// For GRUB2: shimx64.efi → grubx64.efi (signed)
// For systemd-boot: shimx64.efi → grubx64.efi (actually signed systemd-boot)
func (b *BootloaderInstaller) setupSecureBootChain(bootloaderEFI string) (bool, error) {
	shimPath := findShimEFI(b.TargetDir)
	if shimPath == "" {
		return false, nil // No shim available, will use direct boot
	}

	espPath := filepath.Join(b.TargetDir, "boot")
	efiBootDir := filepath.Join(espPath, "EFI", "BOOT")

	if err := os.MkdirAll(efiBootDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create EFI/BOOT directory: %w", err)
	}

	// For systemd-boot, use the fallback mechanism
	if b.Type == BootloaderSystemdBoot {
		return b.setupSystemdBootSecureBootChain(shimPath, efiBootDir)
	}

	// For GRUB2, find the signed grubx64.efi from the container image
	// We must use the signed binary, not the output from grub-install
	signedGrubPath := findSignedGrubEFI(b.TargetDir)
	if signedGrubPath == "" {
		fmt.Println("  Warning: No signed grubx64.efi found in container image")
		fmt.Println("  Secure Boot may fail - using unsigned GRUB from grub-install")
		// Fall back to the provided bootloaderEFI (likely unsigned)
		signedGrubPath = bootloaderEFI
	} else {
		fmt.Printf("  Found signed GRUB: %s\n", signedGrubPath)
	}

	fmt.Println("  Setting up Secure Boot chain with shim...")

	// Copy shim as BOOTX64.EFI (the UEFI default bootloader path)
	shimDest := filepath.Join(efiBootDir, "BOOTX64.EFI")
	if err := copyEFIFile(shimPath, shimDest); err != nil {
		return false, fmt.Errorf("failed to copy shim to BOOTX64.EFI: %w", err)
	}
	fmt.Printf("  Installed shim as BOOTX64.EFI (Secure Boot entry point)\n")

	// Copy the signed grubx64.efi (what shim expects to chain-load)
	// Shim is compiled to look for grubx64.efi in the same directory
	bootloaderDest := filepath.Join(efiBootDir, "grubx64.efi")
	if err := copyEFIFile(signedGrubPath, bootloaderDest); err != nil {
		return false, fmt.Errorf("failed to copy signed grubx64.efi: %w", err)
	}
	fmt.Printf("  Installed signed grubx64.efi (chain-loaded by shim)\n")

	// Copy MOK manager if available (for key enrollment)
	mokPath := findMokManager(b.TargetDir)
	if mokPath != "" {
		mokDest := filepath.Join(efiBootDir, "mmx64.efi")
		if err := copyEFIFile(mokPath, mokDest); err != nil {
			// MOK manager is optional, just warn
			fmt.Printf("  Warning: failed to copy MOK manager: %v\n", err)
		} else {
			fmt.Println("  Installed MOK manager (mmx64.efi)")
		}
	}

	// Also copy fbx64.efi (fallback) if available
	fbPaths := []string{
		filepath.Join(b.TargetDir, "boot", "efi", "EFI", "fedora", "fbx64.efi"),
		filepath.Join(b.TargetDir, "boot", "efi", "EFI", "BOOT", "fbx64.efi"),
		filepath.Join(b.TargetDir, "usr", "lib", "shim", "fbx64.efi"),
		filepath.Join(b.TargetDir, "usr", "lib64", "shim", "fbx64.efi"),
	}
	for _, fbPath := range fbPaths {
		if _, err := os.Stat(fbPath); err == nil {
			fbDest := filepath.Join(efiBootDir, "fbx64.efi")
			if err := copyEFIFile(fbPath, fbDest); err == nil {
				fmt.Println("  Installed fallback bootloader (fbx64.efi)")
			}
			break
		}
	}

	return true, nil
}

// setupSystemdBootSecureBootChain sets up Secure Boot for systemd-boot
// We use shim as BOOTX64.EFI and copy the signed systemd-boot as grubx64.efi.
// Shim is compiled to look for grubx64.efi in the same directory, and it verifies
// the signature using the distro key embedded in shim. Since systemd-boot is signed
// by the same distro key (e.g., Debian), shim will trust and load it.
//
// IMPORTANT: We do NOT install fbx64.efi (fallback bootloader) because it causes
// a "Restore Boot Option" blue screen when it can't find BOOTX64.CSV in the
// expected distro-specific location.
//
// Boot chain: shimx64.efi (BOOTX64.EFI) → grubx64.efi (actually signed systemd-boot)
func (b *BootloaderInstaller) setupSystemdBootSecureBootChain(shimPath, efiBootDir string) (bool, error) {
	// Find signed systemd-boot
	signedSystemdBoot := findSignedSystemdBootEFI(b.TargetDir)
	if signedSystemdBoot == "" {
		fmt.Println("  Warning: No signed systemd-boot found in container image")
		fmt.Println("  Secure Boot may fail with systemd-boot")
		return false, nil
	}

	fmt.Printf("  Found signed systemd-boot: %s\n", signedSystemdBoot)
	fmt.Println("  Setting up Secure Boot chain for systemd-boot...")

	// Copy shim as BOOTX64.EFI (the UEFI default bootloader path)
	shimDest := filepath.Join(efiBootDir, "BOOTX64.EFI")
	if err := copyEFIFile(shimPath, shimDest); err != nil {
		return false, fmt.Errorf("failed to copy shim to BOOTX64.EFI: %w", err)
	}
	fmt.Printf("  Installed shim as BOOTX64.EFI (Secure Boot entry point)\n")

	// Copy signed systemd-boot as grubx64.efi - shim will load it
	// Shim is compiled to look for grubx64.efi, but it only verifies the signature.
	// Since systemd-boot is signed by the same distro key that shim trusts,
	// shim will load it successfully.
	bootloaderDest := filepath.Join(efiBootDir, "grubx64.efi")
	if err := copyEFIFile(signedSystemdBoot, bootloaderDest); err != nil {
		return false, fmt.Errorf("failed to copy signed systemd-boot as grubx64.efi: %w", err)
	}
	fmt.Printf("  Installed signed systemd-boot as grubx64.efi (chain-loaded by shim)\n")

	// Copy MOK manager if available (for key enrollment if needed)
	mokPath := findMokManager(b.TargetDir)
	if mokPath != "" {
		mokDest := filepath.Join(efiBootDir, "mmx64.efi")
		if err := copyEFIFile(mokPath, mokDest); err == nil {
			fmt.Println("  Installed MOK manager (mmx64.efi)")
		}
	}

	// NOTE: We intentionally do NOT copy fbx64.efi (fallback bootloader)
	// fbx64.efi looks for EFI/<distro>/BOOTX64.CSV to restore boot entries,
	// but our setup uses EFI/BOOT/ directly, causing fbx64.efi to fail with
	// a "Restore Boot Option" blue screen.

	// Also copy systemd-boot to EFI/systemd/ for discoverability by bootctl
	espPath := filepath.Join(b.TargetDir, "boot")
	efiSystemdDir := filepath.Join(espPath, "EFI", "systemd")
	if err := os.MkdirAll(efiSystemdDir, 0755); err == nil {
		systemdBootDest := filepath.Join(efiSystemdDir, "systemd-bootx64.efi")
		_ = copyEFIFile(signedSystemdBoot, systemdBootDest)
	}

	return true, nil
}

// DetectBootloader detects which bootloader should be used based on the container
func DetectBootloader(targetDir string) BootloaderType {
	// Check if systemd-boot is preferred (presence of bootctl in container)
	if _, err := os.Stat(filepath.Join(targetDir, "usr", "bin", "bootctl")); err == nil {
		return BootloaderSystemdBoot
	}

	// Default to GRUB2
	return BootloaderGRUB2
}

// registerEFIBootEntry uses efibootmgr to register a boot entry in UEFI firmware
// This ensures the system is bootable even if the firmware doesn't auto-detect the bootloader
func (b *BootloaderInstaller) registerEFIBootEntry() error {
	// Check if efibootmgr is available
	efibootmgrPath, err := exec.LookPath("efibootmgr")
	if err != nil {
		fmt.Println("  efibootmgr not found, skipping EFI boot entry registration")
		return nil
	}

	// Check if we're running on an EFI system (efivars must be accessible)
	if _, err := os.Stat("/sys/firmware/efi/efivars"); os.IsNotExist(err) {
		fmt.Println("  Not running on EFI system, skipping boot entry registration")
		return nil
	}

	// Get the ESP partition device
	espPartition := b.Scheme.BootPartition
	if espPartition == "" {
		return fmt.Errorf("ESP partition not set in scheme")
	}

	// Parse device and partition number from the ESP partition path
	// e.g., /dev/sda1 -> disk=/dev/sda, part=1
	// e.g., /dev/nvme0n1p1 -> disk=/dev/nvme0n1, part=1
	disk, partNum, err := parsePartitionDevice(espPartition)
	if err != nil {
		return fmt.Errorf("failed to parse ESP partition device: %w", err)
	}

	// Determine the EFI bootloader path (relative to ESP root, using backslashes)
	var efiPath string
	switch b.Type {
	case BootloaderGRUB2:
		efiPath = "\\EFI\\BOOT\\BOOTX64.EFI"
	case BootloaderSystemdBoot:
		efiPath = "\\EFI\\BOOT\\BOOTX64.EFI"
	}

	// Create the boot entry
	// Use the OS name as the label
	label := b.OSName
	if label == "" {
		label = "Linux"
	}

	fmt.Printf("  Registering EFI boot entry: %s\n", label)

	args := []string{
		"--create",
		"--disk", disk,
		"--part", partNum,
		"--loader", efiPath,
		"--label", label,
	}

	if b.Verbose {
		args = append(args, "--verbose")
	}

	cmd := exec.Command(efibootmgrPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("efibootmgr failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("  Registered EFI boot entry successfully\n")
	return nil
}

// parsePartitionDevice parses a partition device path into disk and partition number
// Handles both traditional naming (/dev/sda1) and NVMe naming (/dev/nvme0n1p1)
func parsePartitionDevice(partition string) (disk string, partNum string, err error) {
	// Handle NVMe devices: /dev/nvme0n1p1 -> /dev/nvme0n1, 1
	if strings.Contains(partition, "nvme") || strings.Contains(partition, "mmcblk") {
		// Find the last 'p' followed by digits
		for i := len(partition) - 1; i >= 0; i-- {
			if partition[i] == 'p' && i < len(partition)-1 {
				// Check if everything after 'p' is digits
				suffix := partition[i+1:]
				isNum := true
				for _, c := range suffix {
					if c < '0' || c > '9' {
						isNum = false
						break
					}
				}
				if isNum {
					return partition[:i], suffix, nil
				}
			}
		}
		return "", "", fmt.Errorf("cannot parse NVMe/MMC partition: %s", partition)
	}

	// Handle traditional devices: /dev/sda1 -> /dev/sda, 1
	// Find where the partition number starts (first digit at the end)
	for i := len(partition) - 1; i >= 0; i-- {
		if partition[i] < '0' || partition[i] > '9' {
			if i == len(partition)-1 {
				return "", "", fmt.Errorf("no partition number found: %s", partition)
			}
			return partition[:i+1], partition[i+1:], nil
		}
	}

	return "", "", fmt.Errorf("cannot parse partition device: %s", partition)
}
