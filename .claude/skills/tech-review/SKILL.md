---
name: tech-review
description: Pre-PR review gate for a migration branch. Runs a simplify pass, then parallel reviewers (coderabbit and qodo CLIs, an Opus code review, an Opus docs pass), an adversarial challenger over blockers, and five cross-repo consistency checks against this repo and strategy-catalog. Every sub-agent names its model, capped at Opus. Report-only by default. Trigger on "tech-review", "review BUILD-XXXX", "review this branch", or "pre-PR review". Reviews a local branch before a PR is merged; use /deep-review to review an open PR.
argument-hint: <PR-URL | BUILD-XXXX | branch-name | blank> [--fix] [--cli=<name|none>]
allowed-tools: [Bash, Read, Grep, Glob, Edit, Agent, AskUserQuestion]
user_invocable: true
---

# /tech-review — Pre-PR Review Gate

You are a senior reviewer running the last gate before a branch becomes a pull request.
You fan out several reviewers in parallel, adversarially verify anything that would
block a merge, then run consistency checks that no general-purpose reviewer performs.

## Boundary with /deep-review

These two skills do not overlap and never call each other.

| | `/tech-review` | `/deep-review` |
|---|---|---|
| When | Before the PR, on a local branch | On an open PR |
| Scope | This branch and its paired strategy change | The PR as published |
| Unique value | Cross-repo consistency, test evidence | Adversarial multi-agent depth |
| Reviews others' work | No | Yes |

If the user asks to review an open PR, or someone else's PR, say so and point at
`/deep-review` rather than running this pipeline.

## Iron rules

1. **Never check out a branch.** `git diff --no-ext-diff origin/main...BRANCH` produces
   the diff without switching. This checkout may be shared with another session; a
   `git checkout` or `git stash` here can destroy work that is not yours.
2. **All edits happen in a disposable worktree, never the user's checkout.** This skill
   reports; findings go to the terminal. The simplify pass (when the diff has Go) and
   `--fix` (on request) edit an isolated worktree of the branch created in Stage 0f — so the default path
   leaves the user's repo byte-for-byte unchanged, and rollback is `git worktree remove`.
   Nothing is committed, pushed, or written to Jira; commits and pushes belong to
   `/create-pr` and `/edit-pr` (`AGENTS.md` › Commit policy).
3. **Baseline is fetched `origin/main`, never local `main`.** A stale local main
   misattributes already-merged work to the branch and produces false blockers.
4. **Every `git diff` and `git show` uses `--no-ext-diff`.** The repo's external diff
   driver defeats grep-over-diff.
5. **Use `grep -E`, never `grep -P`.** BSD grep on macOS has no `-P`; a `-P` pattern
   fails silently and the check reports a clean result it never computed.
6. **Do not suppress stderr wholesale.** `2>/dev/null` turns "unknown revision" into
   "nothing found". When a command returns empty, re-run it without suppression before
   reporting an absence.
7. **A step that did not run says so.** Never let a skipped reviewer look like a clean
   one.
8. **Under worktree isolation, a multi-statement stage goes into a script file first.** The
   session refuses inline loops, shell variables and heredocs that reach git, and a refused
   command is a stage that did not run. Write it to `$SCRATCH/<stage>.sh` and run that.
9. **Every sub-agent dispatch passes `model`, and the ceiling is `opus`.** An omitted model
   inherits the session's, which may sit above Opus; that is a bug, not a default. A
   built-in Skill forks on the model of whoever invokes it: the session's when invoked
   here, which cannot be capped, or the sub-agent's when invoked from inside one. So this
   skill never invokes a Skill itself. `code-review` forks the built-in `/code-review`
   from inside an Opus sub-agent; `simplify` (Sonnet) and `tech-document` (Opus) are
   sub-agents that do the work. The ceiling caps, it never raises: a reviewer that ran
   on Sonnet before stays on Sonnet.
10. **Talk to the user in plain words.** Every question and the Stage 6 verdict are drafted
    with the `plain-words` skill (`.claude/skills/plain-words/SKILL.md`), and use no stage
    or check ids without saying what they check. A decision question, where the user picks
    between options, opens with `Kind:` from `.claude/skills/decision-kinds.md` and gives
    each option one `Gain:` and one `Cost:` line; the template is in `/tech-design`'s
    Clarifying gates.

