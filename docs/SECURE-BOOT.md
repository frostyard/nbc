# Secure Boot Support in nbc

This document describes how nbc handles Secure Boot for both GRUB2 and systemd-boot bootloaders.

## Overview

Secure Boot is a UEFI feature that ensures only signed bootloaders can execute. The trust chain works as follows:

1. **UEFI Firmware** trusts Microsoft's UEFI CA
2. **Shim** is signed by Microsoft, so firmware trusts it
3. **Shim** contains the distro's key (e.g., Debian, Fedora) and trusts binaries signed by that key
4. **Bootloader** (GRUB or systemd-boot) is signed by the distro, so shim trusts it
5. **Kernel** is either signed or loaded via MOK (Machine Owner Key)

## The Shim Bootloader

Shim is the critical component for Secure Boot. Key facts:

- **Signed by Microsoft**: UEFI firmware trusts it
- **Contains distro key**: Can verify distro-signed binaries
- **Hardcoded to load `grubx64.efi`**: Looks for this file in the same directory
- **Only verifies signature**: Doesn't care what grubx64.efi actually is

This last point is crucial: shim will happily load a signed systemd-boot binary as long as it's named `grubx64.efi` and signed by the distro key.

## EFI Directory Layout

### For systemd-boot (Debian/Ubuntu)

```
/boot/EFI/
├── BOOT/
│   ├── BOOTX64.EFI      ← shimx64.efi.signed (957KB)
│   ├── grubx64.efi      ← systemd-bootx64.efi.signed (125KB) - NOT GRUB!
│   └── mmx64.efi        ← mmx64.efi.signed (850KB, optional)
└── systemd/
    └── systemd-bootx64.efi  ← copy for bootctl discoverability
```

### For GRUB2 (Fedora/RHEL)

```
/boot/EFI/
├── BOOT/
│   ├── BOOTX64.EFI      ← shimx64.efi
│   ├── grubx64.efi      ← signed grubx64.efi from container
│   └── mmx64.efi        ← MOK manager (optional)
└── fedora/              ← or centos, redhat
    └── grub.cfg         ← GRUB configuration
```

## Source Locations for Signed Binaries

### Debian/Ubuntu

| Binary       | Location in Container                                  |
| ------------ | ------------------------------------------------------ |
| Shim         | `/usr/lib/shim/shimx64.efi.signed`                     |
| MOK Manager  | `/usr/lib/shim/mmx64.efi.signed`                       |
| systemd-boot | `/usr/lib/systemd/boot/efi/systemd-bootx64.efi.signed` |
| Fallback     | `/usr/lib/shim/fbx64.efi.signed` (**DO NOT USE**)      |

### Fedora/RHEL/CentOS

| Binary      | Location in Container                |
| ----------- | ------------------------------------ |
| Shim        | `/boot/efi/EFI/{distro}/shimx64.efi` |
| GRUB        | `/boot/efi/EFI/{distro}/grubx64.efi` |
| MOK Manager | `/boot/efi/EFI/{distro}/mmx64.efi`   |

## What NOT to Do

### ❌ Do NOT install fbx64.efi

The fallback bootloader (`fbx64.efi`) causes a "Restore Boot Option" blue screen because:

1. fbx64.efi looks for `EFI/<distro>/BOOTX64.CSV`
2. Our setup uses `EFI/BOOT/` (removable media path)
3. When CSV is not found, fbx64.efi shows the blue screen

We use `efibootmgr` to register boot entries instead.

### ❌ Do NOT use unsigned binaries

- `grub-install` produces **unsigned** GRUB EFI binaries
- These will be rejected by Secure Boot
- Always use the signed binaries from the container image

### ❌ Do NOT use signed systemd-boot directly as BOOTX64.EFI

- Distro signing keys are not in UEFI firmware's trust store
- Only Microsoft-signed binaries (like shim) are trusted directly
- Must go through shim chain

### ❌ Do NOT assume EFI directory case

- Container extraction may create lowercase `efi`
- FAT32 is case-insensitive but case-preserving
- Use two-step rename: `efi` → `efi_tmp` → `EFI`

## Debugging Guide

### "Restore Boot Option" Blue Screen

