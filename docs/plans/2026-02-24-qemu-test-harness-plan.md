# QEMU Test Harness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a shell-based QEMU integration test harness that validates nbc install-to-boot, /etc overlay, A/B layout, and update flows.

**Architecture:** Shell orchestrator (`tests/nbc-qemu-test.sh`) builds nbc, installs an image to a raw disk via `nbc install --via-loopback`, injects an SSH key, boots the disk in QEMU with OVMF/KVM, and runs numbered guest test scripts over SSH. Ported from snosi's `test/` infrastructure.

**Tech Stack:** Bash, QEMU (KVM + OVMF), SSH, loopback devices, GitHub Actions

**Design doc:** `docs/plans/2026-02-24-qemu-test-harness-design.md`

---

### Task 1: Create `tests/lib/vm.sh` -- QEMU lifecycle library

Port from snosi with no functional changes.

**Files:**
- Create: `tests/lib/vm.sh`

**Step 1: Create the file**

```bash
#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# QEMU VM lifecycle library for nbc QEMU testing.
# Sourced by test scripts; not executed directly.
set -euo pipefail

DISK_SIZE="${DISK_SIZE:-35G}"
VM_MEMORY="${VM_MEMORY:-4096}"
VM_CPUS="${VM_CPUS:-2}"
SSH_PORT="${SSH_PORT:-2222}"
QEMU_PID="${QEMU_PID:-}"
QEMU_CONSOLE_LOG="${QEMU_CONSOLE_LOG:-}"
DISK_IMAGE="${DISK_IMAGE:-}"

create_disk() {
    local path="$1"
    truncate -s "$DISK_SIZE" "$path"
    DISK_IMAGE="$path"
    echo "Created disk image: $path ($DISK_SIZE)"
}

# Find OVMF firmware. Prints "CODE_PATH VARS_PATH" to stdout.
find_ovmf() {
    local pairs=(
        "/usr/incus/share/qemu/OVMF_CODE.4MB.fd:/usr/incus/share/qemu/OVMF_VARS.4MB.fd"
        "/usr/share/OVMF/OVMF_CODE_4M.fd:/usr/share/OVMF/OVMF_VARS_4M.fd"
        "/usr/share/OVMF/OVMF_CODE.fd:/usr/share/OVMF/OVMF_VARS.fd"
        "/usr/share/edk2/ovmf/OVMF_CODE.fd:/usr/share/edk2/ovmf/OVMF_VARS.fd"
        "/usr/share/qemu/OVMF_CODE.fd:/usr/share/qemu/OVMF_VARS.fd"
        "/usr/share/edk2-ovmf/x64/OVMF_CODE.fd:/usr/share/edk2-ovmf/x64/OVMF_VARS.fd"
    )
    for pair in "${pairs[@]}"; do
        local code="${pair%%:*}"
        local vars="${pair##*:}"
        if [[ -f "$code" && -f "$vars" ]]; then
            echo "$code $vars"
            return 0
        fi
    done
    echo "Error: OVMF firmware (CODE+VARS) not found" >&2
    return 1
}

vm_start() {
    local disk="${1:-$DISK_IMAGE}"
    [[ -n "$disk" ]] || { echo "Error: No disk image specified" >&2; return 1; }
    [[ -f "$disk" ]] || { echo "Error: Disk image not found: $disk" >&2; return 1; }

    local ovmf_pair
    ovmf_pair=$(find_ovmf)
    local ovmf_code_src="${ovmf_pair%% *}"
    local ovmf_vars_src="${ovmf_pair##* }"

    local workdir="${disk%/*}"
    local ovmf_code="$workdir/OVMF_CODE.fd"
    local ovmf_vars="$workdir/OVMF_VARS.fd"
    cp "$ovmf_code_src" "$ovmf_code"
    cp "$ovmf_vars_src" "$ovmf_vars"

    local pidfile="${disk%.raw}.pid"
    local consolelog="${disk%.raw}-console.log"

    qemu-system-x86_64 \
        -machine q35 \
        -enable-kvm -cpu host \
        -m "$VM_MEMORY" -smp "$VM_CPUS" \
        -drive "if=pflash,format=raw,unit=0,file=$ovmf_code,readonly=on" \
        -drive "if=pflash,format=raw,unit=1,file=$ovmf_vars" \
        -drive "file=$disk,format=raw,if=virtio" \
        -netdev "user,id=net0,hostfwd=tcp::${SSH_PORT}-:22" \
        -device virtio-net-pci,netdev=net0 \
        -display none \
        -monitor none \
        -chardev "file,id=serial0,path=$consolelog" \
        -serial chardev:serial0 \
        -pidfile "$pidfile" \
        -daemonize

    QEMU_PID=$(cat "$pidfile")
    QEMU_CONSOLE_LOG="$consolelog"
    echo "VM started (PID: $QEMU_PID, SSH port: $SSH_PORT)"
    echo "Console log: $consolelog"
}

vm_stop() {
    if [[ -n "$QEMU_PID" ]] && kill -0 "$QEMU_PID" 2>/dev/null; then
        kill "$QEMU_PID"
        local i=0
        while kill -0 "$QEMU_PID" 2>/dev/null && (( i++ < 10 )); do
            sleep 0.5
        done
        echo "VM stopped (PID: $QEMU_PID)"
    else
        echo "VM is not running"
    fi
    QEMU_PID=""
}

vm_cleanup() {
    vm_stop
    if [[ -n "$DISK_IMAGE" && -f "$DISK_IMAGE" ]]; then
        rm -f "$DISK_IMAGE"
        echo "Removed disk image: $DISK_IMAGE"
    fi
    DISK_IMAGE=""
}
```

