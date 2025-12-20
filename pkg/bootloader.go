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

	// Copy kernel and initramfs from /usr/lib/modules to /boot
	if err := b.copyKernelFromModules(); err != nil {
		return fmt.Errorf("failed to copy kernel from modules: %w", err)
	}

	switch b.Type {
	case BootloaderGRUB2:
		return b.installGRUB2()
	case BootloaderSystemdBoot:
		return b.installSystemdBoot()
	default:
		return fmt.Errorf("unsupported bootloader type: %s", b.Type)
	}
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

	// Get root UUID
	rootUUID, err := GetPartitionUUID(b.Scheme.Root1Partition)
	if err != nil {
		return fmt.Errorf("failed to get root UUID: %w", err)
	}

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

	// Get /var UUID for kernel command line mount
	varUUID, err := GetPartitionUUID(b.Scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

	// Get filesystem type (default to ext4 for backward compatibility)
	fsType := b.Scheme.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	// Build kernel command line
	kernelCmdline := []string{
		"root=UUID=" + rootUUID,
		"ro",
		"console=tty0",
		// Mount /var via kernel command line (systemd.mount-extra)
		"systemd.mount-extra=UUID=" + varUUID + ":/var:" + fsType + ":defaults",
	}
	kernelCmdline = append(kernelCmdline, b.KernelArgs...)

	// Create GRUB config
	grubCfg := fmt.Sprintf(`set timeout=5
set default=0

menuentry '%s' {
    linux /vmlinuz-%s %s
    initrd /%s
}
`, b.OSName, kernelVersion, strings.Join(kernelCmdline, " "), initrd)

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

// copyEFIFile copies a file from src to dst
func copyEFIFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()

	_, err = io.Copy(dest, source)
	return err
}

// generateSystemdBootConfig generates systemd-boot configuration
func (b *BootloaderInstaller) generateSystemdBootConfig() error {
	fmt.Println("  Generating systemd-boot configuration...")

	// Get root UUID
	rootUUID, err := GetPartitionUUID(b.Scheme.Root1Partition)
	if err != nil {
		return fmt.Errorf("failed to get root UUID: %w", err)
	}

	// Get /var UUID for kernel command line mount
	varUUID, err := GetPartitionUUID(b.Scheme.VarPartition)
	if err != nil {
		return fmt.Errorf("failed to get var UUID: %w", err)
	}

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

	// Get filesystem type (default to ext4 for backward compatibility)
	fsType := b.Scheme.FilesystemType
	if fsType == "" {
		fsType = "ext4"
	}

	// Build kernel command line
	kernelCmdline := []string{
		"root=UUID=" + rootUUID,
		"rw",
		// Mount /var via kernel command line (systemd.mount-extra)
		"systemd.mount-extra=UUID=" + varUUID + ":/var:" + fsType + ":defaults",
	}
	kernelCmdline = append(kernelCmdline, b.KernelArgs...)

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

// setupSecureBootChain sets up the Secure Boot chain with shim
// The chain is: BOOTX64.EFI (shim) → grubx64.efi/systemd-bootx64.efi
// Returns true if secure boot chain was set up, false if shim not available
func (b *BootloaderInstaller) setupSecureBootChain(bootloaderEFI string) (bool, error) {
	shimPath := findShimEFI(b.TargetDir)
	if shimPath == "" {
		return false, nil // No shim available, will use direct boot
	}

	fmt.Println("  Setting up Secure Boot chain with shim...")

	espPath := filepath.Join(b.TargetDir, "boot")
	efiBootDir := filepath.Join(espPath, "EFI", "BOOT")

	if err := os.MkdirAll(efiBootDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create EFI/BOOT directory: %w", err)
	}

	// Copy shim as BOOTX64.EFI (the UEFI default bootloader path)
	shimDest := filepath.Join(efiBootDir, "BOOTX64.EFI")
	if err := copyEFIFile(shimPath, shimDest); err != nil {
		return false, fmt.Errorf("failed to copy shim to BOOTX64.EFI: %w", err)
	}
	fmt.Printf("  Installed shim as BOOTX64.EFI (Secure Boot entry point)\n")

	// Copy the actual bootloader as grubx64.efi (what shim expects to chain-load)
	// Shim by default looks for grubx64.efi in the same directory
	bootloaderDest := filepath.Join(efiBootDir, "grubx64.efi")
	if err := copyEFIFile(bootloaderEFI, bootloaderDest); err != nil {
		return false, fmt.Errorf("failed to copy bootloader as grubx64.efi: %w", err)
	}
	fmt.Printf("  Installed bootloader as grubx64.efi (chain-loaded by shim)\n")

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

// DetectBootloader detects which bootloader should be used based on the container
func DetectBootloader(targetDir string) BootloaderType {
	// Check if systemd-boot is preferred (presence of bootctl in container)
	if _, err := os.Stat(filepath.Join(targetDir, "usr", "bin", "bootctl")); err == nil {
		return BootloaderSystemdBoot
	}

	// Default to GRUB2
	return BootloaderGRUB2
}
