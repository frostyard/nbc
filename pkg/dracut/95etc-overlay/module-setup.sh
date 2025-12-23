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
    # We need the crypt module to be loaded first if LUKS is in use,
    # so that /var can be unlocked before we try to mount the overlay
    echo "rootfs-block"
    return 0
}

install() {
    # Install the hook script
    inst_hook pre-pivot 50 "$moddir/etc-overlay-mount.sh"
    
    # Install required binaries
    inst_multiple mount umount mkdir
}

installkernel() {
    # Ensure overlay filesystem module is available
    instmods overlay
}
