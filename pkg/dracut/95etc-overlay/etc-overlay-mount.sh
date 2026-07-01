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

SYSROOT="${NEWROOT:-/sysroot}"

# Note: The /run/nbc-booted marker is created by tmpfiles.d after boot,
# not here. Creating it here would be lost when systemd mounts a fresh
# tmpfs on /run after switch_root.

# prepare_etc_lower reconciles the overlay lower directory (.etc.lower) with the
# root filesystem's /etc so the overlay can be mounted with a populated,
# read-only lower layer and a CLEAN upper layer (user modifications only).
#
# Design (see docs/ETC-OVERLAY.md):
#   - lowerdir = /.etc.lower   (base /etc from the container image, read-only)
#   - upperdir = /var/lib/nbc/etc-overlay/upper  (user modifications ONLY)
#
# nbc pre-populates /.etc.lower with the container's /etc at install/update time.
# In that (normal) case we MUST leave the layers untouched: the overlay mount
# over /etc simply shadows the identical copy on the root filesystem, and the
# lower layer supplies the defaults. Copying the root fs /etc into the upper
# layer here would pin the container defaults in the writable upper and
# permanently shadow future A/B updates -- silently defeating /etc updates.
#
# Only when /.etc.lower is empty/missing (its lower layer was never seeded) do we
# seed it by moving the root fs /etc into place.
#
# Globals set for the caller:
#   ETC_LOWER_DIR - path to the lower layer directory
#   ROOT_WAS_RO   - 1 if we remounted the root rw and it must be restored
#   ETC_MOVED     - 1 if we moved /etc into .etc.lower (affects failure rollback)
# Reads global: ETC_LOWER (path to the root fs /etc), SYSROOT
prepare_etc_lower() {
    ETC_LOWER_DIR="$SYSROOT/.etc.lower"
    ROOT_WAS_RO=0
    ETC_MOVED=0

    # Normal case: nbc already populated the lower layer (or a previous boot
    # seeded it). Use it as-is; do NOT seed the upper layer.
    if [ -d "$ETC_LOWER_DIR" ] && [ -n "$(ls -A "$ETC_LOWER_DIR" 2>/dev/null)" ]; then
        info "etc-overlay: Using populated lower layer at $ETC_LOWER_DIR"
        return 0
    fi

    # Fallback: lower layer is empty/missing -> seed it from the root fs /etc.
    #
    # We may need to temporarily remount the root rw to move /etc.
    # /proc/mounts format: device mountpoint fstype options dump pass
    # The 'ro' option is in the 4th field (options), as a comma-separated entry
    # that can appear anywhere (e.g., "ro", "relatime,ro", "defaults,ro,noatime").
    if grep -E "^[^ ]+ $SYSROOT [^ ]+ ([^ ]*,)?ro(,[^ ]*)?( |$)" /proc/mounts >/dev/null 2>&1; then
        info "etc-overlay: Root is mounted read-only, temporarily remounting rw"
        ROOT_WAS_RO=1
        if ! mount -o remount,rw "$SYSROOT"; then
            warn "etc-overlay: Failed to remount root rw, cannot setup overlay"
            return 1
        fi
    fi

    # Remove an empty .etc.lower placeholder if present (created by nbc during
    # install). This must happen AFTER the ro remount check above.
    if [ -d "$ETC_LOWER_DIR" ]; then
        rm -rf "$ETC_LOWER_DIR"
    fi

    if ! mv "$ETC_LOWER" "$ETC_LOWER_DIR"; then
        warn "etc-overlay: Failed to move original /etc"
        if [ "$ROOT_WAS_RO" = "1" ]; then
            mount -o remount,ro "$SYSROOT" 2>/dev/null || true
        fi
        return 1
    fi
    ETC_MOVED=1

    return 0
}

# Allow tests to source this file and exercise prepare_etc_lower() in isolation
# without running the boot-time flow below.
[ -n "${ETC_OVERLAY_TEST:-}" ] && return 0

# Check if overlay is enabled
if ! getargbool 0 rd.etc.overlay; then
    info "etc-overlay: rd.etc.overlay not enabled, skipping overlay setup"
    return 0
fi

info "etc-overlay: Setting up /etc overlay..."
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

# We need the original /etc content available at a hidden location because
# overlayfs requires lowerdir to be a separate path from the mount point.
# prepare_etc_lower sets ETC_LOWER_DIR (and, if it had to seed the lower layer,
# ROOT_WAS_RO / ETC_MOVED for the rollback/restore paths below).
if ! prepare_etc_lower; then
    warn "etc-overlay: Failed to prepare lower layer, /etc overlay disabled"
    return 0
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

    # Restore root to read-only if we changed it
    if [ "${ROOT_WAS_RO:-0}" = "1" ]; then
        info "etc-overlay: Restoring root to read-only"
        if ! mount -o remount,ro "$SYSROOT"; then
            warn "etc-overlay: Failed to remount root ro"
        fi
    fi
else
    warn "etc-overlay: Failed to mount overlay, restoring original /etc"
    # Only undo the move if prepare_etc_lower actually moved /etc into .etc.lower.
    if [ "${ETC_MOVED:-0}" = "1" ]; then
        rmdir "$ETC_LOWER" 2>/dev/null
        mv "$ETC_LOWER_DIR" "$ETC_LOWER"
    fi
    # Restore ro if we changed it
    if [ "${ROOT_WAS_RO:-0}" = "1" ]; then
        mount -o remount,ro "$SYSROOT" 2>/dev/null || true
    fi
    return 0
fi

info "etc-overlay: Setup complete"
