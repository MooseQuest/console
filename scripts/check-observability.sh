#!/usr/bin/env bash
# Consistency check for the observability assets (issue #52):
#   1. the Grafana dashboard is valid JSON;
#   2. alert rules pass `promtool check rules` when promtool is installed
#      (otherwise skipped with a notice; see docs/observability.md);
#   3. every metric name used in the dashboard and alert rules exists in Go
#      code AND is documented in docs/observability.md;
#   4. every CONSOLE_* env var in the docs/design/apm.md table is documented in
#      docs/observability.md.
# Requires: jq, grep, sed. Run from anywhere.
set -euo pipefail
cd "$(dirname "$0")/.."

dash=docs/grafana/console-dashboard.json
rules=docs/prometheus/console-alerts.yml
doc=docs/observability.md
fail=0

jq -e . "$dash" >/dev/null || { echo "FAIL: $dash is not valid JSON"; exit 1; }

if command -v promtool >/dev/null 2>&1; then
  promtool check rules "$rules" || fail=1
else
  echo "notice: promtool not found; skipping 'promtool check rules' (run it manually)"
fi

# Metric names referenced by queries, with histogram suffixes folded to the base name.
names=$( { jq -r '.. | .expr? // empty' "$dash"; cat "$rules"; } \
  | grep -oE '\b(console|go)_[a-z0-9_]+' | sed -E 's/_(bucket|count|sum)$//' | sort -u)

for n in $names; do
  if ! grep -rqF "\"$n\"" --include='*.go' --exclude='*_test.go' internal cmd; then
    # Runtime gauges are named only in a doc comment / Gauge call; accept either form.
    if ! grep -rqE "Gauge\(\"$n\"" --include='*.go' internal; then
      echo "FAIL: metric $n is not defined in code"; fail=1
    fi
  fi
  grep -qF "$n" "$doc" || { echo "FAIL: metric $n is not documented in $doc"; fail=1; }
done

for v in $(grep -oE '`CONSOLE_[A-Z_]+`' docs/design/apm.md | tr -d '`' | sort -u); do
  grep -qF "$v" "$doc" || { echo "FAIL: env var $v is not documented in $doc"; fail=1; }
done

[ "$fail" -eq 0 ] && echo "observability check OK ($(echo "$names" | wc -w | tr -d ' ') metrics)"
exit "$fail"
