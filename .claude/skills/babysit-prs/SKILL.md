---
name: babysit-prs
description: Keep all of your open PRs ready to merge in one pass. Gathers each PR's state read-only (behind or conflicting with main, failing CI compared with main, open review feedback, Jira story), shows one table, and after one approval rebases through edit-pr, fixes CI failures it reproduces locally, runs address-review, re-runs flaky jobs, and moves Jira stories in Review to Closed when their PR merged. Trigger on "babysit-prs", "babysit my PRs", "look after my PRs", "tidy my PRs".
argument-hint: [--dry-run] [--pr N ...]
allowed-tools: [Bash, Read, Write, Edit, Agent, Skill]
user_invocable: true
---

# /babysit-prs — keep your open PRs current, green and answered

One read-only pass over every open PR you authored, one table, one approval, then the work.
Everything that commits or pushes goes through `/edit-pr`; everything that answers a
reviewer goes through `/address-review`. This skill decides what to hand them.

`$SKILL` below is this skill's base directory, printed by the harness when the skill loads;
type it literally. `$SCRATCH` is `${TMPDIR:-/tmp}/babysit-prs`. `BASE` is the main commit
recorded in Stage 0.

## Arguments

The user invoked this with: $ARGUMENTS

| Form | Meaning |
|---|---|
| blank | every open PR you authored on this checkout's upstream repo |
| `--pr N ...` | only these PRs |
| `--dry-run` | stop after the table; change nothing |

## Iron rules

- This skill never commits, amends or pushes. `/edit-pr` does, under `AGENTS.md` › Commit
  policy. Rebases in Stage 1 happen only in throwaway worktrees under `$SCRATCH` and are
  never pushed.
- Nothing is pushed, posted, re-run or moved in Jira before the user approves the table.
- Only PRs you authored, and only the upstream repo of this checkout.
- Rebase only a PR whose merge state is `DIRTY` or `BEHIND`. `CLEAN`, `BLOCKED`, `UNSTABLE`
  and `HAS_HOOKS` are left alone. Every rebase, in Stage 1 and Stage 3, goes onto `BASE`.
- Never resolve a conflict in a `.go` file or under `.claude/skills/`. Those go to "needs you".
- Change code for CI only when the check's kind is `reproducible` and the same command went
  red to green locally with `GOWORK=off`. Kinds `e2e`, `main-red` and `other` are reported.
  Kind `infra` gets one re-run, nothing more.
- Move a Jira story to Closed only when `scripts/jira-sweep` says `close`: a merged PR and no
  open one.
- Every Agent call names its `model`; the ceiling is `opus`.
- Everything the user reads is written with the `plain-words` skill.
- A stop in one PR marks that PR and moves on to the next. Never retry a push.
- Never switch branches, stash or reset in the user's main checkout.
- No snippet in this file may use a dollar sign followed by a digit.

## Stage 0: Setup

Run each line as its own Bash call: a session isolated to a worktree refuses a git call
chained with pipes or other commands.

```bash
git remote get-url origin
git fetch origin main --quiet
git rev-parse origin/main
mkdir -p "${TMPDIR:-/tmp}/babysit-prs"
```

`SLUG` is `OWNER/REPO` from the first line (drop the host and `.git`). `BASE` is the third
line. Type both literally into every later command.

## Stage 1: Gather (read-only)

1. `"$SKILL/scripts/gather-prs" "$SLUG" > "$SCRATCH/prs.json"`. With `--pr`, keep only those.
   Each record has `number`, `branch`, `jira_key`, `freshness` (`current`, `behind`,
   `conflict`), `feedback` (open review items) and `checks` (failing checks, each with a
   `kind` and a `run_id`).
2. `"$SKILL/scripts/jira-sweep" "$SLUG" > "$SCRATCH/sweep.json"`. Each issue has an
   `action`: `close`, `skip-open`, `leave-unmerged` or `no-pr`.
3. For each PR whose freshness is not `current` or that has a `reproducible` check, work in a
   throwaway worktree. The shell guard refuses loops and variables next to `git`, so write
   each PR's commands to `$SCRATCH/pr-<n>.sh` with the Write tool, one git call per line,
   literal values, and run `bash` on it:

   ```bash
   git fetch fork <branch>
   git worktree add --detach <SCRATCH>/wt-<n> fork/<branch>
   git -C <SCRATCH>/wt-<n> -c rerere.enabled=true rebase <BASE>
   ```

   The rebase line only when freshness is `behind` or `conflict`. On a conflict, list the
   paths with `git -C <wt> diff --name-only --diff-filter=U`:
   - **Mechanical:** a numbering clash in `docs/adr/` or a doc table, `go.sum`, or a golden
     file the tests regenerate. Resolve it, run the test that covers it, `git -C <wt> add
     <path>`, then `GIT_EDITOR=true git -C <wt> -c rerere.enabled=true rebase --continue`.
     rerere records the resolution; `/edit-pr --rebase` replays it in Stage 3.
   - **Anything else:** `git -C <wt> rebase --abort`, mark the PR "needs you: conflict in
     <path>", and plan no rebase for it. Its other steps still run.

   For each `reproducible` check, run its command in the worktree:
   `GOTOOLCHAIN=auto GOWORK=off go test ./... -count=1` for "Go build and tests",
   `GOTOOLCHAIN=auto GOWORK=off go test -tags documentation ./buildconfig -count=1` for
   "Documentation tests". Still red: dispatch one Agent (`model: sonnet`) with the failure's
   last 80 lines and the worktree path, told to fix the cause in that worktree only and
   report the files it changed. Run the command again. Green: save
   `git -C <wt> diff > <SCRATCH>/ci-fix-<n>.diff` and its file list. Red: no fix; report it.
   Green before any fix: the failure did not reproduce, so treat it like `infra`.

   When a rebase or a fix happened, record the tree the user will approve:
   `git -C <wt> add -A` then `git -C <wt> write-tree`, saved as `tree` for the PR.
