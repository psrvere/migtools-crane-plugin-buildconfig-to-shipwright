# ADR-0001: The plugin works offline and never calls a cluster

Status: accepted. Decided 2026-08-11 (PR #1). Reviewed 2026-09-02.
Enhancement proposal: this is its stated goal, "work offline on exported data".

## Context

The predecessor, the `crane convert` command, resolved ImageStreamTag and ImageStreamImage
references by calling the live cluster API. That made it impossible to convert an export
from a cluster you no longer have, and tied a migration tool to cluster credentials. crane
hands a plugin exactly one resource plus a flat map of flags. It sees no sibling resources
and no cluster.

## Decision

Every image reference resolves from flags. Strategy images and image sources go through
`resolveImageRef` in `imagestream.go`: the `imagestream-mapping` flag by exact
`namespace/name` key, then `registry-mapping` as a prefix rewrite, and otherwise the
internal-registry form of the reference with a warning. The output image resolves in
`processOutput` in `converter.go`, which applies the same flags with its own code. Nothing
in the plugin opens a Kubernetes client. Anything that would need cluster state becomes a
warning that names the flag which fixes it. Nothing is guessed from a name and nothing is
fetched.

## Rules

- No Kubernetes client in the plugin, ever.
- Strategy images and image sources resolve through `resolveImageRef`. A change there
  changes all three. The output image has its own path in `processOutput`; the two differ
  today on a tag-less name, which `processOutput` looks up with `:latest` appended and
  `resolveImageRef` looks up as written.
- An ImageStream reference that cannot be resolved falls back and warns. It does not fail
  the conversion. Only a reference kind the plugin does not recognise fails.
- A push secret name is never derived from a ServiceAccount the plugin cannot read.

## Consequences

- Correctness depends on the operator passing accurate mappings. A wrong flag gives a Build
  that fails at pull time, not at convert time.
- The fallback URL is a guess. The enhancement proposal review asked for a hard failure when
  no mapping matches. The plugin falls back on purpose: a move between two OpenShift
  clusters is the common case, the internal-registry form is right there, and the warning
  tells everyone else what to pass. The objection is recorded here rather than closed.
- A whole class of features is off the table: validating against the cluster's schema
  offline, deriving credentials from ServiceAccounts. Say so when one is proposed, rather
  than designing it twice.
