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

1. **Dracut module** (`95etc-overlay`) runs during initramfs, before pivot_root
2. Original `/etc` is moved to `/.etc.lower` (hidden by tmpfs mount)
3. Overlayfs mounts over `/etc` with the container's `/etc` as lower layer
4. User writes go to `/var/lib/nbc/etc-overlay/upper`
5. Reads see merged view: user modifications overlay container files

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

The dracut module creates a marker file at `/run/nbc-booted` to indicate the system was booted via nbc. This is similar to `/run/ostree-booted` created by bootc/ostree systems.

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

## Caveats and Limitations

### Read-Write Root Requirement

The root filesystem is currently mounted read-write (`rw`). While the overlay provides persistent `/etc`, the root itself is writable. A fully read-only root (`ro`) requires additional work:

- systemd needs `/etc/machine-id` to exist before `/var` is mounted
- With read-only root, `/etc/machine-id` cannot be created at first boot
- Workarounds (like pre-populating with "uninitialized") have timing issues

### First Boot Behavior

On first boot:

- Overlay directories are created automatically if missing
- Original `/etc` is moved to `/.etc.lower`
- System generates `/etc/machine-id` (written to overlay upper)

### Hidden Lower Layer

The original `/etc` at `/.etc.lower` is hidden by an empty tmpfs mount to:

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
