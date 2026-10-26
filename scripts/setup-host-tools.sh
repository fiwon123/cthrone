#!/bin/bash
set -euo pipefail

# setup-host-tools.sh — host bootstrap for the dev sandbox
#
# Installs/activates mise and authenticates gh as needed.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

if [[ "${1:-}" != "--no-gh" ]]; then
    if ! command -v gh >/dev/null 2>&1 || ! gh auth status >/dev/null 2>&1; then
        echo "[tools] gh CLI not authenticated — starting login (device flow)..."
        gh auth login
    else
        echo "[tools] gh CLI already authenticated."
    fi
fi

if ! command -v mise >/dev/null 2>&1; then
    echo "[tools] Installing mise (to ~/.local/bin via mise.run)..."
    curl -fsSL https://mise.run | sh
fi

if ! grep -qs 'mise activate' ~/.bashrc 2>/dev/null; then
    echo 'eval "$(mise activate bash)"' >> ~/.bashrc
    echo "[tools] Added mise activation to ~/.bashrc"
fi
eval "$(mise activate bash)" 2>/dev/null || true

echo "[tools] Installing project toolchain from mise.toml..."
(cd "$ROOT_DIR" && mise install)

echo "=== Host toolchain ready ==="
