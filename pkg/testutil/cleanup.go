// Package testutil provides test helpers and fixtures for nbc testing.
package testutil

import (
	"os/exec"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// CleanupOrphanedResources removes any orphaned nbc-test-* instances and volumes.
// This should be called at the start of test suites to ensure a clean slate.
// All cleanup errors are silently ignored per CONTEXT.md decisions.
//
// Usage:
//
//	func TestMain(m *testing.M) {
//	    testutil.CleanupAllNbcTestResources()
//	    code := m.Run()
//	    testutil.CleanupAllNbcTestResources()
//	    os.Exit(code)
//	}
func CleanupOrphanedResources(t *testing.T) {
	t.Helper()
	t.Log("Cleaning up orphaned nbc-test-* resources")

	if err := CleanupAllNbcTestResources(); err != nil {
		// Log but don't fail - we tried our best
		t.Logf("Note: cleanup attempt returned error: %v", err)
	}
}

// CleanupAllNbcTestResources removes all nbc-test-* instances and volumes.
// This is the non-test version for use in TestMain where *testing.T is not available.
// Returns error only if cannot connect to Incus; cleanup failures are silent.
//
// Example TestMain usage:
//
//	func TestMain(m *testing.M) {
//	    testutil.CleanupAllNbcTestResources() // Clean slate
//	    code := m.Run()
//	    testutil.CleanupAllNbcTestResources() // Final cleanup
//	    os.Exit(code)
//	}
func CleanupAllNbcTestResources() error {
	// Connect to Incus
	client, err := incus.ConnectIncusUnix("", nil)
	if err != nil {
		return err
	}

	// List all instances
	instances, err := client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return err
	}

	// Delete nbc-test-* instances
	for _, instance := range instances {
		if !strings.HasPrefix(instance.Name, "nbc-test-") {
			continue
		}

		// Force stop (ignore errors - may already be stopped)
		stopReq := api.InstanceStatePut{
			Action:  "stop",
			Force:   true,
			Timeout: -1,
		}
		op, _ := client.UpdateInstanceState(instance.Name, stopReq, "")
		if op != nil {
			_ = op.Wait()
		}

		// Delete instance (ignore errors)
		op, _ = client.DeleteInstance(instance.Name)
		if op != nil {
			_ = op.Wait()
		}
	}

	// List all storage pools and clean up volumes
	pools, err := client.GetStoragePoolNames()
	if err != nil {
		// Can't list pools, but we already cleaned instances
		return nil
	}

	for _, pool := range pools {
		volumes, err := client.GetStoragePoolVolumes(pool)
		if err != nil {
			continue
		}

		for _, vol := range volumes {
			if !strings.HasPrefix(vol.Name, "nbc-test-") {
				continue
			}
			// Delete volume (ignore errors)
			_ = client.DeleteStoragePoolVolume(pool, vol.Type, vol.Name)
		}
	}

	return nil
}

// CleanupOrphanedMounts force unmounts any mounts matching the given pattern.
// This is useful for cleaning up mounts from failed tests.
// All errors are silently ignored.
//
// Example:
//
//	CleanupOrphanedMounts("/mnt/nbc-test-*")
func CleanupOrphanedMounts(pattern string) {
	// Use findmnt to list mounts matching pattern
	cmd := exec.Command("findmnt", "-l", "-n", "-o", "TARGET")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		mount := strings.TrimSpace(line)
		if mount == "" {
			continue
		}

		// Check if mount matches pattern (simple prefix match for now)
		// Pattern like "/mnt/nbc-test-*" becomes prefix "/mnt/nbc-test-"
		prefix := strings.TrimSuffix(pattern, "*")
		if !strings.HasPrefix(mount, prefix) {
			continue
		}

		// Force unmount (ignore errors)
		cmd := exec.Command("umount", "-f", mount)
		_ = cmd.Run()
	}
}
