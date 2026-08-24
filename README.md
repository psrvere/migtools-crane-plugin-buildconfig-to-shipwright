# crane-plugin-buildconfig-to-shipwright

A [crane](https://github.com/konveyor/crane) transform plugin that converts OpenShift `BuildConfig` resources (`build.openshift.io/v1`) to [Shipwright](https://shipwright.io/) `Build` CRs (`shipwright.io/v1beta1`).

## What it does

During crane's transform phase, this plugin:

1. Detects `BuildConfig` resources in the exported namespace
2. Whiteouts the original BuildConfig (marks it for deletion)
3. Generates a corresponding Shipwright `Build` resource
4. Optionally generates a `ServiceAccount` when pull secrets are referenced

All other resource types are passed through unchanged.

## Strategy support

| BuildConfig Strategy | Shipwright ClusterBuildStrategy | Status |
|---------------------|-------------------------------|--------|
| Docker | `buildah` | Supported |
| Source (S2I) | `source-to-image` | Supported |
| Custom | — | Error (no equivalent) |
| JenkinsPipeline | — | Error (migrate to Tekton) |

## Plugin flags

| Flag | Format | Purpose |
|------|--------|---------|
| `registry-mapping` | `old=new,old2=new2` | Rewrite image registry references |
| `imagestream-mapping` | `ns/name:tag=registry/image:tag` | Resolve ImageStreamTag/ImageStreamImage references, and bare DockerImage names that relied on `lookupPolicy.local`, to concrete image URLs |
| `default-build-strategy` | `docker=my-buildah,s2i=my-s2i` | Override default ClusterBuildStrategy names |
| `search-registries` | `reg1,reg2` | Search registries for Buildah |
| `insecure-registries` | `reg1,reg2` | Insecure registries for Buildah |
| `block-registries` | `reg1,reg2` | Blocked registries for Buildah |

## Usage with crane

### 1. Export the namespace

```bash
crane export -n myapp --export-dir ./migration
```

This exports all resources including BuildConfigs, ImageStreams, etc.

### 2. Transform with plugins

```bash
crane transform \
  --export-dir ./migration \
  --transform-dir ./migration/transform \
  --plugin-dir ./plugins
```

The plugin directory should contain the `crane-plugin-buildconfig-to-shipwright` binary. Crane calls it for each resource automatically.

To pass plugin flags, use the `--optional-flags` parameter:

```bash
crane transform \
  --export-dir ./migration \
  --transform-dir ./migration/transform \
  --plugin-dir ./plugins \
  --optional-flags "registry-mapping=image-registry.openshift-image-registry.svc:5000=quay.io/myorg,imagestream-mapping=myns/mybuilder:latest=quay.io/myorg/builder:latest"
```

### 3. Review the output

After transform, the output directory contains:

```
migration/transform/
  resources/
    BuildConfig_build.openshift.io_v1_myapp_myapp-build.yaml  # whiteout
    Build_shipwright.io_v1beta1_myapp_myapp-build.yaml         # new Shipwright Build
    ServiceAccount_v1_myapp_myapp-build.yaml                   # if pull secrets used
  ...
```

Review the generated Shipwright Build YAMLs before applying.

### 4. Apply to the target cluster

```bash
crane apply \
  --transform-dir ./migration/transform \
  --output-dir ./migration/output

kubectl apply -f ./migration/output/resources/
```

### Full example

Migrating a namespace with a Dockerfile-based BuildConfig from OpenShift to a Shipwright-enabled cluster:

```bash
# Export from source cluster
crane export -n myapp --export-dir ./migration

# Transform — OpenShift plugin strips OCP-specific resources,
# BuildConfig plugin converts builds to Shipwright
crane transform \
  --export-dir ./migration \
  --transform-dir ./migration/transform \
  --plugin-dir ./plugins \
  --optional-flags "registry-mapping=image-registry.openshift-image-registry.svc:5000=quay.io/myorg"

# Review generated Shipwright Builds
cat ./migration/transform/resources/Build_shipwright.io_v1beta1_myapp_*.yaml

# Apply to target cluster (Shipwright + Tekton must be installed)
crane apply \
  --transform-dir ./migration/transform \
  --output-dir ./migration/output

kubectl apply -f ./migration/output/resources/
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

```bash
GOTOOLCHAIN=auto go test ./...
```

## Known limitations

- **No live cluster access** — ImageStream references must be resolved via `--imagestream-mapping` or `--registry-mapping` flags. Without them, the plugin falls back to the internal OpenShift registry URL with a warning. Bare image names such as `myapp:latest` that relied on ImageStream `lookupPolicy.local` are warned about and can be resolved with the same flag.
- **Volumes** — BuildConfig volumes are not converted (Shipwright requires BuildStrategy-level support). A warning is emitted.
- **Inline Dockerfiles** — the buildah strategy cannot consume Dockerfile content (BUILD-1495). The plugin preserves it in a ConfigMap named after the BuildConfig with a `-dockerfile` suffix and points at it from the Build annotation `buildconfig-to-shipwright/inline-dockerfile-configmap` (the annotation is the source of truth for the name); commit it to the repository before running the Build.
- **Multiple source types** — Shipwright supports one source per Build. BuildConfigs with multiple sources produce an error.
- **BuildRun not generated** — Only the Build definition is created. Triggering builds is left to the user or CI/CD system.

## Issue tracking

This project is tracked primarily in Jira, under the BUILD project. This is different from
crane, which is tracked primarily in GitHub (Projects and Issues) and uses Jira only for the
non-upstream tracking that is required internally. If you are picking up or filing work for
this plugin, use Jira as the source of truth.

## Related

- [Enhancement proposal](https://github.com/konveyor/enhancements/pull/300)
- [crane-plugin-openshift](https://github.com/migtools/crane-plugin-openshift) — reference crane transform plugin
- [Shipwright documentation](https://shipwright.io/docs/)
