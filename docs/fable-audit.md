# nbc Audit — Findings (Fable)

**Date:** 2026-07-01
**Auditor:** Claude Fable 5 (read-only review)
**Scope:** nbc source (`pkg/`, `cmd/`, `pkg/dracut/`) at commit `71b3978` (v0.16.3), cross-checked against the running `snowloaded`/snosi system this audit executed on.
**Method:** Static read of the full codebase plus read-only inspection of the live system (systemd-boot, LUKS+TPM2, btrfs A/B, `/etc` overlay, ESP, nbc state/cache). No files were modified.

Findings are sorted by impact/severity. Items tagged **[LIVE-CONFIRMED]** were reproduced against the running system, not just read in source.

---

## Severity summary

| # | Severity | Finding | Location |
|---|----------|---------|----------|
| 1 | Critical | `/etc` overlay is defeated — container `/etc` updates silently lost | `etc_persistence.go` + `dracut/95etc-overlay/etc-overlay-mount.sh` |
| 2 | Critical | Tar **symlink** escape → arbitrary host file write as root | `container.go:348` |
| 3 | Critical | Tar **hardlink** escape → host file disclosure into image | `container.go:374` |
| 4 | Critical | Non-atomic `grub.cfg` rewrite can brick both A/B boot entries | `update.go:1280` |
| 5 | High | No image signature/trust verification before root extraction | `container.go` |
| 6 | High | `fbx64.efi` installed on GRUB2 Secure Boot path (blue-screen) | `bootloader.go:898` |
| 7 | High | `/var` LUKS mapping leaked if its mount fails during update | `update.go:730` |
| 8 | High | LUKS passphrase read with `fmt.Scanln` — truncates on space, echoes | `update.go:600,710` |
| 9 | High | `MountPartitions` partial-mount leak, no rollback by caller | `partition.go:341` + `install.go:394` |
| 10 | Medium | Non-atomic write of shared A/B `config.json`, no parse-fail fallback | `config.go:126,225` |
| 11 | Medium | Update path accepts no `context.Context` — uncancellable | `update.go:552` |
| 12 | Medium | `buildKernelCmdline` duplicated across install/update (parity risk) | `bootloader.go:75` vs `system.go:283` |
| 13 | Medium | Kernel prune runs before bootloader rewrite — dangling rollback window | `update.go:967` |
| 14 | Medium | TPM2 enroll writes passphrase to `/tmp` unshredded | `luks.go:190` |
| 15 | Medium | dracut `95etc-overlay` `depends()` doesn't pull in `crypt` | `dracut/95etc-overlay/module-setup.sh:17` |
| 16 | Medium | Path-traversal tar entries silently dropped (no audit trail) | `container.go:230` |
| 17 | Medium | Duplicate `ro` kernel arg in generated `grub.cfg` | `bootloader.go:500` |
| 18 | Medium | Exclusive cache lock held for entire network pull | `cache.go:86` |
| 19 | Medium | Raw `fmt.Print` spinner in `pkg/` violates Reporter convention & races stdout | `cache.go:106` |
| 20 | Low | Flag-driven install wipes disk with no confirmation; help text overstates protection | `cmd/install.go` |
| 21 | Low | Unguarded digest slice `[:19]` can panic | `cmd/download.go:123` |
| 22 | Low | Orphaned kernel without initramfs never pruned | `update.go:1033` |
| 23 | Low | Stale `staged-install` cache never auto-evicted | `cache.go` |
| 24 | Low | Double `CloseLUKSDevices` on encrypted-install failure | `install.go:383,403` |
| 25 | Low | Boot config files not fsynced; zero-entry window during regen | `bootloader.go:706` |
| 26 | Low | `pruneBootKernelPairs` partial removal leaves orphan initrd | `update.go:1051` |
| 27 | Low | Large moby/docker dependency surface for `localhost/` image probing | `go.mod` |

---

## Critical

### 1. `/etc` overlay is defeated — container `/etc` updates are silently lost **[LIVE-CONFIRMED]**
**Location:** `pkg/etc_persistence.go` (`PopulateEtcLower`, ~L118-164) interacting with `pkg/dracut/95etc-overlay/etc-overlay-mount.sh:138-162`

