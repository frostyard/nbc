#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# Main orchestrator for nbc QEMU integration tests.
# Builds nbc, installs an image to a virtual disk, boots a QEMU VM,
# and runs tiered test scripts over SSH.
#
# Usage: ./tests/nbc-qemu-test.sh <container-image-ref>
#
# Examples:
#   ./tests/nbc-qemu-test.sh ghcr.io/frostyard/snow:latest
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Source helper libraries
# shellcheck source=tests/lib/ssh.sh
source "$SCRIPT_DIR/lib/ssh.sh"
# shellcheck source=tests/lib/vm.sh
source "$SCRIPT_DIR/lib/vm.sh"

# Environment variable defaults (VM_MEMORY/VM_CPUS/SSH_PORT also set in lib/vm.sh)
: "${VM_MEMORY:=4096}"
: "${VM_CPUS:=2}"
: "${SSH_PORT:=2222}"
: "${SSH_TIMEOUT:=300}"

# Internal state
WORK_DIR=""
LOOP_DEV=""

usage() {
    echo "Usage: $0 <container-image-ref>" >&2
    echo "" >&2
    echo "  container-image-ref  OCI image reference (e.g. ghcr.io/frostyard/snow:latest)" >&2
    exit 1
}

cleanup() {
    echo ""
    echo "=== Cleanup ==="

    # Stop VM and remove disk
    vm_cleanup

    # Detach loopback if still attached
    if [[ -n "$LOOP_DEV" ]]; then
        losetup -d "$LOOP_DEV" 2>/dev/null || true
        echo "Detached loop device: $LOOP_DEV"
        LOOP_DEV=""
    fi

    # Unmount anything under work dir
    if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
        umount "$WORK_DIR/mnt" 2>/dev/null || true
        rm -rf "$WORK_DIR"
        echo "Removed temp directory: $WORK_DIR"
    fi
}

# --- Argument parsing ---
[[ $# -eq 1 ]] || usage
IMAGE_REF="$1"

trap cleanup EXIT

WORK_DIR=$(mktemp -d)
echo "Temp directory: $WORK_DIR"

# ---------------------------------------------------------------
# Step 1 - Build nbc
# ---------------------------------------------------------------
echo ""
echo "=== Step 1: Build nbc ==="
make -C "$REPO_ROOT" build
NBC_BIN="$REPO_ROOT/nbc"
[[ -x "$NBC_BIN" ]] || { echo "Error: nbc binary not found at $NBC_BIN" >&2; exit 1; }
echo "nbc binary: $NBC_BIN"

# ---------------------------------------------------------------
# Step 2 - Generate SSH keypair
# ---------------------------------------------------------------
echo ""
echo "=== Step 2: Generate SSH keypair ==="
ssh_keygen "$WORK_DIR"

# ---------------------------------------------------------------
# Step 3 - Install image to disk via nbc
# ---------------------------------------------------------------
echo ""
echo "=== Step 3: Install image to disk ==="
DISK_IMG="$WORK_DIR/disk.raw"

"$NBC_BIN" install \
    --image "$IMAGE_REF" \
    --via-loopback "$DISK_IMG" \
    --image-size 35 \
    --force \
    --karg console=ttyS0 \
    --verbose

echo "Installation complete: $DISK_IMG"

# ---------------------------------------------------------------
# Step 4 - Inject SSH key into installed disk
# ---------------------------------------------------------------
echo ""
echo "=== Step 4: Inject SSH key ==="

LOOP_DEV=$(losetup --find --show --partscan "$DISK_IMG")
echo "Loop device: $LOOP_DEV"

# Wait for partition devices to appear
sleep 1
partprobe "$LOOP_DEV" 2>/dev/null || true
udevadm settle 2>/dev/null || true

mkdir -p "$WORK_DIR/mnt"

# On bootc/ostree images, /root is a symlink to /var/roothome.
# Inject SSH key into the var partition (partition 4) where roothome lives.
mount "${LOOP_DEV}p4" "$WORK_DIR/mnt"

# SSH authorized_keys for root (at /var/roothome/.ssh/)
mkdir -p "$WORK_DIR/mnt/roothome/.ssh"
cp "${SSH_KEY}.pub" "$WORK_DIR/mnt/roothome/.ssh/authorized_keys"
chmod 700 "$WORK_DIR/mnt/roothome/.ssh"
chmod 600 "$WORK_DIR/mnt/roothome/.ssh/authorized_keys"
echo "Injected SSH key into /var/roothome/.ssh/"

# Ensure sshd permits root login via the /etc overlay upper dir
SSHD_DROPIN="$WORK_DIR/mnt/lib/nbc/etc-overlay/upper/ssh/sshd_config.d"
mkdir -p "$SSHD_DROPIN"
cat > "$SSHD_DROPIN/99-qemu-test.conf" <<'SSHD_EOF'
PermitRootLogin yes
PubkeyAuthentication yes
SSHD_EOF
echo "Injected sshd config drop-in into /etc overlay"

umount "$WORK_DIR/mnt"

losetup -d "$LOOP_DEV"
LOOP_DEV=""

# Update DISK_IMAGE for vm.sh functions
DISK_IMAGE="$DISK_IMG"

# ---------------------------------------------------------------
# Step 5 - Boot VM
# ---------------------------------------------------------------
echo ""
echo "=== Step 5: Boot VM ==="
vm_start "$DISK_IMG"

# ---------------------------------------------------------------
# Step 6 - Wait for SSH
# ---------------------------------------------------------------
echo ""
echo "=== Step 6: Wait for SSH ==="
wait_for_ssh

# ---------------------------------------------------------------
# Step 7 - Push nbc binary into VM
# ---------------------------------------------------------------
echo ""
echo "=== Step 7: Push nbc binary ==="
vm_scp "$NBC_BIN" root@localhost:/usr/local/bin/nbc
vm_ssh chmod +x /usr/local/bin/nbc
echo "Pushed nbc binary to /usr/local/bin/nbc"

# ---------------------------------------------------------------
# Step 8 - Run test tiers
# ---------------------------------------------------------------
echo ""
echo "=== Step 8: Run tests ==="

declare -a test_names=()
declare -a test_results=()

for test_script in "$SCRIPT_DIR"/tests/*.sh; do
    [[ -f "$test_script" ]] || continue
    test_name="$(basename "$test_script")"
    test_names+=("$test_name")

    echo ""
    echo "--- Running: $test_name ---"

    # Copy test script to VM
    vm_scp "$test_script" root@localhost:/tmp/"$test_name"

    # Execute test script. Pass IMAGE_REF for update tests.
    set +e
    vm_ssh "IMAGE_REF='$IMAGE_REF' bash /tmp/$test_name"
    rc=$?
    set -e

    test_results+=("$rc")

    if [[ "$rc" -eq 0 ]]; then
        echo "--- $test_name: PASSED ---"
    else
        echo "--- $test_name: FAILED ($rc failures) ---"
    fi
done

# ---------------------------------------------------------------
# Step 9 - Summary
# ---------------------------------------------------------------
echo ""
echo "========================================"
echo "           TEST SUMMARY"
echo "========================================"

overall=0
for i in "${!test_names[@]}"; do
    name="${test_names[$i]}"
    rc="${test_results[$i]}"
    if [[ "$rc" -eq 0 ]]; then
        status="PASS"
    else
        status="FAIL"
        overall=1
    fi
    printf "  %-30s %s\n" "$name" "$status"
done

echo "========================================"
if [[ "$overall" -eq 0 ]]; then
    echo "All tiers passed."
else
    echo "Some tiers failed."
fi

exit "$overall"
