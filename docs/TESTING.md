# Testing Guide

This document describes how to test nbc using disk image files and loop devices.

## Test Types

### Unit Tests

Unit tests that don't require root or physical devices:

```bash
make test-unit
```

These tests cover:

- `TestFormatSize` - Size formatting utilities
- `TestGetBootDeviceFromPartition` - Device name parsing
- `TestGetDiskByPath` - Path resolution (skips non-existent devices)

### Integration Tests

Integration tests that use disk images and require root privileges:

```bash
sudo make test-integration
```

These tests cover:

- `TestCreatePartitions` - GPT partition table creation
- `TestFormatPartitions` - Filesystem formatting
- `TestMountPartitions` - Partition mounting/unmounting
- `TestDetectExistingPartitionScheme` - Partition scheme detection

## Test Infrastructure

### Disk Image Creation

The test suite uses **loop devices** to simulate physical disks without requiring actual hardware. The `testutil` package provides utilities for creating and managing test disk images:

```go
// Create a 50GB test disk image attached to a loop device
disk, err := testutil.CreateTestDisk(t, 50)
if err != nil {
    t.Fatalf("Failed to create test disk: %v", err)
}

// Use the disk in tests
device := disk.GetDevice() // e.g., /dev/loop0

// Cleanup is automatic via t.Cleanup()
```

### How It Works

