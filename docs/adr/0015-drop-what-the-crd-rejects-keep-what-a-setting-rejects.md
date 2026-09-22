# ADR-0015: Drop a value the Build CRD rejects, keep one only a cluster setting rejects

Status: accepted. Decided 2026-09-22 (BUILD-2334). Verified against
shipwright-io/build v0.21.0.
Enhancement proposal: not covered there.

## Context

Shipwright v0.21.0 added two ways for a target to refuse something the converter
writes, and they are not the same kind of rule.

The first is in the Build CRD. `Image` carries
`+kubebuilder:validation:XValidation:rule="self.all(k, k != '' && !k.contains('='))"` on
both `Labels` and `Annotations`, and the generated CRD YAML ships it, so an API server
running v0.21.0 rejects the whole Build at apply time when one output label key holds an
`=`. Every cluster on that version enforces it, and the plugin can tell from the field
value alone whether it will fire.

The second is in the controller. `pkg/config/config.go` declares a default list of
forbidden environment variable names, `pkg/env.SetForbiddenEnvVars` installs it at start-up,
`pkg/validate/envvars.go` leaves a Build carrying one unregistered with the reason
`SpecEnvNameForbidden`, and `pkg/env.MergeEnvVars` refuses it again at TaskRun generation.
But the same file reads `FORBIDDEN_ENV_VAR_NAMES` and replaces the whole list with it, so
what is forbidden is a property of the cluster the Build lands on, not of the Build. The
plugin never sees that cluster (ADR-0001).

Rule 6 already says an invalid value is warned about and dropped whole, never clamped. It
was written for values the CRD's own schema rejects, such as a retention limit outside the
CRD's minimum and maximum, and it does not say what to do when the rejection depends on how
the target is configured.

## Decision

Where the rejection is in the Build CRD, drop the value and warn. Emitting it would cost
the operator the whole Build, not just the field, because the API server refuses the object.

Where the rejection comes from a controller setting that an administrator can change, keep
the value and warn. The plugin cannot know whether the rule applies to this target, and
dropping would silently throw away a value that is legal on some clusters. The Build still
applies; it sits unregistered until someone acts, which is a legible failure of the kind
ADR-0007 already accepts for volumes.

Either way the field is recorded through `warnf` (ADR-0003), naming the exact value and
what to do about it. Nothing is dropped silently and nothing is carried silently.

## Rules

- Classify a new upstream restriction before coding it. In the CRD, schema or CEL: drop and
  warn. A controller default, an environment variable or any other cluster setting: keep and
  warn.
- A warning for a kept value says the operator's two ways out: change the Build, or have the
  cluster allow the value.
- A mirror of an upstream list in this repository names the upstream file it was copied
  from, so the next version bump can diff the two. `forbiddenEnvVarNames` in `converter.go`
  keeps the exact entries for that reason, even the five the `LD_` prefix already covers.

## Consequences

- One env list can draw two warnings: the existing one about where `spec.env` lands for that
  strategy, and one per entry the blocklist covers.
- A Build the plugin knowingly leaves unregisterable is a new shape for this repository. It
  is the same trade as ADR-0007: fail on the cluster, legibly, rather than quietly convert
  into something different from what the BuildConfig said.
- The mirrored blocklist goes stale when Shipwright changes its default. Nothing detects
  that, so it is part of reading a Shipwright bump.
- Pinned by `strategy_env_test.go` (`TestConvertForbiddenStrategyEnvWarns`,
  `TestIsForbiddenEnvVar`) and `converter_test.go`
  (`TestConvertOutputImageLabelWithEqualsWarns`).
