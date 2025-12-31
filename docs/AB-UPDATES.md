# A/B Update System

## Overview

The `nbc update` command implements an A/B (dual root) update system that enables safe, atomic system updates with automatic rollback capability.

## How It Works

### Partition Layout

```
/dev/sdX1 - EFI (2GB)          - Boot files
/dev/sdX2 - Boot (1GB)         - Kernels and initramfs
/dev/sdX3 - Root1 (12GB)       - Primary root filesystem
/dev/sdX4 - Root2 (12GB)       - Secondary root filesystem
/dev/sdX5 - Var (remaining)    - Shared /var data
```

### Update Process

1. **Detect Active Partition**

   - Reads `/proc/cmdline` to determine which root partition is currently booted
   - Identifies the inactive partition as the update target

2. **Check If Update Needed**

   - Compares installed image digest with remote image digest
   - Skips update if digests match (system is up-to-date)
   - Use `--force` to reinstall even if up-to-date

3. **Pull New Image**

   - Downloads the latest container image using go-containerregistry
   - Alternatively, uses a pre-staged image from local cache (see `nbc download --for-update`)

4. **Extract to Inactive Partition**

   - Mounts the inactive root partition
   - Clears existing content
   - Extracts new container filesystem

5. **Update Bootloader**

   - Updates GRUB configuration to boot from the new partition
   - Sets new partition as default boot option
   - Keeps old partition as fallback in boot menu

6. **Reboot to Activate**
   - Next boot uses the updated partition
   - Previous partition remains available for rollback

## Usage

### Check for Updates

```bash
# Check if an update is available without installing
sudo nbc update --check
```

### Basic Update

```bash
sudo nbc update \
  --image quay.io/example/myimage:latest
```

The device is automatically detected from the running system. Override with `--device` if needed.

### Update with Custom Kernel Arguments

```bash
sudo nbc update \
  --image localhost/custom-image \
  --karg console=ttyS0 \
  --karg quiet
```

### Skip Image Pull (Use Cached)

```bash
sudo nbc update \
  --image localhost/myimage \
  --skip-pull
```

### Staged Updates (Download Now, Apply Later)

Download an update image without applying it:

```bash
# Download update using system's configured image
sudo nbc download --for-update

# Or specify a different image
sudo nbc download --for-update --image quay.io/example/myimage:v2.0
```

Apply the staged update later:

```bash
# Apply the previously downloaded update
sudo nbc update --local-image
```

Check staged update status:

```bash
nbc status --json
```

The staged update cache is automatically cleared after successful application.

### Dry Run Test

```bash
nbc update \
  --image test/image \
  --dry-run
```

## Boot Menu Options

After an update, the GRUB boot menu provides two options:

```
1. Linux (Updated)    - New system (default)
2. Linux (Previous)   - Previous system (rollback)
```

## Rollback Procedure

If the new system has issues, you can rollback by:

1. **At Boot Time**: Select "Linux (Previous)" from GRUB menu
2. **After Booting**: Run another update to switch back

## File Organization

### Implementation Files

- **[pkg/update.go](../pkg/update.go)** - Update logic and A/B switching

  - `GetActiveRootPartition()` - Detects current root
  - `GetInactiveRootPartition()` - Finds update target
  - `SystemUpdater` - Main update orchestrator
  - `UpdateBootloader()` - GRUB configuration updates

- **[cmd/update.go](../cmd/update.go)** - CLI command interface

### Key Functions

#### Active Partition Detection

```go
func GetActiveRootPartition() (string, error)
```

Reads `/proc/cmdline` to determine which root partition the system booted from.

#### Inactive Partition Selection

```go
func GetInactiveRootPartition(scheme *PartitionScheme) (string, bool, error)
```

Returns the partition that should receive the update.

#### Update Execution

```go
func (u *SystemUpdater) Update() error
```

Performs the complete update:

1. Mount target partition
2. Clear old content
3. Extract new filesystem
4. Update bootloader configuration

## Advantages

