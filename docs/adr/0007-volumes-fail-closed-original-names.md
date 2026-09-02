# ADR-0007: Volumes are converted under their original names and left to fail on the cluster, legibly

Status: accepted. Decided 2026-08-20 (PR #21), reinforced 2026-08-25 (PR #56). Reviewed
2026-09-02, verified on a cluster.
Enhancement proposal: not covered there.

## Context

Shipwright matches a Build's volumes to the strategy by exact name and only accepts a volume
the ClusterBuildStrategy declares overridable. The shipped buildah and source-to-image
strategies declare exactly one, for entitlements. Every other converted volume makes the
Build fail registration with `UndefinedVolume`. The alternative, generic slot volumes in the
shipped strategies plus renaming on the plugin side, was rejected because mount paths live
only in the strategy's steps, so a renamed volume would still not be mounted where the build
expects it.

## Decision

Copy Secret and ConfigMap volumes onto the Build under their exact BuildConfig names and let
the cluster reject them. Spend the plugin's effort on making the rejection easy to fix: one
warning per volume with the three edits to make and the original mount path, and one summary
per BuildConfig naming the exact failure, both pointing at `docs/volume-migration.md`.

## Rules

- No renaming, no slot volumes, no silent drops (`processStrategyVolumes`).
- A volume with an unsupported source, a duplicate name, or no name is skipped with a
  warning. It never fails the whole conversion.

## Consequences

- Every converted Build with a volume ships knowing it will fail on the cluster until the
  operator copies the strategy. That is the intended, visible outcome.
- Verified 2026-09-02: refused with `UndefinedVolume`, registered after the strategy copy,
  built. Two details the warning does not say: the mount in the strategy copy must be
  `readOnly: true` or Shipwright refuses the BuildRun, and a secret key needs `subPath` to
  land as a file. The runbook says both; the warning's wording is on the defects list.
- The transform E2E fixtures use made-up volume names and would fail if applied. They test
  conversion only.
