#!/bin/bash
#
# Cluster E2E test: BuildConfig → crane transform → Shipwright Build on a live cluster
#
# Runs one or more test cases against a Minikube cluster that already has Tekton,
# Shipwright, the source-to-image ClusterBuildStrategy, the registry addon, and the
# fake BuildConfig CRD installed (see hack/setup-minikube-shipwright.sh and
# hack/fake-minikube-buildconfig.sh).
#
# Each test case lives in tests/testdata/e2e-<case>/ and contains:
#   buildconfig.yaml      the source BuildConfig applied to the cluster
#   testcase.env              config + transform flags + cluster/build expectations
#   expected-build.yaml     full expected Build YAML, diffed against the output
#
# Per case the flow is:
#   1. Apply the source BuildConfig to the cluster.
#   2. Convert it with the standard `crane transform` + `crane apply` flow.
#   3. Diff the generated Shipwright Build manifest against the expected YAML
#      golden (expected-build.yaml).
#   4. Apply the Build and confirm Shipwright registers it (if EXPECT_REGISTERED).
#   5. Run a BuildRun and confirm the build result (if RUN_BUILDRUN).
#
# Prerequisites:
#   - kubectl pointing at the target cluster
#   - crane CLI on PATH
#   - Go toolchain (to build the plugin)
#
# Usage:
#   ./tests/e2e-cluster.sh [OPTIONS] [CASE...]
#
# With no CASE arguments every tests/testdata/e2e-* case runs.
# A CASE argument is the directory name, e.g. e2e-s2i-imagestream-nodejs.
#
# Options:
#   --skip-build   Verify the manifest only; skip applying and running the build
#   --keep         Do not delete the test resources on exit
#   --help         Show this help
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CASES_DIR="$SCRIPT_DIR/testdata"
# Test-case dirs are named e2e-* (keeps other testdata, e.g. export/, out of scope).
CASE_GLOB="e2e-*"
WORK_DIR=$(mktemp -d)
PLUGIN_DIR="$WORK_DIR/plugins"
mkdir -p "$PLUGIN_DIR"
PLUGIN_BIN="$PLUGIN_DIR/crane-plugin-buildconfig-to-builds"

# Internal OpenShift registry the fallback output URL uses; testcase.env rewrites it.
export OCP_REGISTRY="image-registry.openshift-image-registry.svc:5000"

SKIP_BUILD=false
KEEP=false
CASES=()

PASS=0
FAIL=0

log()  { echo "=== $*"; }
info() { echo "  $*"; }
pass() { echo "  PASS: $*"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $*"; FAIL=$((FAIL + 1)); }

show_help() {
    sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --skip-build) SKIP_BUILD=true; shift ;;
            --keep) KEEP=true; shift ;;
            --help) show_help ;;
            -*) echo "ERROR: Unknown option: $1" >&2; exit 1 ;;
            *) CASES+=("$1"); shift ;;
        esac
    done
}

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

check_prereqs() {
    command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl not found" >&2; exit 1; }
    command -v crane   >/dev/null 2>&1 || { echo "ERROR: crane CLI not found" >&2; exit 1; }
    kubectl cluster-info >/dev/null 2>&1 || { echo "ERROR: not connected to a cluster" >&2; exit 1; }

    if ! kubectl get crd buildconfigs.build.openshift.io >/dev/null 2>&1; then
        echo "ERROR: BuildConfig CRD missing. Run ./hack/fake-minikube-buildconfig.sh first." >&2
        exit 1
    fi
    # The ClusterBuildStrategy a case needs is case-specific (see BUILD_STRATEGY
    # in testcase.env); it is verified per case in run_case. Here we only confirm
    # Shipwright's strategy CRD is present at all.
    if ! kubectl get crd clusterbuildstrategies.shipwright.io >/dev/null 2>&1; then
        echo "ERROR: Shipwright not installed. Run ./hack/setup-minikube-shipwright.sh first." >&2
        exit 1
    fi
}

# Expand the ${VAR} references a case may use in its assertion patterns.
expand() {
    local s="$1"
    s="${s//\$\{NAMESPACE\}/$NAMESPACE}"
    s="${s//\$\{BUILD_NAME\}/$BUILD_NAME}"
    s="${s//\$\{BUILDER_IMAGE\}/$BUILDER_IMAGE}"
    s="${s//\$\{REGISTRY\}/$REGISTRY}"
    s="${s//\$\{BUILD_STRATEGY\}/$BUILD_STRATEGY}"
    printf '%s' "$s"
}

# Diff the generated Build manifest against the expected YAML golden, expanding
# the ${VAR} placeholders in the golden first. The full unified diff is printed
# on mismatch so the exact field that drifted is visible before any cluster apply.
assert_manifest_yaml() {
    local manifest="$1" golden="$2" expanded difffile line
    expanded=$(mktemp -p "$WORK_DIR")
    difffile=$(mktemp -p "$WORK_DIR")
    while IFS= read -r line || [ -n "$line" ]; do
        expand "$line"; printf '\n'
    done < "$golden" > "$expanded"
    if diff -u "$expanded" "$manifest" > "$difffile" 2>&1; then
        pass "manifest: matches expected YAML"
    else
        fail "manifest: differs from expected YAML"
        sed 's/^/    /' "$difffile"
    fi
}

