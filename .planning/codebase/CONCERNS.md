# Codebase Concerns

**Analysis Date:** 2026-01-26

## Tech Debt

**Deprecated BootcInstaller API:**
- Issue: Legacy `BootcInstaller` type and `NewBootcInstaller()` function are deprecated but still present
- Files: `pkg/bootc.go:17-50`
- Impact: Dual APIs create confusion; new code may accidentally use deprecated API
- Fix approach: Move deprecated types to internal package and remove public exports in next major version

**Deprecated progressAdapter Pattern:**
- Issue: `progressAdapter` wraps new callback-based API for legacy `ProgressReporter`
- Files: `pkg/install.go:191-193`, `pkg/install.go:818`
- Impact: Added complexity bridging two progress reporting systems
- Fix approach: Complete migration to callback-based progress, remove `ProgressReporter` 

**NVMe Multipath Workaround (HACK):**
- Issue: Kernel cmdline adds `nvme_core.multipath=N` to work around unstable NVMe device naming
- Files: `pkg/bootloader.go:155-161`, `pkg/update.go:361-367`
- Impact: Disables NVMe multipath feature; not a proper long-term fix
- Fix approach: Audit all code for hardcoded `/dev/nvme*` paths, migrate to stable identifiers (`/dev/disk/by-id/*` or UUIDs everywhere)

**Hardcoded Paths Throughout:**
- Issue: Many hardcoded paths like `/tmp/nbc-install`, `/var/lib/nbc/state`, `/var/cache/nbc`
- Files: `pkg/bootc.go:46`, `pkg/config.go:13-21`, `pkg/cache.go:23-25`, `pkg/lock.go:13`
- Impact: Not configurable; difficult to test without root access
- Fix approach: Make paths configurable via config or environment variables for flexibility

**Duplicate Code in Bootloader and Update:**
- Issue: `buildKernelCmdline()` has nearly identical implementations in both files
- Files: `pkg/bootloader.go:85-166`, `pkg/update.go:315-372`
- Impact: Bug fixes must be applied twice; easy to have divergent behavior
- Fix approach: Extract shared kernel cmdline builder to common package

## Known Bugs

**Failing Cache Tests:**
- Symptoms: 7 cache-related tests fail (TestImageCache_List_EmptyCache, TestImageCache_IsCached, etc.)
- Files: `pkg/cache_test.go`
- Trigger: Running `go test ./...`
- Workaround: Tests appear to rely on specific directory state; need isolation

**Test Coverage Very Low:**
- Symptoms: Only 18.8% coverage in `pkg/`, 0% in `cmd/` and root
- Files: All packages
- Trigger: Running `go test ./... -cover`
- Workaround: None - significant coverage gaps exist

## Security Considerations

**Root Password Handling:**
- Risk: Root password passed as command argument and stored in process memory
- Files: `pkg/bootc.go:107-131`, `pkg/install.go:82-84`, `cmd/install.go:25`
- Current mitigation: Password passed via stdin to chpasswd (not visible in ps)
- Recommendations: Consider password-from-file only, or eliminate from API entirely

**LUKS Passphrase Handling:**
- Risk: Passphrase handled in memory, passed to cryptsetup via stdin
- Files: `pkg/luks.go:36-52`, `pkg/luks.go:71-79`
- Current mitigation: Uses stdin (not command-line args)
- Recommendations: Document secure passphrase handling; consider memory zeroing

**systemd-cryptenroll Passphrase Workaround:**
- Risk: Passphrase written to temporary file because systemd-cryptenroll doesn't reliably read from stdin
- Files: `pkg/luks.go:189`
- Current mitigation: Temporary file (security risk)
- Recommendations: Investigate if newer systemd versions fixed stdin support; use secure temp file with restricted permissions

**Extensive External Command Execution:**
- Risk: 74+ external command executions; potential for command injection if inputs not sanitized
- Files: `pkg/partition.go`, `pkg/luks.go`, `pkg/bootloader.go`, `pkg/disk.go`, `pkg/dracut.go`
- Current mitigation: Commands use argument arrays (not shell expansion)
- Recommendations: Audit all `exec.Command` calls for user-controlled input sanitization

**Container Image Authentication:**
- Risk: Uses default keychain for registry authentication
- Files: `pkg/bootc.go:194`, `pkg/cache.go:126`, `pkg/container.go:152`
- Current mitigation: Relies on `authn.DefaultKeychain` from go-containerregistry
- Recommendations: Document authentication requirements; consider explicit auth options

## Performance Bottlenecks

**Image Download Without Progress:**
- Problem: Image download shows spinner but no progress percentage
- Files: `pkg/cache.go:101-123`
- Cause: `remote.Image()` doesn't provide download progress callbacks
- Improvement path: Use layer-by-layer download with progress tracking

