# ADR-0010: The default strategy names target the Builds for Red Hat OpenShift catalog

Status: accepted. Decided 2026-09-10 (BUILD-2469). Verified against strategy-catalog cb2432c
and Shipwright v0.19.0.
Enhancement proposal: assumed there, never stated.

## Context

`defaultDockerStrategy` is `buildah` and `defaultS2IStrategy` is `source-to-image`. Both are
names in [strategy-catalog](https://github.com/redhat-openshift-builds/strategy-catalog), which
the Builds for Red Hat OpenShift operator installs. Upstream Shipwright's sample strategies use
neither: its buildah strategies are `buildah-shipwright-managed-push`,
`buildah-strategy-managed-push` and `multiarch-native-buildah`, and it has no `buildah`. A user
running the plugin against upstream Shipwright v0.19.0 therefore gets a Build that Shipwright
refuses to register, and nothing in the documentation told them which Shipwright the defaults
assumed.

`--default-build-strategy` renames the strategy, so the obvious reading is that it makes the
plugin portable to upstream. It does not. A strategy name and the `paramValues` the converter
writes are one unit (ADR-0004), and the two catalogs do not declare the same parameters:

| Parameter the converter can write | catalog `buildah` | upstream `buildah-strategy-managed-push` |
|---|---|---|
| `build-args`, `dockerfile`, the three `registries-*` | yes | yes |
| `no-cache`, `pull`, `squash`, `runtime-stage-from` | yes | no |

| Parameter the converter can write | catalog `source-to-image` | upstream `source-to-image` |
|---|---|---|
| `builder-image` | yes | yes |
| `scripts-url`, `incremental`, `pull-policy`, the three `registries-*` | yes | no |

Upstream's `source-to-image` declares one parameter. Renaming the strategy on an upstream
cluster trades a registration failure on the name for one on the parameters, and only for a
BuildConfig plain enough to use none of the missing fields.

## Decision

The defaults name catalog strategies, and the catalog is the plugin's target. Upstream
Shipwright is not a supported target; a Build the plugin writes is expected to apply to a
cluster running the Builds for Red Hat OpenShift operator.

`--default-build-strategy` exists for a **copy of a catalog strategy** — the volume copy
ADR-0007 asks for, a variant with an extra parameter, a differently named install. It is not a
portability flag, and the documentation does not offer it as one.

## Rules

- `defaultDockerStrategy` and `defaultS2IStrategy` in `converter.go` are catalog names, in the
  same class as the parameter names ADR-0004 governs. Changing either means changing the
  catalog first.
- Docs that name a strategy say which catalog it comes from. A page that offers
  `--default-build-strategy` says it takes a copy of a catalog strategy, never "point it at
  upstream".
- Cluster tests may run against upstream Shipwright, because that is what
  `hack/setup-minikube-shipwright.sh` installs, and they carry the override in the test case's
  own `OPTIONAL_FLAGS`. That override is a testing accommodation, not a supported
  configuration.

## Consequences

- A user on upstream Shipwright gets `BuildRegistrationFailed` and, from the docs, the reason.
  The plugin still emits no warning about it: it cannot see the cluster (ADR-0001), and a
  warning that fired on every Docker and Source conversion would land in the
  `conversion-warnings` annotation of every Build the plugin ever writes.
- The parameter tables above are a point-in-time reading of two repositories. They drift when
  either catalog moves. The cluster E2E's registered check is still the only real drift signal
  (ADR-0004).
- Supporting upstream properly is a different piece of work: a parameter set the converter
  trims per target, or strategies contributed upstream. Neither is in scope here, and neither
  is bought by renaming a strategy.
