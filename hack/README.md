# Development and Testing Scripts

This directory contains scripts for setting up development and E2E testing environments.

## Prerequisites

- **[crane CLI](https://github.com/migtools/crane)** - Required for transform and apply operations
  
  **Installation:**
  
  ```bash
  # Build from source
  git clone https://github.com/migtools/crane.git
  cd crane
  go build -o crane .
  sudo mv crane /usr/local/bin/
  
  # Verify installation
  crane version
  ```

- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [minikube](https://minikube.sigs.k8s.io/docs/start/)

## Quick Start

```bash
# Setup a local Minikube cluster with Shipwright Build
./hack/setup-minikube-shipwright.sh

# This creates:
# - Cluster named "minikube-shipwright"
# - Kubectl context "minikube-shipwright"
# - Tekton Pipelines (required by Shipwright)
# - Shipwright Build v0.19.0
# - Default ClusterBuildStrategies (buildah, source-to-image, etc.)
# - Local registry addon
```

## Script Reference

### `fake-minikube-buildconfig.sh`

Installs a **fake** OpenShift BuildConfig CRD on non-OpenShift clusters (like Minikube) for testing.

**⚠️ Important:** This only installs the CRD definition - it does **NOT** provide actual BuildConfig functionality. BuildConfig resources created using this CRD will not trigger any builds. This is purely for testing the crane plugin's transformation capabilities.

**Usage:**
```bash
./hack/fake-minikube-buildconfig.sh [OPTIONS]
```

**Options:**
- `--verify` - Verify CRD installation

**Examples:**
```bash
# Install fake BuildConfig CRD
./hack/fake-minikube-buildconfig.sh

# Verify installation
./hack/fake-minikube-buildconfig.sh --verify
```

This allows you to create BuildConfig CR objects on Minikube (without any build functionality) and test the crane plugin transformation pipeline that converts them to Shipwright Build resources.

### `setup-minikube-shipwright.sh`

Creates a Minikube cluster with Shipwright Build for testing.

**Usage:**
```bash
./hack/setup-minikube-shipwright.sh [OPTIONS]
```

**Common Options:**
- `--cluster-name NAME` - Minikube profile name (default: `minikube-shipwright`)
- `--k8s-version VERSION` - Kubernetes version (default: `v1.34.10`, required: v1.34+ for Shipwright v0.19.0)
- `--cpus N` - CPU count (default: `4`)
- `--memory MB` - Memory in MB (default: `8192`)
- `--driver DRIVER` - Minikube driver (default: auto-detect)
- `--shipwright-version VER` - Shipwright version (default: `v0.19.0`)
- `--skip-cluster-create` - Only install Shipwright, don't create cluster

**Examples:**
```bash
# Default setup
./hack/setup-minikube-shipwright.sh

# Custom configuration
./hack/setup-minikube-shipwright.sh \
  --cpus 6 \
  --memory 16384 \
  --k8s-version v1.34.10

# Only install Shipwright on existing cluster
./hack/setup-minikube-shipwright.sh --skip-cluster-create
```

## Cleanup

To remove the Minikube cluster:

```bash
minikube delete -p minikube-shipwright
```

## Testing the Plugin

After setting up your environment, test the crane plugin.

**Note:** Make sure you have the `crane` CLI installed (see Prerequisites above).

### 1. Build the Plugin

```bash
cd /path/to/crane-plugin-buildconfig-to-shipwright
go build -o crane-plugin-buildconfig-to-shipwright .
```

### 2. Run E2E Transform Test

```bash
# This tests the transform pipeline without applying to a cluster
./tests/e2e-transform.sh
```

### 3. Test on Real Cluster

The `./tests/e2e-cluster.sh` harness runs the whole flow for you — it applies
each source BuildConfig, converts it, diffs the generated Build against a golden
manifest, applies it, and runs a BuildRun to confirm the build succeeds:

```bash
# Full cluster E2E (needs the Minikube setup from steps above)
./tests/e2e-cluster.sh

# Verify the generated manifest only, skip the actual build
./tests/e2e-cluster.sh --skip-build
```

The manual steps below are for custom, one-off testing.

#### Minikube
```bash
# Set kubectl context (context name: minikube-shipwright)
kubectl config use-context minikube-shipwright

# Build plugin
go build -o /tmp/plugins/crane-plugin-buildconfig-to-shipwright .

# Transform test data
crane transform \
  --export-dir ./tests/testdata/export \
  --transform-dir /tmp/transform \
  --plugin-dir /tmp/plugins

# Apply to cluster
kubectl apply -f /tmp/transform/resources/
```

### 4. Verify Build Resources

```bash
# List Build resources
kubectl get builds

# Describe a Build
kubectl describe build <build-name>

# Trigger a BuildRun (manual)
kubectl create -f - <<EOF
apiVersion: shipwright.io/v1beta1
kind: BuildRun
metadata:
  name: <build-name>-run-1
spec:
  build:
    name: <build-name>
EOF

# Watch BuildRun progress
kubectl get buildrun -w
```

**Note:** When using HTTP registries (like Minikube's local registry) or registries with self-signed certificates, add the `registries-insecure` parameter to your Build. See examples in the setup script output.

## Troubleshooting

### Minikube Issues

**Cluster won't start:**
```bash
# Delete and recreate
minikube delete -p minikube-shipwright
./hack/setup-minikube-shipwright.sh
```

**Registry not accessible:**
```bash
# Check registry addon
minikube addons list -p minikube-shipwright

# Enable if disabled
minikube addons enable registry -p minikube-shipwright
```


## Kubectl Contexts

The setup scripts create and configure kubectl contexts for easy switching:

```bash
# List all contexts
kubectl config get-contexts

# Switch to minikube
kubectl config use-context minikube-shipwright

# View current context
kubectl config current-context
```

## Environment Variables

All scripts support environment variables as an alternative to CLI flags:

```bash
# Example: customize resources and versions
export K8S_VERSION=v1.34.10
export CPUS=6
export MEMORY=16384
export TEKTON_VERSION=v1.15.0
export SHIPWRIGHT_VERSION=v0.19.0
./hack/setup-minikube-shipwright.sh
```

## Related Documentation

- [Shipwright Build Documentation](https://shipwright.io/docs/)
- [Tekton Documentation](https://tekton.dev/docs/)
- [crane CLI](https://github.com/migtools/crane)
