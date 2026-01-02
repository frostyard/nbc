## nbc status

Show current system status

### Synopsis

Display the current nbc system status including:
  - Installed container image reference and digest
  - Boot device and active root partition (slot A or B)
  - Root filesystem mount mode (read-only or read-write)
  - Bootloader type and filesystem type
  - Staged update status (if any downloaded update is ready)

With -v (verbose), also displays:
  - Installation date and kernel arguments
  - Remote update availability check

With --json flag, outputs structured JSON including update check results.

Example:
  nbc status
  nbc status -v     # Verbose output with update check
  nbc status --json # Machine-readable JSON output

```
nbc status [flags]
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
  -n, --dry-run   dry run mode (no actual changes)
      --json      output in JSON format for machine-readable output
  -v, --verbose   verbose output
```

### SEE ALSO

* [nbc](nbc.md)	 - A bootc container installer for physical disks

