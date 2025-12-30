# /etc Overlay Persistence

## Overview

`nbc` implements an overlay-based persistence mechanism for `/etc` that allows user modifications to persist across A/B system updates while keeping the base `/etc` from the container image intact.

This approach uses Linux's overlayfs filesystem to layer a writable directory on top of the read-only container `/etc`, providing:

- **Persistent user modifications**: Changes to `/etc` survive reboots and A/B updates
- **Clean update path**: New container images bring updated `/etc` without overwriting user changes
- **Conflict detection**: Warns when both user and container have modified the same file
- **Automatic setup**: Works transparently after installation

## How It Works

### Overlay Structure

```text
/etc (merged view seen by the system)
├── lowerdir: /.etc.lower (original /etc from container, read-only)
├── upperdir: /var/lib/nbc/etc-overlay/upper (user modifications, writable)
└── workdir: /var/lib/nbc/etc-overlay/work (overlayfs internal)
```

When the system boots:

1. **Installation/Update**: `nbc` populates `/.etc.lower` with the container's `/etc`
2. **Dracut module** (`95etc-overlay`) runs during initramfs, before pivot_root
3. Overlayfs mounts over `/etc` with `/.etc.lower` as the lower layer
4. User writes go to `/var/lib/nbc/etc-overlay/upper`
5. Reads see merged view: user modifications overlay container files
6. `/.etc.lower` is hidden by a tmpfs mount to prevent accidental modification

### Kernel Parameters

The overlay is controlled by kernel command-line parameters:

| Parameter                     | Description                              |
| ----------------------------- | ---------------------------------------- |
| `rd.etc.overlay=1`            | Enable /etc overlay (required)           |
| `rd.etc.overlay.var=UUID=xxx` | /var partition UUID for overlay storage  |
| `rd.etc.overlay.var=/dev/xxx` | /var partition device path (alternative) |

These are automatically added to the bootloader configuration during installation.

## Directory Layout

After installation, the following directories are created on the `/var` partition:

```text
/var/lib/nbc/
├── etc-overlay/
│   ├── upper/    # User modifications to /etc (overlayfs upperdir)
│   └── work/     # Overlayfs workdir (internal use)
└── etc.pristine/ # Snapshot of original /etc at install time
```

## Implementation Details

### Dracut Module

The overlay is set up by a custom dracut module at `/usr/lib/dracut/modules.d/95etc-overlay/`:

- **`module-setup.sh`**: Declares dependencies and installs the hook
- **`etc-overlay-mount.sh`**: Pre-pivot hook that mounts the overlay

The module runs in the `pre-pivot` phase, after the root filesystem is mounted but before systemd takes over. This timing is critical because:

1. `/var` must be mounted first (for overlay storage)
2. `/etc` must not be in use yet (allows moving it to lower layer)
3. Runs before dbus-broker/systemd services need `/etc`

### Installation Process

During `nbc install`:

1. Extract container filesystem to root partition
2. Install dracut module to `/usr/lib/dracut/modules.d/95etc-overlay/`
3. Regenerate initramfs to include the module
4. Create overlay directories on `/var` partition
5. Save pristine `/etc` snapshot for conflict detection
6. Configure bootloader with `rd.etc.overlay=1` kernel parameter

### Update Process

During `nbc update`:

1. Install new container to inactive root partition
2. Install dracut module and regenerate initramfs
3. Overlay directories already exist on shared `/var`
4. Detect conflicts between user modifications and container changes
5. Configure bootloader for new root with overlay parameters

User modifications in `/var/lib/nbc/etc-overlay/upper` automatically apply to the new root's `/etc` when overlay mounts at boot.

### Runtime Detection

A marker file at `/run/nbc-booted` indicates the system was installed and booted via nbc. This is similar to `/run/ostree-booted` created by bootc/ostree systems.

The marker is created by `systemd-tmpfiles` during boot via `/usr/lib/tmpfiles.d/nbc.conf`. This ensures the marker exists after systemd mounts a fresh tmpfs on `/run` following switch_root.

