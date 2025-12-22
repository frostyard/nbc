# Dry-Run Mode

The `--dry-run` (or `-n`) flag allows you to preview what nbc would do without making any actual changes to your system. This is useful for:

- Verifying command syntax and options
- Understanding the installation/update workflow
- Testing automation scripts safely
- Debugging issues without risking data

## Usage

```bash
# Install command
nbc install --image quay.io/example/image:latest --device /dev/sda --dry-run

# Short form
nbc -n install --image quay.io/example/image:latest --device /dev/sda

# Update command
nbc update --image quay.io/example/image:latest --dry-run

# Can be combined with other flags
nbc install \
  --image quay.io/example/image:latest \
  --device /dev/sda \
  --encrypt \
  --passphrase "test" \
  --tpm2 \
  --dry-run
```

## What Gets Skipped in Dry-Run Mode

When `--dry-run` is enabled, the following operations are **not performed**:

### Disk Operations

- ✗ Disk wiping (`sgdisk --zap-all`)
- ✗ Partition creation (`sgdisk`)
- ✗ Filesystem formatting (`mkfs.ext4`, `mkfs.vfat`, `mkfs.btrfs`)
- ✗ Partition mounting/unmounting

### LUKS Encryption

- ✗ LUKS container creation (`cryptsetup luksFormat`)
- ✗ LUKS container opening (`cryptsetup luksOpen`)
- ✗ TPM2 key enrollment (`systemd-cryptenroll`)

### Container Operations

- ✗ Container filesystem extraction
- ✗ File and directory creation on target

### Bootloader

- ✗ GRUB2 installation (`grub-install`)
- ✗ systemd-boot installation (`bootctl`)
- ✗ Kernel and initramfs copying
- ✗ Boot configuration file generation
- ✗ EFI boot entry registration (`efibootmgr`)

### System Configuration

- ✗ `/etc/fstab` generation
- ✗ `/etc/crypttab` generation
- ✗ System directory setup
- ✗ `/etc` persistence configuration

## What Still Happens in Dry-Run Mode

Some read-only operations are still performed:

- ✓ Image reference validation (checks if image exists)
- ✓ Device path resolution
- ✓ Prerequisite tool checks
- ✓ Command-line argument validation
- ✓ Progress messages showing what _would_ happen

## Output Example

```bash
$ nbc install --image localhost/myimage:latest --device /dev/sda --dry-run

Checking prerequisites...
Validating disk /dev/sda...
[DRY RUN] Would pull image: localhost/myimage:latest
[DRY RUN] Would install localhost/myimage:latest to /dev/sda
```

## Implementation Details

The dry-run protection uses an **early-return gate pattern**. High-level functions check the `dryRun` flag at entry and return immediately before performing any modifications:

```go
func (b *BootcInstaller) Install() error {
    if b.DryRun {
        p.MessagePlain("[DRY RUN] Would install %s to %s", b.ImageRef, b.Device)
        return nil
    }
    // ... actual installation code ...
}
```

This pattern ensures that all downstream operations (partition creation, formatting, bootloader installation, etc.) are never reached when dry-run mode is enabled.

### Protected Entry Points

| Function                     | Protection                        |
| ---------------------------- | --------------------------------- |
| `BootcInstaller.Install()`   | Returns before disk operations    |
| `BootcInstaller.PullImage()` | Returns before network operations |
| `BootcInstaller.Verify()`    | Returns before verification       |
| `SystemUpdater.Update()`     | Returns before update operations  |
| `SystemUpdater.PullImage()`  | Returns before network operations |
| `CreatePartitions()`         | Returns mock partition scheme     |
| `FormatPartitions()`         | Skips mkfs commands               |
| `MountPartitions()`          | Skips mount commands              |
| `SetupLUKS()`                | Skips LUKS creation               |
| `WipeDisk()`                 | Skips disk wiping                 |

## Configuration File

You can set dry-run mode as default in your configuration file:

```yaml
# ~/.nbc.yaml
dry-run: true # Always run in dry-run mode unless explicitly disabled
```

To override when dry-run is the default:

```bash
# Currently no --no-dry-run flag, so remove from config to run for real
```

## Testing with Dry-Run

Dry-run mode is also tested in the unit test suite:

```bash
make test-unit
# Includes TestBootcInstaller_DryRun
```