1. **Sparse Files**: Creates sparse disk image files (don't use actual disk space)
2. **Loop Devices**: Attaches images to loop devices (e.g., `/dev/loop0`)
3. **Automatic Cleanup**: Test framework automatically detaches loop devices and removes image files
4. **Root Required**: Loop device operations require root privileges

### Test Utilities

The `pkg/testutil` package provides:

- `CreateTestDisk(t, sizeGB)` - Create disk image and attach to loop device
- `RequireRoot(t)` - Skip test if not running as root
- `RequireTools(t, tools...)` - Skip test if required tools are missing
- `CreateMockContainer(t, imageName)` - Create minimal test container image
- `WaitForDevice(device)` - Wait for device to be ready after partitioning
- `CleanupMounts(t, mountPoint)` - Force unmount all mounts under a path

## Running Tests

### Prerequisites

```bash
# Required tools
sudo apt install gdisk dosfstools e2fsprogs podman rsync

# Or on Fedora/RHEL
sudo dnf install gdisk dosfstools e2fsprogs podman rsync
```

### Run All Tests

```bash
# Run unit tests (no root needed)
make test-unit

# Run integration tests (requires root)
sudo make test-integration

# Or use the test script
sudo ./test_integration.sh
```

### Run Specific Tests

```bash
# Run specific unit test
go test -v ./pkg/... -run TestFormatSize

# Run specific integration test (requires root)
sudo go test -v ./pkg/... -run TestCreatePartitions

# Run installation tests (requires root)
sudo make test-install
# Or directly:
sudo go test -v ./pkg/... -run TestBootcInstaller -timeout 20m

# Run update tests (requires root)
sudo make test-update
# Or directly:
sudo go test -v ./pkg/... -run TestSystemUpdater -timeout 20m
```

## Test Coverage

### Unit Tests (No Root Required)

- Device name parsing (`TestGetBootDeviceFromPartition`)
- Size formatting (`TestFormatSize`)
- Path resolution (`TestGetDiskByPath`)

### Integration Tests (Root Required)

- Partition creation (`TestCreatePartitions`)
- Partition formatting (`TestFormatPartitions`)
- Partition mounting (`TestMountPartitions`)
- Partition scheme detection (`TestDetectExistingPartitionScheme`)

### Installation Tests (Root Required)

- Full system installation (`TestBootcInstaller_Install`)
- Dry-run mode (`TestBootcInstaller_DryRun`)
- Kernel arguments persistence (`TestBootcInstaller_WithKernelArgs`)

### Update Tests (Root Required)

- System updates (`TestSystemUpdater_Update`)
- /etc configuration persistence (`TestSystemUpdater_EtcPersistence`)

## Writing Tests

### Example Unit Test

```go
func TestMyFunction(t *testing.T) {
    result := MyFunction("input")
    if result != "expected" {
        t.Errorf("got %s, want %s", result, "expected")
    }
}
```

### Example Integration Test

```go
func TestMyDiskOperation(t *testing.T) {
    // Check prerequisites
    testutil.RequireRoot(t)
    testutil.RequireTools(t, "sgdisk", "mkfs.ext4")

    // Create test disk
    disk, err := testutil.CreateTestDisk(t, 10) // 10GB
    if err != nil {
        t.Fatalf("Failed to create test disk: %v", err)
    }

    // Perform operations
    device := disk.GetDevice()
    // ... your test code here ...

    // Cleanup is automatic
}
```

### Example Installation Test

```go
func TestMyInstallation(t *testing.T) {
    testutil.RequireRoot(t)
    testutil.RequireTools(t, "losetup", "sgdisk", "mkfs.vfat", "mkfs.ext4", "podman")

    // Create test disk
    disk, err := testutil.CreateTestDisk(t, 50)
    if err != nil {
        t.Fatalf("Failed to create test disk: %v", err)
    }

    // Create mock container
    imageName := "localhost/my-test:latest"
    if err := testutil.CreateMockContainer(t, imageName); err != nil {
        t.Fatalf("Failed to create container: %v", err)
    }

    // Perform installation
    mountPoint := filepath.Join(t.TempDir(), "mnt")
    installer := NewBootcInstaller(imageName, disk.GetDevice())
    installer.SetMountPoint(mountPoint)
    installer.SetVerbose(true)

    defer testutil.CleanupMounts(t, mountPoint)

    if err := installer.Install(); err != nil {
        t.Fatalf("Install failed: %v", err)
    }

    // Verify installation
    // ... verification code ...
}
```

## Test Isolation

Each test:

- Uses isolated temporary directories (`t.TempDir()`)
- Gets its own loop device
- Cleans up automatically on completion or failure
- Does not interfere with other tests

## Continuous Integration

For CI environments:

```bash
# GitHub Actions example
- name: Run tests
  run: |
    # Unit tests (no root)
    make test-unit

    # Integration tests (with root)
    sudo make test-integration
```

## Troubleshooting

### "Test requires root privileges"

Integration tests need root to create loop devices:

```bash
sudo make test-integration
```

### "Required tool not found"

Install missing tools:

```bash
# Ubuntu/Debian
sudo apt install gdisk dosfstools e2fsprogs

# Fedora/RHEL
sudo dnf install gdisk dosfstools e2fsprogs
```

### "No loop devices available"

Load the loop module:

```bash
sudo modprobe loop
```

Or increase max loop devices:

```bash
sudo modprobe loop max_loop=16
```

### Tests hang or leave loop devices attached

The test framework should clean up automatically, but if tests are interrupted:

```bash
# List loop devices
sudo losetup -a

# Detach specific loop device
sudo losetup -d /dev/loop0

# Remove test images
rm -f /tmp/nbc-test-*.img
```

## Performance

Test disk images use **sparse files**, so:

- A 50GB test disk uses ~0 bytes initially
- Only grows as partitions are written
- Typical test disk uses < 100MB actual space

## Safety

Tests are designed to be safe:

- Only operate on loop devices (never real disks)
- Automatically clean up on completion
- Use temporary directories
- No system modification outside test scope

Tests **cannot** accidentally wipe your real disks because they:

1. Only work with loop devices from test images
2. Don't have access to actual `/dev/sd*` or `/dev/nvme*` devices during tests
3. Clean up completely on exit

## Continuous Integration

The project uses GitHub Actions for automated testing on every push and pull request.

### CI Workflow

The `.github/workflows/test.yml` workflow runs:

| Job                   | Description                                       | Runs on       |
| --------------------- | ------------------------------------------------- | ------------- |
| **Lint**              | golangci-lint for code quality                    | ubuntu-latest |
| **Unit Tests**        | All unit tests with coverage                      | ubuntu-latest |
| **Build**             | Cross-compilation for linux/amd64 and linux/arm64 | ubuntu-latest |
| **Integration Tests** | Info only (requires root/loop devices)            | -             |

### Coverage Reporting

Unit tests generate coverage data and upload to Codecov (when configured):

```bash
# Generate coverage locally
make test-coverage

# View coverage report
go tool cover -html=coverage.out
```

### What CI Cannot Test

Due to GitHub Actions limitations, these tests require manual execution:

- **Integration tests** (`sudo make test-integration`) - Require root and loop devices
- **Incus E2E tests** (`./test_incus.sh`) - Require VM creation capability
- **Encryption tests** (`./test_incus_encryption.sh`) - Require LUKS and TPM emulation

### Running CI Locally

You can simulate the CI workflow locally using [act](https://github.com/nektos/act):

```bash
# Install act (macOS)
brew install act

# Run all CI jobs
act

# Run specific job
act -j unit-test
```