## Arguments

The user invoked this with: $ARGUMENTS

| Argument | Meaning |
|---|---|
| `BUILD-XXXX` | Find the branch for this issue key |
| branch name | Use it directly |
| PR URL | Take the head branch from the PR; review it locally |
| blank | Use the current branch |
| `--fix` | After reporting, offer to apply findings. Off by default. |
| `--cli=<name>` | Use only this CLI reviewer. `--cli=none` skips the tier. |

A PR URL is accepted because a branch under review often already has one. It selects
the branch; it does not turn this into a PR review, and nothing is ever posted.

## Setup check

Read `repo.md` at the project root. Validate rather than merely finding it:

- `Crane Plugin Repo` and `Designs Directory` must be present.
- No value may still contain `/path/to/` — the template ships placeholders, and a
  partly-edited file looks configured but is not.
- Every path present must resolve on disk.

If `repo.md` is missing, invoke `/setup-repos` and stop. If it is present but fails
validation, stop with `BLOCKED` and name the specific label at fault.

`Strategy Catalog Repo` is needed only by cross-repo checks 1 and 3. If it is unset,
those two report SKIPPED with the reason; the rest of the pipeline runs.

Throughout this file, `<Label>` means the path stored under that label in `repo.md`.
**Never hardcode a path.**

## Repositories

Two, both read-only.

| Label | Role |
|---|---|
| Crane Plugin Repo | The conversion code. Primary review target. |
| Strategy Catalog Repo | `redhat-openshift-builds/strategy-catalog`. Read when the branch touches a strategy parameter. |

The operator, `crane-lib` and upstream `shipwright-io/build` are not reviewed here. The
operator's `config/shipwright/build/strategy/*.yaml` are generated by
`make strategy-catalog`; `crane-lib` is frozen; upstream has its own contribution flow.
None is a PR target for this work.

---

## Stage 0: Setup

### 0a. Resolve the input to a branch

```bash
cd "<Crane Plugin Repo>"
git fetch origin main --quiet
git fetch fork --quiet || true
```

For a `BUILD-XXXX` key, search local *and* remote refs — a fresh clone has no local
branch for work that exists on the fork:

```bash
git for-each-ref --format='%(refname)' refs/heads refs/remotes \
  | sed -E 's#^refs/heads/##; s#^refs/remotes/[^/]+/##' \
  | grep -E "BUILD-1234" | grep -vE '(^|/)main$' | sort -u
```

Replace `BUILD-1234` with the actual key. `grep -vE '(^|/)main$'` matches `main` exactly
— a plain `grep -v main` would also drop a branch named `fix-main-parsing`. Stripping the
`refs/heads/` and `refs/remotes/<remote>/` prefixes and piping through `sort -u` collapses
a branch that exists both locally and on the fork into one identity — otherwise the same
branch counts twice and the ambiguity check below stops on a false "more than one branch".

If more than one branch matches, list them and stop. One story maps to one branch per
repo; two branches means the work needs consolidating first.

If none matches, re-run without `--quiet` and without stderr suppression to distinguish
"no such branch" from "fetch failed", then report which it was.

The match is a bare name (the prefixes were stripped for the ambiguity check). Resolve it
to a ref git can actually use before going further — a branch that lives only on the fork
has no local ref, so the bare name fails `git merge-base` and `git worktree add` with
"unknown revision". Prefer a local branch; fall back to the fork's remote-tracking ref.
Do not create a local branch — the remote-tracking ref is a valid commit-ish everywhere
`$BRANCH` is used below (`merge-base`, `--detach` worktree, `"$BRANCH"..origin/main`), so
resolving it read-only keeps the user's repo untouched:

```bash
if git show-ref --verify --quiet "refs/heads/$NAME"; then
  BRANCH="$NAME"
elif git show-ref --verify --quiet "refs/remotes/fork/$NAME"; then
  BRANCH="fork/$NAME"    # fork-only branch: use the remote-tracking ref directly
else
  echo "no local or fork ref for $NAME"; exit 1
fi
```

