# ADR-0013: crane carries a named ServiceAccount and its RBAC; the plugin only points at it

Status: accepted. Decided 2026-09-10 (BUILD-2402).
Enhancement proposal: not covered there.

## Context

BUILD-2402 asked the plugin to migrate the account a BuildConfig names, with its secrets,
RoleBindings and ClusterRoleBindings, instead of warning that they stay behind. The plugin
sees one resource per process and has no cluster access (ADR-0001), so it cannot read the
account. It does not need to. crane export writes every namespaced object, the account and
its user-created Secrets and RoleBindings included, plus the ClusterRoleBindings,
ClusterRoles and SecurityContextConstraints that name an exported account
(`cmd/export/cluster.go` in crane). crane-lib's KubernetesPlugin drops the token and
dockercfg secrets and strips the subject namespace from RoleBindings; crane-plugin-openshift
strips the `<name>-dockercfg-*` references from every account. This plugin passes all of it
through untouched. Two things do not come across: crane-lib clears every account's `secrets`
list (BUILD-2343), and the `builder`, `deployer` and `default` accounts are dropped under
`strip-default-rbac`, the first two by crane-plugin-openshift and `default` by crane-lib.

Which of those two runs is the operator's choice, and the difference matters. `crane
transform` runs every plugin it discovered only when no stages are named on the command
line (`cmd/transform/transform.go`); name stages and it runs those. The README names two,
`KubernetesPlugin` and this one, so `default` goes in the documented workflow and `builder`
and `deployer` stay. Having crane-plugin-openshift installed decides nothing.

## Decision

The plugin emits nothing crane already migrates. For a named account it writes the name into
the BuildRun template (ADR-0012) and warns W9: what crane carries, and what to check on the
target. For `builder`, `deployer` or `default` it warns W72 instead. Neither warning states
what happened, because the plugin sees one resource and cannot know which stages ran. W72
says the account may not have come across, gives the command that settles it, and makes the
remedy depend on the answer. A flag that would let the plugin
read the export directory to synthesize or patch the account was declined: one owner per
object, and the single-resource contract stays.

## Rules

- Never emit a ServiceAccount, Secret, RoleBinding or ClusterRoleBinding for a named
  account. Rule 7 (ADR-0006) already forbids a same-named account; this extends it to
  everything crane carries for it.
- W9 names what crane carries and the three checks: the account was in the export,
  `--for=mount` links are re-linked, the `_cluster` resources were read and applied. An SCC
  or ClusterRole is cluster-global and an exported SCC carries the source cluster's own
  users and groups, so one that shares a name with an SCC on the target replaces it, which
  is why the check is by name and not a blanket apply. It stays a
  warning until BUILD-2343 lands, because the secrets-list loss is real and the plugin
  cannot see whether it applies.
- W8, the pull-secret link command, no longer claims crane migrates the account, so it
  and W72 can share a Build without contradicting each other. Its command tail is
  unchanged here; BUILD-2439 later kept an invalid name out of it with a placeholder
  (ADR-0006). The two also have to land on one
  account: W8 links the secret to the account the BuildConfig named, so W72 keeps the
  BuildRun on that account unless the check shows it is missing, and says to carry the same
  secrets over when it does.

## Consequences

- A named account keeps the outcome at `converted-with-warnings`.
- A label-selected export can leave the account out; the plugin cannot tell, which is why
  W9 says to check.
- The SCC grant a generated account needs is still a documented manual step (README,
  ADR-0006). Emitting a RoleBinding for it is a separate decision, proposed as its own
  story. W72 offers the same shape for a dropped account: an account created for this build
  with the SCC granted to it alone, next to the `pipeline` account, which is quicker and is
  a shared namespace identity.
- Pinned by `converter_test.go`: `TestServiceAccountAssociationWarned`,
  `TestServiceAccountBuilderAndDeployerWarnedSeparately`,
  `TestNamedServiceAccountWithPullSecretIsNotGenerated`,
  `TestDroppedServiceAccountWithPullSecretWarnsTwiceWithoutContradiction`.
