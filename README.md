# crane-plugin-buildconfig-to-shipwright

A [crane](https://github.com/migtools/crane) transform plugin that converts OpenShift
`BuildConfig` resources (`build.openshift.io/v1`) into Shipwright `Build` resources
(`shipwright.io/v1beta1`). It runs offline, as part of `crane transform`, and never talks to
a cluster.

## What it does

For every resource in a crane export:

- Anything that is not a BuildConfig passes through untouched.
- A BuildConfig with a Docker or Source strategy and an output image becomes a Shipwright
  `Build`. The original is removed from the output. When the BuildConfig has a pull secret
  and names no ServiceAccount, the plugin also generates a `ServiceAccount` carrying it. A
  BuildConfig that does name a ServiceAccount keeps it: crane migrates that account
  unchanged and the plugin never overwrites it, warning instead with the `oc secrets link`
  command that attaches the pull secret on the target. An
  inline Dockerfile on a Docker strategy is preserved in a `ConfigMap`, pointed at by the
  Build's `buildconfig-to-shipwright/inline-dockerfile-configmap` annotation; commit it to
  the source repository before running the Build. On a Source strategy an inline Dockerfile
  is dropped with a warning, because S2I does not use one.
- A BuildConfig with a Custom or JenkinsPipeline strategy, or no output image, is skipped:
  it stays in the output unchanged with two annotations saying it was skipped and why. A
  BuildConfig the plugin cannot convert is treated the same way, marked failed. Shipwright
  takes one source per Build, so a BuildConfig with more than one source type fails here.
  Neither stops the migration.

Every field the plugin drops or changes produces a warning, in the log and in an annotation
on the Build. The annotation is size-capped, so on a very lossy BuildConfig the log is the
complete list. The full list, field by field, is in [docs/support-matrix.md](docs/support-matrix.md).

| BuildConfig strategy | Shipwright ClusterBuildStrategy | Outcome |
|---|---|---|
| Docker | `buildah` | converted |
| Source (S2I) | `source-to-image` | converted |
| Custom | none | skipped, passed through with two annotations |
| JenkinsPipeline | none | skipped, passed through with two annotations |

## Prerequisites

- **Go 1.26 or newer** to build the plugin.
- **crane built from commit `d566a18f6640cd79c8568749d6621b40486d0625` or newer.** The
  released crane (v0.0.5) does not write the resources a plugin generates: it runs this
  plugin, reports nothing, and produces no Builds. This is the commit CI pins.
- **A target cluster with Shipwright and Tekton**, and the `buildah` and `source-to-image`
  ClusterBuildStrategies. Builds for Red Hat OpenShift ships both. Upstream, CI tests
  against Shipwright v0.20.11.

### Install crane

```bash
git clone https://github.com/migtools/crane.git
cd crane
git checkout d566a18f6640cd79c8568749d6621b40486d0625
go build -o crane .
sudo mv crane /usr/local/bin/
crane version
```

### Build the plugin

```bash
GOTOOLCHAIN=auto go build -o crane-plugin-buildconfig-to-shipwright .
mkdir -p plugins && mv crane-plugin-buildconfig-to-shipwright plugins/
```

crane finds plugins by scanning the directory passed as `--plugin-dir`.

## Usage with crane

### 1. Export the namespace

```bash
crane export -n myapp
```

### 2. Transform

```bash
crane transform BuildConfigPlugin \
  --plugin-dir ./plugins \
  --optional-flags '{"registry-mapping":"image-registry.openshift-image-registry.svc:5000=quay.io/myorg"}'
```

`--optional-flags` takes one JSON object whose keys are the plugin's flags and whose values
are strings. The flags are listed [below](#plugin-flags); `crane transform optionals
--plugin-dir ./plugins` prints them with an example each.

### 3. Write the output, then read it

```bash
crane apply
```

`crane apply` writes the result under `output/`:

```
output/
  output.yaml                                   # everything, concatenated
  resources/myapp/
    Build_shipwright.io_v1beta1_myapp_webapp.yaml
    ServiceAccount__v1_myapp_webapp.yaml         # when a pull secret is used and no ServiceAccount is named
    ConfigMap__v1_myapp_webapp-dockerfile.yaml   # when the BuildConfig has an inline Dockerfile
```

Read each Build's `crane.konveyor.io/conversion-warnings` annotation before applying it.
To find the BuildConfigs that were not converted, look for the outcome annotation on the
objects that still say `kind: BuildConfig`:

```bash
grep -rl 'kind: BuildConfig' output/resources \
  | xargs -r grep -H 'buildconfig-to-shipwright/conversion-'
```

The export also carries the resources the plugin left alone, including the BuildConfigs it
skipped. Read those before step 4: a recreated BuildConfig with an ImageChange or
ConfigChange trigger starts an OpenShift build as soon as it lands.

### 4. Apply to the target cluster

`crane` writes each resource under `output/resources/<namespace>/`, so the apply has to
recurse:

```bash
kubectl apply -R -f output/resources/

kubectl wait --for=jsonpath='{.status.registered}'=True \
  build.shipwright.io/webapp -n myapp --timeout=120s
```

Write `build.shipwright.io`, not `build`, in every kubectl command. On OpenShift the short
name resolves to the OpenShift Build API.

Nothing builds on its own. OpenShift triggers do not exist in Shipwright, so create a
`BuildRun` to start the first build.

Which ServiceAccount it runs as depends on whether the plugin generated one. If it did not,
leave the BuildRun's `serviceAccount` unset and it runs as the namespace `pipeline` account.
If it did, that account carries the BuildConfig's pull secret and the plugin names it in the
Build's `buildconfig-to-shipwright/buildrun-template` annotation, so point the BuildRun at
it. Leaving it unset there drops the pull secret and a private builder image will not pull.
On OpenShift, grant the generated account the SCC buildah needs, scoped to that one account:

```bash
oc adm policy add-scc-to-user pipelines-scc -z <generated-sa> -n <namespace>
```

## Worked examples

[docs/examples](docs/examples/README.md) holds three BuildConfigs taken through the plugin,
with the exact input, the flags, the output, every warning, and the steps on the target
cluster. A test regenerates their output on every CI run, so they cannot drift.

## Plugin flags

| Flag | Format | What it changes |
|---|---|---|
| `registry-mapping` | `old-registry=new-registry,…` | Rewrites the registry prefix of resolved image references. Applies to strategy and source images, and to an output of kind `ImageStreamTag`. An output of kind `DockerImage` is copied as written |
| `imagestream-mapping` | `ns/name:tag=registry/image:tag,…` | Replaces an ImageStreamTag or ImageStreamImage reference, or a bare DockerImage name that relied on `lookupPolicy.local`, with a concrete image. Digest form: `ns/name@sha256:…=…` |
| `default-build-strategy` | `docker=name,s2i=name` | Uses a different ClusterBuildStrategy name |
| `search-registries` | `registry,…` | Buildah search registries |
| `insecure-registries` | `registry,…` | Docker strategy: the `registries-insecure` param. Source strategy: `spec.output.insecure: true` when the output image is on one of them, because Shipwright does the push there |
| `block-registries` | `registry,…` | Buildah blocked registries |

### Redirecting output images

A BuildConfig pushes to the internal OpenShift registry. On the target cluster that registry
may not exist, so the two mapping flags redirect the output. There is no single
`--dest-registry` flag.

- `registry-mapping` rewrites the prefix and keeps the `<namespace>/<name>` path. Mapping the
  internal registry to `quay.io/acme` turns an ImageStreamTag output in namespace `myapp` into
  `quay.io/acme/myapp/webapp:latest`. Quay accepts nested paths like that and creates the
  repository on first push. Docker Hub does not; for registries that take only
  `<org>/<repo>`, name an exact target per BuildConfig with `imagestream-mapping`.
  `registry-mapping` still runs afterwards on the mapped value.
- Without either flag, an ImageStreamTag output keeps its internal-registry form,
  `image-registry.openshift-image-registry.svc:5000/<namespace>/<name>:<tag>`, with a warning.
  That is right when the target is another OpenShift cluster.
- Redirecting an output off the internal registry means the source ImageStream no longer
  updates, so a Deployment or DeploymentConfig that rolled out on it stops firing. The plugin
  warns when this happens. The check is a registry-prefix comparison, not a cluster-aware one.

The plugin cannot read the builder ServiceAccount to work out a push credential. Set
`output.pushSecret` on the BuildConfig, or make sure the BuildRun's ServiceAccount can push
to the target registry. The plugin warns either way.

## Documentation

| Page | For |
|---|---|
| [docs/support-matrix.md](docs/support-matrix.md) | every BuildConfig field: what happens, where it lands, what to do by hand, the warning |
| [docs/examples](docs/examples/README.md) | three worked examples, verified on a cluster |
| [docs/volume-migration.md](docs/volume-migration.md) | why a Build with volumes fails with `UndefinedVolume`, and the strategy-copy fix |
| [docs/architecture.md](docs/architecture.md) | for maintainers and agents: how the plugin runs, the conversion steps, the rules that must stay true |
| [hack/README.md](hack/README.md) | setting up a Minikube cluster with Shipwright for the cluster tests |

## Testing

Three levels.

**Unit tests**, no cluster:

```bash
GOTOOLCHAIN=auto go test ./...
```

These include the tests that keep the documentation honest: the support matrix must list
every warning the code can emit, the architecture page must name every file and step, the
examples must match the plugin's output, and this README's flag examples and version numbers
must match the code and CI.

**Plugin E2E**, the binary driven by crane over sample exports, no cluster. Needs the pinned
crane first on `PATH`:

```bash
./tests/e2e-transform.sh
```

**Cluster E2E**, on a Minikube cluster with Tekton and Shipwright. Converts two BuildConfigs
through crane, diffs each Build against a committed golden file, applies it, and runs a
BuildRun to completion:

```bash
./tests/e2e-cluster.sh              # after ./hack/setup-minikube-shipwright.sh and ./hack/fake-minikube-buildconfig.sh
./tests/e2e-cluster.sh --skip-build # verify the manifests only
```

Pull requests run the unit tests and the cluster E2E.

## Issue tracking

Work on this plugin is tracked in Jira, project BUILD. crane itself is tracked on GitHub.
File and pick up work in Jira.

## Related

- [Enhancement proposal](https://github.com/konveyor/enhancements/pull/300)
- [crane-plugin-openshift](https://github.com/migtools/crane-plugin-openshift), the reference crane transform plugin
- [Shipwright documentation](https://shipwright.io/docs/)
