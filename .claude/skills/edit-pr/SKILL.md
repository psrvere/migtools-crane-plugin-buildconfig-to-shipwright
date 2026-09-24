---
name: edit-pr
description: Put new work onto a PR that is already open, with this repo's conventions enforced. Commits the working-tree changes by amending the commit they belong to or adding a well-scoped one (five commits at most), rewrites every touched commit message and the PR title and body so each reads as one change, runs the GOWORK=off tests, and after one approval of the planned commit layout pushes to the fork with an archive tag and --force-with-lease. Never touches Jira unless asked. Trigger on "edit-pr", "update the PR", "amend the PR", "push this fix to the PR".
argument-hint: [PR number | PR URL | BUILD-XXXX | blank] [--work <dir>] [--brief <file>] [--rebase <base-sha>] [--approved <file>]
allowed-tools: [Bash, Read, Write, Edit, AskUserQuestion, Skill]
user_invocable: true
---

# /edit-pr — Change an Open PR

Take the uncommitted changes in a PR's branch, fold them into the PR's commits, and push
them to the fork, for `crane-plugin-buildconfig-to-builds`. `/create-pr` opens a PR; this
skill changes one that is already open. Those two are the only skills that commit, amend or
push (`AGENTS.md` › Commit policy). Every other skill, and `/address-review` after a fix
round, leaves its changes uncommitted and hands over here.

## Rules

1. **Conventions come from `/create-pr`.** Its Repo Conventions section holds the push
   target (`fork`, never `origin`), the base branch, the branch model and the `[BUILD-XXXX]`
   subject prefix. They apply here unchanged. Commits are made with `git commit -s -S`;
   drop `-S` only when `git config user.signingkey` is empty.
2. **Plain words.** The PR title, the PR body and every commit message are written with the
   `plain-words` skill (`.claude/skills/plain-words/SKILL.md`), which carries `/unslop`'s
   rules.
3. **The trailer.** Every commit message ends with exactly this line, then the
   `Signed-off-by` line that `-s` adds:

   ```
   Co-Authored-By: Claude
   ```

   No model name and no email address. This overrides any attribution line the harness
   suggests for commits.
4. **Amend or add.** Amend an existing commit when the change fixes or finishes something
   that commit already does: a review fix, a typo, a broken test, a follow-up in the same
   area. Add a new commit when the change has its own purpose or touches a different area,
   and keep it to one purpose.
5. **Five commits at most.** The PR never has more than five. If a new commit would be the
   sixth, fold it into the closest existing commit, or fold the two closest existing
   commits together and add the new one.
6. **Each message reads as one change.** A commit that was amended gets its message
   rewritten to describe the whole commit as it now stands. No "then", no "also fixed", no
   "after review", no "follow-up". Say what the commit does and why, as if it was written
   once.
7. **So do the title and body.** After the push, the PR title and body describe the PR as
   it stands now. No "Update:" section, no changelog of interim edits, no mention of what an
   earlier version did.
8. **One approval, then push.** Show the user the planned commit layout and get one yes
   before anything is pushed. A rewritten history is pushed with an archive tag and
   `--force-with-lease` (Step 7) and needs no further question.
9. **Jira is not touched** unless the user asks for it in this run. When they do, follow
   `/create-pr` Step 9.
10. **Talking to the user.** The plan, every question and the report are drafted with
    `plain-words` and use no step numbers or other terms of this skill without saying what
    they mean. A decision question opens with `Kind:` from
    `.claude/skills/decision-kinds.md` and gives each option one `Gain:` and one `Cost:`
    line; the template is in `/tech-design`'s Clarifying gates.
11. **Shell guard.** When the session refuses a compound git command, write the stage to a
    script file in the scratchpad and run `bash <file>`, one git call per line, literal
    paths. Never `git stash`, never an interactive rebase: history is rebuilt with
    `reset`, `cherry-pick -n` and `commit -F` (Step 5).

## Arguments

