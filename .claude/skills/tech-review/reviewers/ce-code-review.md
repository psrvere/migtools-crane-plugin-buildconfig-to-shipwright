---
name: ce-code-review
description: >-
  Conditional escalation. Runs compound-engineering's ce-code-review in
  report-only mode over a large or high-risk diff and normalises its output.
model: sonnet
tools: Bash, Read, Skill
---

# CE code review escalation

You run `compound-engineering:ce-code-review` and translate its output into this skill's
findings schema.

> **Run this at the orchestrator level, not as a wrapped sub-agent.** `ce-code-review` is
> itself a fan-out skill that spawns its own persona sub-agents. A general-purpose wrapper
> that invokes it returns before those grandchildren finish and writes no JSON — the
> escalation silently produces nothing (observed twice in practice). So the tech-review
> orchestrator invokes the Skill tool **directly** and follows the mapping steps below
> itself. Treat this file as the orchestrator's instructions for the escalation, not a
> sub-agent prompt. Only if a future harness forces a wrapper, that wrapper must poll
> `$SCRATCH/ce-code-review.json` until it appears instead of ending its turn.

You are dispatched only when the diff is large or touches something risky. The
orchestrator evaluates that threshold before spawning you, so if you are running, the
diff has already earned the cost.

That cost is real: this skill selects from fourteen reviewer personas and spawns them in
parallel, so you are a fan-out inside a fan-out. Do not run it a second time or widen its
scope.

**Own:** Deep multi-persona review of a diff that warrants it — security, reliability,
API contracts, data migrations, and the always-on correctness and testing personas.

**Do not own:** Applying fixes. Committing. Running when the threshold was not met.

## Procedure

1. Invoke `compound-engineering:ce-code-review` through the Skill tool with
   **`mode:agent`**.

   `mode:agent` is required, not optional. Its default mode applies fixes and commits
   them when the tree is clean. `mode:agent` makes it report-only and returns JSON, which
   is what this pipeline needs.

   Pass `base:<merge-base-sha>` so it reviews this branch's true delta rather than
   detecting a base itself.

   **Run it against the review worktree `$WT`.** The `base:` fast path computes
   `git merge-base HEAD <base>` in the *current* working directory. If the current checkout
   is on some other branch (it usually is — the branch under review lives in `$WT`, a
   detached worktree), it will diff the wrong tree and review the wrong code. Set the
   working directory to `$WT` before invoking, or otherwise make ce-code-review diff `$WT`.
   Its personas inherit the session model; if that model is unavailable to sub-agents on
   this deployment, ce-code-review's own reviewers fall back — but if the whole invocation
   fails on model availability, note it and do not report a clean escalation.

2. If the skill is not available — the compound-engineering plugin is not installed, or
   is installed but not enabled — write:

   ```json
   { "source": "ce-code-review", "status": "unavailable",
     "reason": "compound-engineering plugin not installed or not enabled",
     "findings": [] }
   ```

   and return. Do not inspect the plugin cache directory to find out why. That path is
   version-stamped, is Claude Code internals, and cannot tell you whether a plugin is
   enabled. Trying and reporting the result is the honest check.

3. Its JSON already carries severity and confidence. Map them onto this schema rather
   than re-deriving them. ce-code-review emits the `P0`–`P3` scale; older builds emit
   qualitative labels. Handle both:
   - `P0`, `P1`, `critical`, `high` → `blocker`
   - `P2`, `medium` → `warning`
   - `P3`, `low`, `info` → `info`

4. It reviews whole files for context, so it will report problems on lines the branch
   never touched. Classify scope by the line, not the file: a changed-file list cannot tell
   you whether a given line is in the diff. Check each finding's line against the
   post-`/simplify` hunks and mark it `pre-existing` only when the line falls outside them:

   ```bash
   git -C "$WT" diff --no-ext-diff --unified=0 "$MERGE_BASE" -- "$file"
   ```

   Diff `$WT` (which includes `/simplify`'s uncommitted edits), not the committed branch.

## Output format

The schema in `findings-schema.md`, with `source: "ce-code-review"`.

Write to `$SCRATCH/ce-code-review.json` and return a one-line
count.

## Constraints

- Always `mode:agent`. Never the default mode.
- Never pass a PR number or a branch name as a target — that would let it change review
  scope in ways the orchestrator did not intend. Use `base:` and the current checkout.
- Never let it check out, switch, or stage anything. It states it will not, but this
  checkout may be shared, so verify the tree is unchanged when it returns and report a
  discrepancy as `status: failed`.
- Do not deduplicate against the other reviewers' findings. Overlap is expected; the
  orchestrator and the challenger handle it.
