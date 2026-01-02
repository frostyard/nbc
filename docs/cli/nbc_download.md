## nbc download

Download a container image to local cache

### Synopsis

Download a container image for offline installation or staged updates.

This command downloads a container image and saves it in OCI layout format
for later use. The image can be used for:

  - Offline installation: Embed on a live ISO for installation without
    internet access. Use --for-install to save to /var/cache/nbc/staged-install/

  - Staged updates: Download an update now, apply later at a convenient time.
    Use --for-update to save to /var/cache/nbc/staged-update/

Multiple installation images can be staged (e.g., different editions),
but only one update image at a time.

Examples:
  # Download image for embedding in an ISO
  nbc download --image quay.io/example/myimage:latest --for-install

  # Download update to apply later (uses image from system config)
  nbc download --for-update

  # Download specific update image
  nbc download --image quay.io/example/myimage:v2.0 --for-update

  # JSON output for scripting
  nbc download --image quay.io/example/myimage:latest --for-install --json

```
nbc download [flags]
```

### Options

```
      --for-install    Save to staged-install cache (for ISO embedding)
      --for-update     Save to staged-update cache (for offline updates)
  -h, --help           help for download
  -i, --image string   Container image reference (required for --for-install, uses system config for --for-update)
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc](nbc.md)	 - A bootc container installer for physical disks

