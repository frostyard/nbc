#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Dracut hook to mount /etc overlay
# Runs in pre-pivot phase, after root is mounted but before switching to it
#
# Kernel parameters:
#   rd.etc.overlay=1              - Enable /etc overlay
#   rd.etc.overlay.var=UUID=xxx   - /var partition UUID (optional, uses systemd auto-discovery)
#   rd.etc.overlay.var=/dev/xxx   - /var partition device path

type getarg >/dev/null 2>&1 || . /lib/dracut-lib.sh

# Check if overlay is enabled
if ! getargbool 0 rd.etc.overlay; then
    return 0
fi

info "etc-overlay: Setting up /etc overlay..."

SYSROOT="${NEWROOT:-/sysroot}"
OVERLAY_BASE="$SYSROOT/var/lib/nbc/etc-overlay"
OVERLAY_UPPER="$OVERLAY_BASE/upper"
OVERLAY_WORK="$OVERLAY_BASE/work"
ETC_LOWER="$SYSROOT/etc"

# Get /var partition specification
VAR_DEV=$(getarg rd.etc.overlay.var=)

# Function to mount /var if not already mounted
mount_var() {
    local var_mount="$SYSROOT/var"

    # Check if /var is already mounted (by systemd or previous hook)
    if mountpoint -q "$var_mount" 2>/dev/null; then
        info "etc-overlay: /var already mounted"
        return 0
    fi

    # If no VAR_DEV specified, try to find it via partition type GUID
    # (systemd Discoverable Partitions: 4D21B016-B534-45C2-A9FB-5C16E091FD2D)
    if [ -z "$VAR_DEV" ]; then
        # Try to find /var partition by GPT type GUID
        for dev in /dev/disk/by-parttypeuuid/4d21b016-b534-45c2-a9fb-5c16e091fd2d*; do
            if [ -e "$dev" ]; then
                VAR_DEV="$dev"
                info "etc-overlay: Found /var partition by GPT type: $VAR_DEV"
                break
            fi
        done
    fi

    if [ -z "$VAR_DEV" ]; then
        warn "etc-overlay: No /var partition specified and none found by GPT type"
        warn "etc-overlay: Falling back to read-only /etc from root filesystem"
        return 1
    fi

    # Resolve UUID= syntax
    case "$VAR_DEV" in
        UUID=*)
            local uuid="${VAR_DEV#UUID=}"
            VAR_DEV="/dev/disk/by-uuid/$uuid"
            ;;
        PARTUUID=*)
            local partuuid="${VAR_DEV#PARTUUID=}"
            VAR_DEV="/dev/disk/by-partuuid/$partuuid"
            ;;
    esac

    # Wait for device if needed
    if [ ! -e "$VAR_DEV" ]; then
        info "etc-overlay: Waiting for /var device $VAR_DEV..."
        udevadm settle --timeout=30 2>/dev/null || sleep 5
    fi

    if [ ! -e "$VAR_DEV" ]; then
        warn "etc-overlay: /var device $VAR_DEV not found"
        return 1
    fi

    info "etc-overlay: Mounting /var from $VAR_DEV"
    mkdir -p "$var_mount"

    # Try ext4 first, then btrfs
    if ! mount -t ext4 "$VAR_DEV" "$var_mount" 2>/dev/null; then
        if ! mount -t btrfs "$VAR_DEV" "$var_mount" 2>/dev/null; then
            warn "etc-overlay: Failed to mount /var"
            return 1
        fi
    fi

    return 0
}

# Mount /var first
if ! mount_var; then
    warn "etc-overlay: Cannot mount /var, /etc overlay disabled"
    return 0
fi

# Check if overlay directories exist
if [ ! -d "$OVERLAY_UPPER" ] || [ ! -d "$OVERLAY_WORK" ]; then
    info "etc-overlay: Creating overlay directories..."
    mkdir -p "$OVERLAY_UPPER" "$OVERLAY_WORK"
    if [ $? -ne 0 ]; then
        warn "etc-overlay: Failed to create overlay directories"
        warn "etc-overlay: Falling back to read-only /etc"
        return 0
    fi
