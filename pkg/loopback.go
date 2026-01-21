package pkg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// MinLoopbackSizeGB is the minimum size for a loopback image (35GB)
	// This accounts for: 2GB boot + 2x12GB roots + var space
	MinLoopbackSizeGB = 35

	// DefaultLoopbackSizeGB is the default size when not specified
	DefaultLoopbackSizeGB = 35
)

// LoopbackDevice represents an attached loopback device
type LoopbackDevice struct {
	ImagePath string // Path to the image file
	Device    string // Loop device path (e.g., /dev/loop0)
	SizeGB    int    // Size of the image in GB
}

// CreateLoopbackFile creates a sparse image file of the specified size.
// If the file already exists, it returns an error unless force is true.
func CreateLoopbackFile(imagePath string, sizeGB int, force bool) error {
	// Resolve to absolute path
	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	imagePath = absPath

	// Check if file exists
	if _, err := os.Stat(imagePath); err == nil {
		if !force {
			return fmt.Errorf("image file %s already exists (use --force to overwrite)", imagePath)
		}
		// Remove existing file
		if err := os.Remove(imagePath); err != nil {
			return fmt.Errorf("failed to remove existing image file: %w", err)
		}
	}

	// Validate size
	if sizeGB < MinLoopbackSizeGB {
		return fmt.Errorf("image size %dGB is below minimum %dGB", sizeGB, MinLoopbackSizeGB)
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(imagePath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create sparse file using truncate
	sizeBytes := fmt.Sprintf("%dG", sizeGB)
	cmd := exec.Command("truncate", "-s", sizeBytes, imagePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create image file: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// AttachLoopback attaches an image file as a loopback device.
// Returns the loop device path (e.g., /dev/loop0).
func AttachLoopback(imagePath string) (string, error) {
	// Resolve to absolute path
	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	imagePath = absPath

	// Verify file exists
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("image file %s does not exist", imagePath)
	}

	// Attach using losetup with --partscan to detect partitions
	cmd := exec.Command("losetup", "--find", "--show", "--partscan", imagePath)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("losetup failed: %w\nStderr: %s", err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("losetup failed: %w", err)
	}

	device := strings.TrimSpace(string(output))
	if device == "" {
		return "", fmt.Errorf("losetup returned empty device path")
	}

	return device, nil
}

// DetachLoopback detaches a loopback device.
func DetachLoopback(device string) error {
	if device == "" {
		return nil
	}

	// Verify it's a loop device
	if !strings.HasPrefix(device, "/dev/loop") {
		return fmt.Errorf("not a loop device: %s", device)
	}

	cmd := exec.Command("losetup", "-d", device)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to detach loop device %s: %w\nOutput: %s", device, err, string(output))
	}

	return nil
}

// ParseSizeGB parses a size string and returns the size in GB.
// Accepts plain integers (assumed GB) or values with G/GB suffix.
// Enforces minimum size of MinLoopbackSizeGB.
func ParseSizeGB(sizeStr string) (int, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return DefaultLoopbackSizeGB, nil
	}

	// Remove common suffixes
	sizeStr = strings.ToUpper(sizeStr)
	sizeStr = strings.TrimSuffix(sizeStr, "GB")
	sizeStr = strings.TrimSuffix(sizeStr, "G")
	sizeStr = strings.TrimSpace(sizeStr)

	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		return 0, fmt.Errorf("invalid size value: %s", sizeStr)
	}

	if size < MinLoopbackSizeGB {
		return 0, fmt.Errorf("size %dGB is below minimum %dGB", size, MinLoopbackSizeGB)
	}

	return size, nil
}

// SetupLoopbackInstall creates a loopback image file and attaches it.
// Returns the LoopbackDevice for cleanup and the device path for installation.
func SetupLoopbackInstall(imagePath string, sizeGB int, force bool, progress *ProgressReporter) (*LoopbackDevice, error) {
	// Create the image file
	progress.MessagePlain("Creating loopback image: %s (%dGB)", imagePath, sizeGB)
	if err := CreateLoopbackFile(imagePath, sizeGB, force); err != nil {
		return nil, err
	}

	// Attach as loopback device
	progress.Message("Attaching loopback device...")
	device, err := AttachLoopback(imagePath)
	if err != nil {
		// Clean up the image file on failure
		_ = os.Remove(imagePath)
		return nil, err
	}

	progress.Message("Loopback device: %s", device)

	return &LoopbackDevice{
		ImagePath: imagePath,
		Device:    device,
		SizeGB:    sizeGB,
	}, nil
}

// Cleanup detaches the loopback device.
func (l *LoopbackDevice) Cleanup(progress *ProgressReporter) error {
	if l == nil || l.Device == "" {
		return nil
	}

	if progress != nil {
		progress.MessagePlain("Detaching loopback device %s...", l.Device)
	}
	return DetachLoopback(l.Device)
}
