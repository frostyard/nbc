## nbc cache remove

Remove a cached image by digest

### Synopsis

Remove a cached image by its digest or digest prefix.

You can specify the full digest (sha256:abc123...) or a unique prefix.

Examples:
  nbc cache remove sha256:abc123...
  nbc cache remove sha256-abc1

```
nbc cache remove <digest> [flags]
```

### Options

```
  -h, --help          help for remove
      --type string   Cache type: 'install' or 'update' (auto-detected if not specified)
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc cache](nbc_cache.md)	 - Manage cached container images