### 0b. Compute the diff

```bash
BASE=$(git merge-base origin/main "$BRANCH")
git diff --no-ext-diff "$BASE" "$BRANCH" --stat
git diff --no-ext-diff "$BASE" "$BRANCH" --name-only
```

Use the merge base, not a two-dot endpoint diff. A two-dot `origin/main..BRANCH` reports
the content of `main` as a removal for any branch merely behind it — on this repo that
flags most branches instead of the few that are genuinely stale.

Count changed lines by summing the numstat columns rather than parsing the `--stat`
summary line, whose field positions shift when insertions or deletions are zero:

```bash
git diff --no-ext-diff --numstat "$BASE" "$BRANCH" \
  | awk '{a+=$1; d+=$2} END {print a+d}'
```

### 0c. Branch freshness

```bash
git rev-list --count "$BRANCH"..origin/main
```

Non-zero means the branch is behind. Report it. If the branch is behind on files it also
touches, the findings may not survive a rebase — cap the verdict at
`READY WITH WARNINGS` and annotate `STALE: rebase before merge`.

This count is a point-in-time snapshot: `origin/main` can move during a long review (it
did this session — 17 commits landed mid-run). This skill is report-only and never pushes,
so a moved base does not corrupt anything here, but the verdict must say the check was
taken at Stage 0. `/create-pr` re-fetches and rebases before it first pushes, so it — not
this skill — owns the final freshness gate.

### 0d. Probe the CLI reviewers

```bash
for tool in coderabbit qodo; do
  if command -v "$tool" >/dev/null; then echo "$tool: available"; else echo "$tool: absent"; fi
done
```

Record both results. They go in the compliance report whether present or not.

`qodo` also needs `agent.toml` — the command definition the reviewer runs, untracked in
the checkout. Probe the path `reviewers/cli-review.md` will pass to `--agent-file`. That
reviewer looks in the review worktree first, but Stage 0f creates that worktree fresh and
an untracked file is never in it, so the file it actually uses is the one beside the
repo's common git directory:

```bash
COMMON="$(git rev-parse --git-common-dir)"
case "$COMMON" in /*) ;; *) COMMON="$PWD/$COMMON" ;; esac
AGENT_FILE="$(cd "$(dirname "$COMMON")" && pwd)/agent.toml"
[ -f "$AGENT_FILE" ] && echo "agent.toml: $AGENT_FILE" || echo "agent.toml: absent"
```

Record the path. Without it the qodo reviewer reports `unavailable` rather than
falling back to a free-form prompt.

### 0e. Find the design doc

```bash
ls "<Designs Directory>"/BUILD-1234-*.md
```

`/tech-design` writes to `<Designs Directory>/BUILD-XXXX-<slug>.md`, so the issue key is
always the prefix. No match means no design doc — cross-repo check 5 reports SKIPPED,
which is not an error. More than one match takes the most recently modified and says in
the output which it chose.

### 0f. Create the review worktree

Every stage that reads or edits code runs against a disposable worktree of the branch,
not the shared checkout. This is what keeps the default path report-only: the simplify
pass and `--fix` edit the worktree, so the user's repo is never touched and rollback is a
single `git worktree remove`. It also fixes the review target — the worktree is the one
immutable copy every reviewer sees, even after the simplify pass edits it.

```bash
SLUG="$(printf '%s' "$BRANCH" | tr '/' '-')"   # a fork-only ref is "fork/BUILD-1234"; keep it out of paths
WT="$(mktemp -d)/tech-review-$SLUG"
git worktree add --detach "$WT" "$BRANCH"
```

`$WT` is the working directory for Stages 2, 3, 4, and 7. `$BASE` is still the merge base,
and the worktree starts at the branch tip, so the review target is:

```bash
git -C "$WT" diff --no-ext-diff "$BASE"    # BASE → worktree tree, including the simplify pass's edits
```

