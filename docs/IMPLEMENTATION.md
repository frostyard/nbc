# Implementation Summary

## Overview

Successfully refactored `nbc` to perform bootc-compatible container installations **without** using the external `bootc` command. The application now handles all installation steps natively in Go.

## Architecture

### Core Components

1. **[pkg/partition.go](pkg/partition.go)** - Disk partitioning and formatting

   - GPT partition table creation with `sgdisk`
   - EFI (2GB FAT32), Boot (1GB ext4), Root1 (12GB ext4), Root2 (12GB ext4), Var (remaining ext4)
   - A/B partition scheme for atomic updates
   - Partition mounting and UUID management

2. **[pkg/container.go](pkg/container.go)** - Container filesystem extraction

   - Extracts container images using go-containerregistry (pure Go, no Docker/Podman required)
   - Handles overlay filesystem whiteouts for proper layer merging
   - Preserves SUID/SGID/sticky bits on files and directories
   - Creates system directories and fstab with systemd auto-discovery
   - Supports chroot operations for post-install configuration

3. **[pkg/bootloader.go](pkg/bootloader.go)** - Bootloader installation

   - GRUB2 and systemd-boot support (with automatic detection)
   - Copies kernels/initramfs from /usr/lib/modules to appropriate boot location
   - Configures kernel cmdline with root=UUID and systemd.mount-extra for /var
   - Generates bootloader configuration files
   - Kernel argument customization
   - **Secure Boot support**: Automatic shim detection and EFI chain setup
     - Searches for `shimx64.efi.signed` in common locations
     - Sets up proper boot chain: shim → bootloader
     - Includes MOK manager for key enrollment

4. **[pkg/bootc.go](pkg/bootc.go)** - Main installation orchestrator

   - Coordinates all installation steps
   - Provides progress feedback
   - Handles cleanup on errors

5. **[pkg/disk.go](pkg/disk.go)** - Disk management utilities
   - Disk discovery and enumeration
   - Validation and safety checks
   - Support for SATA, NVMe, virtio, MMC devices

## Installation Process

The complete installation workflow (6 steps):

```
1. Create Partitions
   └─ sgdisk creates GPT with EFI/boot/root1/root2/var partitions
      ├─ EFI: 2GB (ESP partition type, auto-mounted by systemd)
      ├─ Boot: 1GB (XBOOTLDR partition type, auto-mounted by systemd)
      ├─ Root1: 12GB (active root for OS A)
      ├─ Root2: 12GB (inactive root for OS B, for A/B updates)
      └─ Var: remaining space (mounted via systemd.mount-extra)

2. Format Partitions
   ├─ mkfs.vfat for EFI (FAT32)
   ├─ mkfs.ext4 for boot, root1, root2, var
   └─ Retrieve UUIDs with blkid

3. Mount Partitions
   ├─ Mount root1 → /tmp/nbc-install
   ├─ Mount boot → /tmp/nbc-install/boot
   ├─ Mount EFI → /tmp/nbc-install/boot/efi
   └─ Mount var → /tmp/nbc-install/var

4. Extract Container
   ├─ Pull image layers via go-containerregistry
   ├─ Extract each layer handling whiteouts
   ├─ Preserve special file permissions (SUID/SGID)
   └─ Extract filesystem to mounted root1

5. Configure System
   ├─ Create /etc/fstab (minimal, most mounts auto-discovered)
   ├─ Setup system directories (dev, proc, sys, run, tmp)
   ├─ Verify /etc and create backup in /var/etc.backup
   └─ Parse os-release for OS name

   Note: /etc stays on the root filesystem for reliable boot.
   Early attempts to bind-mount /var/etc to /etc failed because
   services like dbus-broker need /etc before /var is mounted.

6. Install Bootloader
   ├─ Detect bootloader type (GRUB2/systemd-boot)
   ├─ Copy kernel/initramfs from /usr/lib/modules
   ├─ Install bootloader to EFI partition
   ├─ Setup Secure Boot chain (if shim available):
   │  ├─ Copy shimx64.efi as BOOTX64.EFI
   │  ├─ Copy bootloader as grubx64.efi (chain-loaded by shim)
   │  └─ Copy mmx64.efi for MOK key enrollment
   ├─ Generate boot configuration with:
   │  ├─ root=UUID=<root1-uuid>
   │  └─ systemd.mount-extra=UUID=<var-uuid>:/var:ext4:defaults
   └─ Set proper kernel cmdline arguments
```

## Key Features

### No External Dependencies

- All functionality implemented natively in Go
- Uses go-containerregistry (no Docker/Podman required)
- Uses standard Linux tools (sgdisk, mkfs, mount, grub/bootctl)
- More transparent and maintainable

### Safety Features