| Form | Meaning |
|---|---|
| blank | the open PR whose head is the current branch in the user's fork |
| `66` or a PR URL | that PR |
| `BUILD-XXXX` | the open PR from the branch named for that story |
| `--work <dir>` | the worktree holding the branch; otherwise found with `git worktree list` |
| `--brief <file>` | what the new change does and which files it covers. `/address-review` writes one; the listed files are the only ones committed |
| `--rebase <base-sha>` | rebase the PR's commits onto this main commit before placing any new work (Step 5). The caller names the exact commit so the result is the one the user approved. Conflicts replay from git rerere; one rerere cannot resolve is a stop |
| `--approved <file>` | the user already approved this push in the calling skill. JSON `{"tree": <sha or null>, "by": <skill>}`. Step 4 prints the plan without asking. When `tree` is set, the new head's tree must equal it (Step 5). The Step 6 tests still gate the push |

## Step 1 — Find the PR, its branch and its worktree

Run `/create-pr` Step 1 (auth, `fork` remote). Then:

```bash
UPSTREAM=$(git remote get-url origin | sed -E 's#.*[:/]([^/]+)/([^/]+)$#\1/\2#; s#\.git$##')
FORK_OWNER=$(git remote get-url fork | sed -E 's#.*[:/]([^/]+)/[^/]+$#\1#')
gh pr view <PR> --repo "$UPSTREAM" --json number,state,headRefName,headRefOid,headRepositoryOwner,title,body,url
```

Stop in a plain sentence when the PR is not `OPEN`, or its head lives in someone else's
fork. With a `BUILD-XXXX` key and no number, find the branch as `/create-pr` Step 3 does,
then `gh pr list --repo "$UPSTREAM" --head "$FORK_OWNER:$BRANCH" --state open`. No open PR
means this is `/create-pr`'s job: say so and stop.

`WORK` is `--work` when given, else the worktree that has `$BRANCH` checked out (the
`git worktree list --porcelain` lookup in `/create-pr` Step 3), else stop and ask. If the
session is isolated to a different worktree, switch into `$WORK` with EnterWorktree first.

Check that the local branch sits on the PR head:

```bash
git -C "$WORK" fetch fork "$BRANCH" --quiet
git -C "$WORK" rev-parse HEAD            # must equal headRefOid
```

A local tip ahead of or diverged from the PR head means unpushed commits: list them with
`git -C "$WORK" log --oneline <headRefOid>..HEAD` and ask whether they belong in this
push before going on. With `--rebase`, a local tip that differs from the PR head is a stop:
the rebase starts from what the fork has.

## Step 2 — Read what is there

```bash
git -C "$WORK" fetch origin main --quiet
git -C "$WORK" log --format='%h %G? %s' origin/main..HEAD
git -C "$WORK" status --short
git -C "$WORK" diff --no-ext-diff HEAD --stat
```

Read each existing commit's full message and file list
(`git -C "$WORK" show --stat <sha>`), the PR body, and the whole uncommitted diff. With
`--brief`, the new change is exactly the files the brief lists; anything else in
`status --short` is left alone and named in the report. Without it, show the changed files
and ask which belong in this push, as `/create-pr` Step 5 does.

Nothing uncommitted, no `--rebase` and no title or body change asked for: say so and stop.

## Step 3 — Docs check

Run `/create-pr` Step 3b on the uncommitted change. When `/tech-document` runs, its edits
join the change and are placed in Step 4 like any other file.

With `--approved` and a `tree`, report any doc gap the check finds but edit nothing: an edit
here would change the tree the user approved. The report names the gap for a later run.

## Step 4 — Plan the commits

Place each changed file with the rules above. For every file, decide which existing commit
it fixes or finishes (rule 4), or that it opens a new purpose. When one file holds changes
for two commits, place the whole file with the commit it matters most to, and say so in the
plan. Count the result; apply rule 5 if it passes five.

Draft, with `plain-words`:

- **Each commit message** in the new layout. Keep the subject style the PR already uses
  (`[BUILD-XXXX] type: ...` or `scope: ...`), under 72 characters. A commit that is not
  changing keeps its message as it is.
