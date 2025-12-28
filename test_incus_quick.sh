#!/bin/bash
# Quick installation test for nbc using Incus virtual machines
# This is a faster subset of test_incus.sh that only tests installation
# Skips A/B update tests and boot verification for faster iteration
#
# Usage:
#   sudo ./test_incus_quick.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
VM_NAME="nbc-quick-$$"
DISK_SIZE="60GB"
TEST_IMAGE="${TEST_IMAGE:-ghcr.io/frostyard/snow:latest}"
BUILD_DIR="/tmp/nbc-test-build-$$"
TIMEOUT=900  # 15 minutes

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    exit 1
fi

echo -e "${GREEN}=== Nbc Quick Installation Test ===${NC}\n"

# Cleanup function
cleanup() {
    local exit_code=$?
    echo -e "\n${YELLOW}Cleaning up test resources...${NC}"

    if incus list --format csv | grep -q "^${VM_NAME},"; then
        incus stop ${VM_NAME} --force 2>/dev/null || true
        incus delete ${VM_NAME} --force 2>/dev/null || true
    fi

    if incus storage volume list default --format csv | grep -q "^custom,${VM_NAME}-disk,"; then
        incus storage volume delete default ${VM_NAME}-disk 2>/dev/null || true
    fi

    [ -d "$BUILD_DIR" ] && rm -rf "$BUILD_DIR"
    echo -e "${GREEN}Cleanup complete${NC}"
    exit $exit_code
}

trap cleanup EXIT INT TERM

# Build
echo -e "${BLUE}=== Building nbc ===${NC}"
mkdir -p "$BUILD_DIR"
make build
cp nbc "$BUILD_DIR/"
echo -e "${GREEN}Build complete${NC}\n"

# Create VM
echo -e "${BLUE}=== Creating VM ===${NC}"
incus launch images:fedora/42/cloud ${VM_NAME} --vm \
    -c limits.cpu=4 \
    -c limits.memory=16GiB \
    -c security.secureboot=false

# Wait for VM
echo "Waiting for VM..."
timeout=60
while [ $timeout -gt 0 ]; do
    if incus exec ${VM_NAME} -- systemctl is-system-running --wait 2>/dev/null | grep -qE "running|degraded"; then
        break
    fi
    sleep 2
    timeout=$((timeout - 2))
done
echo -e "${GREEN}VM ready${NC}\n"

# Create disk
echo -e "${BLUE}=== Creating test disk ===${NC}"
incus storage volume create default ${VM_NAME}-disk size=${DISK_SIZE} --type=block
incus storage volume attach default ${VM_NAME}-disk ${VM_NAME}
sleep 5
echo -e "${GREEN}Disk ready${NC}\n"

# Install tools
echo -e "${BLUE}=== Installing tools ===${NC}"
incus exec ${VM_NAME} -- dnf install -y gdisk util-linux e2fsprogs dosfstools parted rsync btrfs-progs -q
echo -e "${GREEN}Tools installed${NC}\n"

# Push binary
incus file push "$BUILD_DIR/nbc" ${VM_NAME}/usr/local/bin/nbc
incus exec ${VM_NAME} -- chmod +x /usr/local/bin/nbc

# Find disk
TEST_DISK=$(incus exec ${VM_NAME} -- bash -c "
    for disk in \$(lsblk -ndo NAME,TYPE | grep disk | awk '{print \$1}'); do
        if ! lsblk -no NAME /dev/\$disk | grep -q '[0-9]'; then
            echo \"/dev/\$disk\"
            exit 0
        fi
    done
")
echo "Test disk: $TEST_DISK"

# Install
echo -e "${BLUE}=== Installing to disk ===${NC}"
set +e
timeout $TIMEOUT incus exec ${VM_NAME} -- bash -c "echo 'yes' | nbc install \
    --image '$TEST_IMAGE' \
    --device '$TEST_DISK' \
    --verbose" 2>&1 | sed 's/^/  /'
INSTALL_EXIT=${PIPESTATUS[0]}
set -e

if [ $INSTALL_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Installation successful${NC}\n"
else
    echo -e "${RED}✗ Installation failed${NC}"
    exit 1
fi

# Quick verification
echo -e "${BLUE}=== Quick Verification ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    # Check partitions
    PARTITIONS=\$(lsblk -n $TEST_DISK | grep -c part || true)
    echo \"Partitions: \$PARTITIONS\"
    [ \$PARTITIONS -eq 4 ] && echo '✓ Correct partition count' || exit 1

    # Check root filesystem
    ROOT=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep root1 | awk '{print \"/dev/\"\$1}')
    mkdir -p /mnt/check
    mount \$ROOT /mnt/check
    [ -f /mnt/check/etc/nbc/config.json ] && echo '✓ Config exists' || exit 1
    [ -d /mnt/check/usr/lib/dracut/modules.d/95etc-overlay ] && echo '✓ Dracut module exists' || exit 1
    [ -d /mnt/check/.etc.lower ] && echo '✓ .etc.lower directory exists' || exit 1

    # Verify machine-id is set to uninitialized
    if [ -f /mnt/check/etc/machine-id ]; then
        MACHINE_ID=\$(cat /mnt/check/etc/machine-id)
        if [ \"\$MACHINE_ID\" = \"uninitialized\" ]; then
            echo '✓ machine-id is uninitialized (ready for first boot)'
        else
            echo \"⚠ machine-id is set: \$MACHINE_ID\"
        fi
    fi
    umount /mnt/check

    # Check boot partition and verify ro kernel parameter
    BOOT=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep boot | awk '{print \"/dev/\"\$1}')
    mount \$BOOT /mnt/check
    ls /mnt/check/vmlinuz-* >/dev/null 2>&1 && echo '✓ Kernel exists' || exit 1

    # Check for ro in boot config
    if grep -r ' ro ' /mnt/check/loader/entries/*.conf 2>/dev/null || \
       grep -r ' ro ' /mnt/check/grub/grub.cfg 2>/dev/null || \
       grep -r ' ro ' /mnt/check/grub2/grub.cfg 2>/dev/null; then
        echo '✓ ro (read-only root) in boot config'
    else
        echo '⚠ ro not found in boot config (checking for ro parameter)'
    fi
    umount /mnt/check
    rmdir /mnt/check
" 2>&1 | sed 's/^/  /'

echo -e "\n${GREEN}=== Quick Test Passed ===${NC}"
echo "For full testing including A/B updates and boot verification, run test_incus.sh"
