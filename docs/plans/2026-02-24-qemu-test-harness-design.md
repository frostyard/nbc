# QEMU Test Harness Design

## Overview

Add a QEMU-driven integration test tier for nbc that validates full install-to-boot
and A/B update flows. Modeled after snosi's `test/` infrastructure. No changes to nbc
source code -- testing enablement only.

## Goals

- Validate that nbc-installed disks actually boot in a UEFI VM
- Verify /etc overlay persistence works at runtime
- Confirm A/B partition layout is correct post-install
- Test the A/B update flow end-to-end (push nbc into VM, run update)
- Run in CI via GitHub Actions (KVM-enabled runners)
- Support both registry images and local rootfs as test subjects

## File Layout

```
tests/
├── nbc-qemu-test.sh             # Main orchestrator
├── lib/
│   ├── ssh.sh                   # SSH helpers (ported from snosi)
│   └── vm.sh                    # QEMU lifecycle (ported from snosi)
└── tests/
    ├── 01-boot.sh               # Boot health validation
    ├── 02-etc-overlay.sh        # /etc persistence overlay
    ├── 03-ab-layout.sh          # A/B partition layout
    └── 04-update.sh             # A/B update via nbc update
```

Plus:
- `Makefile` target: `test-qemu`
- `.github/workflows/test-qemu.yml`

## Orchestrator Pipeline

`nbc-qemu-test.sh` accepts an image reference and runs:

1. **Build nbc** -- `make build`
2. **Create sparse disk** -- `truncate -s 10G disk.raw`
3. **Install via nbc** -- Attach loopback, run `nbc install --image <ref> --device /dev/loopN --bootloader grub2 --yes`
4. **Inject SSH key** -- Generate ED25519 keypair, loop-mount var partition, write authorized_keys
5. **Boot QEMU** -- OVMF, KVM, Q35, virtio disk, user-mode networking (SSH forwarded), serial console to file
6. **Wait for SSH** -- Poll every 2s up to 300s, dump console log on timeout
7. **Push nbc binary** -- SCP to `/usr/local/bin/nbc` in VM
8. **Run test tiers** -- SCP + execute each `tests/*.sh`, collect exit codes
9. **Summary** -- Print pass/fail table
10. **Cleanup** -- trap-based: stop QEMU, detach loopback, remove temp files

## Libraries

### lib/vm.sh (ported from snosi)

- `find_ovmf()` -- Multi-distro OVMF firmware search (Incus-bundled, Debian, Fedora, Arch paths)
- `create_disk(path)` -- Sparse raw disk creation
- `vm_start(disk)` -- Launch QEMU: Q35, KVM, host CPU, virtio disk, OVMF pflash, user-mode networking with SSH port forward, serial console to file, headless, daemonized with PID file
- `vm_stop()` -- SIGTERM + poll for exit
- `vm_cleanup()` -- Stop VM + remove disk
- Configurable via env: `VM_MEMORY` (default 4096), `VM_CPUS` (default 2), `SSH_PORT` (default 2222)

### lib/ssh.sh (ported from snosi)

- `ssh_keygen(dir)` -- Create ED25519 keypair
- `vm_ssh(cmd...)` -- Execute command in VM via SSH (batch mode, no host key checking, 5s connect timeout)
- `wait_for_ssh()` -- Poll SSH every 2s up to `SSH_TIMEOUT` (default 300s); dump console log on failure
- `vm_scp(src, dest)` -- Copy files to VM

## Guest Test Scripts

Self-contained scripts that run inside the VM. Each uses the `check()` function for TAP-like output:

```bash
check() {
    local desc="$1"; shift
    if "$@" >/dev/null 2>&1; then
        echo "ok - $desc"; (( PASS++ )) || true
    else
        echo "not ok - $desc"; (( FAIL++ )) || true
    fi
}
```

### 01-boot.sh -- Boot Health

- System has booted (systemctl is-system-running)
- Root filesystem is mounted
- /var is mounted as separate partition
- EFI system partition exists
- Kernel command line contains expected parameters

### 02-etc-overlay.sh -- /etc Persistence

- /etc is an overlay mount
- rd.etc.overlay kernel arg present
- Overlay upper/work dirs on var partition
- Write a file to /etc, verify it persists

### 03-ab-layout.sh -- A/B Partition Layout

- 4 partitions exist (EFI, root1, root2, var)
- Correct filesystem types (FAT32, ext4 or btrfs)
- /var/lib/nbc/state/config.json exists and is valid JSON
- Active root is slot A (root1)
- Inactive root (root2) exists but is not mounted

### 04-update.sh -- A/B Update

- nbc binary available at /usr/local/bin/nbc
- Run `nbc update --image <ref> --yes`
- Verify update reports success
- Check inactive root now has content
- (Optional: reboot and verify new slot)

## SSH Key Injection

After nbc install, loop-mount the var partition (partition 4) and write the SSH public key:

```bash
mount /dev/loopNp4 "$mount_point"
mkdir -p "$mount_point/roothome/.ssh"
cat "$SSH_KEY.pub" > "$mount_point/roothome/.ssh/authorized_keys"
chmod 700 "$mount_point/roothome/.ssh"
chmod 600 "$mount_point/roothome/.ssh/authorized_keys"
umount "$mount_point"
```

Exact path depends on nbc's var layout -- verify during implementation.

## Makefile Integration

```makefile
test-qemu:
    sudo -E env "PATH=$$PATH:/usr/sbin:/sbin" ./tests/nbc-qemu-test.sh $(IMAGE)
```

Usage: `make test-qemu IMAGE=ghcr.io/frostyard/snow:latest`

## CI Workflow

`.github/workflows/test-qemu.yml`:
- Trigger: `workflow_dispatch` (manual, like snosi)
- Runner: `ubuntu-latest` with KVM enabled via udev rule
- Dependencies: `qemu-system-x86`, `qemu-utils`, `ovmf`, `podman`
- Free disk space (remove JVM, .NET, Android SDK)
- Build nbc, run `./tests/nbc-qemu-test.sh ghcr.io/frostyard/snow:latest`
- Timeout: 30 minutes
- Permissions: read-only contents and packages

## Differences from snosi

| Aspect | snosi | nbc |
|--------|-------|-----|
| Disk prep | `bootc install to-disk` via podman | `nbc install` via loopback on host |
| Binary in VM | Not needed | nbc binary pushed via SCP for update tests |
| Test focus | bootc installation, sysexts | A/B layout, /etc overlay, updates |
| Image packaging | buildah for local rootfs | Not needed (nbc handles images directly) |
