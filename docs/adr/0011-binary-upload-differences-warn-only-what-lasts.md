# ADR-0011: The binary-build warning names only the upload differences that will last

Status: accepted. Decided 2026-09-17 (BUILD-2475, PR #91 review). Verified on a ROSA cluster
with Builds for Red Hat OpenShift 1.9.0, `oc`, and `shp` built from shipwright-io/cli `main`,
from PR 409 and from PR 355.
Enhancement proposal: not covered there.

## Context

A binary BuildConfig becomes a Build with a Local source, and each build starts with
`shp build upload` instead of `oc start-build --from-dir` (W67). Review on PR #91 pointed out
that the two commands do not send the same files. One test directory went through both on a
cluster. `oc` sent everything except `.git/`. `shp` from `main` left out five things, and
they are not all the same kind of gap:

| What `shp` leaves out | Upstream status |
|---|---|
| Files the top-level `.gitignore` lists | By design. [SHIP-0021](https://github.com/shipwright-io/community/blob/main/ships/0021-local-source-upload.md) asks the CLI to skip them so an upload stays small, and `shp build upload --help` says so. There is no switch and no request for one |
| A symlink pointing outside the directory | Dropped today. After [cli#355](https://github.com/shipwright-io/cli/pull/355) merges the upload fails, which a maintainer asked for on security grounds ([review comment](https://github.com/shipwright-io/cli/pull/355#discussion_r3110712038), [review](https://github.com/shipwright-io/cli/pull/355#pullrequestreview-4140053687)) |
| Anything whose name starts with `.git`, such as `.github/` and `.gitignore` | Bug [cli#408](https://github.com/shipwright-io/cli/issues/408); fix [cli#409](https://github.com/shipwright-io/cli/pull/409) is in review and worked on the cluster |
| Symlinks inside the directory | Bug; fix [cli#355](https://github.com/shipwright-io/cli/pull/355) is in review, has no issue, and worked on the cluster |
| Empty directories | Not reported. Neither fix changes it |

## Decision

W67 names the first two rows, the differences Shipwright keeps on purpose. The three bugs stay
out of the warning text and are listed, with their upstream links, in
[known-limitations.md](../known-limitations.md#what-shp-build-upload-leaves-out).

We file nothing upstream for now. The `.gitignore` rule is the one most likely to hurt a
migration, since a locally built `target/app.jar` is usually ignored, but we wait to see
whether customers bring that case before asking Shipwright for a switch. Empty directories
are not worth an issue: git does not keep them either, and a Dockerfile can create one with
`RUN mkdir`.

## Rules

- The W67 text says what will still be true after the open upstream fixes merge. Do not add
  a sentence about a bug that has a fix in review; put it in known-limitations.md.
- Symlinks pointing outside the directory are described as "not sent", which is true both
  before cli#355 (dropped) and after it (the upload fails).
- `TestConvertBinaryDirectorySource` in `converter_test.go` asserts the `.gitignore` and
  outside-symlink wording, so dropping either needs this record changed first.

## Consequences

- Until cli#409 and cli#355 merge, W67 understates what `shp` drops. The known-limitations
  table covers the gap, and a user reading only the annotation can still miss it.
- Follow up upstream to get [cli#409](https://github.com/shipwright-io/cli/pull/409) and
  [cli#355](https://github.com/shipwright-io/cli/pull/355) merged. When each merges, delete its
  row from the known-limitations table and from the table above; W67 does not change.
- If customers hit the `.gitignore` rule, file a feature request on shipwright-io/cli for a
  switch or an exclude option like `oc start-build --exclude`, and revisit this record.
- W68 (`asFile`) is unchanged: the user builds a fresh directory holding one file, so none of
  these differences applies in practice.