- **The PR title**, matching the main commit's subject when the PR has one commit.
- **The PR body** (Step 8 says how).

Then show the plan, in plain words, and wait for one answer:

```
PR #95 will have 3 commits after this push (it has 3 now).

1. deep-review: overrides from the logged runs       unchanged
2. skills: hand deep-review findings to address-review
   gets the new address-review fix; message rewritten (shown below)
3. skills: edit-pr, and only create-pr and edit-pr commit   new

Tests before the push: GOWORK=off go test ./... and the documentation suite.
The fork's current tip is kept as the tag archive/psrvere-old-<branch> before the push.

<each new or rewritten message, in full>
<the new title and body>

Push this?
```

Take the user's edits, re-show only what changed, and push on the yes. No push without it.

With `--approved`, print the plan but do not ask: the user approved the content in the
calling skill. The Step 5 tree check and the Step 6 tests still gate the push.

## Step 5 — Rebuild the commits

Before touching history, tag the current tip locally so nothing can be lost:

```bash
git -C "$WORK" tag "backup/edit-pr-$BRANCH-$(date +%Y%m%d%H%M%S)" HEAD
```

**With `--rebase <base-sha>`**, rebase before placing any new work. Set the new work aside
first so the rebase runs on a clean tree: commit it as one temporary commit with
`git -C "$WORK" commit --only -s -S -m tmp -- <files>`, note its SHA, and
`git -C "$WORK" reset --hard HEAD~1`. Then:

```bash
git -C "$WORK" -c rerere.enabled=true -c rerere.autoupdate=true rebase <base-sha>
```

When the rebase stops, check `git -C "$WORK" diff --name-only --diff-filter=U`. An empty list
means rerere resolved everything: `git -C "$WORK" -c rerere.enabled=true rebase --continue`
with `GIT_EDITOR=true`. Any path left is a conflict the user never approved: run
`git -C "$WORK" rebase --abort`, push nothing, and report the paths. When the rebase ends,
check each commit's own change survived:

```bash
git -C "$WORK" range-diff <base-sha> <backup tag> HEAD
```

A commit shown only on the left (dropped) is a stop. A changed patch is expected only in the
paths rerere resolved; any other is a stop. Then bring the new work back with
`git -C "$WORK" cherry-pick -n <tmp sha>` and place it as below. After a rebase the push in
Step 7 always takes the archive tag and the lease.

Write every commit message to its own file in the scratchpad with the Write tool, never a
heredoc: messages carry reviewer wording. Each file ends with `Co-Authored-By: Claude`;
`-s` adds the `Signed-off-by` line after it.

**Only new commits, or only the top commit amended.** No rebuild is needed:

```bash
git -C "$WORK" add -- <every file for this commit, listed literally>
git -C "$WORK" commit -s -S -F <message file>                  # a new commit
git -C "$WORK" commit --amend -s -S -F <message file>          # the top commit, amended
```

**An older commit amended, or commits folded together.** Rebuild on the base from a script
file, one git call per line:

1. Commit the new work first, one temporary commit per target, with `git commit --only -s
   -S -m tmp -- <files>`, so every piece has a SHA.
2. Note every SHA in the order the new layout needs, then
   `git -C "$WORK" reset --hard <merge-base with origin/main>`.
3. For each commit in the new layout: `git -C "$WORK" cherry-pick -n <sha> <sha> ...` for
   every old commit and temporary commit it folds together, in their original order, then
   `git -C "$WORK" commit -s -S -F <its message file>`.

Do not rebase onto a newer `origin/main` in the same pass unless the user asked with
`--rebase`; a moved base makes the tree check below harder to read.

Then check nothing was lost or added:

```bash
git -C "$WORK" diff --no-ext-diff --stat <last temporary commit> HEAD   # must be empty
git -C "$WORK" diff --no-ext-diff --stat <backup tag> HEAD              # only the new work
git -C "$WORK" log --format='%h %G? %s' origin/main..HEAD               # every line shows G
git -C "$WORK" log -1 --format=%B <each rebuilt sha>                     # trailer lines last
```