#### Shell Script Detection

```bash
if [ -f /run/nbc-booted ]; then
    echo "Running on nbc-managed system"
fi
```

#### Go Code Detection

The `pkg` package provides a helper function:

```go
import "github.com/frostyard/nbc/pkg"

if pkg.IsNBCBooted() {
    // System was booted via nbc
    fmt.Println("nbc-managed system detected")
}
```

#### Programmatic Use Cases

- Conditionally enable/disable features based on boot method
- Detect whether A/B updates are available
- Validate that nbc dracut module ran successfully
- System inventory and management tools

### Conflict Detection

When updating, `nbc` compares:

- Files in overlay upper (user-modified files)
- Files in new container's `/etc`
- Files in pristine `/etc` snapshot from previous installation

A conflict is detected when:

- A file exists in the overlay upper (user modified it), AND
- The same file differs between pristine and new container (container updated it)

Conflicts are reported but **user modifications take precedence**. The overlay upper layer always wins, meaning container updates to conflicting files are hidden.

```text
Warning: Potential conflicts detected (files modified by both user and update):
  ! resolv.conf
  ! hostname
User modifications in overlay will take precedence over container changes.
```

## Requirements

### Container Image Requirements

- **dracut**: Must be installed at `/usr/bin/dracut` or `/sbin/dracut`
- **overlay kernel module**: Modern Linux kernels include this by default

### Filesystem Requirements

- `/var` partition must be mountable during initramfs (before pivot_root)
- Supports ext4 and btrfs filesystems for `/var`

## Building Images with Pre-included etc-overlay Module

Container image authors can include the etc-overlay dracut module and pre-build the initramfs in their images. This speeds up installation and updates by skipping the initramfs regeneration step.

### Benefits

- **Faster installation**: No need to run dracut during install/update
- **Smaller runtime footprint**: No need to include dracut in the final image (if pre-built)
- **Reproducible builds**: Initramfs is built once in CI, not on each target machine
- **Offline installation**: No dracut execution required on the target system

### Option 1: Install nbc Package (Recommended)

The simplest approach is to install the `nbc` package in your container image. This installs the dracut module and regenerates the initramfs automatically.

```dockerfile
FROM ghcr.io/frostyard/debian-bootc:trixie

# Install nbc - this installs the dracut module to
# /usr/lib/dracut/modules.d/95etc-overlay/
RUN apt-get update && apt-get install -y nbc

# Regenerate initramfs for all kernels with etc-overlay included
RUN for kver in /usr/lib/modules/*/; do \
      kver=$(basename "$kver"); \
      dracut --force --add etc-overlay \
        /usr/lib/modules/$kver/initramfs.img $kver; \
    done

# Optional: Remove dracut to reduce image size (initramfs already built)
# Only do this if you won't need to regenerate initramfs later
# RUN apt-get remove -y dracut-core && apt-get autoremove -y
```

### Option 2: Copy Dracut Module Manually

If you don't want to install the full nbc package, you can copy just the dracut module files:

```dockerfile
FROM ghcr.io/frostyard/debian-bootc:trixie

# Copy the dracut module from nbc source or a built nbc image
COPY --from=ghcr.io/frostyard/nbc:latest \
  /usr/lib/dracut/modules.d/95etc-overlay \
  /usr/lib/dracut/modules.d/95etc-overlay

# Regenerate initramfs with the module
RUN for kver in /usr/lib/modules/*/; do \
      kver=$(basename "$kver"); \
      dracut --force --add etc-overlay \
        /usr/lib/modules/$kver/initramfs.img $kver; \
    done
```

### Option 3: Build Module from Source

For maximum control, embed the module files directly in your Dockerfile:

