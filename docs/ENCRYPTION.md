# Full Disk Encryption (FDE) with LUKS2 and TPM2

nbc supports full disk encryption using LUKS2 with optional TPM2 automatic unlock.

## Quick Start

```bash
# Install with encryption (passphrase only)
nbc install --encrypt --passphrase "your-secure-passphrase" ghcr.io/myorg/myimage:latest /dev/sda

# Install with encryption + TPM2 automatic unlock
nbc install --encrypt --passphrase "your-secure-passphrase" --tpm2 ghcr.io/myorg/myimage:latest /dev/sda
```

## How It Works

### Encrypted Partitions

When `--encrypt` is specified, the following partitions are encrypted with LUKS2:

| Partition | Mapper Name         | Description                   |
| --------- | ------------------- | ----------------------------- |
| Root1     | `/dev/mapper/root1` | Active root filesystem        |
| Root2     | `/dev/mapper/root2` | Inactive root for A/B updates |
| Var       | `/dev/mapper/var`   | Persistent `/var` data        |

The ESP (EFI System Partition) and Boot partitions are **not encrypted** to allow the bootloader to load the kernel and initramfs.

### Encryption Flow

1. **Partition Creation**: Standard GPT partitions are created
2. **LUKS Setup**: Each partition (root1, root2, var) is formatted with LUKS2
3. **LUKS Open**: Encrypted containers are opened to `/dev/mapper/<name>`
4. **Filesystem Creation**: ext4 (or btrfs) filesystem is created on the mapper device
5. **Container Extraction**: OS is extracted to the mounted filesystems
6. **Crypttab Generation**: `/etc/crypttab` is generated for boot-time unlock
7. **Bootloader Config**: Kernel arguments are configured for LUKS unlock
8. **TPM2 Enrollment** (optional): TPM2 key is enrolled for automatic unlock

### Kernel Arguments

With encryption enabled, the bootloader is configured with:

```
root=/dev/mapper/root1 rw rd.luks.uuid=<root1-luks-uuid> rd.luks.name=<uuid>=root1 systemd.mount-extra=/dev/mapper/var:/var:ext4:defaults
```

With TPM2 enabled, `rd.luks.options=tpm2-device=auto` is added to enable automatic unlock.

## TPM2 Automatic Unlock

When `--tpm2` is specified:

1. TPM2 key is enrolled using `systemd-cryptenroll`
2. No PCR binding is used (empty PCRs = unlock regardless of boot state)
3. The passphrase remains as a backup unlock method
4. Initramfs automatically uses TPM2 to unlock the root partition

### Why No PCR Binding?

PCR (Platform Configuration Register) binding ties the encryption key to specific boot measurements. While this provides additional security, it can cause lockout when:

- Kernel is updated
- Initramfs is regenerated
- Bootloader configuration changes
- Firmware updates occur

By using empty PCRs (`--tpm2-pcrs=`), the system will unlock as long as:

- The TPM2 chip is present
- The TPM2 state hasn't been reset
- No tampering is detected

The passphrase always works as a fallback.

## Container Image Requirements

Your bootc container image **must** include LUKS and TPM2 support in the initramfs.

### Debian/Ubuntu

```dockerfile
RUN apt-get install -y \
    cryptsetup \
    cryptsetup-initramfs \
    tpm2-tools

# Rebuild initramfs with LUKS support
RUN update-initramfs -u -k all
```

Required packages:

- `cryptsetup` - LUKS userspace tools
- `cryptsetup-initramfs` - LUKS hook for initramfs-tools
- `tpm2-tools` - TPM2 userspace tools (for TPM2 unlock)
- `libtss2-tcti-device0` - TPM2 TCTI library (often auto-installed)

### Fedora/RHEL/CentOS

```dockerfile
RUN dnf install -y \
    cryptsetup \
    tpm2-tools \
    tpm2-tss

# Dracut should auto-include crypt module
RUN dracut --force --regenerate-all
```

Required packages:

- `cryptsetup` - LUKS userspace tools
- `tpm2-tools` - TPM2 userspace tools
- `tpm2-tss` - TPM2 software stack

Dracut modules (should be included automatically):

- `90crypt` - LUKS support
- `91tpm2-tss` - TPM2 support

## Verification

### During Installation

nbc will check for LUKS/TPM2 support after extracting the container and warn if:

- No LUKS initramfs support is detected
- TPM2 is requested but TPM2 initramfs support is not detected

These are warnings, not errors, since detection is best-effort.

### After Boot

Check encryption status:

```bash
# Verify root is on a LUKS device
lsblk -f | grep crypt

# Check LUKS header
cryptsetup luksDump /dev/sdaX

# Verify TPM2 enrollment
systemd-cryptenroll --tpm2-device=list /dev/sdaX
```

## Troubleshooting

### Boot Prompts for Passphrase (TPM2 Not Working)

1. Check if TPM2 is available: `ls /dev/tpm*`
2. Verify TPM2 enrollment: `systemd-cryptenroll /dev/sdaX`
3. Check kernel args include `rd.luks.options=tpm2-device=auto`
4. Ensure initramfs has TPM2 support

### Cannot Boot After Update

If A/B update breaks TPM2 unlock:

1. Enter passphrase at boot prompt
2. Re-enroll TPM2: `systemd-cryptenroll --wipe-slot=tpm2 --tpm2-device=auto /dev/sdaX`

### Emergency Recovery

Boot from live media and:

```bash
# Unlock with passphrase
cryptsetup luksOpen /dev/sdaX root1

# Mount and fix
mount /dev/mapper/root1 /mnt
```

## Security Considerations

1. **Passphrase Strength**: Use a strong passphrase. It's your backup when TPM2 fails.

2. **Physical Access**: Without PCR binding, anyone with physical access to the TPM2 can unlock the system. The security comes from:

   - TPM2 is bound to the specific hardware
   - Removing the disk and attaching to another machine won't work
   - The passphrase is still required without TPM2

3. **Recovery Key**: Consider adding a recovery key:

   ```bash
   systemd-cryptenroll --recovery-key /dev/sdaX
   ```

4. **Remote Unlock**: For servers, consider adding network-based unlock (NBDE/Tang).

## Implementation Details

### Files Created

| File                          | Purpose                           |
| ----------------------------- | --------------------------------- |
| `/etc/crypttab`               | Defines LUKS devices for systemd  |
| `/boot/loader/entries/*.conf` | Boot entry with LUKS kernel args  |
| `/boot/grub/grub.cfg`         | GRUB config with LUKS kernel args |

### LUKS Format Options

nbc uses LUKS2 with default settings:

- `cryptsetup luksFormat --type luks2`
- Default cipher (typically `aes-xts-plain64`)
- Default key size (typically 256-bit)
- Default PBKDF (argon2id)

### Mapper Device Names

| Partition Role   | Mapper Name |
| ---------------- | ----------- |
| Root1 (active)   | `root1`     |
| Root2 (inactive) | `root2`     |
| Var              | `var`       |

These names are used in:

- `/dev/mapper/<name>` paths
- `/etc/crypttab` entries
- Kernel command line arguments

### System Configuration Storage

Encryption configuration is stored in `/var/lib/nbc/state/config.json` to support A/B updates:

```json
{
  "image_ref": "ghcr.io/example/image:latest",
  "device": "/dev/sda",
  "encryption": {
    "enabled": true,
    "tpm2": true,
    "root1_luks_uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "root2_luks_uuid": "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy",
    "var_luks_uuid": "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"
  }
}
```

This configuration is:

- **Created during install**: LUKS UUIDs for all partitions are stored
- **Read during update**: Used to generate correct kernel arguments for each root
- **Essential for A/B updates**: Each root partition has a different LUKS UUID

## A/B Updates with Encryption

When updating an encrypted system, `nbc update` automatically:

1. Reads the encryption config from `/var/lib/nbc/state/config.json`
2. Determines which root partition is active and which is the target
3. Generates kernel command lines with the correct LUKS UUIDs:
   - **Target root**: Uses root1 or root2 LUKS UUID based on which is inactive
   - **Previous root**: Uses the active root's LUKS UUID for rollback
   - **Var partition**: Always uses the same var LUKS UUID

Example kernel command line for encrypted target:

```
root=/dev/mapper/root2 rw rd.luks.uuid=<root2-uuid> rd.luks.name=<root2-uuid>=root2 rd.luks.uuid=<var-uuid> rd.luks.name=<var-uuid>=var rd.luks.options=<root2-uuid>=tpm2-device=auto rd.luks.options=<var-uuid>=tpm2-device=auto systemd.mount-extra=/dev/mapper/var:/var:ext4:defaults
```
