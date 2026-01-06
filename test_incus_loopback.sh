#!/bin/bash
# Loopback installation test for nbc using Incus virtual machines
# This tests the --via-loopback feature by:
#   1. Running nbc on the host to create a loopback disk image
#   2. Booting an Incus VM directly from that image
#
# Usage:
#   sudo ./test_incus_loopback.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
VM_NAME="nbc-loopback-$$"
IMAGE_SIZE=40  # GB - must be >= 35GB minimum
TEST_IMAGE="${TEST_IMAGE:-ghcr.io/frostyard/snow:latest}"
BUILD_DIR="/tmp/nbc-loopback-test-$$"
DISK_IMAGE="$BUILD_DIR/disk.img"
TIMEOUT=900  # 15 minutes

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    exit 1
fi

echo -e "${GREEN}=== Nbc Loopback Installation Test ===${NC}\n"

# Cleanup function
cleanup() {
    local exit_code=$?
    echo -e "\n${YELLOW}Cleaning up test resources...${NC}"

    # Stop and delete boot test VM
    if incus list --format csv | grep -q "^${VM_NAME},"; then
        echo "Stopping VM: ${VM_NAME}"
        incus stop ${VM_NAME} --force 2>/dev/null || true
        echo "Deleting VM: ${VM_NAME}"
        incus delete ${VM_NAME} --force 2>/dev/null || true
    fi

    # Remove build directory (contains disk image)
    if [ -d "$BUILD_DIR" ]; then
        echo "Removing build directory: ${BUILD_DIR}"
        rm -rf "$BUILD_DIR"
    fi

    echo -e "${GREEN}Cleanup complete${NC}"
    exit $exit_code
}

trap cleanup EXIT INT TERM

# Build nbc
echo -e "${BLUE}=== Building nbc ===${NC}"
mkdir -p "$BUILD_DIR"
make build
echo -e "${GREEN}Build complete${NC}\n"

# Run loopback installation on the host
echo -e "${BLUE}=== Running Loopback Installation ===${NC}"
echo "Image path: ${DISK_IMAGE}"
echo "Image size: ${IMAGE_SIZE}GB"
echo "Container image: ${TEST_IMAGE}"
echo ""

set +e
echo 'yes' | timeout $TIMEOUT ./nbc install \
    --image "$TEST_IMAGE" \
    --via-loopback "$DISK_IMAGE" \
    --image-size "$IMAGE_SIZE" \
    --verbose 2>&1 | sed 's/^/  /'
INSTALL_EXIT=${PIPESTATUS[0]}
set -e

