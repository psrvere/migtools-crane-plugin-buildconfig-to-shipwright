# Architecture decision records

One file per decision that the code must keep obeying. Read the record for the area you are
about to change before changing it. Each record has the same parts: a status line, the
context, the decision, the rules that follow from it, and the consequences.

The founding design is the upstream enhancement proposal,
[konveyor/enhancements PR 300](https://github.com/konveyor/enhancements/pull/300). Every record
here refines it or departs from it, and says which.

| # | Decision |
|---|----------|
| [0001](0001-offline-conversion-no-cluster-calls.md) | The plugin works offline and never calls a cluster |
| [0002](0002-four-state-outcome-model.md) | Four outcomes, and one bad BuildConfig never stops the migration |
| [0003](0003-warnf-single-recording-path.md) | One way to record a warning, and a size cap on the annotation |
| [0004](0004-strategy-param-names-wire-contract.md) | Strategy parameter names are a wire contract, checked on a cluster, not offline |
| [0005](0005-buildrun-template-inert-annotation.md) | Resource requests become a BuildRun template in an annotation, never a live object |
| [0006](0006-never-overwrite-serviceaccount-never-guess-push-secret.md) | Never overwrite a named ServiceAccount, never guess a push credential |
| [0007](0007-volumes-fail-closed-original-names.md) | Volumes are converted under their original names and left to fail on the cluster, legibly |
| [0008](0008-triggers-preserved-not-converted.md) | Triggers are preserved and warned about, never converted |
| [0009](0009-chained-builds-per-buildconfig-notice.md) | Chained builds get a per-BuildConfig notice, never a cross-resource pass or a trigger |

A test checks that every file here has those parts, that the index and the files agree, and
that no link here points at a file that is missing. Nothing checks that a new decision gets a
record; that is on the reviewer. When you decide something the code must keep obeying, add the
next number, and add the rule to the table in `docs/architecture.md`.