Note: `DISK_SIZE` defaults to `35G` (nbc minimum) instead of snosi's `10G`.

**Step 2: Verify syntax**

Run: `bash -n tests/lib/vm.sh`
Expected: No output (no syntax errors)

**Step 3: Commit**

```bash
git add tests/lib/vm.sh
git commit -m "test: add QEMU VM lifecycle library

Ported from snosi's test/lib/vm.sh. Provides OVMF firmware discovery,
QEMU launch/stop/cleanup, and sparse disk creation."
```

---

### Task 2: Create `tests/lib/ssh.sh` -- SSH helper library

Port from snosi with no functional changes.

**Files:**
- Create: `tests/lib/ssh.sh`

**Step 1: Create the file**

```bash
#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# SSH helper library for nbc QEMU testing.
# Provides keypair generation, remote command execution, and connectivity polling.
# Sourced by the test orchestrator; not executed directly.
set -euo pipefail

: "${SSH_TIMEOUT:=300}"
: "${SSH_PORT:=2222}"

SSH_OPTS=(
    -o StrictHostKeyChecking=no
    -o UserKnownHostsFile=/dev/null
    -o LogLevel=ERROR
    -o ConnectTimeout=5
    -o BatchMode=yes
)

ssh_keygen() {
    local keydir="${1:-$(mktemp -d)}"
    SSH_KEY="$keydir/id_ed25519"
    ssh-keygen -t ed25519 -f "$SSH_KEY" -N "" -q
    echo "Generated SSH keypair: $SSH_KEY"
}

vm_ssh() {
    [[ -n "${SSH_KEY:-}" ]] || { echo "Error: SSH_KEY is not set; call ssh_keygen first" >&2; return 1; }
    ssh "${SSH_OPTS[@]}" -p "$SSH_PORT" -i "$SSH_KEY" root@localhost "$@"
}

vm_scp() {
    [[ -n "${SSH_KEY:-}" ]] || { echo "Error: SSH_KEY is not set; call ssh_keygen first" >&2; return 1; }
    scp "${SSH_OPTS[@]}" -P "$SSH_PORT" -i "$SSH_KEY" "$@"
}

wait_for_ssh() {
    [[ -n "${SSH_KEY:-}" ]] || { echo "Error: SSH_KEY is not set; call ssh_keygen first" >&2; return 1; }

    local deadline elapsed=0
    deadline=$((SECONDS + SSH_TIMEOUT))
    echo "Waiting up to ${SSH_TIMEOUT}s for SSH on port ${SSH_PORT}..."

    while (( SECONDS < deadline )); do
        if vm_ssh true 2>/dev/null; then
            elapsed=$((SECONDS - (deadline - SSH_TIMEOUT)))
            echo "SSH available after ${elapsed}s"
            return 0
        fi
        sleep 2
    done

    echo "Error: SSH not reachable after ${SSH_TIMEOUT}s" >&2
    if [[ -n "${QEMU_CONSOLE_LOG:-}" && -f "$QEMU_CONSOLE_LOG" ]]; then
        echo "=== Last 50 lines of VM console ===" >&2
        tail -50 "$QEMU_CONSOLE_LOG" >&2
    fi
    return 1
}
```