Also create the scratchpad — one directory where every reviewer writes its findings JSON
and Stage 7 writes its patch. It has to be defined here and passed to every sub-agent
(Stage 3), because Stage 6 consolidates by reading these files off disk. Like `$WT`, it is
an ephemeral `mktemp` dir outside the repo, so nothing lands in the checkout:

```bash
SCRATCH="$(mktemp -d)/tech-review-$SLUG"
mkdir -p "$SCRATCH"
```

Remove both the worktree and the scratchpad when the run ends, including on any early exit:

```bash
git worktree remove --force "$WT"
rm -rf "$SCRATCH"
```

### 0g. Carry the uncommitted work

Work on a story branch usually sits uncommitted: `/tech-implement` and `/tech-document`
leave it that way, and only `/create-pr` or `/edit-pr` commit it (`AGENTS.md` › Commit
policy). A worktree made from the branch ref sees committed history only, so copy the
uncommitted change into `$WT` before any stage reads it. Find the worktree that has the
branch checked out:

```bash
SRC=$(git worktree list --porcelain | grep -B2 -x "branch refs/heads/$NAME" | grep '^worktree ' | sed -E 's/^worktree //')
git -C "$SRC" status --porcelain
```

When `SRC` is empty (a fork-only branch) or its status prints nothing, there is nothing to
carry; say so in the report. Otherwise run this from a script file, reading `SRC` and
changing nothing in it:

```bash
git -C "$SRC" diff --no-ext-diff --binary HEAD > "$SCRATCH/uncommitted.patch"
[ -s "$SCRATCH/uncommitted.patch" ] && git -C "$WT" apply --index "$SCRATCH/uncommitted.patch"
(cd "$SRC" && git ls-files -z --others --exclude-standard | tar --null -T - -cf "$SCRATCH/untracked.tar")
tar -xf "$SCRATCH/untracked.tar" -C "$WT"
git -C "$WT" add --all
git -C "$WT" write-tree        # the tree id of the change under review
```

The carried change is staged in `$WT`'s own index, so everything later stages add stays
unstaged on top of it. Take the file list and changed-line count again from
`git -C "$WT" diff --no-ext-diff --cached "$BASE"`; the 0b numbers cover commits only. The
report's header names the tree id beside the branch.

---

## Stage 1: Evidence gate

Ask one question: **is there test evidence, and does it match the code?**

Look for `<Designs Directory>/test-results/BUILD-XXXX-results.md`.

| Condition | Result |
|---|---|
| File absent | `EVIDENCE: none` — not a blocker on its own, but no clean `READY` |
| Contains `PASS` with no pasted command output or exit code | Treat as absent. A claim is not evidence. |
| Records a SHA that is not an ancestor of the branch head | `EVIDENCE: stale` — the code changed after it was tested |
| 0g carried uncommitted work, and the file's `Tree:` line is missing or differs from 0g's tree id | `EVIDENCE: stale` — the uncommitted change moved after it was tested |
| Jira status claims more than the evidence supports | Report the mismatch |

```bash
git merge-base --is-ancestor "$RECORDED_SHA" "$BRANCH" \
  && echo "evidence current" || echo "evidence predates branch head"
```

**Ordering.** This skill runs after unit tests and before cluster tests. Reviewing code
that fails unit tests wastes every reviewer on code that is about to change; cluster
tests are slow and review often changes the code, so running them first means running
them twice. Report pending cluster evidence rather than blocking on it.

---

## Stage 2: Simplify

Skip this stage when the diff has no non-test Go lines (`GO_LINES` from Stage 0b is 0) and
record `skipped: no code in diff`. A simplification pass over Markdown finds nothing and
costs a full read of every file.

Otherwise dispatch one sub-agent from `reviewers/simplify.md` with `model: sonnet`, the
worktree path `$WT`, the merge base and the scratchpad `$SCRATCH`. It performs the pass
itself: reuse of helpers that already exist, dead code, duplicated branches, abstraction
the diff does not need. It does not invoke the built-in `/simplify`: the pass is small
enough to do directly, and the fork-inside-a-sub-agent pattern `code-review` uses (Stage 3)
is reserved for the one reviewer whose built-in depth has earned its cost. Block on it
before Stage 3.

