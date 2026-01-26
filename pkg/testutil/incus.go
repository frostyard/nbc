// Package testutil provides test helpers and fixtures for nbc testing.
package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// IncusFixture wraps an Incus client with test-specific helpers for VM management.
// It provides a clean interface for creating, managing, and cleaning up VMs
// during integration tests.
type IncusFixture struct {
	client     incus.InstanceServer
	vmName     string
	volumeName string
	t          *testing.T
}

// NewIncusFixture creates a new Incus test fixture connected to the local socket.
// It registers cleanup handlers via t.Cleanup() before creating any resources,
// ensuring cleanup runs even on test failure.
//
// If Incus is not available (socket doesn't exist or connection fails),
// the test is skipped rather than failed.
func NewIncusFixture(t *testing.T) *IncusFixture {
	t.Helper()

	// Connect to local Incus socket
	client, err := incus.ConnectIncusUnix("", nil)
	if err != nil {
		t.Skipf("Incus not available (skipping): %v", err)
		return nil
	}

	// Generate unique VM name: nbc-test-{sanitized-test-name}-{pid}
	vmName := fmt.Sprintf("nbc-test-%s-%d", sanitize(t.Name()), os.Getpid())

	fixture := &IncusFixture{
		client: client,
		vmName: vmName,
		t:      t,
	}

	// Register cleanup BEFORE creating any resources
	// This ensures cleanup runs even if test panics or calls t.Fatal()
	t.Cleanup(fixture.Cleanup)

	return fixture
}

// VMName returns the unique VM name for this fixture.
func (f *IncusFixture) VMName() string {
	return f.vmName
}

// Client returns the underlying Incus client for advanced operations.
func (f *IncusFixture) Client() incus.InstanceServer {
	return f.client
}

// Cleanup performs best-effort cleanup of all resources created by this fixture.
// It silently ignores errors per CONTEXT.md decisions - cleanup failures
// should not mask test failures.
func (f *IncusFixture) Cleanup() {
	if f.client == nil {
		return
	}

	// Force stop VM (ignore errors - VM may not exist or may already be stopped)
	reqState := api.InstanceStatePut{
		Action:  "stop",
		Force:   true,
		Timeout: -1,
	}
	op, _ := f.client.UpdateInstanceState(f.vmName, reqState, "")
	if op != nil {
		_ = op.Wait() // Best-effort, ignore result
	}

	// Delete VM (ignore errors - VM may not exist)
	op, _ = f.client.DeleteInstance(f.vmName)
	if op != nil {
		_ = op.Wait()
	}

	// Delete storage volume if one was created
	if f.volumeName != "" {
		// Try default pool first, then try other common pool names
		pools := []string{"default", "local", "zfs", "dir"}
		for _, pool := range pools {
			_ = f.client.DeleteStoragePoolVolume(pool, "custom", f.volumeName)
		}
	}
}

// CreateVM launches a new VM with the specified image.
// Uses standard test configuration: 4 CPU, 16GiB RAM, secureboot disabled.
func (f *IncusFixture) CreateVM(image string) error {
	f.t.Helper()
	f.t.Logf("Creating VM %s from image %s", f.vmName, image)

	// Parse image source - could be "images:distro/version" or just "distro/version"
	source := api.InstanceSource{
		Type: "image",
	}

	if strings.Contains(image, ":") {
		parts := strings.SplitN(image, ":", 2)
		source.Server = getImageServer(parts[0])
		source.Protocol = "simplestreams"
		source.Alias = parts[1]
	} else {
		source.Alias = image
	}

	req := api.InstancesPost{
		Name: f.vmName,
		Type: api.InstanceTypeVM,
		InstancePut: api.InstancePut{
			Config: map[string]string{
				"limits.cpu":          "4",
				"limits.memory":       "16GiB",
				"security.secureboot": "false",
			},
		},
		Source: source,
	}

	op, err := f.client.CreateInstance(req)
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait for instance creation: %w", err)
	}

	// Start the VM
	startReq := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}
	op, err = f.client.UpdateInstanceState(f.vmName, startReq, "")
	if err != nil {
		return fmt.Errorf("start instance: %w", err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait for instance start: %w", err)
	}

	return nil
}

// WaitForReady polls the VM until systemd reports system is running.
// Uses the provided context for timeout/cancellation.
func (f *IncusFixture) WaitForReady(ctx context.Context) error {
	f.t.Helper()
	f.t.Logf("Waiting for VM %s to be ready", f.vmName)

	// Poll until systemd reports running or context is cancelled
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for VM ready: %w", ctx.Err())
		default:
		}

		output, err := f.ExecCommand("systemctl", "is-system-running", "--wait")
		if err == nil {
			state := strings.TrimSpace(output)
			if state == "running" || state == "degraded" {
				f.t.Logf("VM %s is ready (state: %s)", f.vmName, state)
				return nil
			}
		}

		// Small delay between polls
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for VM ready: %w", ctx.Err())
		default:
			// Continue polling
		}
	}
}

