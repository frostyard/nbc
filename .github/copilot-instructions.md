# Copilot Instructions for nbc

## Project Overview

nbc is a Go-based tool for installing bootc (bootable container) systems to disk. It handles:

- Container image extraction to disk
- Partition scheme creation with A/B update support
- Bootloader installation (GRUB2 and systemd-boot)
- System configuration for immutable/atomic OS deployments

## CRITICAL: Complete All Changes Before Finishing

**IMPORTANT**: Before marking any task as complete or ending your turn:

1. **Run `make fmt`** - Format all code
2. **Run `make lint`** - Check for linting issues and fix them
3. **Run appropriate tests**:
   - `make test-unit` for unit tests (fast, during development)
   - `make test-integration` for integration tests (before committing)
   - `make test` to run all tests
4. **Verify all tests pass** - Do not complete with failing tests
5. **Fix any issues** found by the above steps

**Never complete a feature or change without running these checks.** This ensures code quality and prevents breaking changes from being committed.

## Critical: Install and Update Parity

**IMPORTANT**: nbc has TWO code paths that must stay in sync:

1. **Installation** (`pkg/bootc.go` - `Install()` function)
2. **A/B Updates** (`pkg/update.go` - `Update()` function)

**Any change to the installation flow MUST also be applied to the update flow.**

Examples of things that must be kept in sync:

- Kernel command line parameters (e.g., `rd.etc.overlay=1`)
- System configuration files (e.g., tmpfiles.d configs)
- Bootloader entry generation
- Filesystem setup and directory creation

When modifying installation logic, always search for the equivalent code in `update.go` and update it accordingly. The `buildKernelCmdline()` function in `update.go` must match the kernel cmdline logic in `bootloader.go`.

## Critical: Use Makefile for Development Tasks

**IMPORTANT**: Always use the Makefile targets for development tasks. Do NOT run `go test`, `go fmt`, or linter commands directly.

### Required Makefile Targets

- **`make fmt`**: Format all Go code (ALWAYS run before committing)
- **`make lint`**: Run linter checks (ALWAYS run before committing)
- **`make test`**: Run all tests
- **`make test-unit`**: Run only unit tests (no root required)
- **`make test-integration`**: Run integration tests (requires root, handles sudo automatically)
- **`make test-all`**: Run unit tests followed by integration tests
- **`make build`**: Build the binary

### Testing Guidelines

When implementing or fixing features:

1. Write unit tests in `*_test.go` files alongside the code
2. Add integration tests for disk/partition operations in `pkg/integration_test.go`
3. Update Incus tests in `test_incus*.sh` for end-to-end validation
4. Run `make test-unit` during development (fast, no root needed)
5. Run `make test-integration` before committing (slower, requires root)
6. Run `make lint` and `make fmt` before every commit

**Never** run `go test ./...` directly - use `make test` or specific targets like `make test-unit`.

## Critical: Run Linter and Formatter Before Committing

**IMPORTANT**: Always run BOTH commands before committing changes:

```bash
make fmt   # Format code
make lint  # Check for issues
```

The linter enforces Go best practices including:

- Error strings should not end with punctuation or newlines (ST1005)
- Standard Go formatting
- Other staticcheck rules

Fix all linter issues before creating commits.

## Code Style & Conventions

### General Go Practices

- Follow standard Go formatting (use `gofmt`)
- **Prefer Go standard library over custom implementations** - use `strings.Contains()` instead of custom helper functions
- Use meaningful variable names (avoid single-letter variables except in short loops)
- Add error context with `fmt.Errorf("context: %w", err)` for error wrapping
- Error strings should not end with punctuation or newlines
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
- follow the existing patterns for json output if applicable

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

**IMPORTANT**: This section documents extensive debugging that was required to get Secure Boot
working correctly with systemd-boot. Read this carefully before making changes.

#### How Shim Works

