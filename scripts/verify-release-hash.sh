#!/usr/bin/env bash
# verify-release-hash.sh — confirm release binary SHA256 matches build record.
# Usage: verify-release-hash.sh <binary_path> <expected_sha256>
# Exit 0 if match, exit 1 if mismatch.
set -euo pipefail

BINARY="${1:-}"
EXPECTED="${2:-}"

if [ -z "$BINARY" ] || [ -z "$EXPECTED" ]; then
  echo "Usage: verify-release-hash.sh <binary_path> <expected_sha256>"
  echo "Example: verify-release-hash.sh bin/lumen FEDF2DB4A385..."
  exit 2
fi

if [ ! -f "$BINARY" ]; then
  echo "FAIL: binary not found at $BINARY"
  exit 1
fi

# Cross-platform SHA256
if command -v shasum &>/dev/null; then
  ACTUAL=$(shasum -a 256 "$BINARY" | awk '{print $1}' | tr '[:lower:]' '[:upper:]')
elif command -v sha256sum &>/dev/null; then
  ACTUAL=$(sha256sum "$BINARY" | awk '{print $1}' | tr '[:lower:]' '[:upper:]')
else
  echo "FAIL: no SHA256 tool found (shasum or sha256sum)"
  exit 1
fi

EXPECTED_UPPER=$(echo "$EXPECTED" | tr '[:lower:]' '[:upper:]')

if [ "$ACTUAL" != "$EXPECTED_UPPER" ]; then
  echo "FAIL: SHA256 mismatch"
  echo "  Expected: $EXPECTED_UPPER"
  echo "  Actual:   $ACTUAL"
  exit 1
fi

echo "PASS: $BINARY SHA256 verified ($ACTUAL)"