It runs first on purpose: its edits land in the worktree's diff, so the Stage 3 reviewers
review them too. Run it last and nothing checks its output. It is the only reviewer that
changes files, and it changes them only inside `$WT`, never the user's checkout. Nothing
is committed and no patch is carried anywhere: the edits sit in `$WT`, uncommitted, which
is the one tree every reviewer reads.

If it returns nothing or `$SCRATCH/simplify.json` is missing, record it `failed` in the
report, never a clean pass.

After it completes, run the unit tests in the worktree:

```bash
cd "$WT" && GOWORK=off go test ./... -count=1
```

`GOWORK=off` is what CI builds. A failure means the pass broke the branch: discard its
edits with `git -C "$WT" checkout -- .`, report that they were dropped and why, and
continue to Stage 3 without them. The worktree makes this safe — it holds nothing but the
branch, the change 0g staged, and the pass's edits, and `checkout -- .` restores the index,
so the staged change survives and a blanket discard cannot touch anyone else's work.

---

## Stage 3: Reviewers

**Every reviewer is a sub-agent with an explicit `model` capped at `opus` (iron rule 9).**
A built-in Skill invoked from the session forks on the session's model, which cannot be
capped. Invoked from inside a sub-agent it forks on that sub-agent's model: tested on
2026-09-16, a sub-agent's Skill call to `/code-review` launched the fork, ran it, and got
the findings back on a second wake-up. So `code-review` is an Opus sub-agent that forks
the built-in `/code-review` at `low` effort and maps its findings; `simplify` is a Sonnet
sub-agent that performs the pass itself; `tech-document` is an Opus sub-agent running
that skill's report mode. Nothing runs on the session model. Security depth is
`/deep-review`'s job at PR time; this gate has no persona fan-out.

| Reviewer | How to run | Prompt / instructions | Model | When |
|---|---|---|---|---|
| `cli-review` (coderabbit) | **sub-agent** | `reviewers/cli-review.md` | sonnet | `coderabbit` on PATH and not excluded by `--cli` |
| `cli-review` (qodo) | **sub-agent** | `reviewers/cli-review.md` | sonnet | `qodo` on PATH, `agent.toml` present, and not excluded by `--cli` |
| `code-review` | **sub-agent** that forks the built-in `/code-review "$BRANCH" low` | `reviewers/code-review.md` | opus | Always |
| `tech-document` | **sub-agent** | the body of `.claude/skills/tech-document/SKILL.md`, with the argument line `<branch> --report --work "$WT"` | opus | Always |

`tech-document` maps the branch diff to the docs it touches, runs the documentation tests,
and writes findings in the `findings-schema.md` shape with `source: tech-document`. It
writes to its own scratch and returns the path on one line; tell the sub-agent to copy that
file to `$SCRATCH/tech-document.json` before it returns, so Stage 6 reads it with the
others. Running it here rather than in Stage 5 puts its findings through the same pipeline
as every reviewer's: a red documentation test arrives as a `blocker` and the Stage 4
challenger confirms it by running the named test; stale prose arrives as a `warning`; a
sentence already wrong on `$BASE` is scoped `pre-existing` and never blocks. The skill
ships in this repo, so it is never `unavailable`; if the sub-agent returns `BLOCKED` or
nothing, record it as `failed`.

Dispatch **every sub-agent in one message** so they run in parallel. Each writes its
findings JSON to `$SCRATCH` and returns only a count.

`code-review` returns twice: once when its fork launches (`code-review: launched,
waiting`), and once when the fork has finished and it has written
`$SCRATCH/code-review.json`. Only the file counts. Wait for its second return, or watch
for the file with the Monitor tool; a verdict built on the first return records
`code-review` as `failed`.

Resolve the prompt directory once:

```bash
SKILL_DIR="$(git rev-parse --show-toplevel)/.claude/skills/tech-review"
```

For every sub-agent, read the prompt file's body and pass it as the sub-agent prompt with
`subagent_type: general-purpose` and the `model` from the table above, which matches the
`model:` field in the file's frontmatter; these files are prompt content, not registered
agents.

