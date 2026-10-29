#!/usr/bin/env bash
# Guards the auth rollout packaging (issue #78):
#   1. no quickstart or user-facing doc, compose file or Dockerfile sets
#      CONSOLE_AUTH to "disabled" in any form (VAR=disabled, VAR: disabled,
#      quoted values, `-e VAR disabled`, ...). The allowed locations are
#      excluded explicitly by not being in the scan list:
#        docs/security/runtime-hardening.md, the CHANGELOG upgrade checklist,
#        and the auth design doc;
#   2. docker-compose.yml passes CONSOLE_ADMIN_TOKENS;
#   3. any Dockerfile HEALTHCHECK (continuations joined) probes /healthz,
#      and none uses /api/health.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0

scan=(README.md docs/getting-started.md docs/docker.md docs/observability.md
  docs/mcp.md docs/api.md docs/status.md docs/index.html docker-compose.yml Dockerfile)
for f in "${scan[@]}"; do
  [ -f "$f" ] || { echo "FAIL: required file missing: $f"; exit 2; }
done

# CONSOLE_AUTH, optional quote, optional separator (= or :) or space, optional quote, disabled.
re='CONSOLE_AUTH["'"'"']?[[:space:]]*[=:]?[[:space:]]*["'"'"']?disabled'
if grep -niE "$re" "${scan[@]}"; then
  echo "FAIL: CONSOLE_AUTH=disabled must not appear in quickstarts, user docs, compose or Dockerfile"
  fail=1
fi
grep -q "CONSOLE_ADMIN_TOKENS" docker-compose.yml || { echo "FAIL: docker-compose.yml must pass CONSOLE_ADMIN_TOKENS"; fail=1; }

# Join backslash continuations, then inspect HEALTHCHECK instructions.
joined=$(sed -e ':a' -e '/\\$/{N;s/\\\n[[:space:]]*/ /;ta' -e '}' Dockerfile)
hc=$(grep -iE '^[[:space:]]*HEALTHCHECK' <<<"$joined" || true)
if [ -n "$hc" ]; then
  if grep -q "api/health" <<<"$hc"; then
    echo "FAIL: Dockerfile HEALTHCHECK must not use /api/health"
    fail=1
  fi
  if ! grep -q "/healthz" <<<"$hc"; then
    echo "FAIL: Dockerfile HEALTHCHECK must probe /healthz"
    fail=1
  fi
fi
[ "$fail" -eq 0 ] && echo "auth docs checks passed"
exit "$fail"
