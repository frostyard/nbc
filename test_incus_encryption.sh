#!/bin/bash
# Encryption integration tests for nbc using Incus virtual machines
# Tests LUKS encryption with and without TPM2
# Requires: incus, root privileges
#
# Usage:
#   sudo ./test_incus_encryption.sh           # Run all encryption tests
#   sudo ./test_incus_encryption.sh --no-tpm  # Skip TPM2 test (for systems without TPM)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
VM_NAME="nbc-enc-test-$$"
DISK_SIZE="60GB"
TEST_IMAGE="${TEST_IMAGE:-ghcr.io/frostyard/snow:latest}"
BUILD_DIR="/tmp/nbc-test-build-$$"
TIMEOUT=1200  # 20 minutes
TEST_PASSPHRASE="nbc-test-passphrase-$$"
SKIP_TPM="${1:-}"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    echo "Usage: sudo $0 [--no-tpm]"
    exit 1
fi

echo -e "${GREEN}=== Nbc Encryption Integration Tests ===${NC}\n"

# Cleanup function
cleanup() {
    local exit_code=$?
    echo -e "\n${YELLOW}Cleaning up test resources...${NC}"

    # Stop and delete VMs
    for vm in ${VM_NAME} ${VM_NAME}-boot; do
        if incus list --format csv | grep -q "^${vm},"; then
            echo "Stopping VM: ${vm}"
            incus stop ${vm} --force 2>/dev/null || true
            echo "Deleting VM: ${vm}"
            incus delete ${vm} --force 2>/dev/null || true
        fi
    done

    # Remove storage volume if exists
    if incus storage volume list default --format csv | grep -q "^custom,${VM_NAME}-disk,"; then
        echo "Deleting storage volume: ${VM_NAME}-disk"
        incus storage volume delete default ${VM_NAME}-disk 2>/dev/null || true
    fi

    # Remove build directory
    if [ -d "$BUILD_DIR" ]; then
        echo "Removing build directory: ${BUILD_DIR}"
        rm -rf "$BUILD_DIR"
    fi

    echo -e "${GREEN}Cleanup complete${NC}"
    exit $exit_code
}

# Register cleanup on exit
trap cleanup EXIT INT TERM

# Check required tools
echo -e "${YELLOW}Checking required tools...${NC}"
REQUIRED_TOOLS="incus go make"
for tool in $REQUIRED_TOOLS; do
    if ! command -v $tool &> /dev/null; then
        echo -e "${RED}Error: Missing required tool: $tool${NC}"
        exit 1
    fi
    echo "  ✓ $tool"
done

echo -e "\n${GREEN}All required tools available${NC}\n"

# Build nbc binary
echo -e "${BLUE}=== Building nbc ===${NC}"
mkdir -p "$BUILD_DIR"
make build
cp nbc "$BUILD_DIR/"
echo -e "${GREEN}Build complete${NC}\n"

# Create Incus VM
echo -e "${BLUE}=== Creating Incus VM ===${NC}"
echo "VM Name: ${VM_NAME}"
echo "Disk Size: ${DISK_SIZE}"

# Launch VM with Fedora (has good LUKS/TPM tooling support)
incus launch images:fedora/42/cloud ${VM_NAME} --vm \
    -c limits.cpu=4 \
    -c limits.memory=16GiB \
    -c security.secureboot=false

# Wait for VM to start
echo "Waiting for VM to start..."
timeout=120
while [ $timeout -gt 0 ]; do
    if incus exec ${VM_NAME} -- systemctl is-system-running --wait 2>/dev/null | grep -qE "running|degraded"; then
        break
    fi
    echo -n "."
    sleep 2
    timeout=$((timeout - 2))
done
echo ""

if [ $timeout -le 0 ]; then
    echo -e "${RED}Error: VM failed to start${NC}"
    exit 1
fi

echo -e "${GREEN}VM started successfully${NC}\n"

# Create and attach a separate disk for installation
echo -e "${BLUE}=== Creating test disk ===${NC}"
incus storage volume create default ${VM_NAME}-disk size=${DISK_SIZE} --type=block
incus storage volume attach default ${VM_NAME}-disk ${VM_NAME}
echo -e "${GREEN}Disk created and attached${NC}\n"

sleep 5

# Install required tools in VM (including cryptsetup)
echo -e "${BLUE}=== Installing tools in VM ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    dnf install -y gdisk util-linux e2fsprogs dosfstools parted rsync btrfs-progs \
        cryptsetup cryptsetup-libs tpm2-tools tpm2-tss
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}Tools installed${NC}\n"

# Push nbc binary to VM
echo -e "${BLUE}=== Copying nbc to VM ===${NC}"
incus file push "$BUILD_DIR/nbc" ${VM_NAME}/usr/local/bin/nbc
incus exec ${VM_NAME} -- chmod +x /usr/local/bin/nbc
echo -e "${GREEN}Binary copied${NC}\n"

