# crane-plugin-buildconfig-to-shipwright

A [crane](https://github.com/konveyor/crane) transform plugin that converts OpenShift `BuildConfig` resources (`build.openshift.io/v1`) to [Shipwright](https://shipwright.io/) `Build` CRs (`shipwright.io/v1beta1`).

## What it does

During crane's transform phase, this plugin:

1. Detects `BuildConfig` resources in the exported namespace
2. Whiteouts the BuildConfig it converts (marks it for deletion). A skipped or failed BuildConfig passes through unchanged
3. Generates a corresponding Shipwright `Build` resource
4. Generates a `ServiceAccount` named after the BuildConfig when a pull secret is referenced and the BuildConfig names no ServiceAccount; a named ServiceAccount is migrated by crane unchanged, and the plugin warns with the `oc secrets link` command that attaches the pull secret on the target

All other resource types are passed through unchanged.

## Strategy support

| BuildConfig Strategy | Shipwright ClusterBuildStrategy | Status |
|---------------------|-------------------------------|--------|
| Docker | `buildah` | Supported |
| Source (S2I) | `source-to-image` | Supported |
| Custom | — | Skipped, passed through unchanged (no equivalent) |
| JenkinsPipeline | — | Skipped, passed through unchanged (migrate to Tekton) |

Every BuildConfig field, what happens to it, and every warning the plugin can emit are listed
in [docs/support-matrix.md](docs/support-matrix.md).

## Plugin flags

| Flag | Format | Purpose |
|------|--------|---------|
| `registry-mapping` | `old=new,old2=new2` | Rewrite image registry references |
| `imagestream-mapping` | `ns/name:tag=registry/image:tag` | Resolve ImageStreamTag/ImageStreamImage references, and bare DockerImage names that relied on `lookupPolicy.local`, to concrete image URLs |
| `default-build-strategy` | `docker=my-buildah,s2i=my-s2i` | Override default ClusterBuildStrategy names |
| `search-registries` | `reg1,reg2` | Search registries for Buildah |
| `insecure-registries` | `reg1,reg2` | Insecure (HTTP/self-signed) registries. Buildah gets the `registries-insecure` param; for a Shipwright-managed push (`source-to-image`) an output image on one of these registries sets `spec.output.insecure=true` |
| `block-registries` | `reg1,reg2` | Blocked registries for Buildah |

### Redirecting output images

A BuildConfig pushes its output to the internal OpenShift registry
(`image-registry.openshift-image-registry.svc:5000/<namespace>/<name>`). To send
converted Builds to a registry the target cluster can reach, use the two mapping
flags — there is no dedicated `--dest-registry` flag:

- `registry-mapping` rewrites the registry prefix and **preserves the
  `<namespace>/<name>` path**. Mapping the internal registry to `quay.io/acme`
  turns an ImageStreamTag output in namespace `myapp` into
  `quay.io/acme/myapp/webapp:latest` — three path segments.
- Registries that accept only `<org>/<repo>` (Quay.io, Docker Hub) reject that
  deeper path. For those, give an exact target per BuildConfig with
  `imagestream-mapping` (`ns/name:tag=registry/image:tag`), which sets the output
  reference. `registry-mapping` still runs afterward, so a prefix it matches on
  the mapped value is rewritten too — keep that in mind when using both flags.

Redirecting an ImageStreamTag output off the internal registry means the source
ImageStream is no longer updated, so anything watching it to roll out (a
Deployment or DeploymentConfig) stops firing. The converter warns when this
happens. The check is a registry-prefix comparison, not a cluster-aware one: it
fires when the resolved image no longer starts with
`image-registry.openshift-image-registry.svc:5000/`. A redirect to a different
in-cluster registry alias is not recognised as internal, and a redirect to a
different path on the same internal registry is not caught.

## Prerequisites

This plugin requires the [crane CLI](https://github.com/konveyor/crane) to be installed.

### Installing crane

```bash
# Build from source
git clone https://github.com/konveyor/crane.git
cd crane
go build -o crane .
sudo mv crane /usr/local/bin/

# Verify installation
crane version
```

## Usage with crane

### 1. Export the namespace

```bash
crane export -n myapp
```

This exports all resources including BuildConfigs, ImageStreams, etc.

### 2. Transform with plugins

```bash
crane transform BuildConfigPlugin \
  --plugin-dir ./plugins
```

The plugin directory should contain the `crane-plugin-buildconfig-to-shipwright` binary. Crane calls it for each resource automatically.

To pass plugin flags, use the `--optional-flags` parameter:

```bash
crane transform BuildConfigPlugin \
  --plugin-dir ./plugins \
  --overwrite \
  --optional-flags "registry-mapping=image-registry.openshift-image-registry.svc:5000=quay.io/myorg,imagestream-mapping=myns/mybuilder:latest=quay.io/myorg/builder:latest"
```

### 3. Review the output

After transform, the output directory contains:

```
transform/
  resources/
    BuildConfig_build.openshift.io_v1_myapp_myapp-build.yaml  # whiteout
    Build_shipwright.io_v1beta1_myapp_myapp-build.yaml         # new Shipwright Build
    ServiceAccount_v1_myapp_myapp-build.yaml                   # if a pull secret is used and no ServiceAccount is named
  ...
```

Review the generated Shipwright Build YAMLs before applying.

### 4. Apply to the target cluster

```bash
crane apply

kubectl apply -f ./output/resources/
```

### Full example

Migrating a namespace with a Dockerfile-based BuildConfig from OpenShift to a Shipwright-enabled cluster:

```bash
# Export from source cluster
crane export -n myapp

# Transform — OpenShift plugin strips OCP-specific resources,
# BuildConfig plugin converts builds to Shipwright
crane transform BuildConfigPlugin \
  --plugin-dir ./plugins \
  --optional-flags "registry-mapping=image-registry.openshift-image-registry.svc:5000=quay.io/myorg"

# Review generated Shipwright Builds
cat ./transform/resources/Build_shipwright.io_v1beta1_myapp_*.yaml

# Apply to target cluster (Shipwright + Tekton must be installed)
crane apply

kubectl apply -f ./output/resources/
```

## Conversion example

**Input — OpenShift BuildConfig:**

```yaml
apiVersion: build.openshift.io/v1
kind: BuildConfig
metadata:
  name: myapp-build
  namespace: myapp
spec:
  source:
    type: Git
    git:
      uri: https://github.com/example/myapp.git
      ref: main
    contextDir: src
    sourceSecret:
      name: git-credentials
  strategy:
    type: Docker
    dockerStrategy:
      dockerfilePath: Dockerfile.prod
      buildArgs:
        - name: GO_VERSION
          value: "1.21"
  output:
    to:
      kind: DockerImage
      name: quay.io/example/myapp:latest
    pushSecret:
      name: quay-push-secret
```

**Output — Shipwright Build:**

```yaml
apiVersion: shipwright.io/v1beta1
kind: Build
metadata:
  name: myapp-build
  namespace: myapp
  annotations:
    crane.konveyor.io/converted-from: build.openshift.io/v1/BuildConfig/myapp-build
spec:
  source:
    type: Git
    git:
      url: https://github.com/example/myapp.git
      revision: main
      cloneSecret: git-credentials
    contextDir: src
  strategy:
    name: buildah
    kind: ClusterBuildStrategy
  paramValues:
    - name: dockerfile
      value: Dockerfile.prod
    - name: build-args
      values:
        - value: "GO_VERSION=1.21"
  output:
    image: quay.io/example/myapp:latest
    pushSecret: quay-push-secret
```

## Building

```bash
GOTOOLCHAIN=auto go build -o crane-plugin-buildconfig-to-shipwright .
```

Requires Go 1.26+ (forced by transitive dependencies).

## Testing

The project uses a three-level testing strategy:

### 1. Unit Tests
Standard Go tests at the method level, testing individual functions and transformation logic.

```bash
GOTOOLCHAIN=auto go test ./...
```

### 2. Plugin E2E Tests
Tests the plugin binary in isolation (with crane), running sample exported resources through the `crane transform` + `crane apply` pipeline and asserting the output manifests.

```bash
./tests/e2e-transform.sh
```

These tests verify the transformation logic works correctly without requiring a live cluster.

### 3. Cluster E2E Tests
Full end-to-end validation on a live Minikube cluster with Tekton, Shipwright, and
the fake BuildConfig CRD. It runs a case per source BuildConfig — an S2I build with
ImageStream builder and output references, and a Docker (Dockerfile) build — through
the standard `crane transform` + `crane apply` flow, verifies each generated
Shipwright Build manifest, applies it to the cluster, and runs a BuildRun to confirm
the image build succeeds.

```bash
# Requires a cluster from ./hack/setup-minikube-shipwright.sh + ./hack/fake-minikube-buildconfig.sh
./tests/e2e-cluster.sh

# Verify the generated manifest only, skip the actual build
./tests/e2e-cluster.sh --skip-build
```

See [`hack/README.md`](hack/README.md) for detailed cluster setup instructions.

## Known limitations

See the [support matrix](docs/support-matrix.md). Rows that lose something come first in every
section, and its [warning reference](docs/support-matrix.md#warning-reference) quotes every
warning the plugin can emit.

## Issue tracking

This project is tracked primarily in Jira, under the BUILD project. This is different from
crane, which is tracked primarily in GitHub (Projects and Issues) and uses Jira only for the
non-upstream tracking that is required internally. If you are picking up or filing work for
this plugin, use Jira as the source of truth.

## Related

- [Enhancement proposal](https://github.com/konveyor/enhancements/pull/300)
- [crane-plugin-openshift](https://github.com/migtools/crane-plugin-openshift) — reference crane transform plugin
- [Shipwright documentation](https://shipwright.io/docs/)