Added `vm_scp()` helper (not in snosi but useful for pushing nbc binary and test scripts).

**Step 2: Verify syntax**

Run: `bash -n tests/lib/ssh.sh`
Expected: No output (no syntax errors)

**Step 3: Commit**

```bash
git add tests/lib/ssh.sh
git commit -m "test: add SSH helper library for QEMU tests

Ported from snosi's test/lib/ssh.sh. Provides ED25519 keypair
generation, SSH/SCP wrappers, and wait-for-SSH polling."
```

---

### Task 3: Create `tests/tests/01-boot.sh` -- Boot health validation

Guest-side test script that runs inside the VM via SSH.

**Files:**
- Create: `tests/tests/01-boot.sh`

**Step 1: Create the file**

```bash
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
```

**Step 2: Verify syntax**

Run: `bash -n tests/tests/01-boot.sh`
Expected: No output

**Step 3: Commit**

```bash
git add tests/tests/01-boot.sh
git commit -m "test: add boot health validation guest script

Tier 1 QEMU test: verifies system boots, root/var/boot are mounted,
and kernel cmdline contains expected parameters."
```

---

### Task 4: Create `tests/tests/02-etc-overlay.sh` -- /etc persistence overlay

**Files:**
- Create: `tests/tests/02-etc-overlay.sh`

**Step 1: Create the file**

```bash
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
```

**Step 2: Verify syntax**

Run: `bash -n tests/tests/02-etc-overlay.sh`
Expected: No output

**Step 3: Commit**

```bash
git add tests/tests/02-etc-overlay.sh
git commit -m "test: add /etc overlay persistence guest script

Tier 2 QEMU test: verifies /etc is an overlay mount, upper/work dirs
exist, and writes to /etc land in the overlay upper directory."
```

---

### Task 5: Create `tests/tests/03-ab-layout.sh` -- A/B partition layout

**Files:**
- Create: `tests/tests/03-ab-layout.sh`

**Step 1: Create the file**

```bash
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
```

**Step 2: Verify syntax**

Run: `bash -n tests/tests/03-ab-layout.sh`
Expected: No output

**Step 3: Commit**

```bash
git add tests/tests/03-ab-layout.sh
git commit -m "test: add A/B partition layout validation guest script

Tier 3 QEMU test: verifies 4 partitions with correct filesystem types,
nbc config.json exists and is valid, active root is slot A, and
inactive root is not mounted."
```

---

### Task 6: Create `tests/tests/04-update.sh` -- A/B update flow

**Files:**
- Create: `tests/tests/04-update.sh`

**Step 1: Create the file**

The `IMAGE_REF` environment variable is passed by the orchestrator when invoking this script.

```bash
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
    /usr/local/bin/nbc update --image "$IMAGE_REF" --yes --verbose

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
        bash -c "ls $MNT/usr/lib/modules/*/vmlinuz* 2>/dev/null | head -1"

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
```

