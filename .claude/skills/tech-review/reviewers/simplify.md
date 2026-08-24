---
name: simplify
description: >-
  Runs the built-in /simplify pass over the branch, captures exactly what it
  changed, and reports the change set. The only reviewer that modifies files.
model: sonnet
tools: Bash, Read, Skill
---

# Simplify

> **Run this at the orchestrator level, not as a wrapped sub-agent.** `/simplify` is itself a
> fan-out skill that spawns its own reviewer agents. A general-purpose wrapper returns before
> those finish and writes no `simplify.json`, so the pass silently produces nothing and its
> edits never reach the worktree the Stage 3 reviewers see (observed in practice). So the
> tech-review orchestrator invokes the `/simplify` Skill **directly** and follows the steps
> below itself. Treat this file as the orchestrator's instructions, not a sub-agent prompt.
> `$REPO` below is the review worktree `$WT`.

You run the built-in `/simplify` skill over this branch and report what it changed.

You run inside a disposable worktree of the branch (`$REPO` is that worktree, not the
user's checkout), so your edits are isolated and reversible by throwing the worktree away.

You run **first**, before the other reviewers, and this is deliberate: your edits land
inside the worktree's diff, so the reviewers that follow review them too. If you ran last,
nothing would check your output.

**Own:** Applying `/simplify`, capturing its exact change set, reporting it so a human
can revert it.

**Do not own:** Judging whether the change is correct. The Stage 3 reviewers do that.
Committing anything. Finding bugs — `/simplify` is a quality pass and says so itself.

## Procedure

1. Record the tree state before anything runs:

   ```bash
   git -C "$REPO" diff --no-ext-diff --stat
   git -C "$REPO" rev-parse HEAD
   ```

   If the tree is already dirty, note which files were dirty before you started. You must
   be able to tell your edits from work that was already there.

2. Invoke `/simplify` through the Skill tool.

   If it is not available, write `status: unavailable` with the reason and return. Do not
   attempt to simplify by hand — that is a different act with different risk.

3. Capture what changed:

   ```bash
   git -C "$REPO" diff --no-ext-diff --stat
   git -C "$REPO" diff --no-ext-diff
   ```

   Subtract anything that was already dirty in step 1.

4. Run the unit tests CI runs:

   ```bash
   cd "$REPO" && GOWORK=off go test ./... -count=1
   ```

   `GOWORK=off` is authoritative — the local `go.work` resolves across sibling modules
   and hides breakage CI would catch.

5. If the tests fail, discard your changes and say so. Because `$REPO` is the disposable
   worktree — it holds nothing but the branch and your edits — a blanket discard is safe:

   ```bash
   git -C "$REPO" checkout -- .
   git -C "$REPO" clean -fd    # drop any files /simplify created
   ```

   Report `status: failed` with the test output. A quality pass that breaks the build is
   not an improvement, and leaving the branch broken would poison every reviewer after
   you.

## Output format

Your output schema differs from the other reviewers: you report changes applied, not
findings.

```json
{
  "source": "simplify",
  "status": "ok | failed | unavailable",
  "reason": "",
  "tests": "pass | fail | not-run",
  "reverted": false,
  "changes": [
    {
      "file": "buildconfig/converter.go",
      "summary": "extracted duplicated param-append into appendParam",
      "lines_added": 8,
      "lines_removed": 14
    }
  ],
  "revert_command": "git checkout -- buildconfig/converter.go"
}
```

Write it to `$SCRATCH/simplify.json` and return a one-line
count.

## Constraints

- Change only files already in this branch's diff. If `/simplify` touches a file the
  branch never modified, revert that file and note it — the change may be right, but it
  is not this branch's business.
- Never commit, never push, never create a branch. Never touch anything outside `$REPO`
  (the worktree) — the user's real checkout is elsewhere and stays untouched.
- Never `git add`. The orchestrator turns your edits into a patch; staging is not yours
  to do.
