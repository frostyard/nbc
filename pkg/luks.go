package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LUKSConfig holds encryption configuration
type LUKSConfig struct {
	Enabled    bool
	Passphrase string // Passphrase for LUKS (mutually exclusive with Keyfile)
	Keyfile    string // Path to keyfile containing passphrase (mutually exclusive with Passphrase)
	TPM2       bool
}

// LUKSDevice represents an opened LUKS container
type LUKSDevice struct {
	Partition  string // Raw partition (e.g., /dev/sda2)
	MapperName string // Device mapper name (e.g., root1)
	MapperPath string // Full path (e.g., /dev/mapper/root1)
	LUKSUUID   string // LUKS container UUID
}

// CreateLUKSContainer creates a LUKS2 container on the given partition
func CreateLUKSContainer(partition, passphrase string) error {
	fmt.Printf("  Creating LUKS container on %s...\n", partition)

	// Create LUKS2 container with passphrase via stdin
	cmd := exec.Command("cryptsetup", "luksFormat",
		"--type", "luks2",
		"--batch-mode",
		"--key-file", "-",
		partition,
	)
	cmd.Stdin = strings.NewReader(passphrase)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create LUKS container on %s: %w", partition, err)
	}

	return nil
}

// OpenLUKS opens a LUKS container and returns the device info
func OpenLUKS(partition, mapperName, passphrase string) (*LUKSDevice, error) {
	fmt.Printf("  Opening LUKS container %s as %s...\n", partition, mapperName)

	// Open the LUKS container
	cmd := exec.Command("cryptsetup", "luksOpen",
		"--key-file", "-",
		partition,
		mapperName,
	)
	cmd.Stdin = strings.NewReader(passphrase)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to open LUKS container %s: %w", partition, err)
	}

	// Get LUKS UUID
	luksUUID, err := GetLUKSUUID(partition)
	if err != nil {
		// Try to close the container before returning error
		_ = CloseLUKS(mapperName)
		return nil, err
	}

	return &LUKSDevice{
		Partition:  partition,
		MapperName: mapperName,
		MapperPath: filepath.Join("/dev/mapper", mapperName),
		LUKSUUID:   luksUUID,
	}, nil
}

// CloseLUKS closes a LUKS container
func CloseLUKS(mapperName string) error {
	fmt.Printf("  Closing LUKS container %s...\n", mapperName)

	cmd := exec.Command("cryptsetup", "luksClose", mapperName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close LUKS container %s: %w", mapperName, err)
	}

	return nil
}

// GetLUKSUUID retrieves the LUKS container UUID (not filesystem UUID)
func GetLUKSUUID(partition string) (string, error) {
	cmd := exec.Command("cryptsetup", "luksUUID", partition)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get LUKS UUID for %s: %w", partition, err)
	}

	uuid := strings.TrimSpace(string(output))
	if uuid == "" {
		return "", fmt.Errorf("empty LUKS UUID for %s", partition)
	}

	return uuid, nil
}

// EnrollTPM2 enrolls a TPM2 key for automatic unlock with no PCRs
func EnrollTPM2(partition string, config *LUKSConfig) error {
	fmt.Printf("  Enrolling TPM2 key on %s (no PCRs)...\n", partition)

	var keyFilePath string
	var cleanup func()

	if config.Keyfile != "" {
		// Use the provided keyfile directly
		keyFilePath = config.Keyfile
		cleanup = func() {} // No cleanup needed
	} else {
		// Write passphrase to a temporary file (systemd-cryptenroll doesn't reliably read from stdin)
		keyFile, err := os.CreateTemp("", "luks-key-*")
		if err != nil {
			return fmt.Errorf("failed to create temporary key file: %w", err)
		}
		keyFilePath = keyFile.Name()
		cleanup = func() { _ = os.Remove(keyFilePath) }

		if _, err := keyFile.WriteString(config.Passphrase); err != nil {
			_ = keyFile.Close()
			cleanup()
			return fmt.Errorf("failed to write to temporary key file: %w", err)
		}
		if err := keyFile.Close(); err != nil {
			cleanup()
			return fmt.Errorf("failed to close temporary key file: %w", err)
		}
	}
	defer cleanup()

	// Use systemd-cryptenroll to add TPM2 key with no PCR binding
	cmd := exec.Command("systemd-cryptenroll",
		"--unlock-key-file="+keyFilePath,
		"--tpm2-device=auto",
		"--tpm2-pcrs=", // Empty PCRs = no binding
		partition,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to enroll TPM2 on %s: %w", partition, err)
	}

	return nil
}

// ValidateInitramfsSupport checks if the extracted container has LUKS/TPM2 support
// Returns warnings (not errors) since initramfs contents vary by distro
func ValidateInitramfsSupport(targetDir string, tpm2Enabled bool) []string {
	var warnings []string

	// Check for dracut LUKS module
	dracutCrypt := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "90crypt")
	// Check for initramfs-tools LUKS hook (Debian/Ubuntu)
	initramfsCrypt := filepath.Join(targetDir, "usr", "share", "initramfs-tools", "hooks", "cryptroot")

	hasCryptSupport := false
	if _, err := os.Stat(dracutCrypt); err == nil {
		hasCryptSupport = true
	}
	if _, err := os.Stat(initramfsCrypt); err == nil {
		hasCryptSupport = true
	}

	if !hasCryptSupport {
		warnings = append(warnings,
			"Could not detect LUKS initramfs support. Ensure the container image includes "+
				"'cryptsetup-initramfs' (Debian/Ubuntu) or dracut 'crypt' module (Fedora/RHEL).")
	}

	if tpm2Enabled {
		// Check for TPM2 support in initramfs
		dracutTPM2 := filepath.Join(targetDir, "usr", "lib", "dracut", "modules.d", "91tpm2-tss")
		hasTPM2Support := false

		if _, err := os.Stat(dracutTPM2); err == nil {
			hasTPM2Support = true
		}

		// Check for TPM2 TCTI library (needed by initramfs)
		tpm2Patterns := []string{
			filepath.Join(targetDir, "usr", "lib", "*", "libtss2-tcti-device.so*"),
			filepath.Join(targetDir, "usr", "lib64", "libtss2-tcti-device.so*"),
		}
		for _, pattern := range tpm2Patterns {
			matches, _ := filepath.Glob(pattern)
			if len(matches) > 0 {
				hasTPM2Support = true
				break
			}
		}

		if !hasTPM2Support {
			warnings = append(warnings,
				"Could not detect TPM2 initramfs support. Ensure the container image includes "+
					"'libtss2-tcti-device0' (Debian/Ubuntu) or 'tpm2-tss' with dracut module (Fedora/RHEL).")
		}
	}

	return warnings
}

// GenerateCrypttab generates /etc/crypttab entries for the LUKS devices
func GenerateCrypttab(devices []*LUKSDevice, tpm2Enabled bool) string {
	var lines []string
	lines = append(lines, "# /etc/crypttab - LUKS encrypted devices")
	lines = append(lines, "# Generated by nbc installer")
	lines = append(lines, "#")
	lines = append(lines, "# <name> <device> <keyfile> <options>")
	lines = append(lines, "")

	options := "luks"
	if tpm2Enabled {
		options = "luks,tpm2-device=auto"
	}

	for _, dev := range devices {
		// Format: name UUID=<luks-uuid> none <options>
		line := fmt.Sprintf("%s UUID=%s none %s", dev.MapperName, dev.LUKSUUID, options)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n") + "\n"
}
