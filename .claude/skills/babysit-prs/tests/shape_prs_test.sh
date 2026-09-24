#!/usr/bin/env bash
# Unit test for scripts/shape-prs.jq against tests/fixtures/prs-raw.json.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$(jq -f "$HERE/../scripts/shape-prs.jq" "$HERE/fixtures/prs-raw.json")

check() {
  if ! printf '%s' "$OUT" | jq -e "$1" >/dev/null; then
    echo "shape_prs_test FAIL: $1"
    exit 1
  fi
}

check '[.[].number] == [90,95,96,97]'
check '.[] | select(.number == 97) | .jira_key == "BUILD-2439" and .freshness == "current" and (.checks | length == 0) and .feedback == 0'
check '.[] | select(.number == 96) | .jira_key == "BUILD-2501" and .freshness == "current" and .feedback == 2'
check '.[] | select(.number == 95) | .jira_key == null and .freshness == "conflict"'
check '.[] | select(.number == 95) | .checks == [{"name":"Build and unit test","workflow":"Go build and tests","conclusion":"FAILURE","run_id":"222","kind":"reproducible"}]'
check '.[] | select(.number == 90) | .draft == true and .freshness == "behind" and .feedback == 1'
check '.[] | select(.number == 90) | [.checks[] | {name, kind}] == [
  {"name":"E2E test on Minikube with Shipwright","kind":"e2e"},
  {"name":"Documentation tests","kind":"main-red"},
  {"name":"Build and unit test","kind":"infra"},
  {"name":"CodeRabbit","kind":"other"}]'
check '.[] | select(.number == 90) | .checks[] | select(.name == "Build and unit test") | .run_id == "555"'
check '.[] | select(.number == 90) | .checks[] | select(.name == "CodeRabbit") | .run_id == null and .workflow == null'
echo "shape_prs_test: ok"