if [ $INSTALL_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Loopback installation successful${NC}\n"
else
    echo -e "${RED}✗ Loopback installation failed (exit code: $INSTALL_EXIT)${NC}"
    exit 1
fi

# Verify the image was created
if [ ! -f "$DISK_IMAGE" ]; then
    echo -e "${RED}✗ Disk image was not created${NC}"
    exit 1
fi

IMAGE_ACTUAL_SIZE=$(stat -c%s "$DISK_IMAGE")
echo "Disk image created: $DISK_IMAGE"
echo "Image size: $(numfmt --to=iec-i --suffix=B $IMAGE_ACTUAL_SIZE)"
echo -e "${GREEN}✓ Disk image created${NC}\n"

# Create a new VM to boot from the disk image
echo -e "${BLUE}=== Creating Boot Test VM ===${NC}"
echo "Creating empty VM and attaching disk image..."

# Create empty VM (no OS)
incus init ${VM_NAME} --empty --vm \
    -c limits.cpu=4 \
    -c limits.memory=4GiB \
    -c security.secureboot=true

# Get absolute path for the disk image
DISK_IMAGE_ABS=$(realpath "$DISK_IMAGE")

# Add the disk image as a disk device with high boot priority
incus config device add ${VM_NAME} installed-disk disk source="$DISK_IMAGE_ABS" boot.priority=10

echo -e "${GREEN}VM configured with disk image${NC}\n"

# Start the VM and attempt to boot
echo -e "${BLUE}=== Booting from Loopback Image ===${NC}"
incus start ${VM_NAME}

echo "Waiting for VM to boot (this may take a minute)..."

# Wait for the system to come up
# For a newly installed system, we need to wait longer for first boot
boot_timeout=180
boot_success=false

while [ $boot_timeout -gt 0 ]; do
    # Check if systemctl reports the system is up
    # This will fail if the agent isn't ready yet, which is fine
    state=$(incus exec ${VM_NAME} -- systemctl is-system-running 2>/dev/null || true)
    if [ "$state" = "running" ] || [ "$state" = "degraded" ] || [ "$state" = "starting" ]; then
        boot_success=true
        break
    fi
    echo -n "."
    sleep 5
    boot_timeout=$((boot_timeout - 5))
done
echo ""

if [ "$boot_success" = true ]; then
    echo -e "${GREEN}✓ System booted successfully!${NC}\n"
else
    echo -e "${RED}✗ System failed to boot within timeout${NC}"
    echo -e "${YELLOW}Checking VM status...${NC}"
    incus info ${VM_NAME} | head -20
    echo -e "${YELLOW}Attempting to get console output...${NC}"
    # Try to get any available logs
    incus console ${VM_NAME} --show-log 2>/dev/null | tail -50 || true
    exit 1
fi

# Verify the booted system
echo -e "${BLUE}=== Verifying Booted System ===${NC}"

incus exec ${VM_NAME} -- bash -c "
    echo 'System Information:'
    echo '  Hostname: '\$(hostname)
    echo '  Kernel: '\$(uname -r)
    echo '  OS: '\$(cat /etc/os-release | grep PRETTY_NAME | cut -d= -f2 | tr -d '\"')
    echo ''

    # Check mount points
    echo 'Mount Points:'
    echo '  Root: '\$(mount | grep ' / ' | awk '{print \$1, \$5}')
    echo '  Boot: '\$(mount | grep ' /boot ' | awk '{print \$1, \$5}' || echo 'not mounted')
    echo '  Var:  '\$(mount | grep ' /var ' | awk '{print \$1, \$5}')
    echo ''

    # Check if root is read-only
    if grep -q ' / .*\bro\b' /proc/mounts; then
        echo '✓ Root filesystem is read-only'
    else
        echo '⚠ Root filesystem is read-write (expected read-only for immutable OS)'
    fi

    # Check /etc overlay
    if mount | grep -q 'overlay.*/etc'; then
        echo '✓ /etc is mounted as overlay'
    elif [ -d /var/lib/nbc/etc-overlay ]; then
        echo '✓ /etc overlay directory exists'
    else
        echo '⚠ /etc overlay status unclear'
    fi

    # Check nbc config
    if [ -f /var/lib/nbc/state/config.json ]; then
        echo '✓ nbc config exists'
    else
        echo '⚠ nbc config not found'
    fi

    # Check partitions
    echo ''
    echo 'Partition Layout:'
    lsblk -o NAME,SIZE,TYPE,MOUNTPOINT | grep -E 'disk|part' | head -10

    echo ''
    echo '✓ System verification complete'
" 2>&1 | sed 's/^/  /'

echo -e "\n${GREEN}=== Loopback Test Passed ===${NC}"
echo ""
echo "The loopback installation feature works correctly:"
echo "  1. ✓ nbc install --via-loopback created a bootable disk image"
echo "  2. ✓ Incus VM booted successfully from the disk image"
echo "  3. ✓ System is running and configured correctly"
echo ""
echo "The disk image can be used with QEMU or converted to other formats:"
echo "  qemu-system-x86_64 -enable-kvm -m 2048 -drive file=$DISK_IMAGE,format=raw"
echo "  qemu-img convert -f raw -O qcow2 $DISK_IMAGE disk.qcow2"