Shim is the first-stage bootloader signed by Microsoft's UEFI CA. It's trusted by all Secure Boot
firmware. Shim then loads and verifies a second-stage bootloader using the **distro's key**
embedded in shim (not Microsoft's key).

Key facts about shim:

1. Shim is **hardcoded** to look for `grubx64.efi` in the same directory
2. Shim **only verifies the signature**, not the binary type - it doesn't care if grubx64.efi is actually GRUB
3. Any binary signed by the distro's key (embedded in shim) will be trusted and loaded
4. Shim is signed by Microsoft, so UEFI firmware trusts it
5. Signed bootloaders (GRUB, systemd-boot) are signed by the distro, so shim trusts them

#### Working Configuration for systemd-boot

```
EFI/BOOT/
├── BOOTX64.EFI   ← shimx64.efi.signed (957KB, Secure Boot entry point)
├── grubx64.efi   ← systemd-bootx64.efi.signed (125KB, chain-loaded by shim)
└── mmx64.efi     ← mmx64.efi.signed (850KB, MOK manager, optional)

EFI/systemd/
└── systemd-bootx64.efi  ← copy of signed systemd-boot (for bootctl discoverability)
```

**CRITICAL: Do NOT include fbx64.efi** - see "Failed Attempts" below.

#### Working Configuration for GRUB2

```
EFI/BOOT/
├── BOOTX64.EFI   ← shimx64.efi.signed (Secure Boot entry point)
├── grubx64.efi   ← signed grubx64.efi from container (chain-loaded by shim)
├── mmx64.efi     ← MOK manager (optional)
└── fbx64.efi     ← fallback (optional, but generally avoid)
```

#### File Locations in Container Images

**Debian/Ubuntu:**

- Shim: `/usr/lib/shim/shimx64.efi.signed`
- MOK manager: `/usr/lib/shim/mmx64.efi.signed`
- Fallback: `/usr/lib/shim/fbx64.efi.signed` (DO NOT USE)
- Signed systemd-boot: `/usr/lib/systemd/boot/efi/systemd-bootx64.efi.signed`
- CSV file: `/usr/lib/shim/BOOTX64.CSV` (for fbx64.efi, not needed)

**Fedora/RHEL/CentOS:**

- Shim: `/boot/efi/EFI/{fedora,centos,redhat}/shimx64.efi`
- Signed GRUB: `/boot/efi/EFI/{fedora,centos,redhat}/grubx64.efi`

#### Failed Attempts (DO NOT REPEAT)

1. **Using unsigned systemd-boot directly as BOOTX64.EFI**

   - Result: "Access Denied" - UEFI Secure Boot rejected unsigned binary
   - Lesson: UEFI firmware only trusts Microsoft-signed binaries (like shim)

2. **Using signed systemd-boot directly as BOOTX64.EFI (no shim)**

   - Result: "Access Denied" - Debian's key is not in QEMU/OVMF's default trust store
   - Lesson: Distro keys are trusted by shim, not by firmware directly

3. **Shim + systemd-boot as grubx64.efi + fbx64.efi (fallback)**

   - Result: "Restore Boot Option" blue screen from fbx64.efi
   - Lesson: fbx64.efi looks for `EFI/<distro>/BOOTX64.CSV` but we use `EFI/BOOT/`
   - The fallback mechanism expects the distro-specific directory structure

4. **Shim + fbx64.efi fallback mechanism with BOOTX64.CSV**

   - Result: Still "Restore Boot Option" blue screen
   - Lesson: fbx64.efi is designed for distro installers, not custom boot setups

5. **Wrong assumption: "systemd-boot needs to know its path"**
   - We initially thought systemd-boot couldn't be loaded as grubx64.efi
   - Actually works fine - systemd-boot finds loader.conf relative to ESP root

#### Why fbx64.efi Causes Problems

fbx64.efi (fallback bootloader) is designed for a very specific use case:

1. Distro installer copies files to `EFI/<distro>/` directory
2. Installer creates `EFI/<distro>/BOOTX64.CSV` with boot entry info
3. fbx64.efi reads CSV and registers the boot entry in UEFI NVRAM

Our setup uses `EFI/BOOT/` (the removable media fallback path), not `EFI/<distro>/`.
When fbx64.efi can't find the CSV file, it shows the "Restore Boot Option" blue screen.

**Solution**: Simply don't install fbx64.efi. We use efibootmgr to register boot entries instead.

#### FAT32 Case Sensitivity

FAT32 is case-insensitive but case-preserving. This causes issues:

- Container extraction may create lowercase `efi` directory
- UEFI specification requires uppercase `EFI`
- `os.Stat("EFI")` succeeds even when stored as `efi` (case-insensitive match)
- Direct `os.Rename("efi", "EFI")` is a no-op on FAT32

**Solution**: Use two-step rename to force case change:

```go
os.Rename("efi", "efi_rename_tmp")
os.Rename("efi_rename_tmp", "EFI")
```

This is implemented in `ensureUppercaseEFIDirectory()`.

#### Testing Secure Boot

1. Create Incus VM with Secure Boot enabled:

   ```bash
   incus launch images:debian/trixie titanoboa --vm \
     -c security.secureboot=true
   ```

2. Check Secure Boot status inside VM:

   ```bash
   mokutil --sb-state
   ```

3. After installation, verify boot chain:
   ```bash
   bootctl status  # Shows "Secure Boot: enabled"
   ```

#### Debugging Boot Failures

If you get a blue screen with "Restore Boot Option":

1. **fbx64.efi is being executed** - remove it from EFI/BOOT/
2. Check that grubx64.efi exists and is the signed bootloader

If you get "Access Denied":

1. **Secure Boot rejected the binary** - use signed binaries
2. For systemd-boot: must go through shim, not directly as BOOTX64.EFI

If the system boots to ISO instead of installed OS:

1. Check boot order: `efibootmgr -v`
2. Set correct order: `efibootmgr -o 0008,0002,...` (your boot entry first)

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
