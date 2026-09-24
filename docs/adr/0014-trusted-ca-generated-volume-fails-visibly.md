# ADR-0014: The trusted CA bundle becomes a generated volume that fails visibly

Status: accepted. Decided 2026-09-23 (BUILD-2265, PR #23). Amends ADR-0007.
Enhancement proposal: not covered there.

## Context

`spec.mountTrustedCA: true` told OpenShift to mount the cluster's trusted CA bundle into the
build, so a build could reach a registry or a git server behind a private CA. Shipwright has
no such field. Until strategy-catalog `cb2432c` there was nowhere to put the bundle either:
ADR-0007 records that the shipped strategies declared one overridable volume, for
entitlements, and a Build volume the strategy does not declare fails registration.

`cb2432c` (BUILD-2342, strategy-catalog PR #30, merged as `2d3a65f`) added a second
overridable volume, `trusted-ca`, to both `buildah` and `source-to-image`. That gives the
field a target, and turns a silent drop into a mapping. It also creates a new way to be
wrong: the volume exists only on a catalog from that commit onward, and the plugin is
offline (ADR-0001), so it cannot see which catalog the target runs.

## Decision

`processMountTrustedCA` appends a `trusted-ca` volume to the Build, backed by a ConfigMap the
plugin generates and labels `config.openshift.io/inject-trusted-cabundle: "true"`, which is
what OpenShift builds themselves rely on: the Cluster Network Operator fills `ca-bundle.crt`
in any ConfigMap carrying that label. The plugin writes no `data`, because it has no bundle
to write and inventing one would be trust material the user did not ask for.

The mount is built to fail rather than to degrade. Only `ca-bundle.crt` is projected and
`optional` is left unset, so a ConfigMap the injector never filled stops the BuildRun pod at
mount time instead of letting the build run without the trust it asked for.

Everything the plugin cannot check offline is said in a warning instead: the injector may not
exist, and the catalog may predate `cb2432c` (W76), and a strategy named through
`--default-build-strategy` may declare no such volume (W77). ADR-0004 settled that shape for
parameter names; this is the same trade for a volume name.

## Rules

- The mapping defers to the BuildConfig's own `trusted-ca` volume whenever one is declared,
  and says so (W75). That includes a volume `processStrategyVolumes` dropped as unsupported:
  a user who named their own CA source must never get the cluster-wide bundle under that name
  by accident.
- Which strategy block is read follows `spec.strategy.type`, the way `Convert` dispatches,
  never whichever of `dockerStrategy`/`sourceStrategy` happens to be non-nil. Both can be
  populated on one BuildConfig.
- The projection names `ca-bundle.crt` and nothing else, and `optional` stays unset. A key
  added to the ConfigMap later never enters the build's trust store, and a missing key is
  visible.
- The ConfigMap name comes from the BuildConfig through `uniqueName`, like the Build, the
  generated ServiceAccount and the inline-Dockerfile ConfigMap. One per conversion, never a
  shared well-known name that an `oc apply` of the output would relabel for CA injection. The
  suffix is `-trusted-ca-migrated`, not the shorter `-trusted-ca`, because the plugin runs
  once per resource and cannot see whether the export already has a ConfigMap of that name;
  crane's own dedup keeps only the last Kind/namespace/name collision and drops the other
  silently. A generated ConfigMap winning that collision would carry the CNO's
  inject-trusted-cabundle label onto a ConfigMap the user owns, and the operator would
  then overwrite its data — a name this specific makes that realistically impossible.
- A strategy the plugin cannot vouch for warns; it never refuses the conversion (rule 15 in
  [architecture.md](../architecture.md)).

## Consequences

- Every converted BuildConfig that sets the field lands in `converted-with-warnings`, because
  W76 is unconditional. [known-limitations.md](../known-limitations.md) carries the operator
  step under "Converted, but needs a step from you".
- A target whose catalog predates `cb2432c` gets a Build Shipwright refuses to register,
  `Registered=False`, reason `UndefinedVolume`. W76 names the commit and the check, which is
  the only thing an offline plugin can do about it.
- Off OpenShift there is no Cluster Network Operator, so the key has to be filled by hand
  before the first BuildRun. The build fails loudly until it is.
- The generated ConfigMap is a third generated resource. Both the architecture page and the
  support matrix list it, and `crane apply` writes it beside the Build.
- Pinned by `trustedca_test.go`: `TestConvertMountTrustedCA`,
  `TestConvertMountTrustedCAVolumesFollowStrategyType`,
  `TestConvertMountTrustedCAUnsupportedSourceCollision`,
  `TestConvertMountTrustedCACustomStrategyWarning`,
  `TestConvertMountTrustedCAPerBuildConfigMaps`.