The `/etc` persistence design (documented at `etc_persistence.go:33-35`) is: `lower` = base `/etc` from the container image (read-only), `upper` = only the files the user changed. On an A/B update the new container `/etc` becomes the new `lower`, so upstream default changes flow through while user edits in `upper` persist on top. For this to work, `upper` must contain **only user deltas**.

On the running system it does not:

```
/var/lib/nbc/etc-overlay/upper : 237 top-level entries
/var/lib/nbc/etc.pristine      : 225 top-level entries
lowerdir /sysroot/.etc.lower   : 0 entries (empty)
diff upper/adduser.conf pristine/adduser.conf → IDENTICAL
```

`upper` holds essentially the entire `/etc`, and files like `adduser.conf` in `upper` are byte-identical to the pristine snapshot — i.e. files the user never touched are pinned in the writable upper layer. Because overlayfs `upper` shadows `lower` per-file, **every future A/B update that changes a default `/etc` file will have no visible effect**: the stale first-boot copy in `upper` always wins. Security-relevant defaults shipped in a new image (`/etc/ssh/sshd_config`, `/etc/sudoers.d/*`, PAM, CA bundle config, etc.) will silently fail to update on an "atomic" system whose entire selling point is reliable atomic updates.

The dracut hook's own comment ("Remove empty `.etc.lower` if it exists (created by nbc during install)", `etc-overlay-mount.sh:182`) shows it was written assuming nbc creates an **empty** `.etc.lower`; `PopulateEtcLower` populating it (and/or seeding the whole `/etc` into `upper`) is a regression against that contract. The mismatch means the script takes its "merge" branch and `cp -a`'s all of `/etc` into `upper` on first boot.

**Impact:** Defeats the core value proposition (atomic `/etc` updates). Silent — no error, no warning; operators believe updates applied.
**Recommendation:** Decide on one ownership model and make nbc and the dracut module agree: either (a) nbc leaves `.etc.lower` empty and the container's `/etc` becomes the lower at boot, with `upper` seeded empty; or (b) nbc computes and stores only the user-delta in `upper` (diff vs pristine) at update time. Add a regression test asserting `upper` contains no file byte-identical to the current container `/etc`.

### 2. Tar symlink escape → arbitrary host file write as root
**Location:** `pkg/container.go:348-370` (`extractTar`, `tar.TypeSymlink`)

The traversal guard at `container.go:230-237` validates only the literal joined path of the current entry name. It never validates `header.Linkname` for symlinks, and never re-validates paths after a symlink is created. A malicious/compromised layer can:

1. Emit symlink entry `usr/lib/x → /etc` (absolute, or `../`-escaping).
2. Emit regular-file entry `usr/lib/x/cron.d/pwn` — the path string still looks contained under `targetDir`, so it passes the check at L235.

When the regular-file branch runs `os.MkdirAll(filepath.Dir(target), ...)` + `os.OpenFile(target, ...)` (L305/L310), the OS follows the attacker's symlink and writes to `/etc/cron.d/pwn` **on the host running nbc**, as root. nbc runs as root for install/update, so a typo-squatted / MITM'd / compromised image ref yields arbitrary host file write — the classic extraction-symlink vulnerability that Docker/containerd/runc defend against with lexical containment or a real chroot.

**Impact:** Root-level host compromise from an untrusted image. No existing test covers symlink/hardlink escape.
**Recommendation:** Reject symlink/hardlink entries whose resolved target escapes `targetDir`; extract through an `openat2(RESOLVE_IN_ROOT)`-style helper or `securejoin`, and never follow existing symlinks when creating parents.

### 3. Tar hardlink escape → host file disclosure into image
**Location:** `pkg/container.go:374-399` (`extractTar`, `tar.TypeLink`)

```go
case tar.TypeLink:
    linkTarget := filepath.Join(targetDir, header.Linkname)
    if err := os.Link(linkTarget, target); err != nil {
        if err := copyFile(linkTarget, target); err != nil { ... }
```

