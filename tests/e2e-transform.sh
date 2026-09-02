#!/bin/bash
#
# E2E test: crane export (skipped, sample data provided) → transform → apply
#
# Tests the full pipeline with the BuildConfig-to-Shipwright plugin
# using sample exported OpenShift resources.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TESTDATA_DIR="$SCRIPT_DIR/testdata"
EXPORT_DIR="$TESTDATA_DIR/export"
WORK_DIR=$(mktemp -d)
TRANSFORM_DIR="$WORK_DIR/transform"
OUTPUT_DIR="$WORK_DIR/output"
PLUGIN_DIR="$WORK_DIR/plugins"

PASS=0
FAIL=0

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

log() { echo "=== $*"; }
pass() { echo "  PASS: $*"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $*"; FAIL=$((FAIL + 1)); }
check() {
    if eval "$1"; then
        pass "$2"
    else
        fail "$2"
    fi
}

# Print the spec.volumes block of a converted Build, so volume assertions cannot
# accidentally match an identically-named field elsewhere in the document.
vol_block() {
    awk '/^  volumes:/{f=1;next} f&&/^  [a-zA-Z]/{f=0} f' "$1"
}

# Count the volume entries in that block.
vol_count() {
    vol_block "$1" | grep -c '^    - '
}

# --- Preflight: crane must be new enough to write plugin-generated resources ---
# crane v0.0.5 and earlier have no --overwrite on apply and do not write a plugin's
# NewResources. Against such a build every Shipwright Build assertion below fails, which
# reads like a plugin bug rather than a stale binary. Fail fast with a real explanation.
#
# --overwrite is necessary but not sufficient. It landed in migtools/crane 6bca8a7
# (2026-06-12), about two months before NewResources support in 24eafd8 (2026-08-13).
# A crane built in that window passes this check and still writes no Build. The commit
# .github/workflows/test-e2e-minikube-pr.yml pins is the real floor; build from there.
if ! command -v crane >/dev/null 2>&1; then
    echo "ERROR: no 'crane' on PATH. Build it from migtools/crane main and put it first on PATH."
    exit 1
fi
if ! crane apply --help 2>&1 | grep -q -- '--overwrite'; then
    echo "ERROR: the 'crane' on PATH is too old — 'crane apply' has no --overwrite flag."
    echo "       Such a build does not write plugin-generated resources, so no Shipwright"
    echo "       Build is ever produced and every assertion below fails misleadingly."
    echo "       Rebuild crane from migtools/crane main and put it first on PATH."
    exit 1
fi

# --- Build the plugin ---
log "Building plugin"
cd "$PROJECT_DIR"
# GOWORK=off matches what CI builds, and is required when this checkout is a git
# worktree nested inside the parent module — the workspace otherwise resolves the
# worktree path as a subpackage of the parent and the build fails.
GOWORK=off GOTOOLCHAIN=auto go build -o "$PLUGIN_DIR/crane-plugin-buildconfig-to-shipwright" .
echo "  Built: $PLUGIN_DIR/crane-plugin-buildconfig-to-shipwright"

# --- Verify plugin metadata ---
log "Testing plugin metadata"
METADATA=$(echo '{}' | "$PLUGIN_DIR/crane-plugin-buildconfig-to-shipwright")
check 'echo "$METADATA" | grep -q "BuildConfigPlugin"' "metadata returns plugin name"
check 'echo "$METADATA" | grep -q "registry-mapping"' "metadata lists registry-mapping flag"
check 'echo "$METADATA" | grep -q "imagestream-mapping"' "metadata lists imagestream-mapping flag"

# --- Step 1: Export (skipped — using provided sample data) ---
log "Step 1: Export (skipped — using sample data from $EXPORT_DIR)"
echo "  Sample resources:"
ls "$EXPORT_DIR/resources/myapp/" | sed 's/^/    /'

# --- Step 2: Transform ---
log "Step 2: crane transform"
crane transform \
    --export-dir "$EXPORT_DIR" \
    --transform-dir "$TRANSFORM_DIR" \
    --plugin-dir "$PLUGIN_DIR" \
    --optional-flags '{"imagestream-mapping":"openshift/nodejs:16-ubi8=registry.access.redhat.com/ubi8/nodejs-16:latest","registry-mapping":"image-registry.openshift-image-registry.svc:5000=quay.io/example","search-registries":" docker.io , ,quay.io "}' \
    --skip-plugins KubernetesPlugin \
    2>&1 | sed 's/^/  /'

# --- Step 3: Verify transform output ---
log "Step 3: Verifying transform output"

# Find the stage directory (name includes plugin name)
STAGE_DIR=$(find "$TRANSFORM_DIR" -maxdepth 1 -type d -name '*BuildConfigPlugin*' | head -1)
if [ -z "$STAGE_DIR" ]; then
    fail "no BuildConfigPlugin stage directory found in $TRANSFORM_DIR"
    echo "  Contents of $TRANSFORM_DIR:"
    ls -R "$TRANSFORM_DIR" 2>/dev/null | sed 's/^/    /'
else
    pass "stage directory exists: $(basename "$STAGE_DIR")"
fi

echo "  Transform directory contents:"
find "$TRANSFORM_DIR" -type f | sort | sed 's/^/    /'

# Check that new Shipwright Build resources were generated
check 'find "$TRANSFORM_DIR" -name "*.yaml" | xargs grep -l "shipwright.io" 2>/dev/null | head -1 | grep -q .' \
    "Shipwright Build resources generated"

# Check that Docker BuildConfig produced a buildah strategy
check 'find "$TRANSFORM_DIR" -name "*.yaml" -exec grep -l "buildah" {} + 2>/dev/null | head -1 | grep -q .' \
    "Docker strategy mapped to buildah"

# Check that S2I BuildConfig produced a source-to-image strategy
check 'find "$TRANSFORM_DIR" -name "*.yaml" -exec grep -l "source-to-image" {} + 2>/dev/null | head -1 | grep -q .' \
    "S2I strategy mapped to source-to-image"

# --- Step 4: Apply ---
log "Step 4: crane apply"
crane apply \
    --transform-dir "$TRANSFORM_DIR" \
    --output-dir "$OUTPUT_DIR" \
    --overwrite \
    2>&1 | sed 's/^/  /'

# --- Step 5: Verify final output ---
log "Step 5: Verifying final output"

echo "  Output directory contents:"
find "$OUTPUT_DIR" -type f | sort | sed 's/^/    /'

# Non-BuildConfig resources should pass through unchanged
check 'find "$OUTPUT_DIR" -name "*ConfigMap*" -type f | head -1 | grep -q .' \
    "ConfigMap passed through"
check 'find "$OUTPUT_DIR" -name "*Service*" -type f | head -1 | grep -q .' \
    "Service passed through"

# BuildConfig originals should NOT be in output (whiteout)
check '! find "$OUTPUT_DIR" -name "*BuildConfig*" -type f | grep -q .' \
    "BuildConfig resources whited out (not in output)"

# Shipwright Build resources should be in output
check 'find "$OUTPUT_DIR" -name "*.yaml" | xargs grep -l "shipwright.io/v1beta1" 2>/dev/null | head -1 | grep -q .' \
    "Shipwright Build resources in final output"

# Verify Docker BuildConfig conversion in detail
# Per-resource files only: output.yaml aggregates every resource, and which file
# find lists first is filesystem-dependent.
DOCKER_BUILD=$(find "$OUTPUT_DIR/resources" -name 'Build_*.yaml' | xargs grep -l "buildah" 2>/dev/null | head -1)
if [ -n "$DOCKER_BUILD" ]; then
    pass "Docker → Shipwright Build found: $(basename "$DOCKER_BUILD")"
    check 'grep -q "kind: Build" "$DOCKER_BUILD"' \
        "  kind is Build"
    check 'grep -q "shipwright.io/v1beta1" "$DOCKER_BUILD"' \
        "  apiVersion is shipwright.io/v1beta1"
    check 'grep -q "Dockerfile.prod" "$DOCKER_BUILD"' \
        "  dockerfile path preserved"
    check 'grep -q "quay.io/example/webapp:latest" "$DOCKER_BUILD"' \
        "  output image preserved"
    check 'grep -q "git-credentials" "$DOCKER_BUILD"' \
        "  git clone secret preserved"
    check 'grep -q "crane.konveyor.io/converted-from" "$DOCKER_BUILD"' \
        "  converted-from annotation present"

    # Strategy volumes → Build spec.volumes. The BuildConfig declares one Secret-backed
    # volume with a destinationPath; the path is deliberately NOT migrated because
    # Shipwright defines mount paths in the BuildStrategy, not in the Build.
    check '[ "$(vol_count "$DOCKER_BUILD")" -eq 1 ]' \
        "  exactly one volume converted"
    check 'vol_block "$DOCKER_BUILD" | grep -qE "^(    - |      )name: build-certs$"' \
        "  volume name preserved"
    check 'vol_block "$DOCKER_BUILD" | grep -q "secretName: build-certs"' \
        "  Secret volume source preserved"
    check '! vol_block "$DOCKER_BUILD" | grep -q "/etc/pki/ca-trust/source/anchors"' \
        "  mount destinationPath not migrated (strategy owns mount paths)"
    # BuildVolume is {Name, corev1.VolumeSource} and carries no mount path, so the
    # negative check above cannot fail on its own. The path has to survive somewhere,
    # and that somewhere is the warning. Assert it, or dropping the warning goes unnoticed.
    check 'grep -q "original BuildConfig destination paths: /etc/pki/ca-trust/source/anchors" "$DOCKER_BUILD"' \
        "  mount destinationPath preserved in the conversion warnings"

    # Registry lists are trimmed and blank entries dropped before they reach
    # paramValues; the search-registries flag above is padded on purpose.
    check 'grep -q "name: registries-search" "$DOCKER_BUILD"' \
        "  registries-search param emitted"
    check 'grep -q "value: docker.io$" "$DOCKER_BUILD" && grep -q "value: quay.io$" "$DOCKER_BUILD"' \
        "  registry entries trimmed"
    check "! grep -q \"value: ' '\" \"\$DOCKER_BUILD\"" \
        "  blank registry entry dropped"
else
    fail "Docker → Shipwright Build not found in output"
fi

# Verify S2I BuildConfig conversion in detail
S2I_BUILD=$(find "$OUTPUT_DIR/resources" -name 'Build_*.yaml' | xargs grep -l "source-to-image" 2>/dev/null | head -1)
if [ -n "$S2I_BUILD" ]; then
    pass "S2I → Shipwright Build found: $(basename "$S2I_BUILD")"
    check 'grep -q "kind: Build" "$S2I_BUILD"' \
        "  kind is Build"
    check 'grep -q "registry.access.redhat.com/ubi8/nodejs-16:latest" "$S2I_BUILD"' \
        "  builder image resolved via imagestream-mapping"
    check 'grep -q "release-2.0" "$S2I_BUILD"' \
        "  git revision preserved"

    # Same contract on the Source strategy, with a ConfigMap-backed volume.
    check '[ "$(vol_count "$S2I_BUILD")" -eq 1 ]' \
        "  exactly one volume converted"
    check 'vol_block "$S2I_BUILD" | grep -qE "^(    - |      )name: app-config$"' \
        "  volume name preserved"
    check 'vol_block "$S2I_BUILD" | grep -q "configMap:"' \
        "  ConfigMap volume source preserved"
    check '! vol_block "$S2I_BUILD" | grep -q "/etc/app-config"' \
        "  mount destinationPath not migrated (strategy owns mount paths)"
    check 'grep -q "original BuildConfig destination paths: /etc/app-config" "$S2I_BUILD"' \
        "  mount destinationPath preserved in the conversion warnings"
else
    fail "S2I → Shipwright Build not found in output"
fi

# --- Summary ---
echo ""
log "Results: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
