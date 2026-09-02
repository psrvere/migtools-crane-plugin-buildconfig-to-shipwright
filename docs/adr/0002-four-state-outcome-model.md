# ADR-0002: Four outcomes, and one bad BuildConfig never stops the migration

Status: accepted. Decided 2026-08-22 (PR #36). Reviewed 2026-09-02.
Enhancement proposal: not covered there; added after the first conversions shipped.

## Context

Conversion originally had three implicit exits, full conversion, pass-through, and error,
and warnings were not an outcome at all. A Build that shipped with half its fields dropped
looked the same as a clean one. Separately, crane aborts the entire migration on any plugin
error. That is correct for a generic framework, since crane only sees an error. Only the
plugin can tell a per-BuildConfig failure from a global one.

## Decision

`Convert` returns an `Outcome` with exactly one of four states: `converted`,
`converted-with-warnings`, `skipped`, `failed`. `Run` switches on it. Converted means the
original is deleted and the new resources take its place. Skipped and failed mean the
original passes through unchanged, with two annotations saying what happened and why, added
by a JSON patch built in `disposition.go`. `Run` returns no error for any of those. It
returns an error only for the three problems before conversion starts: flags that cannot be
parsed, an object that cannot be re-encoded, or one that cannot be decoded as a BuildConfig.

## Rules

- Every BuildConfig ends in exactly one state (`outcome.go`).
- `Run` never returns an error for a per-object failure. Its fallback branch passes the
  object through rather than deleting it.
- A skipped or failed BuildConfig is self-describing in the output directory, not only in
  the log.

## Consequences

- The output directory is the audit trail. There is deliberately no report file; the plugin
  has no run loop and no exit code to hang one on.
- A global failure now looks like many per-object failures, which is noisier.
- A fifth state means touching `Run`, the annotation writer, and the tests together.
- Skipped and failed outcomes carry no warnings. The strategy step runs before the
  output-image gate, so a BuildConfig with no output image does its strategy work, raises
  warnings, and is then skipped with an empty warnings list. That is a known gap, not a
  choice; it is listed in the architecture page's ordering defects.
