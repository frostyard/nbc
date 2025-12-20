# Incus Integration Testing

This document describes the Incus-based integration testing for nbc.

## Overview

The Incus test suite (`test_incus.sh`) provides comprehensive end-to-end testing in isolated virtual machines. Unlike loop device tests, Incus tests run in real VMs with actual bootloaders and complete system environments.

## Prerequisites

### Install Incus

```bash
# Ubuntu/Debian (from package)
sudo apt install incus

# Or use snap
sudo snap install incus

# Or build from source
# See: https://linuxcontainers.org/incus/docs/main/installing/
```

### Initialize Incus

```bash
sudo incus admin init
```

For testing, you can use the default answers for most questions. Ensure you have:

- Default storage pool (ZFS, btrfs, or dir)
- Network bridge configured
- At least 10GB free storage

## Running Tests

```bash
# Run all Incus integration tests (recommended)
sudo make test-incus

# Or run the script directly with preserved PATH
sudo -E env "PATH=$PATH" ./test_incus.sh

# Use a custom/private container image
TEST_IMAGE=ghcr.io/myorg/myimage:latest sudo -E ./test_incus.sh
```

**Important**: The test requires `go` and `make` to be in PATH. The Makefile target automatically preserves your PATH when running with sudo. If running the script directly, use `sudo -E env "PATH=$PATH"` to ensure all tools are available.

### Using Private Container Images

By default, tests use `quay.io/centos-bootc/centos-bootc:stream9` (public image).

To test with a private image:

1. **Login to the registry**:

   ```bash
   docker login ghcr.io
   # or
   podman login ghcr.io
   ```

2. **Run tests with your image**:
   ```bash
   TEST_IMAGE=ghcr.io/myorg/myimage:latest sudo -E ./test_incus.sh
   ```

The test will use credentials from `~/.docker/config.json` automatically.

## What Gets Tested

The Incus integration test performs the following tests:

### 1. List Disks

Verifies `nbc list` can discover available disks in the VM.

### 2. Validate Disk

Tests `nbc validate` correctly identifies suitable installation targets.

### 3. Install to Disk

Performs a complete bootc image installation:

- Creates 5-partition GPT layout (EFI, boot, root1, root2, var)
- Extracts container filesystem
- Installs GRUB2 bootloader
- Configures fstab and system files

### 4. Verify Partition Layout

Checks that exactly 5 partitions are created with correct labels and sizes.

### 5. Verify Bootloader

Validates:

- GRUB installation to EFI partition
- Boot partition contents
- GRUB configuration file

### 6. Verify Root Filesystem

Inspects the installed root filesystem:

- Directory structure
- Nbc configuration file
- fstab entries
- Symlinks to /var

### 7. System Update

Performs an A/B update:

- Pulls new image
- Installs to inactive partition (root2)
- Merges user /etc modifications from active root to new root
- Updates bootloader

### 8. Verify A/B Partitions

Confirms both root partitions contain valid filesystems after update.

### 9. Verify GRUB Boot Entries

Checks GRUB menu has entries for both systems (updated and previous).

### 10. Verify Kernel and Initramfs

Validates kernel and initramfs are properly installed in /boot partition.

## Test Environment

Each test run:

- Creates a fresh Fedora 40 VM
- Attaches a 60GB virtual disk
- Installs required tools (grub2, gdisk, etc.)
- Copies nbc binary to VM
- Runs all tests
- Cleans up VM and resources automatically

## Test Duration

Typical test run takes **10-20 minutes** depending on:

- Network speed (image downloads)
- Disk I/O performance
- CPU speed (image extraction, filesystem operations)

## Troubleshooting

### "incus: command not found"

Install Incus following the [official documentation](https://linuxcontainers.org/incus/docs/main/installing/).

### "Error: Incus is not initialized"

Run `sudo incus admin init` to set up Incus storage and networking.

### "Error: VM failed to start"

Check Incus logs:

```bash
incus info nbc-test-<PID> --show-log
```

Ensure your system supports KVM virtualization:

```bash
# Check for KVM support
lsmod | grep kvm

# If missing, load KVM module
sudo modprobe kvm
sudo modprobe kvm_intel  # or kvm_amd
```

### Test timeout

If tests timeout, increase the timeout value in `test_incus.sh`:

```bash
TIMEOUT=1800  # 30 minutes
```

### Insufficient storage

Incus needs space for:

- VM image (2-3 GB)
- Virtual disk (60 GB)
- Container image (2-5 GB)

Ensure your storage pool has at least 70 GB free:

```bash
incus storage info default
```

## Log Files

Failed tests save logs to `/tmp`:

- `/tmp/nbc-install-<PID>.log` - Installation log
- `/tmp/nbc-update-<PID>.log` - Update log

## Cleanup

The test script automatically cleans up:

- Stops and deletes VMs
- Removes storage volumes
- Deletes temporary build directories

Manual cleanup if needed:

```bash
# List VMs
incus list

# Delete stuck VM
incus delete nbc-test-<PID> --force

# List and delete storage volumes
incus storage volume list default
incus storage volume delete default nbc-test-<PID>-disk
```

## CI/CD Integration

To run Incus tests in CI:

```yaml
# GitHub Actions example
test-incus:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - name: Install Incus
      run: |
        sudo snap install incus
        sudo incus admin init --auto
    - name: Run Incus Tests
      run: sudo make test-incus
```

Note: Many CI runners don't support nested virtualization, so Incus VM tests may need to run on self-hosted runners with KVM support.

## Advanced Usage

### Custom Test Image

Edit `test_incus.sh` to use your own bootc image:

```bash
TEST_IMAGE="quay.io/your-org/your-bootc-image:latest"
```

### Smaller Disk

For faster tests with smaller images:

```bash
DISK_SIZE="30GB"
```

### Keep VM After Tests

Comment out the cleanup trap:

```bash
# trap cleanup EXIT INT TERM
```

Then manually inspect the VM:

```bash
incus exec nbc-test-<PID> -- bash
```

## Comparison with Other Tests

| Test Type   | Isolation | Bootloader | Speed  | Realism |
| ----------- | --------- | ---------- | ------ | ------- |
| Unit        | Process   | No         | Fast   | Low     |
| Loop Device | Process   | No         | Medium | Medium  |
| Incus VM    | Full VM   | Yes        | Slow   | High    |

Incus tests provide the highest confidence that nbc works in real-world scenarios.
