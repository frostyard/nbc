package pkg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SetRootPasswordInTarget sets the root password in the installed system using chpasswd
// The password is passed via stdin for security (not visible in process list)
func SetRootPasswordInTarget(ctx context.Context, targetDir, password string, dryRun bool, progress Reporter) error {
	if password == "" {
		return nil // No password to set
	}

	if dryRun {
		progress.Message("[DRY RUN] Would set root password")
		return nil
	}

	progress.Message("Setting root password...")

	// Use chpasswd with -R flag to handle chroot internally
	// Password is passed via stdin for security
	cmd := exec.CommandContext(ctx, "chpasswd", "-R", targetDir)
	cmd.Stdin = strings.NewReader(fmt.Sprintf("root:%s\n", password))
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set root password: %w", err)
	}

	progress.Message("Root password set successfully")
	return nil
}
