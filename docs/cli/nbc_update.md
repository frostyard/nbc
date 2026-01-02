## nbc update

Update system to a new container image using A/B partitions

### Synopsis

Update the system by installing a new container image to the inactive root partition.

This command performs an A/B system update:
  1. Auto-detects the boot device (or use --device to override)
  2. Detects which root partition is currently active
  3. Checks if an update is available (compares image digests)
  4. Pulls the new container image (unless --skip-pull is specified)
  5. Extracts the new filesystem to the inactive root partition
  6. Updates the bootloader to boot from the new partition
  7. Keeps the old partition as a rollback option

Use --check to only check if an update is available without installing.

After update, reboot to activate the new system. The previous system remains
available in the boot menu for rollback if needed.

Use --download-only to download an update without applying it. The update
will be staged in /var/cache/nbc/staged-update/ and can be applied later
with --local-image or --auto.

With --json flag, outputs streaming JSON Lines for progress updates.

Example:
  nbc update
  nbc update --check              # Just check if update available
  nbc update --download-only      # Download but don't apply
  nbc update --local-image        # Apply staged update
  nbc update --auto               # Use staged update if available, else pull
  nbc update --image quay.io/example/myimage:v2.0
  nbc update --skip-pull
  nbc update --device /dev/sda    # Override auto-detection
  nbc update --force              # Reinstall even if up-to-date
  nbc update --json               # Machine-readable streaming output

```
nbc update [flags]
```

### Options

```
      --auto               Automatically use staged update if available, otherwise pull from registry
  -c, --check              Only check if an update is available (don't install)
  -d, --device string      Target disk device (auto-detected if not specified)
      --download-only      Download update to cache without applying
  -f, --force              Force reinstall even if system is up-to-date
  -h, --help               help for update
  -i, --image string       Container image reference (uses saved config if not specified)
  -k, --karg stringArray   Kernel argument to pass (can be specified multiple times)
      --local-image        Apply update from staged cache (/var/cache/nbc/staged-update/)
      --skip-pull          Skip pulling the image (use already pulled image)
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc](nbc.md)	 - A bootc container installer for physical disks

