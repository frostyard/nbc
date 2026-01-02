## nbc cache list

List cached container images

### Synopsis

List cached container images.

Use --install-images to list images staged for installation (e.g., on ISO).
Use --update-images to list images staged for updates.

With --json flag, outputs a JSON object suitable for GUI installers.

Examples:
  nbc cache list --install-images
  nbc cache list --install-images --json
  nbc cache list --update-images

```
nbc cache list [flags]
```

### Options

```
  -h, --help             help for list
      --install-images   List staged installation images
      --update-images    List staged update images
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc cache](nbc_cache.md)	 - Manage cached container images

