#!/bin/bash
# Integration tests for nbc using disk images and loop devices
# Requires: root privileges, losetup, sgdisk, podman, mkfs.vfat, mkfs.ext4

set -e

# Ensure go is in PATH
if ! command -v go &> /dev/null; then
    echo "Error: go command not found in PATH"
    echo "PATH: $PATH"
    exit 1
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    echo "Usage: sudo $0"
    exit 1
fi

echo -e "${GREEN}=== Nbc Integration Tests ===${NC}\n"

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up test resources...${NC}"

    # Unmount any test mounts
    for mount in $(mount | grep nbc-test | awk '{print $3}'); do
        echo "Unmounting: $mount"
        umount -f "$mount" 2>/dev/null || true
    done

    # Detach loop devices
    for loop in $(losetup -a | grep nbc-test | cut -d: -f1); do
        echo "Detaching loop device: $loop"
        losetup -d "$loop" 2>/dev/null || true
    done

    # Remove test images
    rm -f /tmp/nbc-test-*.img
    rm -rf /tmp/nbc-test-*

    # Remove test container images
    podman rmi -f localhost/nbc-test 2>/dev/null || true

    echo -e "${GREEN}Cleanup complete${NC}"
}

# Register cleanup on exit
trap cleanup EXIT

# Check required tools
echo -e "${YELLOW}Checking required tools...${NC}"
REQUIRED_TOOLS="losetup sgdisk mkfs.vfat mkfs.ext4 podman mount umount"
MISSING_TOOLS=""

for tool in $REQUIRED_TOOLS; do
    if ! command -v $tool &> /dev/null; then
        MISSING_TOOLS="$MISSING_TOOLS $tool"
    else
        echo "  ✓ $tool"
    fi
done

if [ -n "$MISSING_TOOLS" ]; then
    echo -e "${RED}Error: Missing required tools:$MISSING_TOOLS${NC}"
    exit 1
fi

echo -e "\n${GREEN}All required tools available${NC}\n"

# Run Go unit tests
echo -e "${YELLOW}Running unit tests...${NC}"
if go test -v ./pkg/... -run "^(TestFormatSize|TestGetBootDeviceFromPartition|TestGetDiskByPath)$"; then
    echo -e "${GREEN}Unit tests passed${NC}\n"
else
    echo -e "${RED}Unit tests failed${NC}"
    exit 1
fi

# Run integration tests (require root)
echo -e "${YELLOW}Running integration tests (disk operations)...${NC}"
if go test -v ./pkg/... -run "^(TestCreatePartitions|TestFormatPartitions|TestMountPartitions|TestDetectExistingPartitionScheme)$" -timeout 10m; then
    echo -e "${GREEN}Integration tests passed${NC}\n"
else
    echo -e "${RED}Integration tests failed${NC}"
    exit 1
fi

echo -e "${GREEN}=== All Tests Passed ===${NC}"
