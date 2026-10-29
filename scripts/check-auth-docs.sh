#!/usr/bin/env bash
# Guards the auth rollout packaging (issue #78):
#   1. no quickstart, README, Dockerfile or compose file mentions
#      CONSOLE_AUTH=disabled (it is documented only in
#      docs/security/runtime-hardening.md, plus release notes and the design doc);
#   2. docker-compose.yml passes CONSOLE_ADMIN_TOKENS;
#   3. any Dockerfile HEALTHCHECK probes /healthz, never /api/health.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0

quick=(README.md docs/docker.md docs/getting-started.md docker-compose.yml Dockerfile docs/index.html)
if grep -n "CONSOLE_AUTH=disabled" "${quick[@]}"; then
  echo "FAIL: CONSOLE_AUTH=disabled must not appear in quickstarts, compose or Dockerfile"
  fail=1
fi
grep -q "CONSOLE_ADMIN_TOKENS" docker-compose.yml || { echo "FAIL: docker-compose.yml must pass CONSOLE_ADMIN_TOKENS"; fail=1; }
if grep -E "^[[:space:]]*HEALTHCHECK" Dockerfile | grep -q "api/health"; then
  echo "FAIL: Dockerfile HEALTHCHECK must use /healthz"
  fail=1
fi
[ "$fail" -eq 0 ] && echo "auth docs checks passed"
exit "$fail"
