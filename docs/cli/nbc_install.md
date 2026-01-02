## nbc install

Install a bootc container to a physical disk

### Synopsis

Install a bootc compatible container image to a physical disk.

This command will:
  1. Validate the target disk
  2. Pull the container image (unless --skip-pull is specified)
  3. Wipe the disk (after confirmation)
  4. Create partitions (EFI: 2GB, boot: 1GB, root1: 12GB, root2: 12GB, var: remaining)
  5. Extract container filesystem
  6. Configure system and install bootloader
  7. Verify the installation

The dual root partitions enable A/B updates for system resilience.

Supported filesystems: btrfs (default), ext4

With --json flag, outputs streaming JSON Lines for progress updates.

Example:
  nbc install --image quay.io/example/myimage:latest --device /dev/sda
  nbc install --image localhost/myimage --device /dev/nvme0n1 --filesystem ext4
  nbc install --image localhost/myimage --device /dev/nvme0n1 --karg console=ttyS0
  nbc install --image localhost/myimage --device /dev/sda --json
  nbc install --local-image sha256:abc123 --device /dev/sda  # Use staged image
  nbc install --device /dev/sda  # Auto-detect staged image on ISO

```
nbc install [flags]
```

### Options

```
  -d, --device string               Target disk device (required)
      --encrypt                     Enable LUKS full disk encryption for root and var partitions
  -f, --filesystem string           Filesystem type for root and var partitions (ext4, btrfs) (default "btrfs")
  -h, --help                        help for install
  -i, --image string                Container image reference (required unless --local-image or staged image exists)
  -k, --karg stringArray            Kernel argument to pass (can be specified multiple times)
      --keyfile string              Path to file containing LUKS passphrase (alternative to --passphrase)
      --local-image string          Use staged local image by digest (auto-detects from /var/cache/nbc/staged-install/ if not specified)
      --passphrase string           LUKS passphrase (required when --encrypt is set, unless --keyfile is provided)
      --root-password-file string   Path to file containing root password to set during installation
      --skip-pull                   Skip pulling the image (use already pulled image)
      --tpm2                        Enroll TPM2 for automatic LUKS unlock (no PCR binding)
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc](nbc.md)	 - A bootc container installer for physical disks