**No Retry Logic for Network Operations:**
- Problem: Network failures during image pull/digest fetch fail immediately
- Files: `pkg/cache.go:126`, `pkg/update.go:27-32`
- Cause: Single attempt with no retry on transient failures
- Improvement path: Implement exponential backoff retry for remote operations

**Single Sleep-Based Polling:**
- Problem: UI spinner uses 100ms sleep in tight loop
- Files: `pkg/cache.go:115`
- Cause: Simple implementation
- Improvement path: Use channel-based signaling or time.Ticker for cleaner implementation

## Fragile Areas

**Device Naming and Detection:**
- Files: `pkg/device_detect.go`, `pkg/partition.go:100-120`
- Why fragile: Complex logic to parse various device naming formats (nvme, sda, vd, loop, mmcblk)
- Safe modification: Add comprehensive tests for all device types before changes
- Test coverage: `pkg/device_detect_test.go` exists but coverage unclear

**Partition Scheme Logic:**
- Files: `pkg/partition.go:50-65`
- Why fragile: Hardcoded partition sizes (2GB boot, 12GB root1, 12GB root2, remainder var)
- Safe modification: Consider making sizes configurable; ensure tests cover edge cases
- Test coverage: `pkg/partition_test.go` exists

**Update Flow State Machine:**
- Files: `pkg/update.go` (1389 lines - largest file)
- Why fragile: Complex state management across prepare/perform/stage/apply phases
- Safe modification: Carefully trace state through all phases; consider state machine refactor
- Test coverage: `pkg/update_test.go` exists but file size suggests gaps

**EFI/Boot Directory Case Sensitivity:**
- Files: `pkg/bootloader.go:169-256`
- Why fragile: FAT32 case-insensitivity handling with two-step rename workaround
- Safe modification: Test on actual FAT32 filesystem; edge cases around existing directories
- Test coverage: Minimal - tests may not use real FAT32

## Scaling Limits

**Single-Threaded Image Extraction:**
- Current capacity: Sequential layer extraction
- Limit: Large images with many layers process slowly
- Scaling path: Parallel layer extraction where layer dependencies allow

**No Concurrent Installation Support:**
- Current capacity: Single installation at a time (lock file in `/var/run/nbc`)
- Limit: Cannot install to multiple devices simultaneously
- Scaling path: Per-device locking instead of global lock (if needed)

**In-Memory Layer Processing:**
- Current capacity: Layers processed through tar extraction
- Limit: Very large layers may cause memory pressure
- Scaling path: Streaming extraction with bounded memory

## Dependencies at Risk

**Docker SDK Dependency:**
- Risk: Heavy dependency for optional Docker daemon support
- Impact: Adds significant binary size; may conflict with container runtime changes
- Files: `go.mod:9`, `pkg/container.go:13`
- Migration plan: Consider making Docker support optional via build tags

**go-containerregistry:**
- Risk: Critical dependency for OCI image handling
- Impact: Breaking changes would require significant rework
- Files: `pkg/bootc.go:12-14`, `pkg/cache.go:12-14`, `pkg/container.go:14-19`
- Migration plan: Pin to specific version; evaluate alternatives if needed

## Missing Critical Features

**No Rollback Mechanism:**
- Problem: If update fails partway, system may be in inconsistent state
- Blocks: Safe automated updates
- Files: Update flow in `pkg/update.go`

**No Signature Verification:**
- Problem: Container images not verified for signatures/attestations
- Blocks: Secure supply chain requirements

**No Health Check After Update:**
- Problem: No automatic validation that updated system boots correctly
- Blocks: Automated self-healing after failed updates

## Test Coverage Gaps

**cmd/ Package - 0% Coverage:**
- What's not tested: All CLI commands (install, update, cache, lint, status, etc.)
- Files: `cmd/*.go`
- Risk: CLI argument parsing, flag handling, error messages all untested
- Priority: Medium - CLI is stable but bugs could go unnoticed

**Root Package - 0% Coverage:**
- What's not tested: Main entry point
- Files: `main.go`
- Risk: Low - trivial main function
- Priority: Low

**Integration Tests Require Root:**
- What's not tested: Full install/update flows in CI
- Files: `pkg/integration_test.go`, `pkg/*_test.go` with `RequireRoot`
- Risk: Core functionality only tested locally with sudo
- Priority: High - consider container-based testing

**LUKS/TPM2 Paths:**
- What's not tested: Encryption flows in automated tests
- Files: `pkg/luks.go`, `pkg/luks_test.go`
- Risk: Encryption bugs could cause data loss or boot failures
- Priority: High

**Bootloader Installation:**
- What's not tested: grub-install, systemd-boot setup on real EFI partitions
- Files: `pkg/bootloader.go`
- Risk: Boot failures on real hardware
- Priority: High - critical path

---

*Concerns audit: 2026-01-26*