**Step 2: Verify syntax**

Run: `bash -n tests/tests/04-update.sh`
Expected: No output

**Step 3: Commit**

```bash
git add tests/tests/04-update.sh
git commit -m "test: add A/B update validation guest script

Tier 4 QEMU test: runs nbc update inside the VM, verifies the inactive
root partition gets populated with a valid OS, and config.json persists."
```

---

### Task 7: Create `tests/nbc-qemu-test.sh` -- Main orchestrator

This is the main entry point that orchestrates the full test pipeline.

**Files:**
- Create: `tests/nbc-qemu-test.sh`

**Step 1: Create the file**

```bash
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

# Environment variable defaults
: "${DISK_SIZE:=35G}"
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

# Inject SSH key into root partition (partition 2 = root1 = active slot)
mount "${LOOP_DEV}p2" "$WORK_DIR/mnt"
mkdir -p "$WORK_DIR/mnt/root/.ssh"
cp "${SSH_KEY}.pub" "$WORK_DIR/mnt/root/.ssh/authorized_keys"
chmod 700 "$WORK_DIR/mnt/root/.ssh"
chmod 600 "$WORK_DIR/mnt/root/.ssh/authorized_keys"
echo "Injected SSH key into root partition"

umount "$WORK_DIR/mnt"

# Ensure sshd permits root login via the /etc overlay upper dir
mount "${LOOP_DEV}p4" "$WORK_DIR/mnt"
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
```

**Step 2: Verify syntax**

Run: `bash -n tests/nbc-qemu-test.sh`
Expected: No output

**Step 3: Make executable**

Run: `chmod +x tests/nbc-qemu-test.sh`

**Step 4: Commit**

```bash
git add tests/nbc-qemu-test.sh
git commit -m "test: add QEMU test orchestrator

Main entry point that builds nbc, installs an image to a raw disk via
loopback, injects SSH key, boots QEMU, pushes nbc into the VM, and
runs tiered guest test scripts over SSH."
```

---

### Task 8: Add `test-qemu` Makefile target

**Files:**
- Modify: `Makefile`

**Step 1: Add the target**

Add after the `test-incus-go` block (after line 86), before `test-all`:

```makefile
test-qemu: build ## Run QEMU integration tests (requires root, KVM, OVMF)
	@echo "Running QEMU integration tests (requires root)..."
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Re-running with sudo..."; \
		sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" ./tests/nbc-qemu-test.sh $(IMAGE); \
	else \
		./tests/nbc-qemu-test.sh $(IMAGE); \
	fi
```

Note: The `IMAGE` variable must be passed by the user, e.g. `make test-qemu IMAGE=ghcr.io/frostyard/snow:latest`.

**Step 2: Verify Makefile syntax**

Run: `make -n test-qemu IMAGE=test 2>&1 | head -5`
Expected: Shows the commands that would run (no syntax errors)

**Step 3: Commit**

```bash
git add Makefile
git commit -m "test: add test-qemu Makefile target

Usage: make test-qemu IMAGE=ghcr.io/frostyard/snow:latest
Requires root, KVM, and OVMF firmware."
```

---

### Task 9: Add CI workflow `.github/workflows/test-qemu.yml`

**Files:**
- Create: `.github/workflows/test-qemu.yml`

**Step 1: Create the workflow**