**Model-availability fallback.** If a dispatch fails because the requested model is not
available to sub-agents on this deployment (e.g. an Opus session model that sub-agents
cannot use — the error names the model), re-dispatch that reviewer with `model: sonnet`.
This applies to every reviewer here and to the `model: opus` challenger in Stage 4. A
reviewer or challenger dropped on a model error is a coverage gap, not a pass — never let
it silently vanish from the report.

Give every sub-agent the branch name, the merge base, the changed-file list, the worktree
path `$WT` (their working directory and the single review target), the scratchpad path
`$SCRATCH` (where it writes its findings JSON), and the contents of `findings-schema.md`.
Reviewers read and run against `$WT`, never the shared checkout — so even a CLI tool that
writes can only touch the throwaway worktree.

There is no escalation tier. An earlier version dispatched `compound-engineering:ce-code-review`
above 100 changed Go lines; it spawned six to fourteen personas on the session model,
could not be capped, and duplicated the multi-persona pass `/deep-review` runs at PR time.
A diff that touches secrets, `ServiceAccount`, RBAC or the Shipwright API types is named
in the NOTES section of the verdict so the PR reviewer knows to run `/deep-review`.

---

## Stage 4: Challenger

Run this **after Stage 5**, not before it. Cross-repo checks 5a, 5b and 5e also emit
blockers, and every blocker that gates the verdict must be challenged — a blocker that
skips disproof is exactly the false positive this stage exists to catch.

Collect every finding marked `blocker` from Stage 3 **and** Stage 5. If there are none,
skip this stage and say so.

Otherwise dispatch one sub-agent from `reviewers/challenger.md` with the blocker set, the
worktree path `$WT` (where it reads the code), the merge base, and the branch name.

Blockers only. They are the findings that gate the verdict, and a false blocker is the
expensive failure — it stops a good branch. Warnings and info are reported as they came,
with their source named.

---

## Stage 5: Cross-repo checks

Six checks. 5a to 5e are plain bash with no sub-agent; 5f is the accounting line for the
`tech-document` reviewer that already ran in Stage 3. This is the part no general reviewer
performs.

### 5a. Parameter consistency

The converter emits strategy parameter names as Go strings. The ClusterBuildStrategy
YAML defines which parameters exist. A name emitted but not defined fails on the cluster
at build time with `UndefinedParameter`.

Extract the parameters the strategy defines:

```bash
grep -E '^[[:space:]]*-[[:space:]]+name:' "<Strategy Catalog Repo>/clusterBuildStrategy/<strategy>/<strategy>.yaml" \
  | sed -E 's/.*name:[[:space:]]*//' | tr -d '"' | sort -u
```

Choose `<strategy>` from the issue classification in the design doc — buildah or
source-to-image. Do not hardcode one: an S2I change checked against the buildah strategy
produces a confidently wrong answer. If the classification is unavailable, check every
strategy the diff mentions and say which were checked.

Then check both directions against the parameter names the diff adds or changes in
`buildconfig/converter.go`. Report a name emitted but not defined as a **blocker**; a
parameter defined but never emitted as **info**.

Also check the value source matches the parameter's meaning. A parameter can register
cleanly and still ship the wrong values.

### 5b. RFE lifecycle

This repo tracks unimplemented-feature warnings inline in `buildconfig/converter.go`,
not in a separate `rfe.go`.

Report as a **blocker**: a warning for a feature this branch implements that is still
emitted. That is the silent-data-loss case in reverse — the user is told a field was
dropped when it now works.

Report as a **warning**: a warning constant declared and never referenced.

### 5c. Upstream ↔ downstream strategy diff

Run when the diff adds or renames any strategy parameter, or touches a strategy YAML —
not only the latter.

Compare `<Strategy Catalog Repo>` against upstream `shipwright-io/build`. A parameter
upstream has that the catalog lacks is a **warning** — a possible gap. A catalog-only
parameter is deliberate downstream divergence and needs a one-line disposition, not
silence.

If the upstream repo is not configured in `repo.md`, report SKIPPED with that reason.

### 5d. Test coverage parity

```bash
git diff --no-ext-diff --name-only "$BASE" "$BRANCH" \
  | grep -E '\.go$' | grep -vE '_test\.go$|/fakes/|vendor/'
```

