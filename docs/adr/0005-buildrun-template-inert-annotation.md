# ADR-0005: Resource requests become a BuildRun template in an annotation, never a live object

Status: accepted. Decided 2026-08-19 (PR #15), reaffirmed 2026-08-25 (BUILD-2314 closed).
Reviewed 2026-09-02.
Enhancement proposal: departs from it. The review agreed to a separate template file behind
a `--generate-buildrun-template` flag; what shipped is an annotation with no flag.

## Context

A Shipwright Build has no place for CPU and memory requests. They belong on a BuildRun's
`stepResources`. Writing a real BuildRun into the migration output would start a build the
moment the customer applies it. A later story proposed adding the template to every Build
for consistency and was closed as not needed.

## Decision

When, and only when, `spec.resources` has requests or limits, `processResources` renders a
complete BuildRun as YAML into the `buildconfig-to-shipwright/buildrun-template` annotation
on the Build. It is never a resource, never a file, and there is no flag. If the strategy was
renamed with the `default-build-strategy` flag, the step names are unknown, so the resources
are left out of the template with a warning rather than guessed.

## Rules

- One place writes the annotation, behind one check: no requests and no limits means no
  template.
- The step names in the template are only ever the ones verified on the shipped strategies:
  build-and-push for buildah, s2i-generate and buildah for source-to-image.
- A Build without resources carries no template. Anything else that wants to ride on the
  template has to decide first when the template exists. The node selector became a real
  Build field for that reason.

## Consequences

- The template is also the only carrier of the generated ServiceAccount. The Build never
  names that account. A customer who writes their own BuildRun instead of applying the
  template runs as the default account, and the pull secret the plugin set up is never used.
- A BuildConfig with a ServiceAccount but no resources gets no template and no message.
  Nobody owns that gap.
- The trigger step reads this annotation before the step that writes it, so the ConfigChange
  warning never mentions the template. Listed in the architecture page's ordering defects.
- The annotation shares the object's size budget with the warnings and the preserved
  triggers.