collect_diagnostics() {
    local br
    echo ""
    log "Diagnostics for namespace $NAMESPACE"
    kubectl get build,buildrun,pods -n "$NAMESPACE" 2>/dev/null || true
    br=$(kubectl get buildrun -n "$NAMESPACE" -l "build.shipwright.io/name=$BUILD_NAME" \
        -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null || true)
    if [ -n "$br" ]; then
        echo "--- BuildRun $br ---"
        kubectl describe buildrun "$br" -n "$NAMESPACE" 2>/dev/null || true
        echo "--- BuildRun $br pod logs ---"
        kubectl logs -n "$NAMESPACE" -l "buildrun.shipwright.io/name=$br" --all-containers --tail=200 2>/dev/null || true
    fi
}

case_cleanup() {
    [ "$KEEP" = true ] && return
    kubectl delete buildrun -n "$NAMESPACE" -l "build.shipwright.io/name=$BUILD_NAME" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete build "$BUILD_NAME" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete buildconfig "$BUILD_NAME" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
}

run_case() {
    local case_dir="$1"
    local name; name=$(basename "$case_dir")

    log "Test case: $name"

    if [ ! -f "$case_dir/testcase.env" ] || [ ! -f "$case_dir/buildconfig.yaml" ]; then
        fail "$name: missing testcase.env or buildconfig.yaml"
        return
    fi

    # Per-case config. testcase.env references OCP_REGISTRY (exported above).
    # Seed the overridable vars from the inherited environment so testcase.env's
    # ${VAR:-default} form keeps a caller override; a bare `local` would blank them
    # first and always fall through to the default. Expectation vars get defaults
    # here since the cases assign them unconditionally.
    local NAMESPACE="${NAMESPACE:-}" BUILD_NAME="${BUILD_NAME:-}" BUILDER_IMAGE="${BUILDER_IMAGE:-}"
    local REGISTRY="${REGISTRY:-}" BUILD_STRATEGY="${BUILD_STRATEGY:-}" OPTIONAL_FLAGS
    local EXPECT_REGISTERED=false RUN_BUILDRUN=false EXPECT_BUILDRUN=Succeeded BUILD_TIMEOUT="${BUILD_TIMEOUT:-900s}"
    # shellcheck disable=SC1090
    source "$case_dir/testcase.env"
    export NAMESPACE BUILD_NAME BUILDER_IMAGE REGISTRY BUILD_STRATEGY

    info "namespace=$NAMESPACE build=$BUILD_NAME builder=$BUILDER_IMAGE registry=$REGISTRY strategy=$BUILD_STRATEGY"

    # The Build targets BUILD_STRATEGY; it must exist on the cluster.
    if [ -n "$BUILD_STRATEGY" ] && ! kubectl get clusterbuildstrategy "$BUILD_STRATEGY" >/dev/null 2>&1; then
        fail "$name: ClusterBuildStrategy $BUILD_STRATEGY missing (run ./hack/setup-minikube-shipwright.sh)"
        return
    fi

    local case_work="$WORK_DIR/$name"
    local export_dir="$case_work/export"
    local transform_dir="$case_work/transform"
    local output_dir="$case_work/output"
    mkdir -p "$export_dir/resources/$NAMESPACE"
    cp "$case_dir/buildconfig.yaml" "$export_dir/resources/$NAMESPACE/buildconfig.yaml"

    # --- Step 1: Apply the source BuildConfig to the cluster ---
    kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || kubectl create namespace "$NAMESPACE"
    kubectl apply -f "$case_dir/buildconfig.yaml" >/dev/null
    if kubectl get buildconfig "$BUILD_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
        pass "$name: BuildConfig applied to the cluster"
    else
        fail "$name: BuildConfig not present on the cluster"
    fi

    # --- Step 2: crane transform + apply ---
    crane transform BuildConfigToBuildsPlugin \
        --export-dir "$export_dir" \
        --transform-dir "$transform_dir" \
        --plugin-dir "$PLUGIN_DIR" \
        --optional-flags "$OPTIONAL_FLAGS" \
        2>&1 | sed 's/^/    /'
    crane apply \
        --transform-dir "$transform_dir" \
        --output-dir "$output_dir" \
        --overwrite \
        2>&1 | sed 's/^/    /'

    # --- Step 3: Verify the generated Shipwright Build manifest ---
    # crane apply writes both a concatenated output.yaml and per-resource files
    # under resources/. They carry the same Build but differ in YAML list
    # indentation, and `grep -r | head -1` picks between them nondeterministically,
    # which flakes the golden diff. Pin output.yaml (what the goldens are written
    # against) and only fall back to a resource file if it lacks a Build.
    local manifest="$output_dir/output.yaml"
    if [ ! -f "$manifest" ] || ! grep -q "shipwright.io/v1beta1" "$manifest"; then
        manifest=$(grep -rl "shipwright.io/v1beta1" "$output_dir" 2>/dev/null | head -1)
    fi
    if [ -z "$manifest" ]; then
        fail "$name: no Shipwright Build manifest generated"
        case_cleanup
        return
    fi
    pass "$name: Build manifest generated ($(basename "$manifest"))"
    assert_manifest_yaml "$manifest" "$case_dir/expected-build.yaml"

    # BuildConfig original must be whited out of the applied output.
    if grep -rl "kind: BuildConfig" "$output_dir" >/dev/null 2>&1; then
        fail "$name: BuildConfig not whited out of output"
    else
        pass "$name: BuildConfig whited out of output"
    fi

    if [ "$SKIP_BUILD" = true ]; then
        info "$name: skipping cluster apply/build (--skip-build)"
        case_cleanup
        return
    fi

    # --- Step 4: Apply the Build and confirm Shipwright registers it ---
    if [ "$EXPECT_REGISTERED" = true ]; then
        kubectl apply -f "$manifest" >/dev/null
        if kubectl wait --for=jsonpath='{.status.registered}'=True "build/$BUILD_NAME" -n "$NAMESPACE" --timeout=120s >/dev/null 2>&1; then
            pass "$name: Build registered by Shipwright (spec accepted)"
        else
            fail "$name: Build not registered by Shipwright"
            kubectl get "build/$BUILD_NAME" -n "$NAMESPACE" -o jsonpath='{.status.reason}: {.status.message}' 2>/dev/null || true
            collect_diagnostics
            case_cleanup
            return
        fi
    fi

    # --- Step 5: Run a BuildRun and confirm the result ---
    if [ "$RUN_BUILDRUN" = true ]; then
        local buildrun
        buildrun=$(kubectl create -n "$NAMESPACE" -o jsonpath='{.metadata.name}' -f - <<EOF
apiVersion: shipwright.io/v1beta1
kind: BuildRun
metadata:
  generateName: ${BUILD_NAME}-run-
spec:
  build:
    name: ${BUILD_NAME}
EOF
)
        # A BuildRun ends by setting its Succeeded condition to True or False.
        # kubectl wait --for=condition=Succeeded=True never matches a failed run
        # (status False), so it would block the full timeout on any failure. Poll
        # instead and stop the moment the condition reaches either terminal state.
        local want=True
        case "$EXPECT_BUILDRUN" in
            Failed|False) want=False ;;
        esac
        info "$name: created BuildRun $buildrun; waiting up to $BUILD_TIMEOUT for terminal result (expect $EXPECT_BUILDRUN)..."
        local status="" deadline=$((SECONDS + ${BUILD_TIMEOUT%s}))
        while [ "$SECONDS" -lt "$deadline" ]; do
            status=$(kubectl get "buildrun/$buildrun" -n "$NAMESPACE" \
                -o jsonpath='{.status.conditions[?(@.type=="Succeeded")].status}' 2>/dev/null || true)
            if [ "$status" = True ] || [ "$status" = False ]; then break; fi
            sleep 5
        done
        if [ "$status" = "$want" ]; then
            pass "$name: BuildRun reached expected result ($EXPECT_BUILDRUN)"
        else
            fail "$name: BuildRun result=${status:-<timeout>}, expected $EXPECT_BUILDRUN"
            kubectl get "buildrun/$buildrun" -n "$NAMESPACE" \
                -o jsonpath='{range .status.conditions[?(@.type=="Succeeded")]}{.reason}: {.message}{end}' 2>/dev/null || true
            collect_diagnostics
            case_cleanup
            return
        fi
    fi

    case_cleanup
}

