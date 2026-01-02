## nbc cache clear

Clear all cached images

### Synopsis

Clear all cached images from a cache directory.

Use --install to clear staged installation images.
Use --update to clear staged update images.

Examples:
  nbc cache clear --install
  nbc cache clear --update

```
nbc cache clear [flags]
```

### Options

```
  -h, --help      help for clear
      --install   Clear staged installation images
      --update    Clear staged update images
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc cache](nbc_cache.md)	 - Manage cached container images