```dockerfile
FROM ghcr.io/frostyard/debian-bootc:trixie

# Create the dracut module directory
RUN mkdir -p /usr/lib/dracut/modules.d/95etc-overlay

# Create module-setup.sh
RUN cat > /usr/lib/dracut/modules.d/95etc-overlay/module-setup.sh << 'EOF'
#!/bin/bash
check() { return 0; }
depends() { echo "rootfs-block"; return 0; }
install() {
    inst_hook pre-pivot 50 "$moddir/etc-overlay-mount.sh"
    inst_multiple mount umount mkdir
}
installkernel() { instmods overlay; }
EOF

# Create etc-overlay-mount.sh (the actual hook script)
# See pkg/dracut/95etc-overlay/etc-overlay-mount.sh for full content
COPY etc-overlay-mount.sh /usr/lib/dracut/modules.d/95etc-overlay/

# Make scripts executable
RUN chmod +x /usr/lib/dracut/modules.d/95etc-overlay/*.sh

# Regenerate initramfs
RUN for kver in /usr/lib/modules/*/; do \
      kver=$(basename "$kver"); \
      dracut --force --add etc-overlay \
        /usr/lib/modules/$kver/initramfs.img $kver; \
    done
```

### Verifying the Initramfs

After building your image, verify the initramfs includes the etc-overlay module:

```bash
# Inside the container or on extracted image
lsinitrd /usr/lib/modules/$(uname -r)/initramfs.img | grep etc-overlay

# Should show something like:
# usr/lib/dracut/hooks/pre-pivot/50etc-overlay-mount.sh
```

Or use `lsinitramfs` on Debian-based systems:

```bash
lsinitramfs /usr/lib/modules/*/initramfs.img | grep etc-overlay
```

### CI/CD Integration

Example GitHub Actions workflow to verify initramfs:

```yaml
- name: Verify initramfs includes etc-overlay
  run: |
    # Extract and check the image
    podman create --name temp ${{ env.IMAGE_NAME }}
    podman cp temp:/usr/lib/modules - | tar -tf - | grep -q initramfs.img

    # Check initramfs contents
    podman run --rm ${{ env.IMAGE_NAME }} \
      lsinitramfs /usr/lib/modules/*/initramfs.img | grep -q etc-overlay-mount.sh
```

### nbc Behavior with Pre-built Initramfs

When nbc detects that an initramfs already contains the etc-overlay module, it skips regeneration:

```
  Checking initramfs for etc-overlay module...
    ✓ Initramfs for 6.12.0-1-amd64 already has etc-overlay module
  All initramfs images already have etc-overlay module, skipping regeneration
```

This check uses `lsinitrd` (Fedora/RHEL) or `lsinitramfs` (Debian/Ubuntu) to inspect the initramfs contents without extracting them.

## Caveats and Limitations

### Read-Only Root Filesystem

The root filesystem is mounted read-only (`ro`) for immutability, similar to how bootc/ostree systems work. This provides:

- **Protection from accidental modifications**: System files cannot be altered
- **Atomic updates**: The entire root is replaced during A/B updates
- **Reproducibility**: The root filesystem always matches the container image

The `/etc` overlay allows user configuration to persist despite the read-only root.

#### How the Overlay Works with Read-Only Root

During early boot, the dracut etc-overlay module:

1. Temporarily remounts root read-write
2. Moves `/etc` to `/.etc.lower` (the overlay lower layer)
3. Mounts the overlay on `/etc`
4. Remounts root back to read-only

This ensures `/etc` modifications work while the rest of root stays immutable.

#### /etc/machine-id Handling

For read-only root to work with systemd's first-boot detection:

- nbc pre-populates `/etc/machine-id` with "uninitialized" during installation
- On first boot, systemd detects this and generates a real machine-id
- The generated machine-id is written to the overlay upper layer (on `/var`)
- Subsequent boots read the machine-id from the overlay

**Important**: Container images should NOT have a populated `/etc/machine-id`. Use `nbc lint` to detect and fix this issue.

### First Boot Behavior

On first boot:

