#!/usr/bin/env bash
# Moves the Shipwright pin to the newest shipwright-io/build release.
#
# The pin lives in go.mod, in hack/setup-minikube-shipwright.sh, in hack/README.md
# and in README.md, and TestReadmeVersionsMatchPins fails when they disagree, so
# this script rewrites all of them together. Modelled on shipwright-io/build's
# .github/bump-tekton-lts.sh.
#
# Usage: hack/bump-shipwright.sh [--output FILE]
#   --output FILE  append OLD_VERSION, NEW_VERSION, CHANGED, OLD_GO, NEW_GO and
#                  GO_CHANGED as key=value lines (for $GITHUB_OUTPUT)
#
# Set GITHUB_TOKEN to avoid the unauthenticated GitHub API rate limit.

set -euo pipefail

BASEDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
OUTPUT_FILE=

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      shift
      OUTPUT_FILE="$1"
      ;;
    *)
      echo "Usage: $0 [--output FILE]" >&2
      exit 1
      ;;
  esac
  shift
done

output() {
  echo "$1"
  if [ -n "${OUTPUT_FILE}" ]; then
    echo "$1" >>"${OUTPUT_FILE}"
  fi
}

cd "${BASEDIR}"
export GOWORK=off

OLD_VERSION="$(awk '$1 == "github.com/shipwright-io/build" {print $2}' go.mod)"
OLD_GO="$(awk '$1 == "go" {print $2; exit}' go.mod)"

# Newest by version, not by publish date: a patch on an older series can be
# published after a newer minor.
curl_args=(-fsSL -H "Accept: application/vnd.github+json")
if [ -n "${GITHUB_TOKEN:-}" ]; then
  curl_args+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
fi
NEW_VERSION="$(
  curl "${curl_args[@]}" 'https://api.github.com/repos/shipwright-io/build/releases?per_page=100' |
  jq -r '.[] | select(.draft == false and .prerelease == false) | .tag_name' |
  grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' |
  sort --version-sort |
  tail -n 1
)"

if [ -z "${OLD_VERSION}" ] || [ -z "${NEW_VERSION}" ]; then
  echo "could not read the current (${OLD_VERSION}) or latest (${NEW_VERSION}) Shipwright version" >&2
  exit 1
fi

output "OLD_VERSION=${OLD_VERSION}"
output "NEW_VERSION=${NEW_VERSION}"

newest="$(printf '%s\n%s\n' "${OLD_VERSION}" "${NEW_VERSION}" | sort --version-sort | tail -n 1)"
if [ "${NEW_VERSION}" = "${OLD_VERSION}" ] || [ "${newest}" = "${OLD_VERSION}" ]; then
  output "CHANGED=false"
  exit 0
fi
output "CHANGED=true"

go get "github.com/shipwright-io/build@${NEW_VERSION}"
go mod tidy
(cd tests && go mod tidy)

old_re="${OLD_VERSION//./\\.}"
perl -pi -e "s/SHIPWRIGHT_VERSION:-${old_re}\b/SHIPWRIGHT_VERSION:-${NEW_VERSION}/; s/\(default: ${old_re}\)/(default: ${NEW_VERSION})/" hack/setup-minikube-shipwright.sh
# The --k8s-version line states which Kubernetes the old release needed; a
# person has to check that against the new release, so it keeps the old number.
perl -pi -e "s/${old_re}\b/${NEW_VERSION}/g unless /required:/" hack/README.md
perl -pi -e "s/Shipwright ${old_re}\b/Shipwright ${NEW_VERSION}/g" README.md

NEW_GO="$(awk '$1 == "go" {print $2; exit}' go.mod)"
output "OLD_GO=${OLD_GO}"
output "NEW_GO=${NEW_GO}"
if [ "${OLD_GO}" = "${NEW_GO}" ]; then
  output "GO_CHANGED=false"
else
  output "GO_CHANGED=true"
fi
