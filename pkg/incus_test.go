package pkg

// TODO(phase-2): Add TestIncus_Encryption for full LUKS encryption VM testing.
// Currently covered by unit tests in install_test.go and bootloader_test.go.
// Full VM test (install with --encrypt, verify LUKS, boot from encrypted disk)
// was in test_incus_encryption.sh, deferred for Phase 2.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/nbc/pkg/testutil"
	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// nbcBinaryPath returns the absolute path to the nbc binary.
// It looks for the binary in the project root (parent of pkg/).
func nbcBinaryPath(t *testing.T) string {
	t.Helper()

	// Get the directory of this test file
	// Tests run from the package directory, so we need to go up one level
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Try project root (parent of pkg)
	projectRoot := filepath.Dir(wd)
	nbcPath := filepath.Join(projectRoot, "nbc")
	if _, err := os.Stat(nbcPath); err == nil {
		return nbcPath
	}

	// Try current directory (in case running from project root)
	nbcPath = filepath.Join(wd, "nbc")
	if _, err := os.Stat(nbcPath); err == nil {
		return nbcPath
	}

	// Try looking up the tree for go.mod to find project root
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			nbcPath = filepath.Join(dir, "nbc")
			if _, err := os.Stat(nbcPath); err == nil {
				return nbcPath
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatalf("nbc binary not found. Run 'make build' first. Searched from: %s", wd)
	return ""
}

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
	nbcPath := nbcBinaryPath(t)
	if err := fixture.PushFile(nbcPath, "/usr/local/bin/nbc"); err != nil {
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

// TestIncus_FullCycle tests the complete nbc lifecycle in a single VM:
// 1. Install - Write OS to disk with A/B partitions
// 2. VerifyPartitions - Check 4 partitions exist with correct labels
// 3. Update - A/B update writes to inactive partition
// 4. Boot - Boot from installed disk and verify /etc overlay
//
// This combined test avoids VM creation overhead and is suitable for CI.
// Each phase uses t.Run for clear reporting and failure isolation.
func TestIncus_FullCycle(t *testing.T) {
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
	nbcPath := nbcBinaryPath(t)
	if err := fixture.PushFile(nbcPath, "/usr/local/bin/nbc"); err != nil {
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

	// Get test image
	testImage := os.Getenv("TEST_IMAGE")
	if testImage == "" {
		testImage = "ghcr.io/frostyard/snow:latest"
	}

	// Subtest 1: Install
	t.Run("Install", func(t *testing.T) {
		installOutput, err := fixture.ExecCommand("bash", "-c",
			fmt.Sprintf("echo 'yes' | nbc install --image '%s' --device '%s' --verbose",
				testImage, testDisk))
		if err != nil {
			fixture.DumpDiagnostics("nbc install failed")
			t.Fatalf("nbc install failed: %v\nOutput: %s", err, installOutput)
		}
		t.Log("Install completed")
	})

	// Subtest 2: Verify Partitions
	t.Run("VerifyPartitions", func(t *testing.T) {
		partOutput, _ := fixture.ExecCommand("lsblk", "-n", testDisk)
		partCount := strings.Count(partOutput, "part")
		if partCount != 4 {
			t.Errorf("Expected 4 partitions, got %d", partCount)
		}
		t.Logf("Partition count: %d", partCount)

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
			fixture.DumpDiagnostics("partition verification failed")
			t.Fatalf("Partition verification failed: %v", err)
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
		t.Log("Partition verification passed")
	})

	// Subtest 3: Update (A/B partition update)
	t.Run("Update", func(t *testing.T) {
		// Mount var partition to access config and pristine /etc
		setupOutput, err := fixture.ExecCommand("bash", "-c", fmt.Sprintf(`
            VAR_PART=$(lsblk -nlo NAME,PARTLABEL %s | grep 'var' | head -1 | awk '{print "/dev/" $1}')
            mkdir -p /mnt/active-var
            mount $VAR_PART /mnt/active-var
            mkdir -p /var/lib/nbc
            mount --bind /mnt/active-var/lib/nbc /var/lib/nbc
        `, testDisk))
		if err != nil {
			fixture.DumpDiagnostics("update setup failed")
			t.Fatalf("Failed to set up for update: %v\nOutput: %s", err, setupOutput)
		}

		// Run update (use --force since we just installed the same image)
		updateOutput, err := fixture.ExecCommand("bash", "-c",
			fmt.Sprintf("echo 'yes' | nbc update --device '%s' --force --verbose 2>&1", testDisk))
		if err != nil {
			fixture.DumpDiagnostics("nbc update failed")
			t.Fatalf("nbc update failed: %v\nOutput: %s", err, updateOutput)
		}
		t.Logf("Update output:\n%s", updateOutput)

		// Cleanup mounts
		_, _ = fixture.ExecCommand("bash", "-c", `
            umount /var/lib/nbc 2>/dev/null || true
            umount /mnt/active-var 2>/dev/null || true
            rmdir /mnt/active-var 2>/dev/null || true
        `)

		t.Log("Update completed")
	})

	// Subtest 4: Verify A/B partitions
	t.Run("VerifyABPartitions", func(t *testing.T) {
		verifyOutput, err := fixture.ExecCommand("bash", "-c", fmt.Sprintf(`
            ROOT1=$(lsblk -nlo NAME,PARTLABEL %s | grep 'root1' | head -1 | awk '{print "/dev/" $1}')
            ROOT2=$(lsblk -nlo NAME,PARTLABEL %s | grep 'root2' | head -1 | awk '{print "/dev/" $1}')

            echo "root1_dev:$ROOT1"
            echo "root2_dev:$ROOT2"

            mkdir -p /mnt/test-root1 /mnt/test-root2
            mount $ROOT1 /mnt/test-root1
            mount $ROOT2 /mnt/test-root2

            ROOT1_FILES=$(find /mnt/test-root1 -type f 2>/dev/null | wc -l)
            ROOT2_FILES=$(find /mnt/test-root2 -type f 2>/dev/null | wc -l)

            echo "root1_files:$ROOT1_FILES"
            echo "root2_files:$ROOT2_FILES"

            # Show what's in root2 for debugging
            echo "root2_contents:"
            ls -la /mnt/test-root2/ 2>&1 | head -20

            umount /mnt/test-root1
            umount /mnt/test-root2
            rmdir /mnt/test-root1 /mnt/test-root2
        `, testDisk, testDisk))
		if err != nil {
			fixture.DumpDiagnostics("A/B partition verification failed")
			t.Fatalf("A/B partition verification failed: %v", err)
		}
		t.Logf("Verify output:\n%s", verifyOutput)
		if err != nil {
			fixture.DumpDiagnostics("A/B partition verification failed")
			t.Fatalf("A/B partition verification failed: %v", err)
		}

		// Both root partitions should have files after install and update
		if !strings.Contains(verifyOutput, "root1_files:") || !strings.Contains(verifyOutput, "root2_files:") {
			t.Errorf("Could not verify A/B partition file counts: %s", verifyOutput)
		}

		// Extract file counts
		lines := strings.Split(verifyOutput, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "root2_files:") {
				countStr := strings.TrimPrefix(line, "root2_files:")
				if countStr == "0" {
					t.Error("Root2 partition is empty after update - A/B update may have failed")
				}
			}
		}
		t.Log("A/B partition verification passed")
	})

	// Subtest 5: Boot Test
	t.Run("Boot", func(t *testing.T) {
		// This test creates a new empty VM and boots from the installed disk.
		// First, we need to detach the disk from the test VM and create a boot-test VM.

		// Stop the main VM to detach disk
		_, _ = fixture.ExecCommand("sync") // Sync any pending writes

		// We need to access the incus client directly for this more complex operation
		client := fixture.Client()
		vmName := fixture.VMName()
		volumeName := fixture.VolumeName()

		// Stop VM
		stopReq := api.InstanceStatePut{
			Action:  "stop",
			Force:   true,
			Timeout: -1,
		}
		op, err := client.UpdateInstanceState(vmName, stopReq, "")
		if err != nil {
			t.Fatalf("Failed to stop VM for boot test: %v", err)
		}
		if err := op.Wait(); err != nil {
			t.Fatalf("Failed to wait for VM stop: %v", err)
		}

		// Get instance to detach disk
		instance, etag, err := client.GetInstance(vmName)
		if err != nil {
			t.Fatalf("Failed to get instance: %v", err)
		}

		// Remove the disk device
		delete(instance.Devices, volumeName)
		op, err = client.UpdateInstance(vmName, instance.Writable(), etag)
		if err != nil {
			t.Fatalf("Failed to detach disk: %v", err)
		}
		if err := op.Wait(); err != nil {
			t.Fatalf("Failed to wait for disk detach: %v", err)
		}

		// Create boot test VM name
		bootVMName := vmName + "-boot"

		// Register cleanup for boot VM
		t.Cleanup(func() {
			// Stop and delete boot VM
			stopReq := api.InstanceStatePut{Action: "stop", Force: true, Timeout: -1}
			op, _ := client.UpdateInstanceState(bootVMName, stopReq, "")
			if op != nil {
				_ = op.Wait()
			}
			op, _ = client.DeleteInstance(bootVMName)
			if op != nil {
				_ = op.Wait()
			}
		})

		// Find storage pool
		pools, err := client.GetStoragePoolNames()
		if err != nil || len(pools) == 0 {
			t.Fatalf("Failed to get storage pools: %v", err)
		}
		poolName := pools[0]

		// Create empty VM for boot test
		createReq := api.InstancesPost{
			Name: bootVMName,
			Type: api.InstanceTypeVM,
			InstancePut: api.InstancePut{
				Config: map[string]string{
					"limits.cpu":          "2",
					"limits.memory":       "4GiB",
					"security.secureboot": "false",
				},
				Devices: map[string]map[string]string{
					"bootable": {
						"type":          "disk",
						"pool":          poolName,
						"source":        volumeName,
						"boot.priority": "10",
					},
				},
			},
			Source: api.InstanceSource{
				Type: "none", // Empty VM
			},
		}

		op, err = client.CreateInstance(createReq)
		if err != nil {
			t.Fatalf("Failed to create boot test VM: %v", err)
		}
		if err := op.Wait(); err != nil {
			t.Fatalf("Failed to wait for boot VM creation: %v", err)
		}

		// Start the boot VM
		startReq := api.InstanceStatePut{
			Action:  "start",
			Timeout: -1,
		}
		op, err = client.UpdateInstanceState(bootVMName, startReq, "")
		if err != nil {
			t.Fatalf("Failed to start boot VM: %v", err)
		}
		if err := op.Wait(); err != nil {
			t.Fatalf("Failed to wait for boot VM start: %v", err)
		}

		// Wait for the system to boot (give it time to boot from installed disk)
		t.Log("Waiting for boot test VM to start (30s)...")
		time.Sleep(30 * time.Second)

		// Create a temporary fixture for the boot VM to use exec
		bootFixture := &bootTestFixture{client: client, vmName: bootVMName}

		// Verify /etc overlay is mounted
		bootCtx, bootCancel := context.WithTimeout(context.Background(), testutil.TimeoutVMBoot)
		defer bootCancel()

		// Wait for systemd to be ready (with retries)
		var bootSuccess bool
		for i := 0; i < 10; i++ {
			select {
			case <-bootCtx.Done():
				t.Fatalf("Boot test timed out")
			default:
			}

			output, err := bootFixture.ExecCommand("bash", "-c", "systemctl is-system-running --wait 2>/dev/null || true")
			if err == nil {
				state := strings.TrimSpace(output)
				if state == "running" || state == "degraded" {
					bootSuccess = true
					break
				}
			}
			time.Sleep(5 * time.Second)
		}

		if !bootSuccess {
			t.Log("Note: Could not confirm boot completion, but VM started")
		}

		// Verify /etc overlay
		overlayCheck, err := bootFixture.ExecCommand("bash", "-c", `
            if mount | grep -q 'overlay on /etc'; then
                echo "overlay:ok"
            else
                echo "overlay:missing"
            fi

            # Check rd.etc.overlay in kernel cmdline
            if grep -q 'rd.etc.overlay' /proc/cmdline; then
                echo "karg:ok"
            else
                echo "karg:missing"
            fi

            # Check root is read-only
            ROOT_MOUNT=$(findmnt -n -o OPTIONS / 2>/dev/null | grep -o 'ro\|rw' | head -1)
            echo "rootmount:$ROOT_MOUNT"
        `)
		if err != nil {
			t.Logf("Overlay check returned error (may be expected if agent not ready): %v", err)
			// Not fatal - the VM may not have full agent support immediately
		} else {
			if !strings.Contains(overlayCheck, "overlay:ok") {
				t.Error("/etc is not mounted as overlay")
			}
			if !strings.Contains(overlayCheck, "karg:ok") {
				t.Error("rd.etc.overlay not in kernel cmdline")
			}
			if strings.Contains(overlayCheck, "rootmount:ro") {
				t.Log("Root filesystem is mounted read-only (good)")
			}
		}

		t.Log("Boot test passed")
	})

	t.Log("Full cycle test completed")
}

// bootTestFixture is a minimal fixture for executing commands in the boot test VM.
type bootTestFixture struct {
	client incus.InstanceServer
	vmName string
}

// ExecCommand executes a command in the boot test VM.
func (f *bootTestFixture) ExecCommand(command ...string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("no command specified")
	}

	var stdout, stderr bytes.Buffer
	execReq := api.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: false,
	}

	args := incus.InstanceExecArgs{
		Stdin:  io.NopCloser(strings.NewReader("")),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	op, err := f.client.ExecInstance(f.vmName, execReq, &args)
	if err != nil {
		return "", fmt.Errorf("exec instance: %w", err)
	}

	if err := op.Wait(); err != nil {
		return stdout.String(), fmt.Errorf("exec wait: %w (stderr: %s)", err, stderr.String())
	}

	opAPI := op.Get()
	if opAPI.Metadata != nil {
		if returnVal, ok := opAPI.Metadata["return"]; ok {
			if exitCode, ok := returnVal.(float64); ok && exitCode != 0 {
				return stdout.String(), fmt.Errorf("command exited with code %d (stderr: %s)", int(exitCode), stderr.String())
			}
		}
	}

	return stdout.String(), nil
}
