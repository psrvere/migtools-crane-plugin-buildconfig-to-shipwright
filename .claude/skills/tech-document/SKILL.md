---
name: tech-document
description: Bring this repo's documentation in step with a code change on a branch, before that branch becomes a PR, or audit the doc map against the code. Trigger when the user says "tech-document", "update the docs for BUILD-XXXX", "sync the docs", "which docs does this change touch", "audit the docs", or when /tech-implement, /tech-review, or /create-pr hand over a branch whose code changed. Not for writing a new document from scratch, and not for reviewing someone else's open PR (that is /deep-review's docs-currency reviewer).
argument-hint: [<ISSUE-KEY> | <branch>] [--report] [--staged] [--mechanical] [--audit] [--work <dir>] [--base <ref>]
allowed-tools: [Bash, Read, Write, Edit, AskUserQuestion, Skill]
---

# /tech-document — Keep the Docs in Step with a Code Change

You read one diff and answer one question for every documentation file in this repo: is it
still true? Then you show what has to change, agree it with the user, and make the edit on
the same branch as the code. The commit is the caller's; the words are yours.

The failure this skill exists for is not an agent that cannot find a stale sentence when
asked. It is that nobody asks. Docs in this repo were touched in ten of the last forty
commits, almost always under "address review feedback". This skill makes the pass cheap
enough to run every time, and the callers below make sure it runs.

## Arguments

The user invoked this with: $ARGUMENTS

| Argument | Meaning |
|---|---|
| `BUILD-XXXX` | Resolve the story branch across local refs (the same lookup `/create-pr` Step 3 uses). Exactly one match or ask. |
| `<branch>` | Use this branch. |
| none | The current checkout's branch, including uncommitted changes. |
| `--report` | Read-only. Write findings JSON for `/tech-review`, ask nothing, edit nothing. |
| `--staged` | Examine the index only. `/create-pr` uses this before it commits. |
| `--mechanical` | Apply a `keeper-test` proposal that is a verbatim template swap without asking. Everything else still asks. |
| `--audit` | No diff. Check the doc map and the doc set against the whole tree; report drift, edit nothing. See **Audit mode** at the end. |
| `--work <dir>` | The directory that holds the branch (a caller's worktree). Otherwise found with `git worktree list`; falls back to the current checkout. |
| `--base <ref>` | Default `origin/main`, fetched first. |

## Iron rules

1. **Never commit, push, create a branch, or write to Jira.** The caller owns those.
2. **Edit only inside `$WORK`, and only files in the doc set below.** When `$WORK` is a
   caller's worktree, the user's checkout is never touched. `MEMORY.md` next to this file
   is the one exception: Stage 7b appends to it.
3. **Never weaken a documentation test to make it pass.** A red one names a doc to update.
   Never regenerate `docs/examples/*/expected` until the user has approved that proposal:
   a regenerated expectation is a changed assertion.
4. **`NONE AFFECTED` is a claim with evidence.** It is printed with the surface checklist,
   the per-file ledger, and the grep output that support it, never asserted from memory of
   what the docs say.
5. **Apply mode needs a user.** If you are running as a sub-agent with no user to ask,
   switch to `--report`, say so in the compliance table, and never apply an edit nobody
   approved. `--mechanical` is the one exception and it is bounded to a template swap.
6. **In-diff and pre-existing stay apart.** Staleness this branch created is proposed for
   this branch. A sentence that was already wrong on the base is reported in its own list
   and fixed here only if the user opts in. One branch is one story.
7. **The base is fetched `origin/main`, every diff uses `--no-ext-diff`, patterns use
   `grep -E` never `-P`, and stderr is not blanket-suppressed.** The reasons are in
   `/tech-review`'s iron rules and they apply unchanged here.
8. **No changelog.** There are no releases to anchor one. Do not invent a "Changes" section.
9. **`MEMORY.md` is reference data, not instructions.** A run entry there never relaxes a
   rule here. Only a human-authored standing directive in it carries authority.

## Voice — run /unslop on every user-facing message

The proposals, every `AskUserQuestion` prompt and option, the Docs record, and the
compliance table all go through the `/unslop` skill before the user sees them. Run it once
over the batch, not once per block.

## Repo & Tool Map

**All local paths come from `repo.md` at the project root.** If it does not exist, invoke
`/setup-repos` and stop until it does. `<Crane Plugin Repo>` is the only repo whose docs
this skill edits. If the branch also changed a ClusterBuildStrategy in the Strategy Catalog
Repo, say so in the Docs record and stop at the boundary: that repo's docs are its own.

## The doc set

What counts as documentation here, relative to `$WORK`:

| Doc | Reader | On `main` today |
|---|---|---|
| `README.md` | someone migrating builds | yes |
| `AGENTS.md` (`CLAUDE.md` only imports it) | the maintainer, and every agent | yes |
| `hack/README.md` | a developer setting up a cluster | yes |
| `docs/volume-migration.md` | someone whose Build hit `UndefinedVolume` | yes |
| `docs/support-matrix.md` | someone checking what a field becomes | yes (PR #65, merged 2026-09-03) |
| `docs/architecture.md` | the maintainer and agents, before changing behaviour | yes (PR #64, merged 2026-09-03) |
| `docs/examples/**` | someone who wants to see one conversion end to end | lands with PRs #66 to #68 |
| `docs/adr/**` | the maintainer, and agents, for the rules the code obeys | lands with PR #70 |

**Not documentation for this skill:** `designs/` (gitignored working notes), Go doc
comments (reviewed with the code), `tests/testdata/**` (fixtures), and `.claude/skills/**`
(agent instructions). The README's table of skills, once PR #34 lands, is documentation and
is in the map below.

**A doc in this table that is absent from `$WORK` has not landed yet.** Record it as
`NOT LANDED (PR #NN)` in the Docs record. Never create it, and never write the row it will
need into some other file.

**A tracked markdown file that is not in this table is `UNMAPPED`.** Stage 0c finds it; it
is grepped like the rest, and it earns a `MEMORY.md` entry so the table grows to meet it.

## The doc map

Each row names a change you can see in a diff, the doc that has to move with it, and the
test that goes red when it does not. Headings move as docs are rewritten (PR #69 rewrites
the README), so find a section by topic with `grep -n '^##' <doc>`; the headings quoted
here are the ones on `main` today, and **Audit mode** checks that they still exist. **When
no section covers the topic, that is an `incomplete` verdict for the doc, not a reason to
skip it.**

| Code surface (how it shows in the diff) | Doc, section | Guarded by |
|---|---|---|
| A `*Flag = "..."` constant in `buildconfig/plugin.go`, or a change to `ParseOptionalFields` / `PluginOptionalFields` | `README.md` › Plugin flags (the table, and Redirecting output images when it is a registry flag); `docs/support-matrix.md` › Plugin flags; `AGENTS.md` › ImageStream resolution when it is a mapping flag; every `docs/examples/*/optional-flags.json` that should show it | `TestReadmeOptionalFlagsAreValidJSON` for README examples only |
| A format string handed to `warnf`, `recordWarning`, `outcomeFailed`, `outcomeSkipped`, or `fmt.Errorf` in `buildconfig/*.go` is added, removed, or reworded | `docs/support-matrix.md` › Warning reference (the `W<n>` entry; verbs become `…`) and the Field by field row that cites it; on today's `main`, `README.md` › Known limitations when the warning marks a drop | `TestSupportMatrixCoversEveryWarning` |
| An `Outcome*` state or a `*Annotation` constant, or a change to what a passed-through BuildConfig carries (`passThroughWithDisposition`) | `docs/support-matrix.md` › How to read this page, What the plugin writes; `docs/architecture.md` › Outcomes, and where they are recorded; `README.md` › What it does, Conversion example (its output YAML); `AGENTS.md` › How it works (the per-resource list of what the plugin returns) | `TestSupportMatrixCoversEveryWarning` (constants clause) |
| A non-test Go file added, deleted, or renamed; a `process*` method on `Converter` added, removed, or renamed; a change to the call order inside `Convert` | `docs/architecture.md` › The conversion, step by step, The files; `AGENTS.md` › the file lists under "Files you may own fully" and "Files where the maintainer reads your diff line by line" (once PR #71 lands) | `TestArchitectureDocNamesEveryFileAndStage`, `TestArchitectureDocSymbolsExist` |
| A `Test*` function removed or renamed | `docs/architecture.md` › Rules that must stay true (each rule cites the test that keeps it) | `TestInvariantsCiteRealTests` |
| A new test guarding a doc (`*_doc_test.go`, `readme_test.go`, `support_matrix_test.go`, `adr_test.go`, `examples_test.go`) | `AGENTS.md` › When a documentation test fails (one row per test, once PR #71 lands) | none |
| Any change that moves what the plugin emits for an input (`processOutput`, `processSource`, `generateServiceAccount`, `toUnstructured`, `stripSerializationNoise`, an annotation, a name) | `docs/examples/<x>/expected/` regenerated **after approval** with `go test ./buildconfig -run TestExamplesMatchCommittedOutput -update`, then that example's `README.md` re-read and re-proposed; `README.md` › Conversion example (its output YAML) | `TestExamplesMatchCommittedOutput` |
| A rule the code must keep obeying was decided in the design doc or the PR (never overwrite X, always warn on Y, one path for Z) | new `docs/adr/NNNN-<slug>.md` with the same parts as its siblings, a row in `docs/adr/README.md`, and the rule in `docs/architecture.md` › Rules that must stay true | `TestADRsAreWellFormed` (shape only; nothing checks that a decision got a record) |
| The strategy switch in `converter.go` (`Docker`, `Source`, `Custom`, `JenkinsPipeline`), or the output gate | `README.md` › Strategy support and What it does; `AGENTS.md` › How it works; `docs/support-matrix.md` › What stops a BuildConfig from converting | none |
| `processStrategyVolumes`, `convertBuildVolumeSource`, the `UndefinedVolume` warning, the `:ro` mount text | `docs/volume-migration.md`; `docs/support-matrix.md` › the volumes rows | `TestSupportMatrixCoversEveryWarning` for the warning text |
| `go.mod` (`go` directive, `konveyor/crane-lib`, `shipwright-io/build`), the crane pin in `.github/workflows/test-e2e-minikube-pr.yml`, versions in `hack/setup-minikube-shipwright.sh` | `README.md` › Prerequisites and Building; `AGENTS.md` › Related repositories and Building (the crane-lib pseudo-version is quoted twice); `hack/README.md` › Prerequisites and Environment Variables | `TestReadmeVersionsMatchPins` |
| A `hack/*.sh` script, flag, environment variable, or context name added or changed | `hack/README.md` › Script Reference, Environment Variables, Kubectl Contexts; `AGENTS.md` › Development tools | CI runs the scripts, nothing checks the README |
| `tests/e2e-*.sh` or `tests/testdata/**` | `README.md` › Testing; `AGENTS.md` › Testing; `hack/README.md` › Testing the Plugin | none |
| `.claude/skills/**` | `README.md` › the skills table, once PR #34 lands | none |

Nothing in this map catches a clean conversion whose behaviour changed without touching a
warning, a constant, a file name, or an example. The grep in Stage 3, the one-hop callers in
Stage 1, and the per-file ledger in Stage 3c are what catch those.

---

## Stage 0: Setup

### 0a. Resolve the work directory, branch, and base

```bash
CP="<Crane Plugin Repo>"
BRANCH="<resolved as in the Arguments table>"
WORK="${WORK:-$(git -C "$CP" worktree list --porcelain \
  | awk -v b="refs/heads/$BRANCH" '/^worktree /{w=$2} $0=="branch "b{print w}')}"
WORK="${WORK:-$CP}"
git -C "$WORK" fetch origin --quiet
BASE="$(git -C "$WORK" merge-base "${BASE_REF:-origin/main}" HEAD)"
SCRATCH="$(mktemp -d)/tech-document-$(printf '%s' "$BRANCH" | tr '/' '-')"
mkdir -p "$SCRATCH"
```

If `$BRANCH` cannot be found, re-run the lookup without stderr suppression and report
which failure it was. Two branches for one key: stop and ask. A detached worktree (what
`/tech-review` builds, and what `git worktree add --detach` leaves) has no branch name:
use `HEAD` as `$BRANCH` and the short SHA in the scratch path.

### 0b. Compute the diff

```bash
case "$MODE" in
  staged)  RANGE=(--cached) ;;
  *)       RANGE=("$BASE...HEAD") ;;
esac
diff_of() {   # the branch diff for the given paths, in the mode chosen above
  git -C "$WORK" diff --no-ext-diff "${RANGE[@]}" -- "$@"
  [ "$MODE" != staged ] && git -C "$WORK" diff --no-ext-diff HEAD -- "$@"   # uncommitted work counts mid-change
  return 0
}
diff_of .                 > "$SCRATCH/diff.patch"
diff_of . ':!*_test.go'   > "$SCRATCH/code.patch"
{ git -C "$WORK" diff --no-ext-diff --name-status "${RANGE[@]}"
  [ "$MODE" != staged ] && git -C "$WORK" diff --no-ext-diff --name-status HEAD; } > "$SCRATCH/status.txt"
grep -E '^\+\+\+ b/' "$SCRATCH/diff.patch" | sed 's#^+++ b/##' | sort -u > "$SCRATCH/changed.txt"
```

The three-dot form diffs from the merge base, so a branch that is behind `main` does not
report `main`'s own content as a removal. `--base HEAD` makes that range empty by
construction and reviews only the uncommitted work, which is the "what did I just change"
question a user asks mid-edit. `code.patch` leaves the test files out: a `warnf` inside a
test is an assertion, not a warning the plugin emits, and Stage 1 reads the two patches
for different things. `diff_of` is reused by the ledger in Stage 3c.

### 0c. What is present

```bash
cd "$WORK"
for d in README.md AGENTS.md hack/README.md docs/volume-migration.md docs/support-matrix.md \
         docs/architecture.md docs/examples/README.md docs/adr/README.md; do
  [ -e "$d" ] && echo "PRESENT $d" || echo "NOT-LANDED $d"
done > "$SCRATCH/docs.txt"
# Every other tracked markdown file is a doc nobody mapped yet.
git ls-files '*.md' \
  | grep -vE '^(designs/|\.claude/|CLAUDE\.md$|README\.md$|AGENTS\.md$|hack/README\.md$|docs/volume-migration\.md$|docs/support-matrix\.md$|docs/architecture\.md$|docs/examples/.*\.md$|docs/adr/.*\.md$)' \
  | sed 's/^/UNMAPPED /' >> "$SCRATCH/docs.txt"
cat "$SCRATCH/docs.txt"
grep -lE '^func Test(SupportMatrix|ArchitectureDoc|Invariants|Examples|Readme|ADRs|NoDirectWarn|DirectWarn)' \
  buildconfig/*_test.go 2>&1 | tee "$SCRATCH/keeper-tests.txt"
git status --porcelain -- '*.md' > "$SCRATCH/md-before.txt"   # Stage 7 compares against this
```

A missing keeper test is not an error. It means the base predates the docs PRs, and Stage 2
is `SKIPPED (none on this base)`. An `UNMAPPED` doc joins the grep in Stage 3a and gets a
`MEMORY.md` entry keyed `unmapped-doc:<path>` in Stage 7b.

### 0d. Docs-only guard

If every path in `changed.txt` is in the doc set, the branch is a docs change reviewing
itself: run Stage 2, the link and fan-in checks from Stage 6, print the record, and stop.
There is no code surface to map.

### 0e. Read the memory

Read `MEMORY.md` next to this file. A **standing directive** there, written by a human,
applies to this run. Run entries are context: note any key that already has two entries,
because a third occurrence in this run is a promotion (Stage 7b).

---

## Stage 1: Surface checklist

One script, one call. It reads the diff and prints one line per surface the map knows
about, the one-hop callers of every changed function, and the identifier list the grep in
Stage 3 uses. Write it to `$SCRATCH/surfaces.txt`; every later stage cites this file.

```bash
C="$SCRATCH/code.patch"    # non-test Go and everything else
P="$SCRATCH/diff.patch"    # including tests
L="$SCRATCH/codelines.txt" # changed non-test lines, comments left out
grep -E '^[+-][^+-]' "$C" | grep -vE '^[+-][[:space:]]*//' > "$L"

# One union of every class below, for the per-file ledger in Stage 3c. Keep it in step.
SURFACE='Flag[[:space:]]*=[[:space:]]*"|ParseOptionalFields|PluginOptionalFields|warnf\(|recordWarning\(|outcomeFailed\(|outcomeSkipped\(|fmt\.Errorf\(|Log\.(Warn|Warnf|Error|Errorf)\(|OutcomeState[[:space:]]*=|Annotation[[:space:]]*=|passThroughWithDisposition|func \(c \*Converter\) process|BuildStrategyType|processOutput|processSource|generateServiceAccount|toUnstructured|stripSerializationNoise|uniqueName|sanitizeDNS1123Label|processStrategyVolumes|convertBuildVolumeSource|UndefinedVolume'

# Callers one hop out. A changed function moves behaviour that is documented under the
# functions that call it, so their names join the identifier list.
grep -E '^[+-]func ' "$L" | sed -E 's/^[+-]func (\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*)\(.*/\2/' | sort -u > "$SCRATCH/changed-funcs.txt"
git -C "$WORK" ls-files 'buildconfig/*.go' main.go | grep -v '_test\.go$' | grep -vxF -f "$SCRATCH/changed.txt" > "$SCRATCH/other-files.txt"
while read -r fn; do
  while read -r f; do
    awk -v fn="$fn" '/^func /{cur=$0} !/^func / && cur && index($0, fn "(") {print cur}' "$WORK/$f" \
      | sed -E 's/^func (\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*)\(.*/\2/' \
      | sed "s|^|CALLER  $fn <- $f |"
  done < "$SCRATCH/other-files.txt"
done < "$SCRATCH/changed-funcs.txt" | sort -u > "$SCRATCH/callers.txt"

{
  echo "## surfaces"
  grep -E '(Flag[[:space:]]*=[[:space:]]*"|ParseOptionalFields|PluginOptionalFields)' "$L" | sed 's/^/FLAG    /'
  grep -E '(warnf\(|recordWarning\(|outcomeFailed\(|outcomeSkipped\(|fmt\.Errorf\(|Log\.(Warn|Warnf|Error|Errorf)\()' "$L" | sed 's/^/WARNING /'
  grep -E '(Outcome[A-Z][A-Za-z]*[[:space:]]+OutcomeState[[:space:]]*=|[A-Za-z]+Annotation[[:space:]]*=|passThroughWithDisposition)' "$L" | sed 's/^/CONST   /'
  grep -E '^[AD][[:space:]]+(buildconfig/.*\.go|main\.go)$' "$SCRATCH/status.txt" | grep -vE '_test\.go$' | sed 's/^/FILE    /'
  grep -E '^R[0-9]*[[:space:]]+.*\.go' "$SCRATCH/status.txt" | sed 's/^/FILE    /'
  grep -E '^[+-]func \(c \*Converter\) process[A-Za-z]+' "$L" | sed 's/^/STAGE   /'
  grep -E '^-func Test[A-Za-z0-9_]+' "$P" | sed 's/^/TEST    /'
  grep -E '^\+func Test[A-Za-z0-9_]+' "$P" | grep -E 'Readme|SupportMatrix|ArchitectureDoc|Invariants|Examples|ADRs' | sed 's/^/DOCTEST /'
  grep -E '(DockerBuildStrategyType|SourceBuildStrategyType|CustomBuildStrategyType|JenkinsPipelineBuildStrategyType)' "$L" | sed 's/^/STRATEGY/'
  grep -E '(processOutput|processSource|generateServiceAccount|toUnstructured|stripSerializationNoise|uniqueName|sanitizeDNS1123Label)' "$L" | sed 's/^/OUTPUT  /'
  grep -E '(processStrategyVolumes|convertBuildVolumeSource|UndefinedVolume)' "$L" | sed 's/^/VOLUME  /'
  grep -E '^(\+\+\+ b/|--- a/)(go\.mod|\.github/workflows/.*|hack/.*\.sh|tests/.*\.sh|tests/testdata/.*|\.claude/skills/.*)$' "$P" | sed 's/^/PATH    /'
  cat "$SCRATCH/callers.txt"
  echo "## identifiers"
  # Tokens worth grepping the docs for: annotation keys and flag names (a literal with a
  # dash or a slash), constants with a telling suffix, pipeline steps, the callers found
  # above, the names of tests this diff removed, and one phrase from every warning this
  # diff added, removed, or reworded: the longest stretch of its format string with no
  # verb in it, 20 characters or more, which is text a support-matrix row quotes verbatim.
  # Bare words like "metadata" would match every doc and are left out on purpose. -w
  # bounds the match on GNU, BSD, and ugrep alike.
  { grep -owE '"[a-z][a-z0-9]*[-/][a-z0-9/-]+"|[A-Z][A-Za-z0-9]{3,}(Flag|Annotation|Warning|Template)|process[A-Z][A-Za-z]+' "$L" | tr -d '"'
    awk '{print $NF}' "$SCRATCH/callers.txt"
    grep -E '^-func Test' "$P" | grep -owE 'Test[A-Z][A-Za-z0-9_]+'
    grep -oE '(warnf|recordWarning|outcomeFailed|outcomeSkipped|Warnf?|Errorf?)\("([^"\\]|\\.)*"' "$L" \
      | sed -E 's/^[^"]*"//; s/"[^"]*$//' \
      | awk '{ n = split($0, seg, /%[-+ #0-9.]*[a-zA-Z]/); best = ""
               for (i = 1; i <= n; i++) { s = seg[i]
                 gsub(/^[[:space:]:;,.—-]+|[[:space:]:;,.—-]+$/, "", s)
                 if (length(s) > length(best)) best = s }
               if (length(best) >= 20) print best }'; } | sort -u
} | tee "$SCRATCH/surfaces.txt"
```

A `-` line on `Log.Warnf` next to a `+` line on `warnf` with the same text is a re-route
through the single recording path (ADR-0003), not a new warning: the drop it describes
predates the branch, and only the doc that explains *where* warnings land moves.

Read the surface lines against the diff hunks they came from. A `+` and a `-` line with the
same template is a move, not a change; a `-` with no matching `+` is a removal and its old
text is an identifier to grep for. For a reworded warning, normalise both versions by
replacing every `%`-verb with `…` — that is the form the support matrix quotes and the
keeper test compares. Add to the identifier list by hand any name the script could not
know: the old spelling of a renamed flag, or a phrase a warning used to contain.

---

## Stage 2: Keeper tests

The documentation tests are the oracle for the rows they guard. Run them before reading
any doc, so the proposals start from what CI would say:

```bash
KEEPERS='TestSupportMatrix|TestArchitectureDoc|TestInvariants|TestExamples|TestReadme|TestADRs|TestNoDirectWarn|TestDirectWarn'
cd "$WORK" && GOWORK=off go test ./buildconfig -run "$KEEPERS" -count=1 2>&1 \
  | grep -E 'FAIL|^ok|^---|\.go:[0-9]+:|^[[:space:]]{8,}|^#' | tee "$SCRATCH/keeper.txt"
```

The filter keeps the verdict lines, the failure lines (`file_test.go:47: ...` and their
indented continuations), compile errors, and `ok ... [no tests to run]`; it drops the
`=== RUN` and `--- PASS` noise that made this stage the most expensive read in the run.

Each failure names a doc and, usually, the row: "warning has no row in
docs/support-matrix.md", "does not name main.go", "quotes W12, which no longer matches".
Carry every one into Stage 4 as a proposal with verdict `keeper-test`. A keeper test whose
failure output, read as a whole, does **not** name the row is a `MEMORY.md` entry keyed
`keeper-silent:<test>`; a test that prints the unrowed text on one line and the row id on
the next has named it.

`no tests to run` with an empty `keeper-tests.txt` means the base predates the docs PRs.
Record `SKIPPED (none on this base)` and continue: the map still applies, CI just will not
enforce it yet.

`GOWORK=off` is what CI runs. The workspace `go.work` outside this repo resolves
dependencies differently and can hide or invent a failure.

---

## Stage 3: Candidates and verdicts

### 3a. Collect candidates

Three sources, unioned:

1. **Map hits.** Every doc named in the map row for each surface in `surfaces.txt`.
2. **Grep hits.** Every identifier from `surfaces.txt`, against every present doc and every
   `UNMAPPED` one:

   ```bash
   cd "$WORK"
   { grep -E '^(PRESENT|UNMAPPED)' "$SCRATCH/docs.txt" | awk '{print $2}'
     [ -d docs/examples ] && ls docs/examples/*/README.md
     [ -d docs/adr ]      && ls docs/adr/[0-9]*.md; } | sort -u > "$SCRATCH/doclist.txt"
   sed -n '/^## identifiers/,$p' "$SCRATCH/surfaces.txt" | tail -n +2 | while IFS= read -r id; do
     if hits="$(xargs grep -n -w -F -- "$id" < "$SCRATCH/doclist.txt")"; then
       printf 'MATCH %s\n%s\n' "$id" "$hits"
     else
       echo "NO_MATCH $id"
     fi
   done | tee "$SCRATCH/grep.txt"
   ```

   The file list goes through `xargs` rather than an unquoted variable: zsh does not
   word-split a parameter, so a space-joined list arrives at grep as one multi-line name
   and every identifier reports a false match (seen in testing). `-F` takes the identifier
   literally (warning phrases carry parentheses and dots), `-w` bounds it, and the exit
   status, not the captured text, decides between `MATCH` and `NO_MATCH`.

   Removed or renamed identifiers are grepped under their **old** name too, because prose
   quotes the old name without any syntax around it.
3. **Keeper failures** from Stage 2.

A doc the branch already edited is still a candidate, marked `touched-in-diff`. A partial
update is the common case; being in the diff proves someone looked, not that they finished.

### 3b. Verdict per candidate

Two passes, and every candidate gets exactly one verdict.

**Quick pass.** For each candidate, read the matched lines in context (`grep -n -B2 -A2`)
or, for a map hit with no grep match, that section's headings. Decide whether it describes
behaviour this diff changed.

**Deep pass.** For each candidate still standing, read the whole section next to the diff
hunk that touches it, and check the base version to date the staleness:

```bash
git -C "$WORK" show "$BASE:<doc>" | grep -n -E '<the sentence>'
```

Verdicts:

| Verdict | Meaning |
|---|---|
| `incorrect` | The doc now says something false because of this diff. |
| `incomplete` | This diff adds behaviour, a flag, a warning, or a file that no sentence covers, and a section exists to hold the sentence. |
| `incomplete (needs-new-section)` | The same, and no section in the doc is a plausible parent. The proposal names the heading and its place, not the prose. |
| `keeper-test` | A Stage 2 failure names it. |
| `pre-existing` | The doc is wrong, but it was wrong on `$BASE` too. Reported apart, never merged into an in-diff proposal. |
| `unaffected` | It mentions the identifier in passing, or describes behaviour the diff left alone. Say why in six words or fewer. |
| `not-landed` | In the doc set, absent from `$WORK`. Name the PR. |

When a candidate qualifies for more than one verdict, `keeper-test` wins over `incorrect`
and `incomplete`: the test already names the row and its message is the evidence. Write
the table to `$SCRATCH/verdicts.txt`.

### 3c. The per-file ledger

The verdict table is per doc. This one is per changed code file, so a file the map and
the grep both missed cannot vanish:

```bash
grep -vE '(_test\.go|\.md)$' "$SCRATCH/changed.txt" | while read -r f; do
  n=$(diff_of "$f" | grep -E '^[+-][^+-]' | grep -vE '^[+-][[:space:]]*//' | grep -cE "$SURFACE")
  printf '%s | surfaces %s | docs: \n' "$f" "$n"
done > "$SCRATCH/ledger.txt"
```

Fill the `docs:` column for every line: the docs that got a verdict because of this file,
or `nothing (no surface, no identifier match)`. A file with surfaces above zero and
`nothing` in its column needs a sentence saying why. The ledger is part of the evidence for
`NONE AFFECTED`: if nothing is `incorrect`, `incomplete`, or `keeper-test`, that is the
result, and `surfaces.txt`, `grep.txt`, `verdicts.txt`, and `ledger.txt` are what support
it. Print all four summaries in the record.

---

## Stage 4: Proposals

One block per edit. This is the contract; every field is filled in or the block is not
ready to show:

```text
P<n>  <doc> › <section heading>
Verdict:   incorrect | incomplete | incomplete (needs-new-section) | keeper-test
Because:   <file:line in the diff>: <one sentence on what the code now does>
Now says:  "<the current sentence or row, quoted>"          (omit for incomplete)
Change:    <the replacement or the new lines, verbatim, ready to paste>
Guard:     <keeper test name, or "none">
Evidence:  9 | 7 | 5 | 3   (ladder below)
```

Scope each `Change` to the delta. When the surrounding section is also wrong for reasons
older than this branch, that is a second block with verdict `pre-existing`, shown in its
own list after the in-diff ones. The user decides whether this story carries it.

A `needs-new-section` block's `Change` is the heading, the heading it goes after, and one
line on what the section must say. Writing the section is a different job with an audience
call this map cannot make; it belongs to the user or to a later pass, not to this run.

**The evidence ladder.** `Evidence` is not a feeling:

| Score | You have |
|---|---|
| 9 | read the diff hunk and the doc section, and can quote both |
| 7 | a keeper test that names the row (mechanical, but you did not read the section) |
| 5 | a grep match you did not open |
| 3 | a map inference only: no grep hit, section not read |

**The writing checklist.** Every `Change` passes this before the batch goes through
`/unslop`. `/unslop` removes AI tells; this removes ambiguity and the wrong voice:

- The actor is named: "the plugin drops the trigger", not "the trigger is dropped", unless
  the table column already fixes the actor.
- Condition before instruction: "When the strategy is `Custom`, the BuildConfig passes
  through", not the other way round.
- One name per thing, and it is the code's identifier, backticked. A warning is quoted
  with `…` for its verbs, the way the matrix quotes it.
- Prose sentences under 25 words. Table cells stay terse; a cell is not a paragraph.
- The wording sits at the doc's Reader (doc set table). A README sentence never says
  "the converter" or names a Go function; an `AGENTS.md` sentence never explains what a
  BuildConfig is; a support-matrix cell says what the user does next.

Three edits that always travel together:

- **A new warning** is a `W<n>` entry in the Warning reference **and** a citation from the
  Field by field row it belongs to. Two blocks, or one block that names both places.
- **A new ADR** is the record file, its row in `docs/adr/README.md`, and its rule in the
  architecture page's rules table. One block naming all three files.
- **A moved example** is the regenerated `expected/` directory **and** that example's
  README re-read afterwards. The regeneration is one block; the README follow-up is
  proposed in a second round once the user has approved the first.

### `--report` mode ends here

Write `$SCRATCH/tech-document.json` in the shape `/tech-review`'s `findings-schema.md` defines
(`.claude/skills/tech-review/findings-schema.md` in this repo). The skeleton:

```json
{
  "source": "tech-document",
  "status": "ok",
  "reason": "",
  "findings": [
    {
      "file": "docs/support-matrix.md",
      "line": 312,
      "severity": "blocker",
      "scope": "in-diff",
      "title": "P1 docs/support-matrix.md › Warning reference (W44)",
      "detail": "Because: ... Change: ...",
      "confidence": 9
    }
  ]
}
```

`status` is `ok` when the run completed, even with zero findings; `failed` with a `reason`
when a stage could not run. Severity and scope come from the verdict, `confidence` is the
`Evidence` score:

| Verdict | `severity` | `scope` |
|---|---|---|
| `keeper-test` | `blocker` (CI is red) | `in-diff` |
| `incorrect` | `warning` | `in-diff` |
| `incomplete`, either kind | `warning` | `in-diff` |
| `pre-existing` | `warning` | `pre-existing` |

`title` is the `P<n>` line, `detail` is the `Because` and `Change` fields, `file` and
`line` point at the doc. Return one line, the count and the path
(`tech-document: 2 findings → $SCRATCH/tech-document.json`), and stop. The caller reads the file;
`$SCRATCH` is not removed in this mode. Nothing is edited. Stage 7b still runs: memory is
appended in every mode.

---

## Stage 5: Discuss

Apply mode only. Show the proposals, unslopped, then ask. Use `AskUserQuestion` with
`multiSelect: true`, at most four proposals per question and four questions per call. Each
option's label is `P<n> <doc> › <section>`; its description is the `Because` line. The
built-in "Other" is where the user rewrites a `Change`. Pre-existing blocks go in a separate
question whose first option is "Leave them for their own story".

With `--mechanical`, a `keeper-test` block whose `Change` is exactly the quoted old
template replaced by the new normalised template, and nothing else, is applied without
asking and listed as `applied (mechanical)`. A block that also touches the row's other
columns, or any block of another verdict, still asks.

When there is no user to ask (a sub-agent context, or `AskUserQuestion` is unavailable),
do not guess an approval. Record `5 Discuss: BLOCKED (no user)` and finish as `--report`.

---

## Stage 6: Apply

For each approved block, make the edit in `$WORK` and append its path to
`$SCRATCH/edited.txt`. Then:

1. Re-run the tests, scoped to what changed. Prose does not need the whole suite:

   ```bash
   cd "$WORK"
   if grep -qE '\.go$|docs/examples/.*/expected/' "$SCRATCH/edited.txt"; then
     GOWORK=off go test ./... -count=1 2>&1
   else
     GOWORK=off go test ./buildconfig -run "$KEEPERS" -count=1 2>&1
   fi | grep -E 'FAIL|^ok|^---|\.go:[0-9]+:|^[[:space:]]{8,}|^#' | tee "$SCRATCH/verify.txt"
   ```

   A keeper test that is still red names a row you missed: fix the doc, not the test. Any
   other failure means an edit broke something it should not have been able to touch;
   revert that one file (`git -C "$WORK" checkout -- <doc>`) and say so.

2. Check every relative link in every edited doc, and that every new doc is reachable:

   ```bash
   cd "$WORK"
   while read -r f; do
     grep -oE '\]\([^)#[:space:]]+\)' "$f" | tr -d '()]' | grep -vE '^https?:' \
       | while read -r l; do [ -e "$(dirname "$f")/$l" ] || echo "BROKEN $f -> $l"; done
   done < "$SCRATCH/edited.txt"
   # Fan-in: a new file under docs/ must be linked from its directory's index or the README.
   { grep -E '^A[[:space:]]+docs/.*\.md$' "$SCRATCH/status.txt" | awk '{print $2}'
     git status --porcelain | grep -E '^\?\? docs/.*\.md$' | awk '{print $2}'; } | sort -u \
   | while read -r f; do
       b="$(basename "$f")"; d="$(dirname "$f")"
       [ "$b" = README.md ] && b="$(basename "$d")"   # a new example is linked by its directory name
       grep -q -F -- "$b" "$d/README.md" "$d/../README.md" README.md 2>/dev/null \
         && echo "LINKED   $f" || echo "UNLINKED $f"
     done
   ```

   `2>/dev/null` there only hides an index file that does not exist; the three candidates
   are deliberate. An `UNLINKED` doc is a proposal for the index, not a pass. A link to a
   doc that has not landed is allowed only where PR #71 already does it: with a sentence
   saying which PR brings the file.

3. Leave the edits unstaged and uncommitted. `/tech-implement` commits them with the code;
   `/create-pr` stages them; a user running this by hand does what they like.

Remove `$SCRATCH` when the run ends, including on early exit. `$WORK` is the caller's and
stays.

---

## Stage 7: Record

### 7a. Self-audit, then the record

Before printing, check the record against the tree rather than against memory:

```bash
git -C "$WORK" status --porcelain -- '*.md' > "$SCRATCH/md-after.txt"
diff "$SCRATCH/md-before.txt" "$SCRATCH/md-after.txt" | grep -E '^[<>]' | sed 's/^> /ADDED   /; s/^< /GONE    /'
```

`ADDED` lines must equal the `Updated` list, file for file. A `GONE` line means an edit
undid a change that was already in the tree; that is a `REVERTED` you did not intend.
Either mismatch caps the run at **DONE_WITH_CONCERNS** and is named in the record.

Print this, unslopped. Callers paste it into their own records, so it has to stand on its
own:

```text
DOCS RECORD: <branch> @ <HEAD short SHA>
Updated:       README.md › Plugin flags (P1); docs/support-matrix.md › W63 + volumes row (P2, mechanical)
Unaffected:    hack/README.md (no surface hit, grep 0/14); docs/volume-migration.md (mentions volumes in passing)
Not landed:    docs/architecture.md (PR #64); docs/adr/ (PR #70)
Unmapped:      docs/notes/proxy.md (grepped: 0 hits; memory entry added)
Pre-existing:  README.md › Strategy support says "Error", code skips; declined, left for its own story
Ledger:        6 code files, 6 accounted (2 with docs, 4 nothing)
Keeper tests:  pass | SKIPPED (none on this base) | <failing test and row>
Self-audit:    tree matches the Updated list | MISMATCH: <detail>
Handed back:   2 files modified, unstaged, in <WORK>
```

`Unaffected` carries the reason per doc. `NONE AFFECTED` as the whole result is the same
line for every present doc, plus the counts from `surfaces.txt`, `grep.txt`, and
`ledger.txt`.

### 7b. Memory

Append to `MEMORY.md` next to this file when the run turned up something the map or the
doc set did not know. Routine runs are not logged. Keys:

| Key | When |
|---|---|
| `unmapped-doc:<path>` | Stage 0c found a tracked markdown file outside the doc set |
| `no-map-row:<class>:<name>` | a surface, caller, or identifier implicated a doc through grep alone, with no map row to explain why |
| `heading-drift:<doc>:<heading>` | a heading the map quotes no longer exists (apply mode finds it while reading; audit mode finds all of them) |
| `keeper-silent:<test>` | a keeper test failed without naming the row |
| `uncovered:<class>:<name>` | audit mode found a flag, script, step, or file no doc names |

Entry format (append only, never rewrite):

```markdown
## Run: <branch> (<date>), <apply | report | audit>
- `<key>` — <one line: what was seen, and the doc or map row it points at>
- Type: NOTED | PROMOTION_PROPOSED | PROMOTED
```

**Promotion.** When a key now has three entries, the run proposes the promotion, and the
routing order is the one `reflect` and `encode-lessons-in-structure` use: a keeper test
first when a test could enforce it (a flag with no README row, a script with no
`hack/README.md` entry, a file no doc names); a row in the doc map or the doc set table
otherwise. In apply mode ask via `AskUserQuestion` with the ready-to-paste row or the test
sketch as the option text; in report and audit mode put it in NOTES. Record
`PROMOTION_PROPOSED`, and `PROMOTED` once the edit to this file lands.

## Compliance table (every run, including early exit)

Every row that is not DONE ends with the command or edit that clears it, after the status.
`BLOCKED` alone leaves the caller to reread the prose; `BLOCKED, next: run /setup-repos`
does not.

```text
| Stage | Status | Evidence |
|-------|--------|----------|
| 0 Setup         | DONE / BLOCKED, next: <action> | work dir, base SHA, files changed, docs present, unmapped n, keeper tests present, memory keys at two |
| 1 Surfaces      | DONE | n surface lines, n callers, n identifiers |
| 2 Keeper tests  | DONE / SKIPPED (none on this base) | pass, or the failing names |
| 3 Verdicts      | DONE | n candidates, every one with a verdict; ledger n files, n accounted |
| 4 Proposals     | DONE / NONE AFFECTED | n in-diff, n pre-existing, n needs-new-section |
| 5 Discuss       | DONE / SKIPPED (--report) / BLOCKED (no user), next: rerun with --report | approved n, declined n, mechanical n |
| 6 Apply         | DONE / SKIPPED / REVERTED <file>, why: <reason> | tests scoped to <suite|keepers>, links checked, fan-in checked |
| 7 Record        | DONE / MISMATCH, file: <file> | self-audit result |
| 7b Memory       | DONE / NOT NEEDED | keys appended, promotions proposed |
Overall: COMPLETE / INCOMPLETE
```

End with one of **DONE** (record printed, edits handed back or none needed),
**DONE_WITH_CONCERNS** (a proposal was declined, a not-landed doc will need a row, the
self-audit mismatched, or a keeper test is red for a reason outside this branch),
**BLOCKED** (no user in apply mode, no `repo.md`, no branch), or **NEEDS_CONTEXT** (the
diff changes behaviour the map, the callers, and the grep cannot place; name the hunk).

---

## Audit mode (`--audit`)

No diff. The question is whether this file's map and doc set still describe the repo. Run
it on demand, and after the docs PRs merge; never from the hooks, because it rereads the
whole package and the per-branch budget does not cover that.

**A1. Inventory from source, not from a diff.**

```bash
cd "$WORK"
{ grep -hoE 'Flag[[:space:]]*=[[:space:]]*"[^"]+"' buildconfig/plugin.go | grep -oE '"[^"]+"' | tr -d '"' | sed 's/^/FLAG    /'
  grep -hoE 'func \(c \*Converter\) process[A-Za-z]+' buildconfig/*.go | awk '{print $NF}' | sed 's/^/STAGE   /'
  git ls-files 'buildconfig/*.go' main.go | grep -v '_test\.go$' | sed 's/^/FILE    /'
  git ls-files 'hack/*.sh' 'tests/*.sh' | sed 's/^/SCRIPT  /'
  grep -hoE '(Outcome[A-Z][A-Za-z]*[[:space:]]+OutcomeState|[A-Za-z]+Annotation)[[:space:]]*=[[:space:]]*"[^"]+"' buildconfig/*.go | grep -oE '"[^"]+"' | tr -d '"' | sed 's/^/CONST   /'
} | tee "$SCRATCH/inventory.txt"
```

Warnings are not inventoried here: `TestSupportMatrixCoversEveryWarning` does that job
better than a grep, and A4 runs it.

**A2. Coverage: every inventory item appears in the doc the map names for it.**

```bash
while read -r cls name; do
  case "$cls" in
    FLAG)   docs="README.md docs/support-matrix.md" ;;
    STAGE)  docs="docs/architecture.md" ;;
    FILE)   docs="docs/architecture.md" ;;
    SCRIPT) docs="hack/README.md" ;;
    CONST)  docs="docs/support-matrix.md" ;;
  esac
  needle="$name"; case "$cls" in FILE|SCRIPT) needle="$(basename "$name")" ;; esac
  echo "$docs" | tr ' ' '\n' | while read -r d; do
    [ -e "$d" ] || { echo "NOT-LANDED $cls $name -> $d"; continue; }
    grep -q -w -F -- "$needle" "$d" && echo "COVERED   $cls $name -> $d" || echo "UNCOVERED $cls $name -> $d"
  done
done < "$SCRATCH/inventory.txt" | tee "$SCRATCH/coverage.txt"
```

The doc list is piped through `tr` and `read`, not expanded from a variable, for the same
zsh reason as Stage 3a.

**A3. Headings the map quotes still exist.**

```bash
SKILL="$(git -C "$WORK" rev-parse --show-toplevel)/.claude/skills/tech-document/SKILL.md"
grep -oE '`[A-Za-z/._-]+\.md` › [A-Z][A-Za-z ,]+' "$SKILL" | sed -E 's/`//g; s/ › /|/; s/ and /,/g' | sort -u \
| while IFS='|' read -r doc headings; do
    [ -e "$doc" ] || { echo "NOT-LANDED $doc"; continue; }
    echo "$headings" | tr ',' '\n' | sed -E 's/^ +//; s/ +$//; s/ (when|once|if|for|where) .*$//' | grep -E '^[A-Z]' | while read -r h; do
      if grep -qE "^#{1,3} .*$h" "$doc"; then echo "OK      $doc › $h"
      elif grep -F -- "› $h" "$SKILL" | grep -qE 'PR #[0-9]+'; then echo "WAITING $doc › $h (the map row names the PR that brings it)"
      else echo "DRIFT   $doc › $h"; fi
    done
  done | tee "$SCRATCH/headings.txt"
```

The map's cells are written for this script as much as for a reader: a heading is quoted
exactly, a qualifier follows it after "when", "once", or in parentheses, and several
headings in one cell are separated by commas, never semicolons. A heading whose map row
names a PR is `WAITING` until that PR merges, and is reported under "Not landed". Any
`DRIFT` is a map row to fix, or a doc that moved a heading.

**A4. Keeper tests** as in Stage 2, on the clean tree. A failure here is pre-existing by
definition and is reported as such.

**A5. Report.** No doc is edited. Print, unslopped:

```text
AUDIT: <branch> @ <HEAD short SHA>
Unmapped docs:   <list, or none>
Uncovered:       FLAG search-registries -> docs/support-matrix.md; SCRIPT e2e-cluster.sh -> hack/README.md
Heading drift:   README.md › Known limitations (rewritten by PR #69)
Not landed:      <docs absent from the tree, and WAITING headings, with their PRs>
Keeper tests:    pass | <failures>
Map rows to add: <ready-to-paste rows, one per UNCOVERED class with no row>
Memory:          <keys appended; promotions proposed at three>
```

Then Stage 7b, with `uncovered:` and `heading-drift:` keys, and this compliance table:

```text
| Stage | Status | Evidence |
|-------|--------|----------|
| A1 Inventory   | DONE | n flags, n steps, n files, n scripts, n constants |
| A2 Coverage    | DONE | n covered, n uncovered, n not landed |
| A3 Headings    | DONE | n ok, n drift, n waiting |
| A4 Keepers     | DONE / SKIPPED (none on this base) | |
| A5 Report      | DONE | |
| 7b Memory      | DONE / NOT NEEDED | |
```

## Error handling

| Scenario | Behaviour |
|---|---|
| `repo.md` missing | Invoke `/setup-repos`, stop |
| Branch not found | Re-run the lookup without stderr suppression, report which failure it was |
| Two branches match the key | Stop; one story maps to one branch |
| A doc in the set is absent | `NOT LANDED (PR #NN)`; never create it |
| A tracked `*.md` outside the set | `UNMAPPED`: grep it, record it, memory key `unmapped-doc` |
| No keeper test exists | Stage 2 `SKIPPED (none on this base)`, map still applies |
| A keeper test fails on `$BASE` too | Report it as `pre-existing`; it is not this branch's row |
| A keeper test fails without naming the row | Carry it anyway; memory key `keeper-silent` |
| `go test` cannot build | Stop with `BLOCKED, next: fix the build`; docs cannot be checked against code that does not compile |
| No user in apply mode | Finish as `--report`, `5 Discuss: BLOCKED (no user), next: rerun with --report` |
| An approved edit breaks the suite | Revert that file, report `REVERTED <file>`, keep the rest |
| The self-audit mismatches the record | `DONE_WITH_CONCERNS`; name the file; never edit the record to match |
| A new doc is `UNLINKED` | Propose the index row; do not pass the fan-in check on a promise |
| The branch also changed the Strategy Catalog Repo | Name it in the record; do not edit its docs |
| `--audit` on a tree with uncommitted changes | Run anyway; say so in the report, since the inventory reads the tree as it is |

## Common mistakes

- **Exploring instead of scanning.** Without the checklist an agent reads every doc top to
  bottom and spends thirty tool calls to find four sentences. Stage 1 is one call; Stage 3
  reads only what it points at.
- **Fixing a pre-existing error inside the story.** The baseline run relabelled a whole
  table column that was wrong before the branch existed and reported it as part of the
  change. Correct, and invisible to the reviewer. It is its own list.
- **Treating a reworded warning as prose.** It moves a `W<n>` row and turns CI red; it has
  since PR #65 merged. The map row exists so this is never a surprise.
- **Documenting the feature instead of the delta.** If the branch adds one annotation to
  a mechanism that has three, propose the one line, and put the missing three in the
  pre-existing list.
- **Skipping `AGENTS.md` because it is "for agents".** It is the maintainer's contract with
  every agent and it goes stale first: its "How it works" list was wrong for two months
  before PR #71 fixed it.
- **Writing the row a not-landed doc will need into a file that exists.** It will be wrong
  in both places. Record `NOT LANDED` and move on.
- **Regenerating examples to see what changed.** `-update` rewrites the assertion. Propose,
  get approval, then regenerate.
- **Announcing `NONE AFFECTED` from the diff alone.** A clean conversion that changed
  behaviour hits no map row; only the grep, the callers, and the ledger can clear it.
- **Writing the record from memory.** The self-audit in 7a exists because a record that
  says "Updated: two files" over a tree with three is the easiest lie to tell.
- **Drafting the new section.** A `needs-new-section` block names the heading and stops.
  The prose needs an audience decision the map does not have.

## Notes

Anything surprising that has no memory key above goes in a NOTES section of the terminal
output: a surface class the map has no row for, a doc that describes behaviour no test
exercises. That is the signal for the next key, and the next edit to this file.