- Comprehensive prerequisite checking
- Mounted partition detection
- Confirmation prompts before destructive operations
- Dry-run mode for testing
- Automatic cleanup on errors

### Flexibility

- Multiple device type support (SATA, NVMe, virtio, MMC, loop devices)
- A/B partition scheme for atomic updates
- Custom kernel arguments
- Automatic bootloader detection (GRUB2 vs systemd-boot)
- Systemd Discoverable Partitions integration
- Configurable mount points
- **Filesystem choice**: ext4 (default) or btrfs for root/var partitions

### User Experience

- Clear progress indicators (Step X/6)
- Verbose output option for debugging
- Detailed error messages
- Post-installation verification

## Usage Examples

### Basic Installation

```bash
sudo ./nbc install \
  --image quay.io/fedora/fedora-coreos:stable \
  --device /dev/sda
```

### With Custom Kernel Args

```bash
sudo ./nbc install \
  --image localhost/custom-image \
  --device /dev/nvme0n1 \
  --karg console=ttyS0 \
  --karg quiet
```

### Dry Run Test

```bash
./nbc install \
  --image test/image \
  --device /dev/sdb \
  --dry-run
```

## Technical Improvements

### Removed Dependencies

- ❌ bootc command not required
- ❌ Docker/Podman not required (uses go-containerregistry directly)
- ✅ Uses standard Linux utilities only

### Added Functionality

- ✅ Direct GPT partitioning with A/B update scheme
- ✅ Filesystem creation and mounting
- ✅ Container extraction via go-containerregistry (pure Go)
- ✅ Overlay filesystem whiteout handling
- ✅ Native bootloader installation (GRUB2 and systemd-boot)
- ✅ System configuration (fstab, directories)
- ✅ Systemd Discoverable Partitions support
- ✅ Kernel cmdline generation with systemd.mount-extra

### Code Organization

- Modular package structure
- Clear separation of concerns
- Reusable components
- Comprehensive error handling

## Testing Recommendations

1. **Virtual Machine Testing**

   ```bash
   # Create a test VM with extra disk
   # Test with various container images
   sudo ./nbc install --image <test-image> --device /dev/vdb
   ```

2. **Dry Run Validation**

   ```bash
   # Verify workflow without changes
   ./nbc install --image <image> --device /dev/sdX --dry-run
   ```

3. **Different Device Types**
   - Test on SATA devices (/dev/sda)
   - Test on NVMe devices (/dev/nvme0n1)
   - Test on virtio devices (/dev/vda)

## Future Enhancements

Potential improvements:

- [x] A/B partition scheme (implemented)
- [x] systemd-boot support (implemented)
- [ ] Support for custom partition layouts
- [ ] BTRFS/XFS filesystem options
- [ ] RAID/LVM support
- [ ] Encrypted root filesystem
- [ ] Multi-boot configurations
- [ ] Progress bars for long operations
- [ ] Automatic A/B updates
- [ ] Rollback capability
- [ ] Pre/post installation hooks
- [x] Secure Boot support (implemented)

## Files Modified/Created

**New Files:**

- `pkg/partition.go` - Partitioning logic
- `pkg/container.go` - Container extraction
- `pkg/bootloader.go` - Bootloader installation

**Modified Files:**

- `pkg/bootc.go` - Complete rewrite of Install() method
- `README.md` - Updated documentation

**Unchanged Files:**

- `pkg/disk.go` - Disk management utilities
- `cmd/*.go` - CLI commands
- `main.go` - Entry point

## Dependencies

### Required Tools

- `go-containerregistry` (embedded) - Container image operations
- `sgdisk` - GPT partitioning
- `mkfs.vfat` - FAT32 formatting (EFI partition)
- `mkfs.ext4` - ext4 formatting (boot, root, var partitions)
- `mount/umount` - Filesystem mounting
- `blkid` - UUID retrieval
- `partprobe` - Kernel partition update
- `udevadm` - Device node synchronization
- `grub-install` or `grub2-install` - GRUB bootloader (if using GRUB)
- `bootctl` - systemd-boot bootloader (if using systemd-boot)

### Go Packages

- `github.com/spf13/cobra` - CLI framework
- `github.com/spf13/viper` - Configuration
- `github.com/google/go-containerregistry` - Container image handling
- Standard library for core logic (os, os/exec, archive/tar, path/filepath)

## Conclusion

The `nbc` tool provides a complete, self-contained solution for installing bootc-compatible containers to physical disks with A/B partition scheme for atomic updates. It eliminates dependencies on external tools like bootc, Docker, or Podman by using native Go libraries (go-containerregistry) and standard Linux utilities. The implementation leverages systemd Discoverable Partitions for automatic mounting and provides a foundation for robust, atomic OS updates.