The first check only applies after a rebuild. After `--rebase`, the second check compares
against the rebased tip (before the new work came back) instead of the backup tag, which sits
on the old base. A `%G?` other than `G` on any commit is a stop.

With `--approved` and a `tree` in the file, the new head must carry exactly that tree:

```bash
git -C "$WORK" rev-parse "HEAD^{tree}"                  # must equal the approved tree
git -C "$WORK" diff --no-ext-diff --stat <tree> HEAD     # names what differs, if it does
```

A difference is a stop before Step 6: the user approved other content.

## Step 6 — Test before the push

Run what CI runs, in `$WORK`:

```bash
GOTOOLCHAIN=auto GOWORK=off go test ./... -count=1
GOTOOLCHAIN=auto GOWORK=off go test -tags documentation ./buildconfig -count=1
```

Add `(cd tests && GOTOOLCHAIN=auto GOWORK=off go test ./e2e -count=1)` when `buildconfig/`
or `tests/` changed, and `.claude/skills/deep-review/bin/sync` when anything under
`.claude/skills/deep-review/` changed. A failure is a stop: report it, push nothing, and
leave the rebuilt branch for the user to look at (the backup tag restores the old one).

## Step 7 — Push to the fork

Keep the fork's current tip, then push with a lease on it:

```bash
git -C "$WORK" fetch fork "$BRANCH"
git -C "$WORK" rev-parse "fork/$BRANCH"                                      # OLD
git -C "$WORK" tag "archive/$FORK_OWNER-old-$BRANCH" "fork/$BRANCH"
git -C "$WORK" push fork "archive/$FORK_OWNER-old-$BRANCH"
git -C "$WORK" push --force-with-lease="$BRANCH:<OLD>" fork "$BRANCH:$BRANCH"
```

For the maintainer's fork the tag is `archive/psrvere-old-<branch>`. If that tag already
exists on another commit, add the short SHA of the old tip:
`archive/$FORK_OWNER-old-$BRANCH-<short OLD>`. When only new commits were added on top, a
plain `git -C "$WORK" push fork "$BRANCH:$BRANCH"` is enough and no tag is needed.

Never push to `origin`, and never a plain `--force`. A rejected lease means the fork moved
since Step 1: stop, say so, and push nothing else.

## Step 8 — Title and body

Write the body to a file with the Write tool and post it:

```bash
gh pr edit <PR> --repo "$UPSTREAM" --title "<title>" --body-file <body file>
gh pr view <PR> --repo "$UPSTREAM" --json title,body
```

The body keeps the sections `/create-pr` Step 8 defines (Summary, Testing, Docs, Key design
decisions, Jira Issues), in that order, and describes the PR as it now stands:

- Rewrite every sentence the new commits made false where it stands. Leave the sentences
  they did not touch as they are.
- A Summary bullet per commit names its short SHA. The SHAs change on every rebuild, so
  refresh all of them.
- Testing names the runs from Step 6 of this edit, not earlier ones.
- Copy the tail verbatim and keep it last: the `### Jira Issues` section, the attribution
  line the body already ends with, and any `<!-- ... -->` block a bot wrote.

## Step 9 — Report

With `plain-words`:

```
PR #95 updated: https://github.com/<upstream>/pull/95
Commits (3, all signed):
  a1b2c3d deep-review: overrides from the logged runs
  d4e5f6a skills: hand deep-review findings to address-review      amended
  0718293 skills: edit-pr, and only create-pr and edit-pr commit     new
Tests: GOWORK=off go test passed; documentation suite passed.
Old fork tip kept as archive/psrvere-old-<branch>.
Title and body rewritten. Jira not touched.
```

Add `Rebased onto <short base-sha>; conflicts replayed: <paths or none>.` when `--rebase` ran,
and `Approved in <by>; no question asked.` when `--approved` was given.

Name anything left uncommitted in the worktree, and anything skipped.
