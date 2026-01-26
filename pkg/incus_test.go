package pkg

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/frostyard/nbc/pkg/testutil"
)

// TestMain provides clean slate for VM tests.
// It cleans up orphaned resources from previous runs before and after the test suite.
func TestMain(m *testing.M) {
	// Clean any orphaned resources from previous runs
	_ = testutil.CleanupAllNbcTestResources()
	code := m.Run()
	// Final cleanup
	_ = testutil.CleanupAllNbcTestResources()
	os.Exit(code)
}

// TestIncus_Install tests nbc installation in a VM.
// This is the primary integration test validating:
// - Partition creation (4 partitions)
// - Filesystem formatting
// - Container extraction
// - Bootloader installation
// - /etc overlay setup
func TestIncus_Install(t *testing.T) {
	testutil.RequireRoot(t)
	testutil.RequireTools(t, "incus")

	ctx, cancel := context.WithTimeout(context.Background(), testutil.TimeoutVM)
	defer cancel()

	// Create fixture (cleanup registered automatically)
	fixture := testutil.NewIncusFixture(t)

	// Create VM with Fedora image
	if err := fixture.CreateVM("images:fedora/42/cloud"); err != nil {
		t.Fatalf("Failed to create VM: %v", err)
	}

	// Wait for VM to be ready
	if err := fixture.WaitForReady(ctx); err != nil {
		fixture.DumpDiagnostics("VM boot failed")
		t.Fatalf("VM not ready: %v", err)
	}

	// Create baseline snapshot for potential reset
	if err := fixture.CreateBaselineSnapshot("baseline"); err != nil {
		t.Fatalf("Failed to create baseline snapshot: %v", err)
	}

	// Attach test disk (60GB)
	if err := fixture.AttachDisk("test-disk", "60GB"); err != nil {
		t.Fatalf("Failed to attach disk: %v", err)
	}

	// Install required tools
	_, err := fixture.ExecCommand("dnf", "install", "-y", "-q",
		"gdisk", "util-linux", "e2fsprogs", "dosfstools", "parted", "rsync", "btrfs-progs")
	if err != nil {
		t.Fatalf("Failed to install tools: %v", err)
	}

	// Build and push nbc binary
	// (assumes binary built externally via Makefile target)
	if err := fixture.PushFile("./nbc", "/usr/local/bin/nbc"); err != nil {
		t.Fatalf("Failed to push nbc: %v", err)
	}
	_, _ = fixture.ExecCommand("chmod", "+x", "/usr/local/bin/nbc")

	// Find test disk (unpartitioned disk)
	diskOutput, err := fixture.ExecCommand("bash", "-c", `
        for disk in $(lsblk -ndo NAME,TYPE | grep disk | awk '{print $1}'); do
            if ! lsblk -no NAME /dev/$disk | grep -q '[0-9]'; then
                echo "/dev/$disk"
                exit 0
            fi
        done
    `)
	if err != nil || strings.TrimSpace(diskOutput) == "" {
		t.Fatalf("Failed to find test disk: %v", err)
	}
	testDisk := strings.TrimSpace(diskOutput)
	t.Logf("Test disk: %s", testDisk)

	// Run nbc install
	testImage := os.Getenv("TEST_IMAGE")
	if testImage == "" {
		testImage = "ghcr.io/frostyard/snow:latest"
	}

	installOutput, err := fixture.ExecCommand("bash", "-c",
		fmt.Sprintf("echo 'yes' | nbc install --image '%s' --device '%s' --verbose",
			testImage, testDisk))
	if err != nil {
		fixture.DumpDiagnostics("nbc install failed")
		t.Fatalf("nbc install failed: %v\nOutput: %s", err, installOutput)
	}
	t.Logf("Install completed")

	// Verify partition count
	partOutput, _ := fixture.ExecCommand("lsblk", "-n", testDisk)
	partCount := strings.Count(partOutput, "part")
	if partCount != 4 {
		t.Errorf("Expected 4 partitions, got %d", partCount)
	}

	// Verify key files exist
	verifyOutput, err := fixture.ExecCommand("bash", "-c", fmt.Sprintf(`
        ROOT=$(lsblk -nlo NAME,PARTLABEL %s | grep root1 | awk '{print "/dev/"$1}')
        VAR=$(lsblk -nlo NAME,PARTLABEL %s | grep var | awk '{print "/dev/"$1}')
        mkdir -p /mnt/check /mnt/check-var
        mount $ROOT /mnt/check
        mount $VAR /mnt/check-var
        
        # Check config exists
        [ -f /mnt/check-var/lib/nbc/state/config.json ] && echo "config:ok" || echo "config:missing"
        
        # Check dracut module
        [ -d /mnt/check/usr/lib/dracut/modules.d/95etc-overlay ] && echo "dracut:ok" || echo "dracut:missing"
        
        # Check .etc.lower
        [ -d /mnt/check/.etc.lower ] && echo "etclower:ok" || echo "etclower:missing"
        
        umount /mnt/check-var
        umount /mnt/check
        rmdir /mnt/check /mnt/check-var
    `, testDisk, testDisk))
	if err != nil {
		fixture.DumpDiagnostics("verification failed")
		t.Fatalf("Verification failed: %v", err)
	}

	if !strings.Contains(verifyOutput, "config:ok") {
		t.Error("Config file not found in /var/lib/nbc/state/")
	}
	if !strings.Contains(verifyOutput, "dracut:ok") {
		t.Error("Dracut etc-overlay module not found")
	}
	if !strings.Contains(verifyOutput, "etclower:ok") {
		t.Error(".etc.lower directory not found")
	}

	t.Log("Install test passed")
}