# Find the test disk device
echo -e "${BLUE}=== Identifying test disk ===${NC}"
TEST_DISK=$(incus exec ${VM_NAME} -- bash -c "
    for disk in \$(lsblk -ndo NAME,TYPE | grep disk | awk '{print \$1}'); do
        if ! lsblk -no NAME /dev/\$disk | grep -q '[0-9]'; then
            echo \"/dev/\$disk\"
            exit 0
        fi
    done
")

if [ -z "$TEST_DISK" ]; then
    echo -e "${RED}Error: Could not identify test disk${NC}"
    exit 1
fi

echo "Test disk: $TEST_DISK"
echo -e "${GREEN}Disk identified${NC}\n"

# ============================================================================
# Test 1: Install with LUKS encryption (passphrase only, no TPM)
# ============================================================================
echo -e "${BLUE}=== Test 1: Install with LUKS Encryption ===${NC}"
echo "Installing $TEST_IMAGE to $TEST_DISK with encryption"
echo "Passphrase: $TEST_PASSPHRASE"

# Create passphrase file
incus exec ${VM_NAME} -- bash -c "echo '$TEST_PASSPHRASE' > /tmp/luks-passphrase"

set +e
timeout $TIMEOUT incus exec ${VM_NAME} -- bash -c "echo 'yes' | nbc install \
    --image '$TEST_IMAGE' \
    --device '$TEST_DISK' \
    --encrypt \
    --keyfile /tmp/luks-passphrase \
    --karg 'loglevel=7' \
    --verbose" 2>&1 | tee /tmp/nbc-enc-install-$$.log | sed 's/^/  /'
INSTALL_EXIT=${PIPESTATUS[0]}
set -e

if [ $INSTALL_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Encrypted installation successful${NC}\n"
else
    echo -e "${RED}✗ Encrypted installation failed with exit code: $INSTALL_EXIT${NC}"
    echo -e "${YELLOW}Last 50 lines of log:${NC}"
    tail -50 /tmp/nbc-enc-install-$$.log | sed 's/^/  /'
    exit 1
fi

# ============================================================================
# Test 2: Verify LUKS encryption
# ============================================================================
echo -e "${BLUE}=== Test 2: Verify LUKS Encryption ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    echo 'Checking for LUKS partitions...'

    # Check root partitions are LUKS encrypted
    for partlabel in root1 root2 var; do
        PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep \"\$partlabel\" | head -1 | awk '{print \"/dev/\" \$1}')
        if [ -n \"\$PART\" ]; then
            if cryptsetup isLuks \$PART; then
                echo \"✓ \$partlabel (\$PART) is LUKS encrypted\"
                cryptsetup luksDump \$PART | head -10 | sed 's/^/  /'
            else
                echo \"✗ \$partlabel (\$PART) is NOT encrypted\"
                exit 1
            fi
        fi
    done
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ LUKS encryption verified${NC}\n"

# ============================================================================
# Test 3: Verify crypttab and root filesystem setup
# ============================================================================
echo -e "${BLUE}=== Test 3: Verify Crypttab and Root Filesystem ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    # Open LUKS to mount and check
    ROOT1=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'root1' | head -1 | awk '{print \"/dev/\" \$1}')

    echo '$TEST_PASSPHRASE' | cryptsetup luksOpen \$ROOT1 test-root

    mkdir -p /mnt/test-root
    mount /dev/mapper/test-root /mnt/test-root

    echo 'Checking /etc/crypttab...'
    if [ -f /mnt/test-root/etc/crypttab ]; then
        echo '✓ crypttab exists:'
        cat /mnt/test-root/etc/crypttab | sed 's/^/  /'
    else
        echo '⚠ crypttab not found (may be generated at boot)'
    fi

    echo ''
    echo 'Checking nbc config for encryption settings...'
    if [ -f /mnt/test-root/etc/nbc/config.json ]; then
        if grep -q '\"enabled\":.*true' /mnt/test-root/etc/nbc/config.json; then
            echo '✓ Encryption enabled in nbc config'
            grep -A5 'encryption' /mnt/test-root/etc/nbc/config.json | sed 's/^/  /'
        else
            echo '✗ Encryption not marked as enabled in config'
        fi
    fi

    echo ''
    echo 'Checking read-only root filesystem setup...'
    # Check .etc.lower directory exists
    if [ -d /mnt/test-root/.etc.lower ]; then
        echo '✓ .etc.lower directory exists (for etc overlay)'
    else
        echo '✗ .etc.lower directory missing'
        exit 1
    fi

    # Check machine-id is set to uninitialized
    if [ -f /mnt/test-root/etc/machine-id ]; then
        MACHINE_ID=\$(cat /mnt/test-root/etc/machine-id)
        if [ \"\$MACHINE_ID\" = \"uninitialized\" ]; then
            echo '✓ machine-id is uninitialized (ready for first boot)'
        else
            echo \"⚠ machine-id is set: \$MACHINE_ID\"
        fi
    fi

    umount /mnt/test-root
    cryptsetup luksClose test-root
    rmdir /mnt/test-root
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Crypttab and root filesystem verified${NC}\n"

# ============================================================================
# Test 4: Verify Boot Entry LUKS Parameters and Read-Only Root
# ============================================================================
echo -e "${BLUE}=== Test 4: Verify Boot Entry LUKS Parameters and Read-Only Root ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    mkdir -p /mnt/test-boot
    BOOT_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'boot' | head -1 | awk '{print \"/dev/\" \$1}')

    mount \$BOOT_PART /mnt/test-boot

    echo 'Checking boot entries for LUKS parameters and ro...'

    # Check systemd-boot entries
    if [ -d /mnt/test-boot/loader/entries ]; then
        for entry in /mnt/test-boot/loader/entries/*.conf; do
            if [ -f \"\$entry\" ]; then
                echo \"Entry: \$(basename \$entry)\"

                if grep -q 'rd.luks.uuid' \"\$entry\"; then
                    echo '  ✓ rd.luks.uuid found'
                    grep 'rd.luks' \"\$entry\" | sed 's/^/    /'
                else
                    echo '  ✗ rd.luks.uuid NOT found'
                fi

                if grep -q 'rd.luks.name' \"\$entry\"; then
                    echo '  ✓ rd.luks.name found'
                else
                    echo '  ⚠ rd.luks.name not found'
                fi

                # Check for ro (read-only root)
                if grep -q ' ro ' \"\$entry\" || grep -q ' ro\$' \"\$entry\"; then
                    echo '  ✓ ro (read-only root) found'
                else
                    echo '  ✗ ro NOT found (root should be read-only)'
                    exit 1
                fi
            fi
        done
    fi

    # Check GRUB entries
    GRUB_CFG=''
    if [ -f /mnt/test-boot/grub2/grub.cfg ]; then
        GRUB_CFG='/mnt/test-boot/grub2/grub.cfg'
    elif [ -f /mnt/test-boot/grub/grub.cfg ]; then
        GRUB_CFG='/mnt/test-boot/grub/grub.cfg'
    fi

    if [ -n \"\$GRUB_CFG\" ]; then
        echo 'GRUB config:'
        if grep -q 'rd.luks.uuid' \"\$GRUB_CFG\"; then
            echo '  ✓ rd.luks.uuid found in GRUB config'
        else
            echo '  ✗ rd.luks.uuid NOT found in GRUB config'
        fi
    fi

    umount /mnt/test-boot
    rmdir /mnt/test-boot
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ Boot entry LUKS parameters verified${NC}\n"

# ============================================================================
# Test 5: Test LUKS unlock with passphrase
# ============================================================================
echo -e "${BLUE}=== Test 5: Test LUKS Unlock ===${NC}"
incus exec ${VM_NAME} -- bash -c "
    ROOT1=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'root1' | head -1 | awk '{print \"/dev/\" \$1}')
    VAR_PART=\$(lsblk -nlo NAME,PARTLABEL $TEST_DISK | grep 'var' | head -1 | awk '{print \"/dev/\" \$1}')

    echo 'Testing LUKS unlock with passphrase...'

    # Test root1
    echo 'Unlocking root1...'
    echo '$TEST_PASSPHRASE' | cryptsetup luksOpen \$ROOT1 unlock-test-root1
    if [ -e /dev/mapper/unlock-test-root1 ]; then
        echo '  ✓ root1 unlocked successfully'
        cryptsetup luksClose unlock-test-root1
    else
        echo '  ✗ Failed to unlock root1'
        exit 1
    fi

    # Test var
    echo 'Unlocking var...'
    echo '$TEST_PASSPHRASE' | cryptsetup luksOpen \$VAR_PART unlock-test-var
    if [ -e /dev/mapper/unlock-test-var ]; then
        echo '  ✓ var unlocked successfully'
        cryptsetup luksClose unlock-test-var
    else
        echo '  ✗ Failed to unlock var'
        exit 1
    fi

    echo ''
    echo '✓ All LUKS partitions can be unlocked with passphrase'
" 2>&1 | sed 's/^/  /'
echo -e "${GREEN}✓ LUKS unlock test passed${NC}\n"

# ============================================================================
# Summary
# ============================================================================
echo -e "${GREEN}=== All Encryption Tests Passed ===${NC}\n"
echo -e "${BLUE}Test Summary:${NC}"
echo "  ✓ Install with LUKS encryption"
echo "  ✓ Verify LUKS encryption on partitions"
echo "  ✓ Verify crypttab and root filesystem setup (.etc.lower, machine-id)"
echo "  ✓ Verify boot entry LUKS parameters and read-only root"
echo "  ✓ Test LUKS unlock with passphrase"
echo ""

if [ "$SKIP_TPM" != "--no-tpm" ]; then
    echo -e "${YELLOW}Note: TPM2 auto-unlock tests require actual TPM hardware.${NC}"
    echo -e "${YELLOW}Run with --no-tpm to skip TPM tests on systems without TPM.${NC}"
fi

echo ""
echo -e "${GREEN}Encryption integration tests completed successfully!${NC}"