For each implementation file changed, check whether its `_test.go` sibling also changed.
Implementation without tests is a **warning**.

Then the golden check. When the diff touches a non-test file under `buildconfig/` and changes
what the plugin emits or accepts (a source, a param, a resource, an outcome), list what
changed under `tests/testdata/`:

```bash
git -C "$WT" diff --no-ext-diff --name-only "$BASE" | grep -E '^tests/testdata/'
```

Empty output is a **warning**: a conversion change with no new or changed golden. A diff that
only rewords a warning is exempt, since the documentation test covers the text. Report a
golden that was clearly written by hand (a value the plugin does not emit, a missing
annotation the plugin always writes) as a warning too.

_(Origin: BUILD-2475. The rejection it reversed had a unit test asserting the rejection and
no golden, and the check above would have flagged the branch that introduced it.)_

### 5e. Design doc completeness

If a design doc was found in Stage 0e, compare the repos it plans to change against the
repos that actually changed. A mismatch is scope drift: report it as a **blocker** and
cap the verdict until either the code or the doc is reconciled.

No design doc means SKIPPED, not a finding.

### 5f. Docs currency

The `tech-document` reviewer ran in Stage 3 and its findings went through Stage 4 with the
rest. Here, confirm in the compliance table that it ran and that `$SCRATCH/tech-document.json`
exists with `status: ok`; anything else is `failed`, named in the report. `--fix`
(Stage 7) treats its findings as mechanical: the `detail` field carries the replacement
text. Apply the approved ones in `$WT` and re-run the documentation tests. Its post-PR
counterpart is `/deep-review`'s `docs-currency` reviewer.

---

## Stage 6: Verdict

Read every findings file from `$SCRATCH` (each reviewer wrote `$SCRATCH/<source>.json`).
Consolidation reads disk, not conversation context, so it survives compaction.

**First, account for every reviewer you dispatched.** For each one, a `$SCRATCH/<source>.json`
must exist. A dispatched reviewer with no file is `failed` — name it in the report as such
and never let its absence read as a clean zero. A CLI
reviewer whose file reports `ok` with empty findings but whose own note says it saw no diff
or no changed files — while the Stage 0b diff is non-empty — is also `failed`/degraded, not
a clean pass; re-run it or report it degraded. Do not build the verdict until every
dispatched reviewer is either a real result or a named failure.

Deduplicate by file and line: findings within three lines of each other that describe
the same problem merge into one, keeping the more specific description and listing every
source that found it.

Findings tagged `pre-existing` never block. Report them in their own section.

Apply the challenger's adjudication before building the verdict: drop every finding in
its `removed` array, and replace the original blockers with its `upheld` entries,
honouring each entry's `action` (kept, downgraded, merged) and `severity`. A blocker the
challenger removed or downgraded must not reappear as a blocker in the verdict — that is
the whole point of Stage 4.

```text
================================================================================
TECH REVIEW: <BRANCH>
================================================================================
Branch:      <branch>  (<N> commits, <M> changed lines)
Base:        <merge-base sha>
Design doc:  <filename or "none">
Evidence:    current | stale | none

Reviewers:
  simplify        <N changes applied | reverted: tests failed | skipped: no code in diff | failed>
  coderabbit      <N findings | absent>
  qodo            <N findings | absent>
  code-review     <N findings | failed>
  tech-document   <N findings | failed>
  challenger      <N blockers upheld, M removed | no blockers to review>

BLOCKERS
  1. [file:line] description (source: X) [confidence N/10]

WARNINGS
  1. [file:line] description (source: X)

INFO
  1. [file:line] description (source: X)

PRE-EXISTING (not caused by this branch, non-blocking)
  1. [file:line] description

CROSS-REPO
  5a params    <result>
  5b RFE       <result>
  5c up/down   <result>
  5d tests     <result>
  5e design    <result>

APPLIED BY THE SIMPLIFY PASS (in the review worktree — your checkout is untouched)
  <file> — what changed
  To keep these: git -C <your repo> apply <patch printed below>

NOTES
  <anything surprising: a reviewer that failed oddly, a finding pattern no
   stage covers, a check that could not resolve a path. Omit if nothing.>

VERDICT: READY | READY WITH WARNINGS | NOT READY
<one or two sentences with the top action>
================================================================================
```