✅ **Atomic Updates**: Either fully succeeds or fully fails - no partial states
✅ **Zero Downtime**: System remains operational during update
✅ **Instant Rollback**: Switch back to previous version at boot time
✅ **Safe Testing**: New system can be tested before commitment
✅ **Shared Data**: /var partition is shared, preserving application data

## Safety Features

- **Confirmation Prompt**: Requires explicit "yes" to proceed
- **Dry Run Mode**: Test without making changes
- **Separate Partitions**: Update failure doesn't affect running system
- **Boot Menu**: Always provides fallback option
- **Data Preservation**: /var remains untouched during updates

## Update Workflow Example

```bash
# Initial installation (creates both root partitions)
sudo nbc install \
  --image myimage:v1.0 \
  --device /dev/sda

# Reboot into root1 (partition 3)

# First update (writes to root2 - partition 4)
sudo nbc update \
  --image myimage:v1.1 \
  --device /dev/sda

# Reboot into root2 (partition 4)
# root1 still has v1.0 for rollback

# Second update (writes back to root1 - partition 3)
sudo nbc update \
  --image myimage:v1.2 \
  --device /dev/sda

# Reboot into root1 (partition 3)
# root2 still has v1.1 for rollback
```

## Technical Details

### Partition State Detection

The system determines the active partition by parsing kernel command line:

```bash
$ cat /proc/cmdline
BOOT_IMAGE=/vmlinuz-6.5.0 root=UUID=xxx-xxx-xxx ro console=tty0
```

The UUID is matched against partition UUIDs to identify which root is active.

### GRUB Configuration

After update, GRUB config provides both options:

```grub
set timeout=5
set default=0

menuentry 'Linux (Updated)' {
    linux /vmlinuz-6.5.0 root=UUID=new-uuid ro console=tty0
    initrd /initramfs-6.5.0.img
}

menuentry 'Linux (Previous)' {
    linux /vmlinuz-6.5.0 root=UUID=old-uuid ro console=tty0
    initrd /initramfs-6.5.0.img
}
```

### Shared /var Partition

Both root filesystems mount the same /var partition, ensuring:

- Application data persists across updates
- Logs are maintained
- Databases remain accessible
- Configuration in /var is preserved

### Encryption Support

A/B updates fully support LUKS-encrypted systems. The encryption configuration is stored in `/var/lib/nbc/state/config.json` during installation and automatically loaded during updates.

For encrypted systems, the update process:

1. **Reads encryption config** from the system config file
2. **Generates correct LUKS kernel arguments** for each root partition:
   - `rd.luks.uuid` - LUKS container UUID
   - `rd.luks.name` - Mapper device name (root1, root2, or var)
   - `rd.luks.options` - TPM2 unlock options (if enabled)
3. **Creates separate boot entries** with the appropriate LUKS UUIDs for:
   - Target partition (the one being updated)
   - Active partition (for rollback)

The system config stores LUKS UUIDs for all partitions:

```json
{
  "encryption": {
    "enabled": true,
    "tpm2": true,
    "root1_luks_uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "root2_luks_uuid": "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy",
    "var_luks_uuid": "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"
  }
}
```

This ensures that after an update, both boot entries (new and previous) have the correct LUKS kernel arguments for their respective root partitions.

## Comparison to Other Systems

### vs Traditional Package Updates

- ✅ Atomic (all-or-nothing)
- ✅ Instant rollback
- ✅ No package conflicts

### vs OSTree/rpm-ostree

- ✅ Simpler implementation
- ✅ Standard ext4 filesystems
- ⚠️ Larger disk space requirement

### vs Docker/Kubernetes Updates

- ✅ Full system updates (not just containers)
- ✅ Includes kernel and bootloader
- ✅ Bare metal support

## Future Enhancements

Potential improvements:

- [ ] Automatic health checks before switching
- [ ] Scheduled updates
- [ ] Multi-version retention (keep more than 2 versions)
- [ ] Delta updates (only transfer changes)
- [ ] Automatic rollback on boot failure
- [ ] Status command to show active partition
- [ ] Update history tracking
