#!/bin/bash
#
# Setup Minikube cluster with Shipwright Build for development and E2E testing
#
# Prerequisites:
#   - minikube
#   - kubectl
#   - helm (optional, for Shipwright installation via Helm)
#
# Usage:
#   ./hack/setup-minikube-shipwright.sh [OPTIONS]
#
# Options:
#   --cluster-name NAME    Minikube profile name (default: minikube-shipwright)
#   --k8s-version VERSION  Kubernetes version (default: v1.34.10)
#   --cpus N               CPU count (default: 4)
#   --memory MB            Memory in MB (default: 8192)
#   --driver DRIVER        Minikube driver (default: auto-detect)
#   --shipwright-version   Shipwright version (default: v0.19.0)
#   --skip-cluster-create  Skip cluster creation, only install Shipwright
#   --help                 Show this help
#
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-minikube-shipwright}"
K8S_VERSION="${K8S_VERSION:-v1.34.10}"
CPUS="${CPUS:-4}"
MEMORY="${MEMORY:-8192}"
DRIVER="${DRIVER:-}"
SHIPWRIGHT_VERSION="${SHIPWRIGHT_VERSION:-v0.19.0}"
SKIP_CLUSTER_CREATE="${SKIP_CLUSTER_CREATE:-false}"

log() { echo "==> $*"; }
error() { echo "ERROR: $*" >&2; exit 1; }

show_help() {
    sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
}

check_prereqs() {
    local missing=()
    command -v minikube >/dev/null 2>&1 || missing+=("minikube")
    command -v kubectl >/dev/null 2>&1 || missing+=("kubectl")

    if [ ${#missing[@]} -gt 0 ]; then
        error "Missing required tools: ${missing[*]}"
    fi
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --cluster-name)
                if [ $# -lt 2 ] || [[ "$2" == -* ]]; then
                    error "--cluster-name requires a valid value (got: ${2:-<missing>})"
                fi
                CLUSTER_NAME="$2"
                shift 2
                ;;
            --k8s-version)
                if [ $# -lt 2 ] || [[ "$2" == -* ]]; then
                    error "--k8s-version requires a valid value (got: ${2:-<missing>})"
                fi
                K8S_VERSION="$2"
                shift 2
                ;;
            --cpus)
                if [ $# -lt 2 ] || [[ "$2" == -* ]]; then
                    error "--cpus requires a valid value (got: ${2:-<missing>})"
                fi
                CPUS="$2"
                shift 2
                ;;
            --memory)
                if [ $# -lt 2 ] || [[ "$2" == -* ]]; then
                    error "--memory requires a valid value (got: ${2:-<missing>})"
                fi
                MEMORY="$2"
                shift 2
                ;;
            --driver)
                if [ $# -lt 2 ] || [[ "$2" == -* ]]; then
                    error "--driver requires a valid value (got: ${2:-<missing>})"
                fi
                DRIVER="$2"
                shift 2
                ;;
            --shipwright-version)
                if [ $# -lt 2 ] || [[ "$2" == -* ]]; then
                    error "--shipwright-version requires a valid value (got: ${2:-<missing>})"
                fi
                SHIPWRIGHT_VERSION="$2"
                shift 2
                ;;
            --skip-cluster-create)
                SKIP_CLUSTER_CREATE=true
                shift
                ;;
            --help)
                show_help
                ;;
            *)
                error "Unknown option: $1"
                ;;
        esac
    done
}

