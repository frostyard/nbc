#!/bin/bash
# SPDX-License-Identifier: LGPL-2.1-or-later
# SSH helper library for nbc QEMU testing.
# Provides keypair generation, remote command execution, and connectivity polling.
# Sourced by the test orchestrator; not executed directly.
set -euo pipefail

: "${SSH_TIMEOUT:=300}"
: "${SSH_PORT:=2222}"

SSH_OPTS=(
    -o StrictHostKeyChecking=no
    -o UserKnownHostsFile=/dev/null
    -o LogLevel=ERROR
    -o ConnectTimeout=5
    -o BatchMode=yes
)

ssh_keygen() {
    local keydir="${1:-$(mktemp -d)}"
    SSH_KEY="$keydir/id_ed25519"
    ssh-keygen -t ed25519 -f "$SSH_KEY" -N "" -q
    echo "Generated SSH keypair: $SSH_KEY"
}

vm_ssh() {
    [[ -n "${SSH_KEY:-}" ]] || { echo "Error: SSH_KEY is not set; call ssh_keygen first" >&2; return 1; }
    ssh "${SSH_OPTS[@]}" -p "$SSH_PORT" -i "$SSH_KEY" root@localhost "$@"
}

vm_scp() {
    [[ -n "${SSH_KEY:-}" ]] || { echo "Error: SSH_KEY is not set; call ssh_keygen first" >&2; return 1; }
    scp "${SSH_OPTS[@]}" -P "$SSH_PORT" -i "$SSH_KEY" "$@"
}

wait_for_ssh() {
    [[ -n "${SSH_KEY:-}" ]] || { echo "Error: SSH_KEY is not set; call ssh_keygen first" >&2; return 1; }

    local deadline elapsed=0
    deadline=$((SECONDS + SSH_TIMEOUT))
    echo "Waiting up to ${SSH_TIMEOUT}s for SSH on port ${SSH_PORT}..."

    while (( SECONDS < deadline )); do
        if vm_ssh true 2>/dev/null; then
            elapsed=$((SECONDS - (deadline - SSH_TIMEOUT)))
            echo "SSH available after ${elapsed}s"
            return 0
        fi
        sleep 2
    done

    echo "Error: SSH not reachable after ${SSH_TIMEOUT}s" >&2
    if [[ -n "${QEMU_CONSOLE_LOG:-}" && -f "$QEMU_CONSOLE_LOG" ]]; then
        echo "=== Last 50 lines of VM console ===" >&2
        tail -50 "$QEMU_CONSOLE_LOG" >&2
    fi
    return 1
}
