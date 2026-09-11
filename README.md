# BuildConfigToBuildsPlugin for crane

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

Every field the plugin reads and then drops or changes produces a warning, in the log and in
an annotation on the Build. The few fields it never reads are listed in the support matrix. The annotation is size-capped, so on a very lossy BuildConfig the log is the
complete list. The full list, field by field, is in [docs/support-matrix.md](docs/support-matrix.md).
The short list of what does not migrate, and what is planned, is in
[docs/known-limitations.md](docs/known-limitations.md).

| BuildConfig strategy | Shipwright ClusterBuildStrategy | Outcome |
|---|---|---|
| Docker | `buildah` | converted |
| Source (S2I) | `source-to-image` | converted |
| Custom | none | skipped, passed through with two annotations |
| JenkinsPipeline | none | skipped, passed through with two annotations |

## Prerequisites

- **crane v0.11.0-alpha.1 or newer**, with this plugin installed into it. crane ships with
  no plugins of its own; `crane plugin-manager` fetches them. Both steps are below, and
  neither needs a Go toolchain.
- **A target cluster with Shipwright and Tekton**, and the `buildah` and `source-to-image`
  ClusterBuildStrategies. Builds for Red Hat OpenShift ships both. Upstream, CI tests
  against Shipwright v0.19.0.

### Install crane

Take the binary for your platform from the [releases
page](https://github.com/migtools/crane/releases):

```bash
CRANE_VERSION=v0.11.0-alpha.1
curl -Lo crane "https://github.com/migtools/crane/releases/download/${CRANE_VERSION}/crane_linux_amd64"
chmod +x crane
sudo mv crane /usr/local/bin/
crane version
```

Assets are named `crane_<os>_<arch>`, so `crane_darwin_arm64`, `crane_linux_arm64` and
`crane_windows_amd64.exe` are there too, and `checksums.txt` on the same release verifies
them.

### Install the plugin

crane keeps a plugin index at
[migtools/crane-plugins](https://github.com/migtools/crane-plugins). `plugin-manager` reads
it and downloads the released binary for your platform:

```bash
crane plugin-manager add BuildConfigToBuildsPlugin
crane plugin-manager list
```

The binary lands in `$HOME/.local/share/crane/plugins`. `--global` installs to
`/usr/local/share/crane/plugins` for every user on the machine instead. `crane transform`
searches both of those, plus `/usr/share/crane/plugins` and a `plugins/` directory under
the current working directory, so neither install needs `--plugin-dir` below. A binary you
keep anywhere else, one you were handed or built yourself, does:

```bash
crane transform KubernetesPlugin BuildConfigToBuildsPlugin --plugin-dir /path/to/plugins
```

A crane built from [migtools/mta-crane](https://github.com/migtools/mta-crane) has this
plugin compiled in, so skip this section there. Still name the stage in every
`crane transform` below: the built-in is registered off by default and runs only when named.

crane's own [README](https://github.com/migtools/crane/blob/main/README.md) covers
installing crane, the plugin manager and the export, transform and apply cycle in general.
Working on the plugin rather than using it means building crane and the plugin from source.
That is in [hack/README.md](hack/README.md).

## Usage with crane

### 1. Export the namespace

```bash
crane export -n myapp
```

### 2. Transform

```bash
crane transform KubernetesPlugin BuildConfigToBuildsPlugin \
  --optional-flags '{"registry-mapping":"image-registry.openshift-image-registry.svc:5000=quay.io/myorg"}'
```

Name both stages. `BuildConfigToBuildsPlugin` is this plugin. `KubernetesPlugin` is crane's
built-in one, which strips `uid`, `resourceVersion` and `status`; those fields stop a
resource applying to a different cluster, so leave it in. Naming stages runs those two and
nothing else, which keeps any other plugin you have installed out of this migration.

`--optional-flags` takes one JSON object whose keys are the plugin's flags and whose values
are strings. It reaches every stage that runs, and a stage ignores a key it does not
declare. The flags are listed [below](#plugin-flags); `crane transform optionals` prints
them with an example each.

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
`BuildRun` to start the first build. The triggers the BuildConfig had are in
[docs/trigger-migration.md](docs/trigger-migration.md): a listener for webhooks, a Pipeline
or a CronJob for ImageChange.

Which ServiceAccount it runs as depends on whether the plugin generated one. If it did not,
leave the BuildRun's `serviceAccount` unset and it runs as the namespace `pipeline` account.
If it did, that account carries the BuildConfig's pull secret, so point the BuildRun at it.
The plugin names it in the Build's `buildconfig-to-shipwright/buildrun-template` annotation
when the BuildConfig also set resources; otherwise it is the account named after the
BuildConfig next to the Build. Leaving it unset drops the pull secret and a private builder
image will not pull.
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
| [docs/known-limitations.md](docs/known-limitations.md) | what does not migrate, what to do instead, and what is planned |
| [docs/examples](docs/examples/README.md) | three worked examples, verified on a cluster |
| [docs/volume-migration.md](docs/volume-migration.md) | why a Build with volumes fails with `UndefinedVolume`, and the strategy-copy fix |
| [docs/trigger-migration.md](docs/trigger-migration.md) | getting builds to fire again: a listener for webhooks, a Pipeline and a CronJob for ImageChange, the first BuildRun for ConfigChange |
| [docs/architecture.md](docs/architecture.md) | for maintainers and agents: how the plugin runs, the conversion steps, the rules that must stay true |
| [hack/README.md](hack/README.md) | setting up a Minikube cluster with Shipwright for the cluster tests |

## Working on the plugin

Building crane and the plugin from source, the three levels of tests, and setting up a
Minikube cluster with Tekton and Shipwright are in [hack/README.md](hack/README.md) and
[AGENTS.md](AGENTS.md). Pull requests run the unit tests and the cluster E2E.

## Issue tracking

Work on this plugin is tracked in Jira, project BUILD. crane itself is tracked on GitHub.
File and pick up work in Jira.

## Related

- [Enhancement proposal](https://github.com/konveyor/enhancements/pull/300)
- [crane-plugin-openshift](https://github.com/migtools/crane-plugin-openshift), the reference crane transform plugin
- [Shipwright documentation](https://shipwright.io/docs/)
