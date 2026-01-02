## nbc lint

Check a container image for common issues

### Synopsis

Lint a container image for common issues that may cause problems
when installed with nbc.

Checks include:
  - SSH host keys (should not be baked into images)
  - machine-id (should be empty or 'uninitialized')
  - Random seed files (should not be shared)

Exit codes:
  0 - No errors found (warnings are allowed)
  1 - One or more errors found

Use --local to run inside a container build (e.g., as the last step in a
Dockerfile) to check the current filesystem instead of pulling an image.

Use --fix with --local to automatically fix issues (remove SSH keys, truncate
machine-id, etc.). Fixed issues don't count as errors.

Examples:
  # Lint a remote image
  nbc lint ghcr.io/myorg/myimage:latest
  nbc lint --json docker.io/library/fedora:latest

  # Lint the current filesystem (inside a container build)
  nbc lint --local

  # Lint and fix issues in a Dockerfile:
  # RUN nbc lint --local --fix

```
nbc lint [image] [flags]
```

### Options

```
      --fix     Automatically fix issues (only valid with --local)
  -h, --help    help for lint
      --local   Lint the current filesystem instead of a container image (for use inside container builds)
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc](nbc.md)	 - A bootc container installer for physical disks

