# nbc

[![Tests](https://github.com/frostyard/nbc/actions/workflows/test.yml/badge.svg)](https://github.com/frostyard/nbc/actions/workflows/test.yml)

A Go application for installing bootc-compatible containers to physical disks with A/B partitioning and atomic updates.

## Overview

`nbc` is a command-line tool that installs bootc-compatible container images directly to physical disks. It handles the complete installation process including partitioning, filesystem creation, container extraction, and bootloader installation - all without requiring the `bootc` command itself.

The tool implements an A/B partition scheme for safe, atomic system updates with automatic rollback capability.

## Source Image Requirements

For successful installation and updates, the source container image must meet the following requirements:

### Kernel and Initramfs Location

The kernel and initramfs files **must** be located in `/usr/lib/modules/$KERNEL_VERSION/` within the container image:

- **Kernel**: `/usr/lib/modules/$KERNEL_VERSION/vmlinuz` or `/usr/lib/modules/$KERNEL_VERSION/vmlinuz-$KERNEL_VERSION`
- **Initramfs**: One of the following in the same directory:
  - `/usr/lib/modules/$KERNEL_VERSION/initramfs.img`
  - `/usr/lib/modules/$KERNEL_VERSION/initrd.img`
  - `/usr/lib/modules/$KERNEL_VERSION/initramfs-$KERNEL_VERSION.img`
  - `/usr/lib/modules/$KERNEL_VERSION/initrd.img-$KERNEL_VERSION`

During installation or update, `nbc` will automatically copy these files from `/usr/lib/modules/$KERNEL_VERSION/` to the shared `/boot` partition.

### Filesystem Structure

The container image should follow standard Linux Filesystem Hierarchy Standard (FHS):

- `/usr`: System binaries and libraries (read-only in production)
- `/etc`: Configuration files (user modifications merged during A/B updates)
- `/var`: Variable data (symlinked to shared partition)
- `/home`: User home directories (symlinked to `/var/home`)
- `/root`: Root user home directory (symlinked to `/var/roothome`)
- `/opt`: Optional packages
- `/srv`: Service data (symlinked to `/var/srv`)
- `/tmp`: Temporary files (mounted as tmpfs at runtime)

### Required System Components

The image should contain:

- **Linux kernel modules** in `/usr/lib/modules/$KERNEL_VERSION/`
- **dracut**: Required for initramfs generation with etc-overlay module
- **System libraries** in `/usr/lib` and `/usr/lib64`
- **Essential binaries** in `/usr/bin` and `/usr/sbin`
- **Basic system configuration** in `/etc`

### Optional but Recommended

- **systemd**: For service management and system initialization
- **NetworkManager** or similar for network configuration
- **SSH server**: For remote access
- **Package manager**: For installing additional software after deployment

**Note**: Container images can include a pre-built initramfs with the etc-overlay module to skip dracut regeneration during installation. See [docs/ETC-OVERLAY.md](docs/ETC-OVERLAY.md#building-images-with-pre-included-etc-overlay-module) for details.

### Secure Boot Support

For Secure Boot compatibility, the image should include:

- **shimx64.efi.signed**: The signed shim bootloader (typically from the `shim-signed` package)
- **mmx64.efi**: MOK (Machine Owner Key) manager for key enrollment (optional)

`nbc` automatically detects these files and sets up the proper EFI boot chain:

```
EFI/BOOT/
├── BOOTX64.EFI   ← shimx64.efi (Secure Boot entry point)
├── grubx64.efi   ← actual bootloader (chain-loaded by shim)
├── mmx64.efi     ← MOK manager (for key enrollment)
└── fbx64.efi     ← fallback bootloader (optional)
```

If shim is not found in the image, `nbc` falls back to direct boot (no Secure Boot).

### Example Image Structure

```
/
├── usr/
│   ├── bin/
│   ├── lib/
│   ├── lib64/
│   └── lib/modules/
│       └── 6.11.0-1.el9.x86_64/
│           ├── vmlinuz
│           ├── initramfs.img
│           └── kernel/
├── etc/
│   ├── fstab
│   ├── hostname
│   └── systemd/
├── var/  (will be symlinked to shared partition)
├── home/  (will be symlinked to /var/home)
└── root/  (will be symlinked to /var/roothome)
```

### Building Compatible Images

To create a compatible bootc image, ensure your Containerfile/Dockerfile includes kernel installation:

```dockerfile
FROM quay.io/centos/centos:stream9

# Install kernel and other packages
RUN dnf install -y kernel kernel-modules initramfs-tools

# Kernel and initramfs will be in /usr/lib/modules/$(uname -r)/
# No need to manually move them - nbc handles the extraction
```

## Features

- 🔍 **Disk Discovery**: List and inspect available physical disks
- ✅ **Validation**: Verify disks are suitable for installation
- 🚀 **Automated Installation**: Complete installation workflow with safety checks
- 🔄 **A/B Updates**: Dual root partition system for safe, atomic updates with rollback
- 🔧 **Kernel Arguments**: Support for custom kernel arguments
- 💾 **/etc Overlay Persistence**: User modifications to /etc persist via overlayfs across A/B updates
- 🏷️ **Multiple Device Types**: Supports SATA (sd\*), NVMe (nvme\*), virtio (vd\*), and MMC devices
- 🛡️ **Safety Features**: Confirmation prompts and force flag for automation
- 📝 **Detailed Logging**: Verbose output for troubleshooting
- 🔐 **Configuration Storage**: Stores image reference for easy updates
- 🔒 **Secure Boot Support**: Automatic shim detection and Secure Boot chain setup
- 📀 **Filesystem Choice**: Support for ext4 (default) and btrfs filesystems
- 🔑 **Full Disk Encryption**: LUKS2 encryption with optional TPM2 automatic unlock

## Prerequisites

Before using `nbc`, ensure you have the following installed:

- **sgdisk**: GPT partition table manipulation tool (usually in `gdisk` package)
- **mkfs tools**: `mkfs.vfat`, `mkfs.ext4` for filesystem creation
- **GRUB2**: `grub-install` or `grub2-install` for bootloader installation
- **Root privileges**: Required for disk operations

**Note**: Container image handling is built-in using [go-containerregistry](https://github.com/google/go-containerregistry). No external container runtime (podman/docker) is required!

### System Requirements

- Linux operating system (tested on Fedora, Ubuntu, CentOS Stream)
- x86_64 or ARM64 architecture
- Root/sudo access for disk operations
- Minimum 50GB disk space (43GB for system partitions + space for /var)

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/frostyard/nbc.git
cd nbc

# Build the binary
make build

# Install to system (optional)
sudo make install
```

### From Release

Download the latest release from [GitHub Releases](https://github.com/frostyard/nbc/releases):

```bash
# Download for your architecture
curl -LO https://github.com/frostyard/nbc/releases/latest/download/nbc-linux-amd64

# Make executable
chmod +x nbc-linux-amd64

# Install
sudo mv nbc-linux-amd64 /usr/local/bin/nbc
```

## Usage

### List Available Disks

```bash
# List all available disks
nbc list

# List with verbose output
nbc list -v
```

Example output:

```
Available disks:

Device: /dev/sda
  Size:      238.5 GB (238475288576 bytes)
  Model:     Samsung SSD 850
  Removable: false
  Partitions:
    - /dev/sda1 (512.0 MB) mounted at /boot/efi
    - /dev/sda2 (237.5 GB) mounted at /

Device: /dev/nvme0n1
  Size:      1.0 TB (1000204886016 bytes)
  Model:     Samsung SSD 970 EVO
  Removable: false
  Partitions: none
```

### Validate a Disk

```bash
# Check if a disk is suitable for installation
nbc validate --device /dev/sda

# Or use device aliases
nbc validate -d /dev/disk/by-id/ata-Samsung_SSD_850
```

### Install to Disk

```bash
# Basic installation
nbc install \
  --image quay.io/centos-bootc/centos-bootc:stream9 \
  --device /dev/sda

# With btrfs filesystem instead of ext4
nbc install \
  --image quay.io/centos-bootc/centos-bootc:stream9 \
  --device /dev/sda \
  --filesystem btrfs

# With custom kernel arguments
nbc install \
  --image quay.io/my-org/my-image:latest \
  --device /dev/nvme0n1 \
  --karg console=ttyS0 \
  --karg quiet

# Skip image pull (use already pulled image)
nbc install \
  --image localhost/my-custom-image \
  --device /dev/sda \
  --skip-pull

# Skip confirmation prompt (for automation)
nbc install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --force

# Dry run (test without making changes)
nbc install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --dry-run

# With full disk encryption (LUKS2)
nbc install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --encrypt \
  --passphrase "your-secure-passphrase"

# With encryption + TPM2 automatic unlock
nbc install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --encrypt \
  --passphrase "your-secure-passphrase" \
  --tpm2
```

### Update System

The A/B update system allows you to safely update your system by installing to an inactive root partition.

**The boot device is automatically detected** - no need to specify `--device` when running on the installed system:

```bash
# Update to latest version of the installed image (auto-detects device)
nbc update

# Update to a specific image
nbc update --image quay.io/my-org/my-image:v2.0

# Check if an update is available (without installing)
nbc update --check

# Skip pulling (use already pulled image)
nbc update \
  --image localhost/my-image:latest \
  --skip-pull

# Force update without confirmation
nbc update --force

# Force reinstall even if already up-to-date
nbc update --force

# Add custom kernel arguments for the new system
nbc update \
  --karg console=ttyS0 \
  --karg debug

# Override auto-detection (rarely needed)
nbc update --device /dev/sda
```

The update command automatically compares the installed image digest with the remote image. If they match, the update is skipped (unless `--force` is used).

After update, reboot to activate the new system. The previous version remains available in the boot menu for rollback.

### Check System Status

View the current system status including installed image, digest, and active partition:

```bash
# Show current status
nbc status

# Verbose output (includes update check)
nbc status -v
```

Example output:

```
nbc System Status
====================
Image:        quay.io/centos-bootc/centos-bootc:stream9
Digest:       sha256:abc123de
Device:       /dev/sda
Active Root:  /dev/sda3 (Slot A)
Bootloader:   grub2
```

With verbose mode (`-v`), additional information is shown including install date, kernel arguments, and whether an update is available.

### Download Images for Offline Use

Download container images to a local cache for offline installation or staged updates:

```bash
# Download image for embedding in a live ISO (offline installation)
nbc download --image quay.io/example/myimage:latest --for-install

# Download update for later application (uses image from system config)
nbc download --for-update

# Download a specific update image
nbc download --image quay.io/example/myimage:v2.0 --for-update

# JSON output for scripting
nbc download --image quay.io/example/myimage:latest --for-install --json
```

Staged images are stored in OCI layout format:

- Installation images: `/var/cache/nbc/staged-install/` (supports multiple images)
- Update images: `/var/cache/nbc/staged-update/` (single image)

### Apply Staged Updates

```bash
# Download update without applying
nbc download --for-update

# Later, apply the staged update
nbc update --local-image
```

### Manage Image Cache

```bash
# List staged installation images (JSON for GUI installers)
nbc cache list --install-images --json

# List staged update image
nbc cache list --update-images

# Remove a cached image by digest
nbc cache remove sha256:abc123...

# Clear all staged installation images
nbc cache clear --install

# Clear staged update
nbc cache clear --update
```

### Global Flags

```bash
# Verbose output
nbc install --image IMAGE --device DEVICE -v

# Dry run mode (no actual changes)
nbc install --image IMAGE --device DEVICE --dry-run
```

## How It Works

`nbc` performs a native installation without requiring the `bootc` command. The system is designed with A/B partitioning for safe, atomic updates.

### A/B Partitioning Scheme

`nbc` creates a GPT partition table with dual root partitions for atomic updates:

1. **EFI System Partition** (2GB, FAT32): UEFI boot files and bootloader
2. **Boot Partition** (1GB, ext4): Shared kernel and initramfs files
3. **Root Partition 1** (12GB, ext4): First root filesystem (OS A)
4. **Root Partition 2** (12GB, ext4): Second root filesystem (OS B)
5. **Var Partition** (remaining space, ext4): Shared `/var` for both systems

This layout enables:

- **Atomic Updates**: Install new version to inactive partition without affecting running system
- **Safe Rollback**: Previous system remains bootable via GRUB menu
- **Shared Data**: `/var` partition shared between both systems for persistent data
- **Zero Downtime**: Switch between versions with a simple reboot

### Installation Process

The initial installation follows these steps:

1. **Prerequisites Check**: Verifies required tools (sgdisk, mkfs, grub) are available
2. **Disk Validation**: Ensures the target disk meets requirements (size, not mounted)
3. **Image Pull**: Downloads the container image using built-in Go libraries (unless `--skip-pull` is used)
4. **Confirmation**: Prompts user to confirm data destruction (unless `--force` is used)
5. **Disk Wipe**: Removes existing partition tables and filesystem signatures
6. **Partitioning**: Creates the 5-partition GPT layout
7. **Formatting**: Formats all partitions (FAT32 for EFI, ext4 for others)
8. **Mounting**: Mounts partitions in correct order for extraction
9. **Extraction**: Extracts container filesystem to Root Partition 1
10. **System Setup**: Creates `/var` structure and overlay directories
11. **Dracut Setup**: Installs etc-overlay dracut module and regenerates initramfs
12. **Configuration**: Creates `/etc/fstab`, `/etc/nbc/config.json`
13. **Bootloader Installation**: Installs and configures bootloader with kernel parameters

### Update Process

Updates use the inactive root partition for safe atomic updates:

1. **Active Detection**: Determines which root partition is currently booted
2. **Target Selection**: Selects the inactive partition as update target
3. **Image Pull**: Downloads the new container image (unless `--skip-pull` is used)
4. **Mounting**: Mounts target partition and boot partition
5. **Clearing**: Removes old content from target partition
6. **Extraction**: Extracts new filesystem to target partition
7. **Dracut Setup**: Installs etc-overlay dracut module and regenerates initramfs
8. **Conflict Detection**: Detects files modified by both user and container
9. **Bootloader Update**: Updates bootloader to boot from new partition by default
10. **Dual Boot Menu**: Creates menu entries for both updated and previous systems

After reboot, the system boots from the new partition with overlay `/etc` automatically applied. The old partition remains available for rollback via the boot menu.

### /etc Overlay Persistence

`nbc` uses an overlayfs-based approach to persist user modifications to `/etc` across A/B updates. This is handled by a custom dracut module that runs during early boot.

**How it works:**

1. **Initial Install**: `/etc` from the container image is extracted normally
2. **Early Boot**: A dracut module (`95etc-overlay`) mounts overlayfs over `/etc`
3. **Overlay Structure**:
   - **Lower layer**: Original `/etc` from container (read-only, moved to `/.etc.lower`)
   - **Upper layer**: `/var/lib/nbc/etc-overlay/upper` (writable, stores modifications)
   - **Merged view**: `/etc` (what the system sees)

**Kernel parameters** (automatically configured):

- `rd.etc.overlay=1` - Enable the overlay
- `rd.etc.overlay.var=UUID=xxx` - Location of `/var` partition for overlay storage

**Behavior during updates:**

- New container's `/etc` becomes the new lower layer
- User modifications in overlay upper are automatically merged at boot
- Conflicts are detected and reported (user modifications take precedence)

**Benefits of overlay approach:**

- User modifications persist without explicit merge during updates
- Both A and B root partitions share the same overlay on `/var`
- Clean separation between container configuration and user changes
- Conflict detection warns when container and user both modify the same file

**Detecting nbc-managed boot:**

A marker file `/run/nbc-booted` is created on systems installed with nbc, similar to `/run/ostree-booted` for bootc/ostree systems.

For detailed documentation, see [docs/ETC-OVERLAY.md](docs/ETC-OVERLAY.md).

## System Configuration

After installation, `nbc` writes a configuration file to `/etc/nbc/config.json`:

```json
{
  "image_ref": "quay.io/example/bootc-image:latest",
  "image_digest": "sha256:abc123...",
  "device": "/dev/sda",
  "install_date": "2025-12-16T10:30:00Z",
  "kernel_args": ["console=ttyS0", "quiet"],
  "bootloader_type": "grub2"
}
```

This configuration is automatically used during updates:

- **image_ref**: Used if no `--image` flag is provided
- **image_digest**: Compared with remote digest to detect if update is needed

## Configuration File

Create `~/.nbc.yaml` for user defaults:

```yaml
# Enable verbose logging
verbose: false

# Enable dry-run mode by default
dry-run: false
# Default kernel arguments
# kernel-args:
#   - console=ttyS0
#   - quiet
```

See [.nbc.yaml.example](.nbc.yaml.example) for a complete example.

## Safety Features

- **Unmounted Check**: Refuses to install if any partition is mounted
- **Size Validation**: Ensures disk has minimum 50GB space
- **Confirmation Prompt**: Requires typing "yes" before wiping disk (unless `--force`)
- **Dry Run Mode**: Test operations without making changes
- **Verbose Logging**: Track exactly what's happening
- **A/B Rollback**: Previous system always available in boot menu

## Troubleshooting

### "grub-install or grub2-install not found"

Install GRUB2:

```bash
# Fedora/RHEL/CentOS
sudo dnf install grub2-efi-x64 grub2-tools

# Ubuntu/Debian
sudo apt install grub-efi-amd64 grub2-common
```

### "sgdisk not found"

Install gdisk:

```bash
# Fedora/RHEL/CentOS
sudo dnf install gdisk

# Ubuntu/Debian
sudo apt install gdisk
```

### "podman is not available"

Install podman:

```bash
# Fedora/RHEL/CentOS
sudo dnf install podman

# Ubuntu/Debian
sudo apt install podman
```

### "device does not exist"

Ensure you're using the correct device path. Use `nbc list` to see available devices.

### "partition is mounted"

Unmount all partitions before installation:

```bash
sudo umount /dev/sda1
sudo umount /dev/sda2
# etc...
```

### Permission Denied

Run nbc with sudo:

```bash
sudo nbc install --image IMAGE --device DEVICE
```

## Documentation

- [A/B Updates](docs/AB-UPDATES.md) - Detailed documentation on the A/B update system
- [/etc Overlay Persistence](docs/ETC-OVERLAY.md) - How /etc modifications persist across updates
- [Secure Boot](docs/SECURE-BOOT.md) - Secure Boot chain setup and troubleshooting
- [Encryption](docs/ENCRYPTION.md) - Full disk encryption with LUKS2 and TPM2
- [Incus Integration Tests](docs/INCUS-TESTS.md) - VM-based testing documentation
- [Implementation Details](IMPLEMENTATION.md) - Technical implementation details

## Testing

### Unit Tests

```bash
# Run unit tests (no root required)
make test-unit

# Run linter
make lint
```

### Integration Tests

Integration tests require root privileges to perform disk operations:

```bash
# Run basic integration tests (loop devices)
sudo make test-integration

# Run bootc installation tests
sudo make test-install

# Run A/B update tests
sudo make test-update
```

### Incus VM Tests

For comprehensive end-to-end testing in isolated virtual machines:

```bash
# Install Incus first: https://linuxcontainers.org/incus/docs/main/installing/
# Initialize Incus: incus admin init

# Run full integration tests in Incus VM
sudo make test-incus
```

The Incus test suite:

- Creates an isolated VM with dedicated virtual disk
- Tests complete installation workflow
- Verifies partition layout and bootloader
- Tests A/B update functionality
- Validates kernel/initramfs installation
- Checks GRUB configuration for both boot options
- Automatically cleans up all resources

**Note**: Incus tests take 10-20 minutes depending on network speed and system performance.

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Acknowledgments

- [go-containerregistry](https://github.com/google/go-containerregistry) - Pure Go library for working with container images
- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [Viper](https://github.com/spf13/viper) - Configuration management
- [GRUB2](https://www.gnu.org/software/grub/) - Bootloader

## Related Projects

- [bootc](https://github.com/containers/bootc) - Transactional, in-place operating system updates using OCI/Docker container images
- [go-containerregistry](https://github.com/google/go-containerregistry) - Go library for working with container registries
- [OSTree](https://github.com/ostreedev/ostree) - Operating system and container image management

## Warning

⚠️ **THIS TOOL WILL DESTROY ALL DATA ON THE TARGET DISK** ⚠️

Always double-check the device path before running install commands. Use `--dry-run` to test without making changes.

## Missing / Planned Features

- someone ought to actually test this
- root mount RO
- export container as squashfs/erofs/similar, mount that instead of fs copy