main() {
    parse_args "$@"
    check_prereqs

    log "Cluster context: $(kubectl config current-context)"

    # --- Build the plugin once ---
    log "Building plugin"
    cd "$PROJECT_DIR"
    GOTOOLCHAIN=auto go build -o "$PLUGIN_BIN" .

    # --- Resolve the set of cases to run ---
    local dirs=()
    if [ "${#CASES[@]}" -gt 0 ]; then
        local c
        for c in "${CASES[@]}"; do
            if [ -d "$CASES_DIR/$c" ]; then
                dirs+=("$CASES_DIR/$c")
            else
                echo "ERROR: unknown test case: $c" >&2; exit 1
            fi
        done
    else
        local d
        for d in "$CASES_DIR"/$CASE_GLOB/; do
            [ -d "$d" ] && dirs+=("${d%/}")
        done
    fi

    if [ "${#dirs[@]}" -eq 0 ]; then
        echo "ERROR: no test cases found under $CASES_DIR" >&2; exit 1
    fi

    local dir
    for dir in "${dirs[@]}"; do
        run_case "$dir"
    done

    # --- Summary ---
    echo ""
    log "Results: $PASS passed, $FAIL failed"
    [ "$FAIL" -gt 0 ] && exit 1 || exit 0
}

main "$@"