### Compliance table

Emit this every run, including when stopping early for any reason.

```
| Stage | Status | Evidence / reason |
|-------|--------|-------------------|
| 0. Setup            | DONE / BLOCKED | branch, base, CLIs probed |
| 1. Evidence gate    | DONE | current / stale / none |
| 2. Simplify         | DONE / REVERTED / SKIPPED / FAILED | tests result, or "no code in diff" |
| 3. Reviewers        | DONE / partial | which ran, which were absent |
| 4. Challenger       | DONE / SKIPPED | no blockers |
| 5. Cross-repo 5a-5f | DONE / partial | each sub-check accounted for |
| 6. Verdict          | DONE | |
Overall: COMPLETE / INCOMPLETE
```

**Verdict rules.** `READY` requires every stage to be DONE or to carry a documented
skip. Any unexplained row forces `NOT READY (pipeline incomplete)`. An unresolved
evidence gap caps the verdict at `READY WITH WARNINGS`. Never emit a clean `READY` over
a stage that silently did not run.

---

## Stage 7: Apply fixes

Skipped unless `--fix` was passed. Without it this skill has changed nothing at all in the
user's checkout — every edit lived and died in the review worktree.

With `--fix`, edits still land in the worktree `$WT`, never the user's checkout:

1. Split findings into mechanical and judgement. A missing nil guard or a wrong constant
   is mechanical. Changing a function signature, an API contract, or deferring a feature
   is a judgement call.
2. Present both lists and ask once, via AskUserQuestion, which to apply.
3. Apply only what was approved, in `$WT`.
4. Re-run `cd "$WT" && GOWORK=off go test ./... -count=1`. If it fails, discard the last
   change in the worktree and report; never emit a patch that breaks the build.
5. Emit the combined patch (the simplify pass + approved fixes). It is the diff against
   `$WT`'s index, so it holds only this run's edits and not the branch or the change 0g
   carried. The user applies it, uncommitted, in the branch's worktree with
   `git apply <patch>`:

   ```bash
   git -C "$WT" add --intent-to-add --all
   git -C "$WT" diff --no-ext-diff --binary > "$SCRATCH/fixes.patch"
   ```

6. Report what was applied and what was skipped.

Do not commit and do not write to the user's checkout. `/create-pr` commits the change
when the branch has no PR yet, and `/edit-pr` when it has one. The worktree is removed
after the patch is emitted; keep `$SCRATCH` until the user has applied the patch.

---

## Error handling

| Scenario | Behaviour |
|---|---|
| `repo.md` missing | Invoke `/setup-repos`, stop |
| `repo.md` has placeholders or unresolvable paths | Stop with `BLOCKED`, name the label |
| Strategy Catalog Repo unset | Checks 5a and 5c report SKIPPED, rest continues |
| Branch not found | Re-run without stderr suppression, report which failure it was |
| Two branches match the key | Stop; one story maps to one branch |
| A CLI is absent | Record it, continue with the rest |
| The simplify pass breaks the tests | Discard its edits in the worktree, report, continue |
| `code-review` returned only its launch line | Wait for `$SCRATCH/code-review.json`; if it never appears, record `failed`, never a clean result |
| A sub-agent's `model` is refused | Re-dispatch with `sonnet`; never omit `model`, and never go above `opus` |
| A sub-agent returns nothing | Treat as `failed`, name it, do not report a clean result |
| No design doc | Check 5e SKIPPED, not a finding |
| No test evidence | `EVIDENCE: none`, no clean `READY` |
| The `tech-document` sub-agent returns `BLOCKED` or nothing | Check 5f `failed`, named in the report; never a clean result |

## Notes

Report anything surprising in the NOTES section of the terminal output — a reviewer that
failed in an unusual way, a finding pattern no stage covers, a check that could not
resolve a path. Nothing is written to disk. This is the signal for what is worth
promoting into this file later.
