# ADR-0008: Triggers are preserved and warned about, never converted

Status: accepted. Decided 2026-08-20 (PR #19), extended 2026-08-21 (PR #20). Reviewed
2026-09-02, verified against Builds for Red Hat OpenShift 1.9.0.
Enhancement proposal: not covered there.

## Context

Shipwright's Build API has a trigger section. The plugin's primary target, Builds for Red Hat
OpenShift, does not ship anything that reads it: on a 1.9.0 cluster the only Shipwright
workloads are the build controller and its webhook. A trigger written onto a Build there
would look configured and do nothing. Upstream Shipwright has a separate triggers component,
shipwright-io/triggers, released and maintained, that a user can install. The plugin cannot
know whether the target has it, and the OpenShift webhook URLs stop working either way.
Emitting triggers is parked on the stories about the operator shipping that component.

## Decision

For every trigger on a BuildConfig, warn once with its type, add one summary warning, and
keep the original triggers as sanitised JSON in the
`buildconfig-to-shipwright/original-triggers` annotation on the Build, so a tool or a person
can rebuild them later without the original BuildConfig. Write nothing into the Build's own
trigger section.

## Rules

- Inline webhook secret values and the last-triggered image ID never enter the annotation
  (`sanitizeTrigger` in `triggers.go`).
- An empty image-change trigger is preserved as-is. That shape means "watch the strategy's
  image", and dropping it loses intent.

## Consequences

- Nothing reads the annotation today. It is a bet on a future component.
- It shares the object's size budget with the warnings and the BuildRun template.
- House rule: emit nothing rather than configuration that looks configured and does
  nothing. The same rule was applied to the cluster proxy and to pull-policy validation.
- When the product ships triggers, or for upstream users who install the component, the
  intended next step is a flag that emits the Build's trigger section from the preserved
  annotation. Build that rather than a second preservation scheme.
