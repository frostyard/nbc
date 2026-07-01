#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# Tier 2: /etc overlay persistence validation for nbc-installed systems.
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

echo "# Tier 2: /etc overlay persistence"

check "Kernel cmdline has rd.etc.overlay=1" \
    grep -q "rd.etc.overlay=1" /proc/cmdline

check "/etc is an overlay mount" \
    bash -c 'findmnt -n -o FSTYPE /etc | grep -q overlay'

check "Overlay upper dir exists" \
    test -d /var/lib/nbc/etc-overlay/upper

check "Overlay work dir exists" \
    test -d /var/lib/nbc/etc-overlay/work

check "/.etc.lower exists" \
    test -d /.etc.lower

# Regression guard for issue #84: the overlay upper layer must hold only user
# modifications, NOT a copy of the container's default /etc. If default files
# leak into upper they permanently shadow the lower layer and silently defeat
# /etc updates on every A/B update. os-release ships in every image and is
# never user-modified, so it must not appear in the upper layer.
check "Overlay upper is not polluted with container defaults (os-release)" \
    bash -c '! test -e /var/lib/nbc/etc-overlay/upper/os-release'

# Test persistence: write a file to /etc and verify it's there
MARKER="/etc/nbc-qemu-test-marker"
check "Can write to /etc overlay" \
    bash -c "echo 'test-$(date +%s)' > $MARKER"

check "Written file persists in /etc" \
    test -f "$MARKER"

# Verify the write went to the overlay upper dir, not the lower
check "Write landed in overlay upper" \
    test -f /var/lib/nbc/etc-overlay/upper/nbc-qemu-test-marker

echo ""
echo "# Results: $PASS passed, $FAIL failed, $(( PASS + FAIL )) total"
exit "$FAIL"
