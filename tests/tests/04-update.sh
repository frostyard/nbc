#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# Tier 4: A/B update validation for nbc-installed systems.
# Runs INSIDE the booted VM via SSH. Fully self-contained.
# Requires: IMAGE_REF environment variable set to the container image reference.
#           nbc binary at /usr/local/bin/nbc.
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

echo "# Tier 4: A/B update"

# Verify prerequisites
check "nbc binary is available" \
    test -x /usr/local/bin/nbc

if [[ -z "${IMAGE_REF:-}" ]]; then
    echo "not ok - IMAGE_REF is not set"
    (( FAIL++ )) || true
    echo ""
    echo "# Results: $PASS passed, $FAIL failed, $(( PASS + FAIL )) total"
    exit "$FAIL"
fi
echo "# image ref: $IMAGE_REF"

# Find the boot disk
ROOT_DEV=$(findmnt -n -o SOURCE /)
DISK_DEV=$(lsblk -no PKNAME "$ROOT_DEV" 2>/dev/null | head -1)
DISK_DEV="/dev/$DISK_DEV"
echo "# disk device: $DISK_DEV"

# Record state before update
ACTIVE_PART="$ROOT_DEV"
echo "# active root before update: $ACTIVE_PART"

# Run nbc update
echo "# Running nbc update..."
check "nbc update succeeds" \
    /usr/local/bin/nbc update --image "$IMAGE_REF" --force --verbose

# After update, the inactive root should have content.
# If active is partition 2 (root1), inactive is partition 3 (root2).
if [[ "$ACTIVE_PART" == *2 ]]; then
    INACTIVE_PART="${DISK_DEV}3"
else
    INACTIVE_PART="${DISK_DEV}2"
fi
echo "# inactive root after update: $INACTIVE_PART"

# Mount inactive root and verify it has content
MNT=$(mktemp -d)
check "Can mount inactive root" \
    mount "$INACTIVE_PART" "$MNT"

if mountpoint -q "$MNT"; then
    check "Inactive root has /usr" \
        test -d "$MNT/usr"

    check "Inactive root has /etc" \
        test -d "$MNT/etc"

    # Check that a kernel exists in the inactive root
    check "Inactive root has kernel in /usr/lib/modules" \
        bash -c "test -n \"\$(ls $MNT/usr/lib/modules/*/vmlinuz* 2>/dev/null)\""

    umount "$MNT" 2>/dev/null || true
fi
rmdir "$MNT" 2>/dev/null || true

# Verify config.json was updated
check "config.json still exists after update" \
    test -f /var/lib/nbc/state/config.json

check "config.json is still valid JSON" \
    python3 -c "import json; json.load(open('/var/lib/nbc/state/config.json'))"

echo ""
echo "# Results: $PASS passed, $FAIL failed, $(( PASS + FAIL )) total"
exit "$FAIL"
