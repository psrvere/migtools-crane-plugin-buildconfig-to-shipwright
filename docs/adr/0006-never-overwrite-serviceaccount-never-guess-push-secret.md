# ADR-0006: Never overwrite a named ServiceAccount, never guess a push credential

Status: accepted. Decided 2026-08-25 (PR #55, PR #50). Reviewed 2026-09-02.
Enhancement proposal: not covered there.

## Context

crane migrates a BuildConfig's named ServiceAccount as its own resource, which this plugin
never sees. If the plugin generated an account with the same name to carry the pull secret,
apply would overwrite the real one: crane keeps the last duplicate, and the pull-secrets list
replaces rather than merges. On the push side, the plugin cannot read the builder account on
the source cluster, and a guessed secret name gives a Build that Shipwright rejects for a
missing secret.

## Decision

Pull side: when the BuildConfig names a ServiceAccount and also has a pull secret, generate
nothing and warn with a complete `oc secrets link` command the operator can paste. Only when
no account is named, generate one, named after the BuildConfig, carrying the pull secret.

Push side: when the BuildConfig names no push secret, ship the Build without one and warn.
Never invent a name. This applies to ImageStreamTag and DockerImage outputs alike, with a
different remedy in each warning.

## Rules

- `generateServiceAccount` names the account through `uniqueName` from the BuildConfig's
  name, never from `spec.serviceAccount`.
- No state carries across BuildConfigs. The old cache of accounts is gone, because crane
  runs the plugin once per resource in a fresh process.
- A missing push secret always produces at least `converted-with-warnings`.

## Consequences

- The warning embeds three user-supplied names so it can be pasted. Accepted on purpose.
- Two separate warnings fire when a named account and a pull secret appear together, one
  per story, kept apart so their tests stay independent.
- A flag for a default push secret was declined to keep the command line small. Revisit only
  with evidence that operators cannot set `spec.output.pushSecret` themselves.
- On OpenShift with the Builds operator, a generated account cannot run a build pod until
  it is granted the `pipelines-scc` security context constraint. The default `pipeline`
  account has it; a generated one does not. BUILD-2402 is the story about migrating the
  account's permissions rather than warning.
- buildah uses the account's pull secret to pull the base image, so the secret must be a
  valid credential for that registry. A placeholder that names the registry breaks the
  pull.
