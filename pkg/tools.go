package pkg

import (
	"fmt"
	"os/exec"
)

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
		"rsync",
	}

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found: %w", tool, err)
		}
	}

	return nil
}
