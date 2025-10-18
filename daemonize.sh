#!/usr/bin/env bash

set -euo pipefail

# Ensure required tools are available before running `make daemonize`.
missing=()

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    missing+=("$1")
  fi
}

require_cmd go
require_cmd make
require_cmd systemctl

if ((${#missing[@]} > 0)); then
  printf "Error: missing required command(s): %s\n" "${missing[*]}" >&2
  echo "Please install the missing dependencies and re-run." >&2
  exit 1
fi

# Ensure user systemd directory exists (Makefile copies there).
mkdir -p "$HOME/.config/systemd/user"

echo "Dependencies OK. Running: make daemonize"
make daemonize

echo "Done. If desired, enable the service with:"
echo "  systemctl --user daemon-reload"
echo "  systemctl --user enable --now peerdrived.service"
