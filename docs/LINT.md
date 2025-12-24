# Container Image Linting

The `nbc lint` command checks container images for common issues that cause problems when installed with nbc. This document describes each lint check, why it matters, and how issues are remediated.

## Usage

```bash
# Lint a remote container image
nbc lint ghcr.io/myorg/myimage:latest

# Lint with JSON output (for CI/CD)
nbc lint --json docker.io/library/fedora:latest

# Lint the current filesystem (inside a container build)
nbc lint --local

# Lint and automatically fix issues (inside a container only)
nbc lint --local --fix
```

## Exit Codes

| Code | Meaning                                |
| ---- | -------------------------------------- |
| 0    | No errors found (warnings are allowed) |
| 1    | One or more errors found               |

## Lint Checks

### ssh-host-keys

**Severity:** Error

**What it checks:** Detects SSH host keys (`/etc/ssh/ssh_host_*_key` and `/etc/ssh/ssh_host_*_key.pub`) baked into the container image.

**Why it matters:** SSH host keys are used to uniquely identify a machine. If they're baked into a container image:

- Every system installed from that image will have the same host keys
- This is a security vulnerability (enables MITM attacks)
- SSH clients will show warnings when connecting to different hosts with the same keys

**Auto-fix behavior:** Removes all SSH host key files. They will be regenerated automatically at first boot by `sshd-keygen.service` or equivalent.

**Manual fix:**

```dockerfile
RUN rm -f /etc/ssh/ssh_host_*
```

---

### machine-id

**Severity:** Error

**What it checks:** Detects a non-empty `/etc/machine-id` that contains a value other than "uninitialized".

**Why it matters:** The machine-id is a unique identifier for each system. If baked into an image:

- Every system will have the same machine-id
- This breaks systemd's assumptions about unique system identity
- Can cause issues with logging, D-Bus, and other systemd services
- May cause problems with software licensing and telemetry

**Auto-fix behavior:** Truncates `/etc/machine-id` to zero length. systemd will generate a new unique ID at first boot.

**Manual fix:**

```dockerfile
RUN truncate -s 0 /etc/machine-id
# Or alternatively:
RUN echo "uninitialized" > /etc/machine-id
```

---

### random-seed

**Severity:** Warning

**What it checks:** Detects random seed files at:

- `/var/lib/systemd/random-seed`
- `/var/lib/random-seed` (legacy location)

**Why it matters:** Random seed files contain entropy used to initialize the random number generator. If shared across systems:

- Reduces the entropy available at boot
- Could potentially weaken cryptographic operations
- Not a critical security issue, but not ideal

**Auto-fix behavior:** Removes the random seed files. They will be regenerated at boot.

**Manual fix:**

```dockerfile
RUN rm -f /var/lib/systemd/random-seed /var/lib/random-seed
```

---

## Safety Checks

### Container Environment Detection

When using `--fix`, the lint command verifies it's running inside a container by checking for:

- `/.dockerenv` (Docker)
- `/run/.containerenv` (Podman)

This prevents accidentally running `--fix` on a host system, which could remove important files.

## Adding New Lint Checks

When adding a new lint check to `pkg/lint.go`:

1. Create the check function following the `LintCheck` signature
2. Register it in `RegisterDefaultChecks()`
3. **Update this document** with:
   - Check name and severity
   - What it checks
   - Why it matters
   - Auto-fix behavior
   - Manual fix instructions

See the documentation in `pkg/lint.go` for detailed implementation instructions.

## CI/CD Integration

Use JSON output for machine-readable results:

```bash
nbc lint --json ghcr.io/myorg/myimage:latest
```

Example JSON output:

```json
{
  "image": "ghcr.io/myorg/myimage:latest",
  "success": true,
  "issues": [
    {
      "check": "ssh-host-keys",
      "severity": "error",
      "message": "SSH host key found in image",
      "path": "/etc/ssh/ssh_host_rsa_key"
    }
  ],
  "error_count": 1,
  "warning_count": 0
}
```

### Dockerfile Integration

Add lint as the final step in your Dockerfile:

```dockerfile
# Install nbc (adjust for your base image)
COPY --from=ghcr.io/frostyard/nbc:latest /nbc /usr/local/bin/nbc

# Lint and fix issues
RUN nbc lint --local --fix
```

Or just lint without fixing (fail the build if issues exist):

```dockerfile
RUN nbc lint --local
```