```yaml
name: QEMU integration tests

on:
  workflow_dispatch:
    inputs:
      image_ref:
        description: 'Container image to test'
        required: false
        default: 'ghcr.io/frostyard/snow:latest'

jobs:
  test-qemu:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    permissions:
      contents: read
      packages: read

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Free disk space
        run: |
          sudo rm -rf /usr/lib/jvm /usr/share/dotnet /usr/share/swift \
            /usr/local/.ghcup /usr/local/lib/android /opt/microsoft \
            /opt/google /opt/az /opt/hostedtoolcache
          docker system prune -af || true
          df -h

      - name: Enable KVM
        run: |
          echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | \
            sudo tee /etc/udev/rules.d/99-kvm4all.rules
          sudo udevadm control --reload-rules
          sudo udevadm trigger --name-match=kvm

      - name: Install dependencies
        run: |
          sudo apt-get update
          sudo apt-get install -y qemu-system-x86 qemu-utils ovmf podman skopeo

      - name: Login to GitHub Container Registry
        run: echo "${{ secrets.GITHUB_TOKEN }}" | podman login ghcr.io -u ${{ github.actor }} --password-stdin

      - name: Run QEMU integration tests
        run: |
          sudo -E env "PATH=$PATH:/usr/sbin:/sbin" \
            ./tests/nbc-qemu-test.sh ${{ inputs.image_ref || 'ghcr.io/frostyard/snow:latest' }}
```

**Step 2: Verify YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/test-qemu.yml'))"`
Expected: No errors

**Step 3: Commit**

```bash
git add .github/workflows/test-qemu.yml
git commit -m "ci: add QEMU integration test workflow

Manual dispatch (workflow_dispatch) that builds nbc, installs an image
to a raw disk, boots it in QEMU, and runs guest test scripts over SSH.
Requires KVM and OVMF on the runner."
```

---

### Task 10: Set executable bits and final verification

**Files:**
- Modify: `tests/nbc-qemu-test.sh` (chmod)
- Modify: `tests/tests/01-boot.sh` (chmod)
- Modify: `tests/tests/02-etc-overlay.sh` (chmod)
- Modify: `tests/tests/03-ab-layout.sh` (chmod)
- Modify: `tests/tests/04-update.sh` (chmod)

**Step 1: Set executable bits on all scripts**

Run:
```bash
chmod +x tests/nbc-qemu-test.sh tests/tests/01-boot.sh tests/tests/02-etc-overlay.sh tests/tests/03-ab-layout.sh tests/tests/04-update.sh
```

**Step 2: Run shellcheck on all scripts**

Run:
```bash
shellcheck tests/nbc-qemu-test.sh tests/lib/vm.sh tests/lib/ssh.sh tests/tests/*.sh
```

Expected: No errors (warnings about sourced files are OK).

**Step 3: Verify directory structure**

Run:
```bash
find tests/ -type f | sort
```

Expected:
```
tests/lib/ssh.sh
tests/lib/vm.sh
tests/nbc-qemu-test.sh
tests/tests/01-boot.sh
tests/tests/02-etc-overlay.sh
tests/tests/03-ab-layout.sh
tests/tests/04-update.sh
```

**Step 4: Verify Makefile integration**

Run:
```bash
make help | grep qemu
```

Expected: Shows the `test-qemu` target with description.

**Step 5: Commit if any remaining changes**

```bash
git add -A tests/
git commit -m "test: set executable bits on QEMU test scripts"
```

---

## File Summary

| File | Purpose |
|------|---------|
| `tests/lib/vm.sh` | QEMU lifecycle: OVMF discovery, VM start/stop/cleanup |
| `tests/lib/ssh.sh` | SSH: keypair gen, vm_ssh, vm_scp, wait_for_ssh |
| `tests/nbc-qemu-test.sh` | Orchestrator: build, install, inject SSH, boot, run tests |
| `tests/tests/01-boot.sh` | Guest: boot health, mounts, kernel cmdline |
| `tests/tests/02-etc-overlay.sh` | Guest: /etc overlay, persistence, upper dir writes |
| `tests/tests/03-ab-layout.sh` | Guest: 4 partitions, filesystem types, config.json, slot A active |
| `tests/tests/04-update.sh` | Guest: nbc update, inactive root populated, config persists |
| `Makefile` | New `test-qemu` target |
| `.github/workflows/test-qemu.yml` | CI workflow (manual dispatch) |