create_cluster() {
    if [ "$SKIP_CLUSTER_CREATE" = true ]; then
        log "Skipping cluster creation (--skip-cluster-create)"
        return
    fi

    log "Creating minikube cluster: $CLUSTER_NAME"
    log "  Kubernetes version: $K8S_VERSION"
    log "  CPUs: $CPUS, Memory: ${MEMORY}MB"

    local driver_arg=""
    if [ -n "$DRIVER" ]; then
        driver_arg="--driver=$DRIVER"
    fi

    # Check if profile exists (works for both running and stopped clusters)
    if minikube profile list -o json 2>/dev/null | grep -q "\"Name\":\"$CLUSTER_NAME\""; then
        log "Cluster $CLUSTER_NAME already exists"
        if [ -t 0 ] && [ "${ASSUME_YES:-false}" != true ]; then
            read -p "Delete and recreate? (y/N): " -r || REPLY=n
        else
            REPLY=n  # default to using existing cluster in non-interactive mode
        fi
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            log "Deleting existing cluster..."
            minikube delete -p "$CLUSTER_NAME"
        else
            log "Using existing cluster"
            return
        fi
    fi

    minikube start \
        -p "$CLUSTER_NAME" \
        --kubernetes-version="$K8S_VERSION" \
        --cpus="$CPUS" \
        --memory="${MEMORY}mb" \
        $driver_arg \
        --addons=registry,metrics-server

    log "Cluster created successfully"

    # Ensure context name matches cluster name
    log "Setting kubectl context to $CLUSTER_NAME"
    kubectl config use-context "$CLUSTER_NAME"
}

install_tekton() {
    log "Installing Tekton Pipelines (required by Shipwright)"

    # Pin to tested Tekton release (can be overridden via TEKTON_VERSION env var)
    local TEKTON_VERSION="${TEKTON_VERSION:-v1.15.0}"

    kubectl apply -f "https://github.com/tektoncd/pipeline/releases/download/${TEKTON_VERSION}/release.yaml"

    log "Waiting for Tekton to be ready..."
    kubectl rollout status deployment/tekton-pipelines-controller \
        -n tekton-pipelines \
        --timeout=300s

    log "Tekton Pipelines $TEKTON_VERSION installed"
}

install_shipwright() {
    log "Installing Shipwright Build $SHIPWRIGHT_VERSION"

    # Install Shipwright Build Controller
    # Use server-side apply to handle large CRD annotations (Kubernetes 1.34+ issue)
    kubectl apply --server-side -f "https://github.com/shipwright-io/build/releases/download/${SHIPWRIGHT_VERSION}/release.yaml"

    log "Waiting for Shipwright namespace to be created..."
    local retries=0
    while ! kubectl get namespace shipwright-build &>/dev/null; do
        sleep 2
        retries=$((retries + 1))
        if [ $retries -gt 30 ]; then
            error "Timeout waiting for shipwright-build namespace"
        fi
    done

    log "Waiting for Shipwright deployment to be created..."
    retries=0
    while ! kubectl get deployment shipwright-build-controller -n shipwright-build &>/dev/null; do
        sleep 2
        retries=$((retries + 1))
        if [ $retries -gt 30 ]; then
            error "Timeout waiting for shipwright-build-controller deployment"
        fi
    done

    log "Waiting for Shipwright controller to be ready..."
    kubectl wait --for=condition=available deployment/shipwright-build-controller \
        -n shipwright-build \
        --timeout=300s

    log "Shipwright Build $SHIPWRIGHT_VERSION installed"
}

install_build_strategies() {
    log "Installing default ClusterBuildStrategies"

    # Install common build strategies
    local STRATEGIES_VERSION="${SHIPWRIGHT_VERSION}"
    local STRATEGIES_BASE="https://github.com/shipwright-io/build/releases/download/${STRATEGIES_VERSION}/sample-strategies.yaml"

    # Use server-side apply for large CRDs
    kubectl apply --server-side -f "$STRATEGIES_BASE"

    log "Installed ClusterBuildStrategies:"
    kubectl get clusterbuildstrategy -o custom-columns=NAME:.metadata.name --no-headers | sed 's/^/  - /'
}

