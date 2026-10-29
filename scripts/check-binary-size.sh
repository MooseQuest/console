#!/usr/bin/env bash
# Guards the instrumentation performance budget (docs/design/apm.md 4.6):
#   1. the default binary links no go.opentelemetry.io package, and
#   2. the default linux/amd64 binary is within 200 KB of the recorded baseline
#      in .github/binary-size-baseline (bytes).
# Pass --update to rewrite the baseline from the current build.
set -euo pipefail

cd "$(dirname "$0")/.."
baseline_file=.github/binary-size-baseline
budget=204800 # 200 KB

# Build with exactly the go.mod floor toolchain (as scripts/dist.sh does), so the
# measured size -- and therefore --update -- is reproducible regardless of the
# locally installed Go. Go fetches the toolchain on demand.
export GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)"

if go list -deps ./cmd/console | grep -E '^go\.opentelemetry\.io'; then
  echo "FAIL: default binary depends on OpenTelemetry (see packages above)" >&2
  exit 1
fi

out=$(mktemp)
trap 'rm -f "$out"' EXIT
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$out" ./cmd/console
size=$(wc -c <"$out" | tr -d ' ')

if [ "${1:-}" = "--update" ]; then
  echo "$size" >"$baseline_file"
  echo "baseline updated to $size bytes"
  exit 0
fi

base=$(tr -d ' \n' <"$baseline_file")
growth=$((size - base))
echo "binary size: $size bytes (baseline $base, growth $growth, budget $budget)"
if [ "$growth" -gt "$budget" ]; then
  echo "FAIL: default binary grew more than $budget bytes over the baseline" >&2
  echo "If the growth is intentional, run scripts/check-binary-size.sh --update and explain it in the PR." >&2
  exit 1
fi