// ExecCommand executes a command inside the VM and returns stdout.
// Returns an error if the command fails or returns non-zero exit code.
func (f *IncusFixture) ExecCommand(command ...string) (string, error) {
	f.t.Helper()

	if len(command) == 0 {
		return "", fmt.Errorf("no command specified")
	}

	// Create buffers for stdout/stderr
	var stdout, stderr bytes.Buffer

	// Set up exec request
	execReq := api.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: false,
	}

	// Execute with proper I/O handling
	args := incus.InstanceExecArgs{
		Stdin:  io.NopCloser(strings.NewReader("")),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	op, err := f.client.ExecInstance(f.vmName, execReq, &args)
	if err != nil {
		return "", fmt.Errorf("exec instance: %w", err)
	}

	// Wait for completion
	if err := op.Wait(); err != nil {
		return stdout.String(), fmt.Errorf("exec wait: %w (stderr: %s)", err, stderr.String())
	}

	// Check exit code from operation metadata
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

// PushFile uploads a local file to the VM.
func (f *IncusFixture) PushFile(localPath, remotePath string) error {
	f.t.Helper()
	f.t.Logf("Pushing file %s to %s:%s", localPath, f.vmName, remotePath)

	// Read local file
	content, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}

	// Get file info for permissions
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}

	// Create file in VM
	args := incus.InstanceFileArgs{
		Content:   bytes.NewReader(content),
		Mode:      int(info.Mode().Perm()),
		Type:      "file",
		WriteMode: "overwrite",
	}

	return f.client.CreateInstanceFile(f.vmName, remotePath, args)
}

// AttachDisk creates and attaches a block storage volume to the VM.
// The volume is tracked for cleanup.
func (f *IncusFixture) AttachDisk(volumeName string, size string) error {
	f.t.Helper()
	f.t.Logf("Attaching disk %s (%s) to VM %s", volumeName, size, f.vmName)

	// Find default storage pool
	pools, err := f.client.GetStoragePoolNames()
	if err != nil {
		return fmt.Errorf("get storage pools: %w", err)
	}

	if len(pools) == 0 {
		return fmt.Errorf("no storage pools available")
	}
	poolName := pools[0] // Use first available pool

	// Create storage volume
	volReq := api.StorageVolumesPost{
		Name:        volumeName,
		Type:        "custom",
		ContentType: "block",
		StorageVolumePut: api.StorageVolumePut{
			Config: map[string]string{
				"size": size,
			},
		},
	}

	if err := f.client.CreateStoragePoolVolume(poolName, volReq); err != nil {
		return fmt.Errorf("create storage volume: %w", err)
	}

	f.volumeName = volumeName

	// Get current instance config
	instance, etag, err := f.client.GetInstance(f.vmName)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	// Add disk device
	if instance.Devices == nil {
		instance.Devices = make(map[string]map[string]string)
	}

	instance.Devices[volumeName] = map[string]string{
		"type":   "disk",
		"source": volumeName,
		"pool":   poolName,
	}

	// Update instance
	op, err := f.client.UpdateInstance(f.vmName, instance.Writable(), etag)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait for device attach: %w", err)
	}

	return nil
}

// CreateSnapshot creates a snapshot of the VM with the given name.
func (f *IncusFixture) CreateSnapshot(name string) error {
	f.t.Helper()
	f.t.Logf("Creating snapshot %s of VM %s", name, f.vmName)

	req := api.InstanceSnapshotsPost{
		Name:     name,
		Stateful: false,
	}

	op, err := f.client.CreateInstanceSnapshot(f.vmName, req)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}

	return op.Wait()
}

// RestoreSnapshot restores the VM to a previously created snapshot.
// The VM will be stopped before restoring and started after.
func (f *IncusFixture) RestoreSnapshot(name string) error {
	f.t.Helper()
	f.t.Logf("Restoring VM %s to snapshot %s", f.vmName, name)

	// Stop VM first
	stopReq := api.InstanceStatePut{
		Action:  "stop",
		Force:   true,
		Timeout: -1,
	}
	op, err := f.client.UpdateInstanceState(f.vmName, stopReq, "")
	if err != nil {
		return fmt.Errorf("stop instance: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait for stop: %w", err)
	}

	// Get current instance config
	instance, etag, err := f.client.GetInstance(f.vmName)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	// Restore snapshot
	instance.Restore = name
	op, err = f.client.UpdateInstance(f.vmName, instance.Writable(), etag)
	if err != nil {
		return fmt.Errorf("restore snapshot: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("wait for restore: %w", err)
	}

	// Start VM
	startReq := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}
	op, err = f.client.UpdateInstanceState(f.vmName, startReq, "")
	if err != nil {
		return fmt.Errorf("start instance: %w", err)
	}

	return op.Wait()
}

// sanitize converts a test name to a safe VM name.
// Replaces slashes with dashes, removes other special characters, and lowercases.
func sanitize(name string) string {
	// Replace / with - (common in subtests)
	name = strings.ReplaceAll(name, "/", "-")

	// Remove any characters that aren't alphanumeric, dash, or underscore
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	name = re.ReplaceAllString(name, "")

	// Lowercase for consistency
	name = strings.ToLower(name)

	// Truncate to reasonable length (Incus has limits)
	if len(name) > 40 {
		name = name[:40]
	}

	return name
}

// getImageServer returns the URL for a named image server.
func getImageServer(name string) string {
	servers := map[string]string{
		"images": "https://images.linuxcontainers.org",
		"ubuntu": "https://cloud-images.ubuntu.com/releases",
	}

	if url, ok := servers[name]; ok {
		return url
	}

	// Return as-is if not a known alias
	return name
}
