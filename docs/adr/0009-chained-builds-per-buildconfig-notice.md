# ADR-0009: Chained builds get a per-BuildConfig notice, never a cross-resource pass

Status: accepted. Decided 2026-09-03 (BUILD-2326).
Enhancement proposal: not covered there.

## Context

crane runs the plugin once per resource, so no conversion can see another BuildConfig.
OpenShift's chained-build pattern, a builder image built in the namespace or an artifact
image copied from with `source.images` and `paths`, relied on an ImageChange trigger to run
the consumer after the producer. Shipwright has no ordering between BuildRuns and no working
image trigger (ADR-0008). The plugin can tell that an input is an `ImageStreamTag` in the
BuildConfig's own namespace. It cannot tell whether another BuildConfig builds that image or
whether it was imported.

## Decision

Notice chains on the consumer, one BuildConfig at a time, and word every notice as "if
another BuildConfig builds that image". Where a warning already names the image, the dropped
ImageChange trigger or the `source.images` paths warning, append the run-order sentence to
that warning. Where no warning names the image, print an info line and leave the outcome
`converted`: nothing was lost, so nothing is recorded. Write nothing into the Build for it.

## Rules

- A chain candidate is an `ImageStreamTag` in the BuildConfig's own namespace
  (`chainCandidate` in `chain.go`). Other kinds and other namespaces get no notice.
- The run-order sentence is one constant, `chainRunOrderSentence`, appended to an existing
  warning or to the info line. It is never a warning of its own.
- The info line goes through `c.Log.Info`, not `warnf`, so a conversion with no other loss
  stays `converted`. This is not an exception to ADR-0003: nothing was dropped.
- One info line per image. An image a warning already names, the dropped ImageChange trigger
  or the `source.images` paths warning, gets no info line. `chainWatchedByTrigger` and
  `chainWarnedBySourceImages` in `chain.go` are the one place each of those two decisions is
  made, so the warning and the info line cannot drift into naming the same image twice. The
  sentence itself rides on every warning that names the image, so a BuildConfig whose trigger
  and whose `source.images` both point at one image reads it on both warnings. Each warning
  stands alone and repeating the remedy on both is the lesser evil.

## Consequences

- The info line reaches crane's terminal output and nowhere on the Build. A consumer that
  reads only the annotations cannot see it, and this is the one case with no warning to fall
  back on. A separate informational annotation is the intended follow-up if that matters. No
  story is filed for it.
- An imported ImageStream in the same namespace gets the same notice; the plugin cannot tell
  it apart, which is why the notice says "if".
- A cross-BuildConfig chain graph belongs to a pass over crane's written output, not to the
  plugin. No story is filed for it.
- The artifact chain also needs the multi-stage `COPY --from` rewrite the paths warning
  describes, because Shipwright's OCI artifact source unpacks the whole image at the context
  root.
- Pinned by `chain_test.go`.
