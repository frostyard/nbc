# systemd.mount-extra Format Verification

## Analysis Summary

Based on systemd source code analysis, the **current code is INCORRECT**.

## Evidence from systemd Source

### 1. fstab-generator.c (mount_array_add function)

```c
static int mount_array_add(bool for_initrd, const char *str) {
        _cleanup_free_ char *what = NULL, *where = NULL, *fstype = NULL, *options = NULL;
        int r;

        r = extract_many_words(&str, ":", EXTRACT_CUNESCAPE | EXTRACT_DONT_COALESCE_SEPARATORS,
                               &what, &where, &fstype, &options);
        if (r < 0)
                return r;
        if (r < 2)
                return -EINVAL;
        if (!isempty(str))
                return -EINVAL;

        return mount_array_add_internal(for_initrd, TAKE_PTR(what), TAKE_PTR(where), fstype, TAKE_PTR(options));
}
```

**Parse order**: `what:where:fstype:options`

### 2. vmspawn.c Usage Example

```c
if (strv_extendf(&arg_kernel_cmdline_extra, "systemd.mount-extra=\"%s:%s:virtiofs:%s\"",
                 id, clean_target, mount->read_only ? "ro" : "rw") < 0)
```

Format: `<id>:<clean_target>:virtiofs:<mount_options>`

- `id` = device/source identifier
- `clean_target` = mount point destination
- `virtiofs` = filesystem type
- mount options

### 3. Standard mount(8) Terminology

From mount(8) manpage and fstab(5):

- **what** = device/source (the thing to mount FROM)
- **where** = mount point (the place to mount TO)

Example: `mount /dev/sda1 /mnt` → mount WHAT (/dev/sda1) WHERE (/mnt)

## Current Code (WRONG)

### bootloader.go line 251 and 362:

```go
"systemd.mount-extra=/var:UUID=" + varUUID + ":ext4:defaults"
```

This is parsed as:

- what = `/var` (mount point - WRONG!)
- where = `UUID=...` (device - WRONG!)
- fstype = `ext4`
- options = `defaults`

## Correct Format

Should be:

```go
"systemd.mount-extra=UUID=" + varUUID + ":/var:ext4:defaults"
```

This would be parsed as:

- what = `UUID=...` (device - CORRECT!)
- where = `/var` (mount point - CORRECT!)
- fstype = `ext4`
- options = `defaults`

## Impact

This bug would cause systemd to:

1. Try to mount the mount point `/var` as a device
2. Try to mount TO the device UUID
3. Result in mount failure during boot

The system may fail to boot or /var may not be mounted, leading to various runtime issues.

## Files Requiring Fix

1. `/home/bjk/projects/frostyard/nbc/pkg/bootloader.go` line 251 (generateGRUBConfig)
2. `/home/bjk/projects/frostyard/nbc/pkg/bootloader.go` line 355 (generateSystemdBootConfig)

Both occurrences need the format corrected to:

```go
"systemd.mount-extra=UUID=" + varUUID + ":/var:ext4:defaults"
```
