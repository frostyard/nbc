#!/bin/bash
# SPDX-License-Identifier: GPL-2.0-or-later
#
# Dracut module for /etc overlay persistence
# This module sets up an overlayfs for /etc with the root filesystem as the
# lower (read-only) layer and a directory on /var as the upper (writable) layer.
#
# This allows user modifications to /etc to persist across A/B updates while
# keeping the base /etc from the container image.

check() {
    # Only include this module if rd.etc.overlay is set
    # or if we're in hostonly mode and the system uses etc overlay
    return 0
}

depends() {
    # Declare the crypt module as a dependency so it is force-included in the
    # initramfs even for generic (non-hostonly) image builds where the build
    # host has no LUKS devices for crypt's own check() to auto-detect. Without
    # this, an encrypted /var could not be unlocked before we mount the overlay.
    # rootfs-block is kept for block-device setup ordering.
    echo "crypt rootfs-block"
    return 0
}

install() {
    # Install the hook script
    inst_hook pre-pivot 50 "$moddir/etc-overlay-mount.sh"

    # Install required binaries
    # grep is needed to detect read-only root mount in /proc/mounts
    inst_multiple mount umount mkdir grep
}

installkernel() {
    # Ensure overlay filesystem module is available
    instmods overlay
}
