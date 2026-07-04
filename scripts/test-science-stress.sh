#!/usr/bin/env bash
# Run GUI stress tests one-by-one (small steps, won't hang the whole suite).
set -euo pipefail
cd "$(dirname "$0")/.."
TIMEOUT="${TIMEOUT:-60s}"
TESTS=(
  TestStressHealthConcurrent
  TestStressConfigAndDoctorConcurrent
  TestStressSSEClients
  TestStressMutateRateLimit
  TestStressOasisReadOnly
)
echo "▶ science stress (one test at a time, timeout=${TIMEOUT})"
for t in "${TESTS[@]}"; do
  echo "  · ${t}"
  go test ./internal/science/gui/... -count=1 -timeout "$TIMEOUT" -run "^${t}$"
done
echo "✓ stress tests passed"