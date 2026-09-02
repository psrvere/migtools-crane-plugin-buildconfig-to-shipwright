# ADR-0004: Strategy parameter names are a wire contract, checked on a cluster, not offline

Status: accepted. Decided 2026-08-25 (BUILD-2317 closed, BUILD-2323, BUILD-2328). Reviewed
2026-09-02, verified against Builds for Red Hat OpenShift 1.9.0.
Enhancement proposal: not covered there.

## Context

The plugin writes `paramValues` whose names must match what the referenced
ClusterBuildStrategy declares. Ship a parameter the strategy does not declare and Shipwright
refuses to register the Build. Three stories proposed a static guard: constants shared across
repositories, a cross-repository golden test, and vendored strategy files validated at
conversion time. All three were rejected, the last after being fully built. An offline copy
of the catalog always passed against the catalog it was built from, and passed wrongly
against a cluster whose strategies reuse the same names with different parameters.

## Decision

Parameter names stay plain strings in the converter, pinned by plain-string assertions in the
unit tests. Drift is caught by applying a converted Build to a real cluster and asserting that
Shipwright registers it. A new parameter lands in the strategy catalog first and the converter
second, because the operator does not accept hand-edited strategies.

Verified 2026-09-02: every parameter the plugin emits is declared by the shipped strategy it
targets. buildah: build-args, dockerfile, no-cache, pull, runtime-stage-from, squash, and the
three registries lists. source-to-image: builder-image and the registries lists. The step
names in the BuildRun template, build-and-push for buildah and s2i-generate plus buildah for
source-to-image, match too.

## Rules

- The parameter-name constants at the top of `converter.go` and the bare literals in
  `processDockerStrategy`, `processSourceStrategy` and `addRegistries` are wire values. A
  rename without a catalog change is a silent drop on the cluster.
- Tests assert the wire strings directly, never through the constants.
- The drift signal is the cluster E2E's registered check in `tests/e2e-cluster.sh`, not a
  golden file.

## Consequences

- Nothing catches a typo until a cluster run, and the merge gate deliberately does not
  require the cluster job.
- Check any future "validate offline against a vendored schema" proposal against this
  outcome before building it.
- `addRegistries` routes the `insecure-registries` flag to `spec.output.insecure` for
  source-to-image rather than to the `registries-insecure` parameter. The comment in the
  code says the strategy has no such parameter. It does; the real reason is that the
  strategy's push step does not read the registries file, so the parameter would have no
  effect on the push.
