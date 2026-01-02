## nbc cache

Manage cached container images

### Synopsis

Manage cached container images for offline installation and staged updates.

Subcommands:
  list    - List cached images
  remove  - Remove a cached image by digest
  clear   - Clear all cached images

Examples:
  nbc cache list --install-images
  nbc cache list --update-images
  nbc cache remove sha256-abc123...
  nbc cache clear --install
  nbc cache clear --update

### Options

```
  -h, --help   help for cache
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc](nbc.md)	 - A bootc container installer for physical disks
* [nbc cache clear](nbc_cache_clear.md)	 - Clear all cached images
* [nbc cache list](nbc_cache_list.md)	 - List cached container images
* [nbc cache remove](nbc_cache_remove.md)	 - Remove a cached image by digest

