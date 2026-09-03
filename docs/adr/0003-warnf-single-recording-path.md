# ADR-0003: One way to record a warning, and a size cap on the annotation

Status: accepted. Decided 2026-08-25 (PR #57). Reviewed 2026-09-02.
Enhancement proposal: not covered there.

## Context

The outcome model shipped in one PR, and eleven places still logged field drops straight to
the logger, invisible to it. The worst was triggers: every trigger on a BuildConfig was
dropped and the object was still reported as cleanly converted. Separately, the warnings
annotation had one writer that used assignment, so a second contributor would have
overwritten it. Kubernetes rejects an object whose annotations total more than 256 KiB, and
warning text contains user-controlled names.

## Decision

`warnf` in `outcome.go` is the only thing that counts as a recorded drop. It prefixes the
message with the BuildConfig's namespace and name, appends it to the converter's list, and
logs it at WARN. The one place that logs at ERROR instead, the inline Dockerfile on a Docker
strategy, calls `recordWarning` directly so it still counts. `Convert` writes the warnings annotation exactly once, from the same list
that decides the outcome, cut at 32 KiB by `boundedWarnings` with a note saying how many
were left out. The log always has the full list.

## Rules

- No `c.Log.Warn` call for a drop anywhere in the package. A test asserts the count is zero.
- Attribution happens inside the recorder, never in the message strings. `Convert` sets the
  current namespace and name on entry and clears them on exit.
- One writer for the annotation, and the 32 KiB cap.

## Consequences

- The rule was convention plus a grep at review until the test landed. One direct log call
  reopens the exact defect, so the test exists. An open PR that predates this record
  records its warnings with `c.Log.Warnf` and has to be ported before it lands.
- The support matrix test, `TestSupportMatrixCoversEveryWarning` in PR #65, fails when a
  warning template appears in the code with no row in the matrix. It does not catch a direct
  log call; the zero-count test does.
- The 32 KiB cap is a deliberate fraction of the ceiling, leaving room for the BuildRun
  template and the preserved triggers on the same object.
- Warnings are prose with no stable IDs, so tooling that wants to count categories has to
  match strings. The matrix keeps the strings stable by test (PR #65).
