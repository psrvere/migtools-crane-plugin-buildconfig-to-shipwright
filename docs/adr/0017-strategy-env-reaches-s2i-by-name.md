# ADR-0017: Strategy env reaches s2i by name, from `spec.env`

Status: accepted. Decided 2026-09-24 (BUILD-2500).
Enhancement proposal: not covered there.

## Context

On OpenShift, `sourceStrategy.env` reached the assemble script and was written into the output
image. openshift-controller-manager resolved every `valueFrom` kind before the build started,
expanded `$(OTHER)` references against earlier entries, and read a `fieldRef` from the Build
object. s2i then wrote each entry into its generated Dockerfile as `ENV`.

Shipwright's `spec.env` sets the environment of every strategy step, but the source-to-image
strategy does not hand it to s2i. The strategy takes a `build-env` array parameter instead,
since Builds 1.9.0, and passes each item to `s2i build -e`. Each item is `NAME=VALUE` text,
so it cannot carry a reference to a ConfigMap or a Secret the way `spec.env` can.

## Decision

`sourceStrategy.env` is copied to `spec.env` unchanged, and `build-env` names each entry as
`NAME=$(NAME)`, in order. Shipwright merges `spec.env` into the `s2i-generate` step, and the
kubelet expands `$(NAME)` in that step's arguments from its environment. s2i gets the
resolved value, whatever kind of entry produced it.

This is the mechanism Shipwright uses for its own ConfigMap and Secret parameters: the
parameter value becomes `$(SHP_SECRET_PARAM_…)` and the step gets an env var with `valueFrom`.

The alternative was the build-args mapping in `processDockerStrategy`: literal values inline,
ConfigMap and Secret keys as `ObjectKeyRef`. It shows each value on the Build, but it has no
form for a `fieldRef`, cannot express `optional: true`, and leaves `$(OTHER)` unexpanded
unless `spec.env` is kept as well.

## Rules

- `spec.env` keeps every `sourceStrategy.env` entry. `build-env` refers to it and never
  copies a value, so a Secret value never appears in the Build or the TaskRun.
- A name that cannot sit inside `$(...)` (the build-arg rules, plus `(` and `)`) stays in
  `spec.env`, is left out of `build-env`, and is named in W81.
- A `fieldRef` or `resourceFieldRef` entry is passed like any other, and W82 says it now reads
  the BuildRun pod, not the OpenShift Build.
- A name on Shipwright's blocklist is named in `build-env` like any other. W79 already says
  the Build will not register until `spec.env` changes (ADR-0016). An entry taken out of
  `spec.env` has to leave `build-env` too, or s2i gets the literal text `$(NAME)`; the
  support matrix row says so.
- Only `sourceStrategy.env` feeds `build-env`. Git proxy variables that `processGitProxyConfig`
  adds to `spec.env` are for the clone and stay out of it.

## Consequences

- The Build shows `NAME=$(NAME)` rather than the value; the value is in `spec.env`. Deleting
  an entry from `spec.env` later leaves s2i the literal text `$(NAME)`.
- Secret-sourced entries end up in the output image as `ENV`, exactly as they did on
  OpenShift. The support matrix says so.
- A target whose source-to-image strategy has no `build-env` parameter refuses the Build with
  `UndefinedParameter`. The released strategies without it, every Builds release before 1.9,
  already refuse the `builder-image` parameter the plugin has always emitted. Upstream
  Shipwright's sample strategy has `builder-image` but not `build-env`; ADR-0010 keeps
  upstream out of scope, and issue #98 tracks running cluster CI on the catalog strategies.
- `TestConvertSourceEnvBuildEnv` in `strategy_env_test.go` keeps these rules.
