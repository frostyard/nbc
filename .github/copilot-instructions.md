# Copilot Instructions for nbc

## Project Overview

nbc is a Go-based tool for installing bootc (bootable container) systems to disk. It handles:

- Container image extraction to disk
- Partition scheme creation with A/B update support
- Bootloader installation (GRUB2 and systemd-boot)
- System configuration for immutable/atomic OS deployments

## Code Style & Conventions

### General Go Practices

- Follow standard Go formatting (use `gofmt`)
- Use meaningful variable names (avoid single-letter variables except in short loops)
- Add error context with `fmt.Errorf("context: %w", err)` for error wrapping
- Prefer explicit error handling over panics
- Use structured logging with clear context

### Project-Specific Patterns

#### Error Handling

```go
// Always wrap errors with context
if err != nil {
    return fmt.Errorf("failed to <action>: %w", err)
}
```

#### Console Output

- Use `fmt.Printf()` for user-facing messages
- Indent sub-operations with " " (two spaces)
- Example:
  ```go
  fmt.Println("Installing bootloader...")
  fmt.Println("  Installing GRUB2...")
  fmt.Println("  GRUB2 installation complete")
  ```

#### File Paths

- Always use `filepath.Join()` for path construction
- Never use string concatenation for paths
- Use absolute paths for critical operations

## Architecture Patterns

### Partition Scheme

- The project uses an A/B partition scheme for atomic updates
- Root partitions: Root1 (active) and Root2 (inactive)
- Separate partitions for: ESP, Boot (XBOOTLDR), Var
- Partition type GUIDs follow systemd Discoverable Partitions Specification

### Bootloader Support

- Two bootloader types: `BootloaderGRUB2` and `BootloaderSystemdBoot`
- Bootloader detection is automatic based on container contents
- Kernel and initramfs are copied from `/usr/lib/modules/` to appropriate boot partition
- **Secure Boot**: Automatic shim detection sets up boot chain (shim → bootloader)

### Secure Boot Chain

**IMPORTANT**: Shim is compiled to trust specific bootloader binaries signed by the distro's key.
You must use the **signed** binaries from the container image, not unsigned binaries from `grub-install`.

Shim is hardcoded to look for `grubx64.efi` in the same directory. However, shim verifies signatures
using the distro key embedded in it. Any binary signed by that key will be trusted and loaded.

**For GRUB2**: shimx64.efi → grubx64.efi (signed GRUB from container)

```
EFI/BOOT/
├── BOOTX64.EFI   ← shimx64.efi (Secure Boot entry point)
├── grubx64.efi   ← signed GRUB from container (chain-loaded by shim)
├── mmx64.efi     ← MOK manager (optional)
└── fbx64.efi     ← fallback (optional)
```

**For systemd-boot (Debian/Ubuntu)**: shimx64.efi → grubx64.efi (actually signed systemd-boot!)

Since shim looks for `grubx64.efi` but only verifies the signature (not the actual content),
we copy the **signed systemd-boot** as `grubx64.efi`. Shim loads it because it's signed by
the same distro key that shim trusts.

```
EFI/BOOT/
├── BOOTX64.EFI   ← shimx64.efi (Secure Boot entry point)
├── grubx64.efi   ← signed systemd-boot (renamed! chain-loaded by shim)
├── mmx64.efi     ← MOK manager (optional)
└── fbx64.efi     ← fallback (optional)
EFI/systemd/
└── systemd-bootx64.efi  ← copy of signed systemd-boot (for discoverability)
```

Shim locations searched:

- `/boot/efi/EFI/{fedora,centos,redhat,debian,ubuntu}/shimx64.efi`
- `/usr/lib{,64}/shim/shimx64.efi.signed`
- `/usr/share/shim/shimx64.efi.signed`

Signed bootloader locations:

- GRUB: `/boot/efi/EFI/{fedora,centos,redhat}/grubx64.efi`
- systemd-boot: `/usr/lib/systemd/boot/efi/systemd-bootx64.efi.signed`

### Container Extraction

- Uses `go-containerregistry` library for OCI image handling
- Handles tar layer extraction with proper overlay filesystem semantics
- Processes whiteout files (`.wh.` prefix) for deletion markers
- Preserves file permissions including SUID/SGID/sticky bits

## Critical Technical Details

### systemd.mount-extra Format

**IMPORTANT**: The `systemd.mount-extra` kernel parameter follows this format:

```
systemd.mount-extra=DEVICE:MOUNTPOINT:FSTYPE:OPTIONS
```

Example: `systemd.mount-extra=UUID=abc123:/var:ext4:defaults`

**Incorrect**: `systemd.mount-extra=/var:UUID=abc123:ext4:defaults` (mount point and device are reversed)

### Partition Type GUIDs

Use standard systemd Discoverable Partitions GUIDs:

- Root (x86-64): `4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709`
- Boot (XBOOTLDR): `BC13C2FF-59E6-4262-A352-B275FD6F7172`
- ESP: `C12A7328-F81F-11D2-BA4B-00A0C93EC93B`
- Var: `4D21B016-B534-45C2-A9FB-5C16E091FD2D`

### Mount Automation

The system relies on systemd auto-discovery:

- Root: Specified via kernel cmdline `root=UUID=...`
- `/boot`: Auto-mounted by systemd (XBOOTLDR partition type)
- `/boot/efi`: Auto-mounted by systemd (ESP partition type)
- `/var`: Mounted via kernel cmdline `systemd.mount-extra` parameter

## Testing Considerations

### Disk Operations

- All disk operations should check for existing partitions
- Use `--force` flag pattern for destructive operations
- Always verify UUIDs can be retrieved after partition creation
- Test with both GRUB2 and systemd-boot bootloaders

### Container Extraction

- Test with multi-layer images
- Verify whiteout handling (file deletions)
- Check SUID/SGID bit preservation
- Ensure symlinks are created correctly

## Common Gotchas

1. **Bootloader paths differ**: systemd-boot uses `/boot/efi` (ESP) for kernels, GRUB uses `/boot`
2. **Partition sync**: After creating partitions, call `partprobe` or use `BLKRRPART` ioctl
3. **UUID timing**: UUIDs may not be immediately available after partition creation
4. **Chroot mounts**: Always clean up bind mounts in defer statements
5. **File permissions**: Set ownership before setting SUID/SGID bits (ownership clears them)

## Dependencies

### External Commands

The project shells out to these system commands:

- `sgdisk` - GPT partition manipulation
- `mkfs.ext4`, `mkfs.fat` - Filesystem creation
- `grub-install` or `grub2-install` - GRUB bootloader
- `bootctl` - systemd-boot bootloader
- `blkid` - UUID retrieval
- `mount`, `umount` - Filesystem mounting
- `chroot` - Change root for post-install operations

### Go Libraries

- `github.com/google/go-containerregistry` - Container image handling
- Standard library: `os`, `os/exec`, `path/filepath`, `archive/tar`

## Documentation

- Keep README.md updated with usage examples
- Document new partition schemes in IMPLEMENTATION.md
- Update TESTING.md with new test scenarios
- Add integration test notes to INCUS-TESTS.md

## Security Considerations

- Validate all user input paths (prevent path traversal)
- Check file permissions when extracting containers
- Verify partition operations target correct devices
- Use `--removable` flag for GRUB to ensure boot compatibility