**Cause**: fbx64.efi is being executed

**Solution**: Remove fbx64.efi from `EFI/BOOT/`

```bash
# From rescue environment
mount /dev/sda1 /mnt
rm /mnt/EFI/BOOT/fbx64.efi
umount /mnt
reboot
```

### "Access Denied" Error

**Cause**: Secure Boot rejected an unsigned or incorrectly-signed binary

**Solutions**:

1. Ensure shim is used as BOOTX64.EFI (not systemd-boot directly)
2. Verify grubx64.efi is the **signed** binary from the container
3. Check that shim and bootloader are from the same distro

### System Boots to Wrong Device

**Cause**: Boot order not set correctly

**Solution**:

```bash
# Check current order
efibootmgr -v

# Set your OS first (replace 0008 with your boot entry)
efibootmgr -o 0008,0002,0003
```

### bootctl Shows "Secure Boot: disabled"

**Possible causes**:

1. Secure Boot not enabled in firmware
2. Booted directly, not through shim

**Verification**:

```bash
mokutil --sb-state  # Should say "SecureBoot enabled"
```

## Investigation History (December 2025)

This section documents the debugging process that led to the current implementation.

### Initial Problem

After installing a Debian system with systemd-boot using nbc, Secure Boot failed with various symptoms including "Restore Boot Option" blue screen.

### Failed Attempt 1: Using unsigned bootloaders

- Tried using `grub-install` output directly
- Result: Secure Boot rejected the unsigned binary

### Failed Attempt 2: Signed systemd-boot as BOOTX64.EFI

- Assumption: Distro-signed binaries are in Microsoft's trust chain
- Reality: They're not - only shim is Microsoft-signed
- Result: "Access Denied"

### Failed Attempt 3: Shim + systemd-boot + fbx64.efi

- Included fbx64.efi thinking it was needed for boot recovery
- Result: "Restore Boot Option" blue screen
- Root cause: fbx64.efi couldn't find BOOTX64.CSV

### Failed Attempt 4: Incorrect assumption about systemd-boot path

- Assumed systemd-boot couldn't work as grubx64.efi because it "needs to know its path"
- This was wrong - systemd-boot finds loader.conf relative to ESP root
- Wasted time trying alternative approaches

### Working Solution

1. **Shim as BOOTX64.EFI** - Microsoft-signed, trusted by firmware
2. **Signed systemd-boot as grubx64.efi** - Distro-signed, trusted by shim
3. **MOK manager** - For key enrollment if needed
4. **NO fbx64.efi** - Causes blue screen

### Key Insight

Shim doesn't care what `grubx64.efi` actually is. It only:

1. Looks for a file named `grubx64.efi` in the same directory
2. Verifies the signature against the embedded distro key
3. Loads and executes it if signature is valid

This means systemd-boot works perfectly as `grubx64.efi` because:

- It's signed by Debian's key
- Shim trusts Debian's key
- systemd-boot doesn't care what filename it's loaded as

## Testing Secure Boot

### Create a Secure Boot VM with Incus

```bash
# Create VM with Secure Boot enabled
incus launch images:debian/trixie myvm --vm \
  -c security.secureboot=true

# Attach install ISO
incus config device add myvm iso disk \
  source=/path/to/installer.iso boot.priority=10

# Start and connect
incus start myvm
incus console myvm
```

### Verify Secure Boot Status

```bash
# Inside the running system
mokutil --sb-state
# Should output: SecureBoot enabled

bootctl status
# Should show: Secure Boot: enabled (user)
```

### Verify Correct Binaries

```bash
# Check file sizes to identify binaries
ls -la /boot/EFI/BOOT/

# Expected:
# BOOTX64.EFI ~957KB (shim)
# grubx64.efi ~125KB (systemd-boot) or ~2MB (GRUB)
# mmx64.efi   ~850KB (MOK manager)
```

## Related Files

- `pkg/bootloader.go` - Main bootloader installation code
- `setupSystemdBootSecureBootChain()` - systemd-boot Secure Boot setup
- `setupSecureBootChain()` - GRUB2 Secure Boot setup
- `findShimEFI()`, `findSignedSystemdBootEFI()`, etc. - Binary locators
