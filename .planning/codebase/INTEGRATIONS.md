# External Integrations

**Analysis Date:** 2026-01-26

## Container Registries

**OCI Container Registry:**
- Purpose: Pull bootable container images
- SDK/Client: `github.com/google/go-containerregistry`
- Auth: Uses Docker credential helpers via `authn.DefaultKeychain`
- Supports: Any OCI-compliant registry (Docker Hub, GHCR, Quay.io, private registries)

**Local Container Daemon:**
- Purpose: Access locally-built images (localhost/)
- SDK/Client: `github.com/docker/docker/client`
- Sockets tried: `/var/run/podman/podman.sock`, `/var/run/docker.sock`
- Fallback: Direct `podman` CLI for OCI layout export

## Data Storage

**Databases:**
- None - All state stored in JSON files

**File-based State:**
- System config: `/var/lib/nbc/state/config.json`
- Legacy config: `/etc/nbc/config.json` (migrated to /var/lib on write)
- Image cache: `/var/cache/nbc/staged-install/` and `/var/cache/nbc/staged-update/`
- Cache metadata: `metadata.json` per cached image

**File Storage:**
- OCI Layout directories for cached images
- Each cached image stored at `/var/cache/nbc/<digest-dir>/`

**Caching:**
- Local OCI image cache with metadata
- File-based locking for concurrent access (`/var/lock/nbc-*.lock`)

## System Integrations

**Disk Management (via exec):**
- `sgdisk` - GPT partition creation
- `parted` - Partition alignment
- `mkfs.ext4`, `mkfs.btrfs`, `mkfs.vfat` - Filesystem creation
- `blkid` - UUID/partition discovery
- `lsblk` - Disk enumeration

**LUKS Encryption (via exec):**
- `cryptsetup` - LUKS container creation, open, close
- `systemd-cryptenroll` - TPM2 key enrollment

**Bootloader (via exec):**
- `grub-install` / `grub2-install` - GRUB2 installation
- `efibootmgr` - EFI boot entry registration
- systemd-boot: Direct file copy (no install command)

**Initramfs:**
- `dracut` - Initramfs regeneration in chroot
- Custom dracut module: `95etc-overlay` (embedded in package)

**Mount/Device:**
- `mount`, `umount` - Filesystem mounting
- `losetup` - Loop device management (for testing)
- `/dev/mapper/*` - LUKS device mapper paths

## Authentication & Identity

**Container Registry Auth:**
- Provider: Docker credential helpers
- Implementation: `authn.DefaultKeychain` from go-containerregistry
- Reads: `~/.docker/config.json`, credential helpers

**System Auth:**
- No user authentication - requires root privileges
- LUKS: Passphrase via stdin or TPM2 auto-unlock

## Monitoring & Observability

**Error Tracking:**
- None (local CLI tool)

**Logs:**
- Stdout/stderr for all output
- `--verbose` flag for detailed output
- `--json` flag for machine-readable JSON Lines output

**Progress Reporting:**
- Step-based progress for long operations
- JSON Lines format for CI/automation

## CI/CD & Deployment

**Hosting:**
- GitHub repository: `github.com/frostyard/nbc`

**CI Pipeline:**
- GitHub Actions (`.github/workflows/`)
- `test.yml` - Lint, security scan, unit tests, build verification
- `release.yml` - GoReleaser Pro builds on tag push
- `snapshot.yml` - Nightly/snapshot builds

**Release Artifacts:**
- GitHub Releases (via GoReleaser)
- Frostyard package repository (via R2/Cloudflare)

**Package Distribution:**
- Cloudflare R2 bucket: `frostyardrepo`
- URL: `https://repository.frostyard.org`
- Signed with GPG (`REPOGEN_GPG_KEY` secret)

## Webhooks & Callbacks

**Incoming:**
- None

**Outgoing:**
- None

## Environment Variables

**Build-time:**
- `GORELEASER_KEY` - GoReleaser Pro license

**CI Secrets (GitHub):**
- `GITHUB_TOKEN` - GitHub release creation
- `GORELEASER_KEY` - GoReleaser Pro
- `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` - Cloudflare R2
- `CLOUDFLARE_ZONE`, `CLOUDFLARE_API_TOKEN` - Cache purge
- `REPOGEN_GPG_KEY` - Package signing

**Runtime (optional):**
- Docker credential helpers may use various env vars
- `DOCKER_CONFIG` - Docker config directory override

## TPM2 Integration

**Purpose:** Auto-unlock LUKS volumes without passphrase

**Components:**
- `systemd-cryptenroll` - Key enrollment
- `/dev/tpm0`, `/dev/tpmrm0` - TPM device nodes
- Kernel cmdline: `rd.luks.options=<UUID>=tpm2-device=auto`

**Requirements:**
- TPM2 hardware
- TPM2 support in initramfs (dracut `91tpm2-tss` module)
- `libtss2-tcti-device` library

## Boot Marker System

**Purpose:** Detect if system booted via nbc

**Implementation:**
- `tmpfiles.d` config creates `/run/nbc-booted` marker
- Location: `/usr/lib/tmpfiles.d/nbc.conf`
- Similar pattern to `/run/ostree-booted`

## External Tool Requirements

**Required at runtime (must be installed):**
- `sgdisk`, `parted` - Partitioning
- `mount`, `umount` - Filesystem operations
- `blkid`, `lsblk` - Device discovery
- `mkfs.*` - Filesystem creation
- `rsync` - File synchronization
- `efibootmgr` - UEFI boot entry management

**Required for encryption:**
- `cryptsetup` - LUKS operations
- `systemd-cryptenroll` - TPM2 enrollment (optional)

**Required for bootloader:**
- `grub-install` OR systemd-boot binaries in container image

**Optional:**
- `podman` or `docker` - For localhost:/ images
- `dracut` - For initramfs regeneration

---

*Integration audit: 2026-01-26*