- Overlay directories are created automatically if missing
- `/.etc.lower` contains the container's `/etc` (populated during install/update)
- System generates `/etc/machine-id` (written to overlay upper)

### Hidden Lower Layer

The container's `/etc` at `/.etc.lower` is hidden by an empty tmpfs mount to:

- Prevent confusion from seeing duplicate `/etc` contents
- Avoid accidental modifications to the read-only layer

To inspect the lower layer, unmount the hiding tmpfs:

```bash
umount /.etc.lower
ls /.etc.lower/
```

### Overlay Upper Layer Persistence

The upper layer persists indefinitely. Old modifications are never automatically cleaned. To "reset" `/etc` to container defaults:

```bash
# WARNING: This removes ALL user /etc modifications
rm -rf /var/lib/nbc/etc-overlay/upper/*
rm -rf /var/lib/nbc/etc-overlay/work/*
reboot
```

### Files That May Cause Issues

Some files in `/etc` have special considerations:

| File                        | Consideration                                          |
| --------------------------- | ------------------------------------------------------ |
| `/etc/machine-id`           | Generated on first boot, persists in overlay           |
| `/etc/fstab`                | Created by nbc, user modifications persist             |
| `/etc/passwd`, `/etc/group` | User additions persist, container updates may conflict |
| `/etc/shadow`               | User password changes persist                          |
| `/etc/resolv.conf`          | Network config, may be overwritten by NetworkManager   |

### A/B Update Compatibility

The overlay is shared between A and B root partitions via the common `/var` partition. This means:

- User modifications apply to whichever root is active
- Rolling back to previous root still sees current `/etc` modifications
- To truly roll back `/etc`, manually restore from pristine snapshot

## Troubleshooting

### Check if System Booted via nbc

```bash
# Check for runtime marker
if [ -f /run/nbc-booted ]; then
    echo "nbc-managed boot detected"
else
    echo "Not an nbc-managed boot"
fi
```

If `/run/nbc-booted` doesn't exist but overlay is mounted, the dracut module may have partially failed.

### Check if Overlay is Active

```bash
# Should show overlay mount on /etc
mount | grep 'overlay on /etc'

# Check kernel parameters
grep 'rd.etc.overlay' /proc/cmdline
```

### Verify Overlay Directories

```bash
# Check upper layer content
ls -la /var/lib/nbc/etc-overlay/upper/

# Count user-modified files
find /var/lib/nbc/etc-overlay/upper -type f | wc -l
```

### Check Lower Layer (Hidden)

```bash
# Unmount the hiding tmpfs
umount /.etc.lower

# Inspect original /etc from container
ls /.etc.lower/

# Re-hide (or just reboot)
mount -t tmpfs -o size=0,mode=000 tmpfs /.etc.lower
```

### Overlay Not Mounting

If `/etc` is not an overlay after boot:

1. Check kernel cmdline: `cat /proc/cmdline | grep rd.etc.overlay`
2. Check dracut module: `ls /usr/lib/dracut/modules.d/95etc-overlay/`
3. Check initramfs includes module: `lsinitrd | grep etc-overlay`
4. Check dmesg for overlay errors: `dmesg | grep -i overlay`

### Regenerate Initramfs

If the dracut module is present but not in initramfs:

```bash
# Find kernel version
KVER=$(uname -r)

# Regenerate with etc-overlay module
dracut --force --add etc-overlay /boot/initramfs-$KVER.img $KVER
```

## Implementation Files

| File                                            | Purpose                                         |
| ----------------------------------------------- | ----------------------------------------------- |
| `pkg/dracut/95etc-overlay/module-setup.sh`      | Dracut module definition                        |
| `pkg/dracut/95etc-overlay/etc-overlay-mount.sh` | Pre-pivot hook script                           |
| `pkg/dracut.go`                                 | Installs dracut module, regenerates initramfs   |
| `pkg/etc_persistence.go`                        | Creates overlay directories, conflict detection |
| `pkg/bootloader.go`                             | Adds kernel parameters for overlay              |