fi

# Ensure overlay module is loaded
modprobe overlay 2>/dev/null || true

# Check if /etc exists (it should, from the root filesystem)
if [ ! -d "$ETC_LOWER" ]; then
    warn "etc-overlay: $ETC_LOWER does not exist, cannot setup overlay"
    return 0
fi

# Mount the overlay
# - lowerdir: /etc from root filesystem (read-only base from container)
# - upperdir: modifications persist on /var
# - workdir: required by overlayfs for atomic operations
info "etc-overlay: Mounting overlay (lower=$ETC_LOWER, upper=$OVERLAY_UPPER)"

# We need to move the original /etc to a hidden location because overlayfs
# requires lowerdir to be a separate path from the mount point.
# Use a consistent hidden name so it's predictable.
ETC_LOWER_DIR="$SYSROOT/.etc.lower"

# Check if we already have a lower dir from a previous boot
# This happens on subsequent boots - reuse the existing lower layer
if [ -d "$ETC_LOWER_DIR" ] && [ "$(ls -A "$ETC_LOWER_DIR" 2>/dev/null)" ]; then
    # Previous lower dir exists with content - this is a normal reboot
    info "etc-overlay: Reusing existing lower layer at $ETC_LOWER_DIR"
    # Check if current /etc is empty (just a mount point from before)
    if [ ! "$(ls -A "$ETC_LOWER" 2>/dev/null)" ]; then
        info "etc-overlay: Current /etc is empty mount point, will overlay on existing lower"
    else
        # /etc has content - this shouldn't happen, but handle gracefully
        # The current /etc might be leftover from a failed overlay attempt
        info "etc-overlay: Current /etc has content, merging with lower layer"
        # Copy any new files from /etc to upper layer (they are customizations)
        # Avoid overwriting existing entries in the upper layer to preserve user changes
        for src in "$ETC_LOWER"/*; do
            # Handle case where the glob doesn't match anything
            [ -e "$src" ] || continue
            name=${src##*/}  # basename
            if [ -e "$OVERLAY_UPPER/$name" ]; then
                info "etc-overlay: Skipping existing upper-layer entry: $name"
                continue
            fi
            cp -a "$src" "$OVERLAY_UPPER/" 2>/dev/null || true
        done
    fi
else
    # First boot or lower dir is empty/missing - move /etc to lower dir
    if [ -d "$ETC_LOWER_DIR" ]; then
        # Empty lower dir exists, remove it
        rm -rf "$ETC_LOWER_DIR"
    fi

    if ! mv "$ETC_LOWER" "$ETC_LOWER_DIR"; then
        warn "etc-overlay: Failed to move original /etc"
        return 0
    fi
fi

# Create/ensure the mount point exists
mkdir -p "$ETC_LOWER"

# Mount the overlay
if mount -t overlay overlay \
    -o "lowerdir=$ETC_LOWER_DIR,upperdir=$OVERLAY_UPPER,workdir=$OVERLAY_WORK" \
    "$ETC_LOWER"; then
    info "etc-overlay: /etc overlay mounted successfully"

    # Hide the lower directory by bind-mounting an empty tmpfs over it
    # This prevents users from seeing the duplicate /etc contents at /.etc.lower
    mount -t tmpfs -o size=0,mode=000 tmpfs "$ETC_LOWER_DIR" 2>/dev/null || true
    info "etc-overlay: Hidden lower directory at /.etc.lower"
else
    warn "etc-overlay: Failed to mount overlay, restoring original /etc"
    rmdir "$ETC_LOWER" 2>/dev/null
    mv "$ETC_LOWER_DIR" "$ETC_LOWER"
    return 0
fi

info "etc-overlay: Setup complete"
