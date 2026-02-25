#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# Tier 1: Boot health validation for nbc-installed systems.
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

echo "# Tier 1: Boot health"

# Wait for boot to settle. Accept "degraded" since some services may
# fail in headless VMs (e.g., graphical targets).
sys_state="starting"
for _ in $(seq 1 60); do
    sys_state=$(systemctl is-system-running 2>/dev/null || true)
    [[ "$sys_state" == "starting" ]] || break
    sleep 2
done
echo "# system state: $sys_state"
check "System has booted" \
    test "$sys_state" = "running" -o "$sys_state" = "degraded"

check "Root filesystem is mounted" \
    findmnt -n /

check "/var is a separate mount" \
    findmnt -n /var

check "EFI system partition exists" \
    findmnt -n /boot

check "Kernel cmdline has console=ttyS0" \
    grep -q "console=ttyS0" /proc/cmdline

check "Kernel cmdline has systemd.mount-extra for /var" \
    grep -q "systemd.mount-extra=.*:/var:" /proc/cmdline

echo ""
echo "# Results: $PASS passed, $FAIL failed, $(( PASS + FAIL )) total"
exit "$FAIL"
