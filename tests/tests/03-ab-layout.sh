#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# Tier 3: A/B partition layout validation for nbc-installed systems.
# Runs INSIDE the booted VM via SSH. Fully self-contained.
set -euo pipefail

PASS=0
FAIL=0

check() {
    local desc="$1"
    shift
    if "$@" >/dev/null 2>&1; then
        echo "ok - $desc"
        (( PASS++ )) || true
    else
        echo "not ok - $desc"
        (( FAIL++ )) || true
    fi
}

echo "# Tier 3: A/B partition layout"

# Find the boot disk device. The root device tells us which disk we booted from.
ROOT_DEV=$(findmnt -n -o SOURCE /)
echo "# root device: $ROOT_DEV"

# Determine the parent disk from the root device.
# For virtio: /dev/vda2 -> /dev/vda; for loop: /dev/loop0p2 -> /dev/loop0
DISK_DEV=$(lsblk -no PKNAME "$ROOT_DEV" 2>/dev/null | head -1)
DISK_DEV="/dev/$DISK_DEV"
echo "# disk device: $DISK_DEV"

# Verify 4 partitions exist
PART_COUNT=$(lsblk -n -o NAME "$DISK_DEV" | grep -c -v "^$(basename "$DISK_DEV")$" || true)
echo "# partition count: $PART_COUNT"
check "Disk has 4 partitions" \
    test "$PART_COUNT" -eq 4

# Check EFI partition (partition 1) is FAT
check "Partition 1 (EFI) is vfat" \
    bash -c "lsblk -n -o FSTYPE ${DISK_DEV}1 2>/dev/null | grep -q vfat"

# Check root1 (partition 2) filesystem type
ROOT1_FS=$(lsblk -n -o FSTYPE "${DISK_DEV}2" 2>/dev/null | head -1)
echo "# root1 fstype: $ROOT1_FS"
check "Partition 2 (root1) is ext4 or btrfs" \
    bash -c "[[ '$ROOT1_FS' == 'ext4' || '$ROOT1_FS' == 'btrfs' ]]"

# Check root2 (partition 3) filesystem type
ROOT2_FS=$(lsblk -n -o FSTYPE "${DISK_DEV}3" 2>/dev/null | head -1)
echo "# root2 fstype: $ROOT2_FS"
check "Partition 3 (root2) is ext4 or btrfs" \
    bash -c "[[ '$ROOT2_FS' == 'ext4' || '$ROOT2_FS' == 'btrfs' ]]"

# Check var (partition 4) filesystem type
VAR_FS=$(lsblk -n -o FSTYPE "${DISK_DEV}4" 2>/dev/null | head -1)
echo "# var fstype: $VAR_FS"
check "Partition 4 (var) is ext4 or btrfs" \
    bash -c "[[ '$VAR_FS' == 'ext4' || '$VAR_FS' == 'btrfs' ]]"

# Check nbc config exists and is valid JSON
check "nbc config.json exists" \
    test -f /var/lib/nbc/state/config.json

check "nbc config.json is valid JSON" \
    python3 -c "import json; json.load(open('/var/lib/nbc/state/config.json'))"

# Active root should be root1 (partition 2) -- first install uses slot A
check "Active root is partition 2 (slot A)" \
    bash -c "[[ '$ROOT_DEV' == *2 ]]"

# Root2 (inactive) should not be mounted
check "Inactive root (partition 3) is not mounted" \
    bash -c "! findmnt -n ${DISK_DEV}3"

echo ""
echo "# Results: $PASS passed, $FAIL failed, $(( PASS + FAIL )) total"
exit "$FAIL"