4. For each `e2e` check: `gh run view <run_id> --repo <SLUG> --log-failed | tail -60`, and
   one line of suspected cause for the table. No fix.
5. For each PR, check whether a local worktree already holds its branch
   (`git worktree list --porcelain`) and, if so, whether it has uncommitted changes
   (`git -C <wt> status --porcelain`, one call per worktree). Uncommitted work means
   someone, often another session, is mid-change on that PR: plan nothing for it and list it
   under "needs you: uncommitted work in <wt>". Its triage would read that work as if it
   were pushed, and `/address-review` refuses such a worktree anyway.

6. For each PR with `feedback` above 0, first read who wrote it: run `get-pr-feedback`
   from the `address-review` skill's own scripts directory with `<n> <SLUG>`, and pipe it
   to `jq -c '.items[] | {n, kind, author}'`.
   When every item is yours and the newest is a comment, not a review, you already answered
   your own review round (typically "addressed in <sha>" under your deep-review verdict).
   `/address-review` would rebuild that verdict's findings as fixes, so skip it: show
   "Reviews: answered in your own comment" and plan no second push. Otherwise run
   `/address-review <n> --dry-run`. Copy the
   `table.json` it prints to `$SCRATCH/review-<n>.json`; its own scratch is wiped on the
   next run. A table with no `fix` or `answer` row (your own deep-review verdict, your own
   "addressed in <sha>" comment, bot boilerplate) means the PR has nothing to address: show
   it as "Reviews: nothing to address" and plan no second push.

## Stage 2: The table, then wait

Print plain text, one block per PR, then Jira, then "needs you". An example:

```
PR #97  [BUILD-2439] fix: keep invalid names out of the paste-ready commands
  Rebase    behind main; 1 conflict in docs/adr/README.md (numbering), resolved, tests pass
  CI        Go build and tests red: TestNames fails; reproduced; fix drafted (diff below)
            E2E red on main too, not this PR's doing
  Reviews   4 items: 3 fix, 1 answer (table below)
  Pushes    first: rebase and CI fix through edit-pr; second: review fixes through address-review

PR #96  [BUILD-2501] ci: open monthly PRs for dependency bumps
  Nothing to do

Jira
  BUILD-2315  Review → Closed   (PR #55 merged 2026-08-28)
  BUILD-2410  PR #60 closed without merging; left in Review
  BUILD-1491  no PR found; left in Review

Needs you
  PR #99  conflict in buildconfig/converter.go; rebase skipped, its other steps still run

Approve all? Or name rows to drop, e.g. "drop 97 CI", "drop 97 reviews", "drop BUILD-2315".
```

Under the blocks, print each CI diff and each PR's review table. Ask in chat text and wait.
Apply the drops, re-print only what changed, and write what is approved to
`$SCRATCH/plan.json`. Nothing approved, or `--dry-run`: stop here and remove the throwaway
worktrees (Stage 4).

## Stage 3: Act, one PR at a time

For each PR with approved rows:

1. Find the worktree that has the branch checked out (`git worktree list --porcelain`), or
   create one under `.claude/worktrees/<branch>` from `fork/<branch>`. Switch in with
   EnterWorktree.
2. **Rebase or CI fix approved.** Apply the CI fix with `git apply <SCRATCH>/ci-fix-<n>.diff`.
   Write a brief to `$SCRATCH/brief-<n>.md` with the Write tool: `Files:` the fix's files;
   `Change:` one plain line per fix; `Tests:` the command that went red to green. Write
   `{"tree": "<tree>", "by": "babysit-prs"}` to `$SCRATCH/approved-<n>.json`. Then run
   `/edit-pr <n> --work <wt> --approved <SCRATCH>/approved-<n>.json`, adding
   `--brief <SCRATCH>/brief-<n>.md` when there is a fix and `--rebase <BASE>` when a rebase
   was approved. Check afterwards that `git ls-remote fork refs/heads/<branch>` shows the new
   head; if not, record why and go to the next PR.
3. **Re-runs approved.** `gh run rerun <run_id> --failed --repo <SLUG>`.
4. **Reviews approved.** `/address-review <n> --approved <SCRATCH>/review-<n>.json`. It
   pushes through `/edit-pr` and replies where each comment was left.

Then the approved Jira rows. For each: `jira issue move <KEY> Closed`, then a comment written
with `plain-words` to `$SCRATCH/jira-<KEY>.md`, for example "PR #55 merged on 2026-08-28:
<url>", ending with the line `_Co-Authored-By: Claude Code._`. Post it with
`jira issue comment add <KEY> --template <SCRATCH>/jira-<KEY>.md --no-input`; a positional
body can hang.

## Stage 4: Report and clean up

Written with `plain-words`, three lists:

- **Done:** per PR, what was pushed (new head), re-run or answered; per story, the move.
- **Skipped:** each with its reason (tree changed since approval, test failed, lease
  rejected, dropped by you).
- **Needs you:** conflicts left alone, CI it could not fix, `e2e` and `main-red` failures,
  review comments that arrived after approval.

Then remove each throwaway worktree with `git worktree remove --force <SCRATCH>/wt-<n>`,
one call per line from a script file, and delete `$SCRATCH`.
