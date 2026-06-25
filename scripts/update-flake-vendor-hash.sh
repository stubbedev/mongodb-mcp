#!/usr/bin/env bash
# Recompute the Go vendorHash in flake.nix.
#
# Strategy: temporarily set vendorHash to the well-known fake hash, attempt a
# build (which fails with the expected hash), parse it, and patch flake.nix.
set -euo pipefail

cd "$(dirname "$0")/.."

FAKE="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
NIX=(nix --extra-experimental-features "nix-command flakes")

sed -i "s#vendorHash = \"sha256-[^\"]*\"#vendorHash = \"${FAKE}\"#" flake.nix

# Build is expected to fail; capture the expected hash from its output.
got="$("${NIX[@]}" build --no-link 2>&1 | grep -oE 'got: +sha256-[A-Za-z0-9+/=]+' | awk '{print $2}' | head -n1 || true)"

if [[ -z "${got}" ]]; then
  # Either it already matched (fake == real, impossible) or build succeeded.
  echo "no hash mismatch detected; vendorHash may already be correct" >&2
  exit 0
fi

sed -i "s#vendorHash = \"${FAKE}\"#vendorHash = \"${got}\"#" flake.nix
echo "vendorHash updated to ${got}"