setup_local_registry() {
    log "Setting up local registry access"

    # Enable minikube registry addon if not already enabled
    minikube addons enable registry -p "$CLUSTER_NAME" 2>/dev/null || true

    # Get registry service details
    # Minikube registry addon creates a service on port 80 internally,
    # accessible from within the cluster at registry.kube-system.svc.cluster.local
    if kubectl get svc registry -n kube-system &>/dev/null; then
        local CLUSTER_IP=$(kubectl get svc registry -n kube-system -o jsonpath='{.spec.clusterIP}' 2>/dev/null || echo "")
        if [ -n "$CLUSTER_IP" ]; then
            log "Registry addon enabled"
            log "In-cluster registry: registry.kube-system.svc.cluster.local:80"
            log ""
            log "For Build output.image, use one of:"
            log "  - registry.kube-system.svc.cluster.local:80/myimage:tag (in-cluster)"
            log "  - localhost:5000/myimage:tag (requires 'minikube ssh -- -L 5000:localhost:5000')"
        else
            log "WARNING: Registry service found but ClusterIP unavailable"
        fi
    else
        log "WARNING: Registry addon not available"
    fi
}

verify_installation() {
    log "Verifying installation..."

    # Check Tekton
    if ! kubectl get deployment tekton-pipelines-controller -n tekton-pipelines &>/dev/null; then
        error "Tekton installation verification failed"
    fi

    # Check Shipwright
    if ! kubectl get deployment shipwright-build-controller -n shipwright-build &>/dev/null; then
        error "Shipwright installation verification failed"
    fi

    # Check strategies
    local strategy_count=$(kubectl get clusterbuildstrategy --no-headers 2>/dev/null | wc -l)
    if [ "$strategy_count" -eq 0 ]; then
        error "No ClusterBuildStrategies found"
    fi

    log "Verification successful"
    log ""
    log "Available ClusterBuildStrategies:"
    kubectl get clusterbuildstrategy -o custom-columns=NAME:.metadata.name --no-headers | sed 's/^/  - /'
}

print_summary() {
    log "Setup complete!"
    log ""
    log "Cluster: $CLUSTER_NAME"
    log "Context: $(kubectl config current-context)"
    log ""
    log "To use this cluster:"
    log "  kubectl config use-context $CLUSTER_NAME"
    log ""
    log "To test with crane-plugin-buildconfig-to-shipwright:"
    log "  1. Build the plugin: go build -o crane-plugin-buildconfig-to-shipwright ."
    log "  2. Run E2E test: ./tests/e2e-transform.sh"
    log "  3. Apply transformed resources to this cluster"
    log ""
    log "Example Build resource (uses insecure HTTP registry):"
    log "  kubectl apply -f - <<EOF"
    log "  apiVersion: shipwright.io/v1beta1"
    log "  kind: Build"
    log "  metadata:"
    log "    name: example-build"
    log "  spec:"
    log "    source:"
    log "      type: Git"
    log "      git:"
    log "        url: https://github.com/shipwright-io/sample-go"
    log "    strategy:"
    log "      name: buildah"
    log "      kind: ClusterBuildStrategy"
    log "    paramValues:"
    log "    - name: registries-insecure"
    log "      values:"
    log "      - value: registry.kube-system.svc.cluster.local:80"
    log "    output:"
    log "      image: registry.kube-system.svc.cluster.local:80/example:latest"
    log "  EOF"
    log ""
    log "Note: registries-insecure parameter is required for HTTP registry"
}

main() {
    parse_args "$@"
    check_prereqs
    create_cluster

    # Set kubectl context (skip if using existing cluster with --skip-cluster-create)
    if [ "$SKIP_CLUSTER_CREATE" != true ]; then
        log "Using kubectl context: $CLUSTER_NAME"
        kubectl config use-context "$CLUSTER_NAME"
    else
        log "Using current kubectl context: $(kubectl config current-context)"
    fi

    install_tekton
    install_shipwright
    install_build_strategies
    setup_local_registry
    verify_installation
    print_summary
}

main "$@"
