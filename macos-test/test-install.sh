#!/usr/bin/env bash
# Lerd macOS test installer — uses a locally built binary instead of downloading.
set -euo pipefail

cd "$(dirname "$0")"

case "$(uname -m)" in
  arm64)  binary="./lerd-arm64" ;;
  x86_64) binary="./lerd-amd64" ;;
  *) echo "Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

echo "Detected $(uname -m) — using $binary"
chmod +x "$binary" lerd-arm64 lerd-amd64 install.sh

# Remove the quarantine flag Gatekeeper sets on files transferred from another machine.
xattr -d com.apple.quarantine "$binary" 2>/dev/null || true

bash install.sh --local "$binary"
