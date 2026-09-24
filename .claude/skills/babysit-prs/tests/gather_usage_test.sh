#!/usr/bin/env bash
# Usage-level tests for gather-prs and jira-sweep. No network.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)

for name in gather-prs jira-sweep; do
  S="$HERE/../scripts/$name"
  if bash "$S" a/b extra >/dev/null 2>&1; then echo "gather_usage_test FAIL: $name with two args should exit 1"; exit 1; fi
  out=$(bash "$S" "not a slug" 2>&1 || true)
  if ! grep -q 'cannot resolve OWNER/REPO' <<<"$out"; then echo "gather_usage_test FAIL: $name bad slug message missing"; exit 1; fi
done
echo "gather_usage_test: ok"