`header.Linkname` is joined with `targetDir` but never checked for traversal. Because `targetDir` is a freshly-formatted partition on a *different* device than host root, any escaping `Linkname` (e.g. `../../../../etc/shadow`) makes `os.Link` fail with `EXDEV`, and the code silently falls back to `copyFile`, which reads the host file and writes its contents into the new root. An attacker exfiltrates arbitrary root-readable host files (keys, secrets, other users' data) into a built image that may later be shipped as a bootable appliance.

**Impact:** Host secret disclosure into produced images; near-guaranteed code path since cross-device link always fails here.
**Recommendation:** Same containment fix as #2; validate `Linkname` before both `os.Link` and the `copyFile` fallback.

### 4. Non-atomic `grub.cfg` rewrite can brick both A/B boot entries
**Location:** `pkg/update.go:1280` (`os.WriteFile(grubCfgPath, …)`)

`os.WriteFile` truncates in place — no temp-file + rename, no directory fsync. GRUB2 keeps **both** the current entry and the "previous" rollback entry in this one `grub.cfg`. A crash or power loss mid-write leaves the file truncated, so the intact, previously-bootable rollback root also becomes unreachable — a full bootloader-level brick that directly contradicts the A/B "always have a safe rollback" design.

The running system uses systemd-boot (separate per-entry `bootc.conf` / `bootc-previous.conf`), which is safer, but the GRUB2 path has this hazard. Note the systemd-boot generator has a related but milder version of this: it deletes all `entries/*.conf` before writing new ones (see #25).

**Impact:** Unbootable machine on power loss during update (GRUB2 installs).
**Recommendation:** Write to `grub.cfg.tmp`, fsync, `os.Rename` over the original, fsync the directory.

---

## High

### 5. No image signature/trust verification before root extraction
**Location:** `pkg/container.go` (pull/extract path)

Images are pulled and extracted by digest via go-containerregistry (which guarantees content integrity), but there is **no cosign/sigstore/policy verification** that the image is *trusted* — grep for `cosign|sigstore|signature|policy|gpg` finds nothing in the pull path. Combined with #2/#3, this is trust-on-first-use: whatever ref is configured (or typo'd) is extracted to disk as root with no provenance check.

**Impact:** No defense against a malicious/substituted image beyond transport TLS.
**Recommendation:** Support (and default to, where feasible) signature verification against a configured public key / policy before extraction; at minimum document the trust assumption prominently.

### 6. `fbx64.efi` installed on GRUB2 Secure Boot path
**Location:** `pkg/bootloader.go:898-913` (`setupSecureBootChain`, GRUB2 branch)

The GRUB2 Secure Boot path copies `fbx64.efi` into `EFI/BOOT/`. The systemd-boot path (`setupSystemdBootSecureBootChain`, L967-970) deliberately refuses to, with the comment that `fbx64.efi` looks for `EFI/<distro>/BOOTX64.CSV` and, absent it, fails with a "Restore Boot Option" blue screen. Nothing in the tree ever writes `BOOTX64.CSV` (zero matches), and the GRUB2 layout is the same `EFI/BOOT/`. CLAUDE.md is explicit: **"Do NOT include fbx64.efi."** The GRUB2 path violates the project's own documented rule.

**Impact:** GRUB2 + Secure Boot installs can hit the blue-screen/unbootable fallback the systemd-boot code was written to avoid.
**Recommendation:** Remove the `fbx64.efi` copy from the GRUB2 path.

*(Running system is systemd-boot; `/boot/EFI/BOOT/` correctly contains only `BOOTX64.EFI`, `grubx64.efi`, `mmx64.efi` — no `fbx64.efi`. The bug is latent for GRUB2 installs.)*

### 7. `/var` LUKS mapping leaked if its mount fails during update
**Location:** `pkg/update.go:730-740`

`varLUKSOpened` is set true right after opening the `/var` LUKS container, but the `defer` that closes it is registered **after** the `mount` call and its error return. If the mount fails, the function returns before the defer is registered, leaving `/dev/mapper/var` open indefinitely. The correct pattern is used a few lines up for the root partition (defer registered immediately after open, before mount at L619-626) — it just wasn't applied to the `/var` path.

**Impact:** Orphaned dm-crypt mapping blocks clean recovery / re-runs; a later run sees `/dev/mapper/var` exists and skips re-open.
**Recommendation:** Register the `CloseLUKS` defer immediately after a successful open, before attempting the mount.

### 8. LUKS passphrase read with `fmt.Scanln`
**Location:** `pkg/update.go:600-605` and `:710-715`

```go
fmt.Print("Enter LUKS passphrase: ")
var passphrase string
_, err := fmt.Scanln(&passphrase)
```

`fmt.Scanln` stops at the first whitespace, so a passphrase containing a space is silently truncated (`correct horse battery` → `correct`), producing a confusing "wrong password" during an update — the worst time to be locked out. It also echoes the secret to the terminal (scrollback / shoulder-surf exposure). `golang.org/x/term` is already a dependency.

**Impact:** Users with spaces in their passphrase are silently locked out during update; secret echoed.
**Recommendation:** Use `term.ReadPassword` (no echo, whole-line read); apply to both call sites.

### 9. `MountPartitions` partial-mount leak with no rollback
**Location:** `pkg/partition.go:341-392` + caller `pkg/install.go:394-407`

`MountPartitions` mounts root1 → boot → var sequentially with no cleanup on failure. In `install.go`, the `UnmountPartitions`/`RemoveAll` cleanup `defer` is only registered **after** `MountPartitions` returns successfully — so if the boot or var mount fails (e.g. udev-not-settled race), root1 stays mounted and no defer ever runs.

**Impact:** Leaked mount at the mountpoint; a retried `nbc install` fails on busy device/mountpoint until manual `umount`.
**Recommendation:** Make `MountPartitions` roll back its own successful mounts on error, or register the cleanup defer before calling it.

---

## Medium

### 10. Non-atomic write of shared A/B `config.json`, no parse-fail fallback
**Location:** `pkg/config.go:126` (`WriteSystemConfig`), `:225` (`WriteSystemConfigToVar`)

`os.WriteFile(SystemConfigFile, …)` truncates in place. This file lives on the shared `/var` and is read by both A/B slots (`/var/lib/nbc/state/config.json`). A crash mid-write (it's written right after an update, `update.go:781` — exactly the risky window) corrupts it for *both* slots. `ReadSystemConfig` (`config.go:173`) only falls back to the legacy path when the new file is entirely absent (`os.IsNotExist`), not when it exists but fails to parse — so a corrupt-but-present file hard-fails `status`/`update`/`download` with no self-healing.
**Recommendation:** temp-file + rename + dir fsync; on parse error, attempt the legacy fallback and/or surface a repair path.

### 11. Update path accepts no `context.Context`
**Location:** `pkg/update.go:552` (`Update`), `:1413` (`PerformUpdate`)

Unlike `Installer.Install(ctx)` (which threads `ctx` and checks `ctx.Err()` between phases), `Update()` takes no context and every internal I/O call uses `context.Background()` (e.g. L565, 651, 669, 699, 717, 744, 776, 781; system.go:334/350; update.go:1188/1194/1257/1308/1313/1322). CLAUDE.md requires "All exported I/O functions accept `context.Context` for cancellation."
**Impact:** `nbc update` cannot be timed out or Ctrl-C-cancelled cleanly.
**Recommendation:** Thread a context through `Update`/`PerformUpdate` as install does.

### 12. `buildKernelCmdline` duplicated across install and update
**Location:** `pkg/bootloader.go:75` (install) vs `pkg/system.go:283` (update)

Two independent implementations build the kernel cmdline. They currently agree (verified: `root=`, `rd.luks.*`, `systemd.mount-extra` for `/boot` and `/var`, `rd.etc.overlay*`, the `nvme_core.multipath=N` hack, trailing user args — matching the live `bootc.conf`). But this is exactly the parity hazard CLAUDE.md's "Install and Update Parity" section warns about: a future change to one won't force the other.
**Recommendation:** Extract one shared helper (e.g. in `steps.go`) used by both.

### 13. Kernel prune runs before bootloader rewrite — dangling rollback window
**Location:** `pkg/update.go:967` (`pruneBootKernelPairs` in step 6, before `UpdateBootloader` step 7)

Pruning keeps only the new target and currently-running kernels and deletes others (L1051-1056), including the kernel the *not-yet-rewritten* `bootc-previous.conf` still references. A crash between step 6 and step 7 leaves the previous/rollback entry pointing at deleted kernel/initrd files. The default entry still boots, so not a full brick, but the rollback path is broken until the next successful update.
**Recommendation:** Rewrite bootloader entries before pruning, or compute the keep-set from the entries about to be written.

### 14. TPM2 enroll writes passphrase to `/tmp` unshredded
**Location:** `pkg/luks.go:190-209` (`EnrollTPM2`)

The passphrase is written via `os.CreateTemp("", "luks-key-*")` (resolves to `os.TempDir()`, often disk-backed in a live/rescue installer), then only `os.Remove`d — no overwrite/shred. Inconsistent with `CreateLUKSContainer`/`OpenLUKS`, which pass secrets via stdin (`--key-file -`) and never touch disk.
**Impact:** Plaintext disk-encryption passphrase recoverable from temp storage.
**Recommendation:** Pass the key via stdin/pipe/memfd; if a file is unavoidable, place it on tmpfs and shred before unlink.

### 15. dracut `95etc-overlay` `depends()` doesn't pull in `crypt`
**Location:** `pkg/dracut/95etc-overlay/module-setup.sh:17-22`

The comment says the module needs `crypt` first (to unlock `/var` before mounting the overlay) but `depends()` returns `rootfs-block` — a lower-level module `crypt` itself depends on, not the reverse. In a non-hostonly/generic image build (no LUKS device for `crypt`'s `check()` to auto-detect), `crypt` may not be included at all. `luks.go`'s `ValidateInitramfsSupport` only checks for the `90crypt` module *directory* in the source tree, not inclusion in the built initramfs, so it wouldn't catch this.
**Recommendation:** `echo "crypt rootfs-block"` in `depends()`.

### 16. Path-traversal tar entries silently dropped
**Location:** `pkg/container.go:230-237`

On a traversal-detected entry the loop just `continue`s — no `Progress` warning, no error. Combined with a lenient `VerifyExtraction`, an image with tampering attempts installs and reports success with no audit trail.
**Recommendation:** Log (and consider failing) on rejected entries.

### 17. Duplicate `ro` kernel arg in generated `grub.cfg`
**Location:** `pkg/bootloader.go:500-508` (`generateGRUBConfig`)

The filter loop over `kernelCmdline[1:]` skips only `"rw"`, but `buildKernelCmdline` places `"ro"` at index 1, so the output is `root=… ro console=tty0 ro rd.luks…`. Harmless in practice but indicates an incomplete filter.
**Recommendation:** Skip both `"ro"` and `"rw"`, or start the loop at index 2.

### 18. Exclusive cache lock held for the entire network pull
**Location:** `pkg/cache.go:86-90` (`Download`)

`AcquireCacheLock()` (exclusive, non-blocking `LOCK_NB`) is taken before `remote.Image(...)` and released only on return, so it's held for the whole transfer. Concurrent read-only ops (`list`, `status`, `IsCached`) fail immediately with "another nbc process is modifying the cache" instead of degrading.
**Recommendation:** Download to a temp area without the exclusive lock; take the exclusive lock only for the final atomic move/commit.

### 19. Raw `fmt.Print` spinner in `pkg/` violates Reporter convention and races stdout
**Location:** `pkg/cache.go:106-125` (`Download`)

A goroutine writes spinner frames directly to `os.Stdout` via `fmt.Printf`, contradicting "never raw `fmt.Print` in `pkg/`". It also races the caller's `progress.Message` writes on the stdout cursor and misbehaves for JSON/silent/log-file reporters.
**Recommendation:** Route progress through the `Reporter`.

---

## Low

### 20. Flag-driven install wipes disk with no confirmation
**Location:** `cmd/install.go` (`runInstall`); `pkg/install.go:340` comment; `pkg/disk.go:173-178` (`ValidateDisk`)

The flag path goes straight from parsing to `installer.Install()`; the only guard (`ValidateDisk`) refuses only if a target partition is *currently mounted*. An unmounted wrong `/dev/sdX` (typo, spare drive) is wiped with no prompt and no `--yes`/`--force`. The command's `Long` help says "Wipe the disk (after confirmation)" — misleading for the flag path (the interactive wizard does confirm). Notably, `ValidateDisk` also would not stop overwriting the disk currently booted from if it weren't mounted.
**Recommendation:** Require `--yes`/`--force` for the non-interactive destructive path; fix the help text; add an explicit guard against the running-system disk.

### 21. Unguarded digest slice can panic
**Location:** `cmd/download.go:123`

`remoteDigest[:19]+"..."` — the neighboring lines (128-129) and `cmd/update.go:216-217` correctly use `[:min(19, len(...))]`. A short/malformed digest panics here instead of erroring cleanly.
**Recommendation:** Use the `min` guard consistently.

### 22. Orphaned kernel without initramfs is never pruned **[LIVE-CONFIRMED]**
**Location:** `pkg/update.go:1033` (`pruneBootKernelPairs` / `findBootKernelPairs`)

`findBootKernelPairs` only returns kernels that have a matching initramfs, so a `vmlinuz-*` with no initrd is invisible to pruning and lingers forever. On the running system:

```
/boot/vmlinuz-6.19.11+deb13-amd64   (present, 13 MB)
/boot/initramfs-6.19.11*            → absent
```

The 6.19.11 kernel has no initramfs and is never cleaned up.
**Impact:** Minor ESP bloat / confusion; on a small ESP this accumulates.
**Recommendation:** Also remove `vmlinuz-*` files with no matching initrd that aren't the current/previous version.

### 23. Stale `staged-install` cache never auto-evicted **[LIVE-CONFIRMED]**
**Location:** `pkg/cache.go` (only manual `Clear`; no post-install/post-update eviction)

`staged-install` is consumed at first install but never cleaned afterward. On the running system:

```
/var/cache/nbc/staged-install  → 3.1 GB, dated Jan 8 (install was 2025-12-31)
```

A full image sits unused indefinitely. `staged-update` similarly is not evicted after a successful apply.
**Recommendation:** Evict `staged-install` after a successful install and `staged-update` after a successful update (or add a retention policy).

### 24. Double `CloseLUKSDevices` on encrypted-install failure
**Location:** `pkg/install.go:383` and `:403-410`

An unconditional `defer scheme.CloseLUKSDevices(ctx)` (L383) plus the general cleanup defer (L403-410, which also calls it when `scheme.Encrypted`) means encrypted installs close LUKS twice on failure, likely emitting a spurious "already closed" warning.
**Recommendation:** Consolidate to a single cleanup path.

### 25. Boot config files not fsynced; zero-entry window during regen
**Location:** `pkg/bootloader.go:706-726` (`generateSystemdBootConfig`), also `copyKernelFromModules` L272-277

`grub.cfg`, `loader.conf`, and `entries/*.conf` are written with plain `os.WriteFile` (no temp+rename, no dir fsync), unlike EFI binaries which go through `copyEFIFile` (size-validated + `Sync()`). Worse, the generator deletes **all** existing `entries/*.conf` before writing the new one, so there's a crash window with zero valid entries on the ESP.
**Recommendation:** Write new entry first (temp+rename), then remove stale ones; fsync files and the directory.

### 26. `pruneBootKernelPairs` partial removal leaves orphan initrd
**Location:** `pkg/update.go:1051-1056`

Kernel and initrd are removed as two separate `os.Remove`s; if the first succeeds and the second fails, the function returns leaving an orphan initrd. Self-limiting (future scans skip incomplete pairs) but leaves cruft.
**Recommendation:** Tolerate/aggregate the second removal rather than early-returning.

### 27. Large moby/docker dependency surface for `localhost/` image probing
**Location:** `go.mod` (`github.com/docker/docker v28.5.2+incompatible`, `github.com/docker/cli v29.0.3+incompatible`); used at `pkg/container.go:134`

The full docker daemon-client dependency is pulled in largely to probe `localhost/`-prefixed image refs. Worth confirming this attack/maintenance surface (arbitrary docker/podman socket API driven by `c.ImageRef`) is intended.
**Recommendation:** Confirm necessity; consider gating behind a build tag or narrowing to the minimal client.

---

## Notes / observed-healthy

- Live systemd-boot Secure Boot layout is correct: `BOOTX64.EFI` (shim), `grubx64.efi` (= systemd-boot, per the intended naming), `mmx64.efi`; no `fbx64.efi`. Secure Boot is enabled and both LUKS volumes are TPM2-enrolled (SRK, no PCR binding — matches `EnrollTPM2`'s "no PCRs" design).
- `flock`-based locking (`pkg/lock.go`) self-releases on process death — no stale-lock bug.
- Root-partition LUKS open/close ordering in `update.go` (L580-624) is correct (defer before mount) — the bug in #7 is only the `/var` path.
- `Workflow.Run` checks `ctx.Err()` before each step and wraps step errors by name.
- Extraction-failure messaging in `Update()` correctly warns the operator not to reboot and that the previous install is intact.
- The many failed `snap-*.mount` units and the tpm2 `tss` user warnings in the live journal are **pre-existing OS/snapd/tss-packaging issues, unrelated to nbc**.
