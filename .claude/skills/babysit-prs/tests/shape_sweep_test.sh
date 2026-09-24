#!/usr/bin/env bash
# Unit test for scripts/shape-sweep.jq against tests/fixtures/sweep-raw.json.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$(jq -f "$HERE/../scripts/shape-sweep.jq" "$HERE/fixtures/sweep-raw.json")
check() {
  if ! printf '%s' "$OUT" | jq -e "$1" >/dev/null; then echo "shape_sweep_test FAIL: $1"; exit 1; fi
}
check '.[] | select(.key == "BUILD-2460") | .action == "close" and ([.prs[].number] == [77])'
check '.[] | select(.key == "BUILD-24") | .action == "no-pr" and (.prs == [])'
check '.[] | select(.key == "BUILD-2502") | .action == "skip-open" and ([.prs[].number] == [95,80])'
check '.[] | select(.key == "BUILD-2410") | .action == "leave-unmerged"'
echo "shape_sweep_test: ok"
