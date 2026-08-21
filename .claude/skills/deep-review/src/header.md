---
name: deep-review
description: Deep multi-agent PR review — triages the change, dispatches up to 6 specialised sub-agents in parallel (correctness, security, intent-coherence, style, docs, cross-repo contracts) behind a security-triage pre-pass, then runs an adversarial challenger pass to strip false positives and produces a severity-ranked verdict. Report-only by default. Trigger on "deep-review", "deep review this PR", "fan-out review", or "adversarial review". Reviews an open PR by number or URL; it has no local-branch mode.
argument-hint: <pr-url|pr-number> [--post] [--only=correctness,security,...]
allowed-tools: [Bash, Read, Grep, Glob, Agent, AskUserQuestion]
user_invocable: true
---

# /deep-review — Multi-Agent PR Review

<!-- ══════════════════════════════════════════════════════════════════════ -->
<!-- GENERATED FILE — DO NOT EDIT.                                          -->
<!-- Built by bin/sync from:  src/header.md + vendor/SKILL.md               -->
<!-- Edit src/header.md instead, then run: .claude/skills/deep-review/bin/sync --build -->
<!-- ══════════════════════════════════════════════════════════════════════ -->

This skill runs the fullsend PR-review orchestrator (vendored verbatim under
`vendor/`, Apache-2.0 — see `vendor/NOTICE`) outside the fullsend harness.

**The LOCAL OVERRIDES section immediately below supersedes anything in the
vendored orchestrator that follows it.** Where the vendored text and an
override disagree, the override wins. Everything the overrides do not mention
is followed exactly as written upstream.

---

## LOCAL OVERRIDES (authoritative — read before the orchestrator below)

### O1. Paths — where the vendored files actually live

The vendored orchestrator refers to files by their upstream layout. Use these
paths instead. `$SKILL_DIR` is the directory containing this file.

| Orchestrator says | Read this instead |
|---|---|
| `sub-agents/{name}.md` | `$SKILL_DIR/vendor/sub-agents/{name}.md` |
| `meta-prompt.md` | `$SKILL_DIR/vendor/meta-prompt.md` |
| `../docs-review/SKILL.md` | `$SKILL_DIR/vendor/docs-review/SKILL.md` |
| the forge skill / `pr-review/github` | `$SKILL_DIR/vendor/github/SKILL.md` |
| `agents/review.md` (agent definition) | `$SKILL_DIR/vendor/agent-review.md` |

Resolve `$SKILL_DIR` once at the start:

```bash
SKILL_DIR="$(git rev-parse --show-toplevel)/.claude/skills/deep-review"
ls "$SKILL_DIR/vendor/sub-agents/" || { echo "vendor/ missing — run bin/sync"; exit 1; }
```

`vendor/agent-review.md` is the agent definition the orchestrator calls
authoritative for prohibitions and the output schema. Read it before step 1 and
honour it — **except** where these overrides say otherwise.

### O2. Always interactive mode — never pipeline mode

`$FULLSEND_OUTPUT_DIR` is never set here. Per the vendored skill that means
**interactive mode**. Consequences:

- Do **not** write `agent-result.json`. Render the review to the terminal.
- Do **not** run `fullsend-check-output` — it is not installed. Skip that step
  entirely rather than trying to substitute a validator.
- Ignore every instruction addressed to "the post-script". There is no
  post-script; see O3 for what replaces its one safety-critical job.

### O3. Report-only by default — posting requires `--post` AND confirmation

This repo's `origin` is the shared upstream `migtools/crane-plugin-buildconfig-to-shipwright`.
Posting a review is an outward-facing action against someone else's PR.

- **Default (no `--post`):** print the full review to the terminal. Post
  nothing. Apply no edits. Create no comments. This is the safe default and is
  what you do unless the user typed `--post`.
- **With `--post`:** show the exact review body and the intended verdict, then
  ask for explicit confirmation via AskUserQuestion before running
  `gh pr review`. Never post without that confirmation.

**Protected-path rule (replaces the post-script's enforcement).** Upstream
relies on `post-review.sh` to downgrade `approve` → `comment` when a PR touches
sensitive paths. That script does not exist here, so enforce it yourself:

> If the PR touches any of `.claude/`, `.github/`, `AGENTS.md`, `CLAUDE.md`,
> `Makefile`, `go.mod`, `go.sum`, or `LICENSE`, you may **never** emit
> `approve`. Downgrade to `comment` and say why in the summary.

This is a hard rule, not a heuristic. It holds even with `--post` and even if
every sub-agent returned clean.

**Deliberate divergence from upstream.** `vendor/agent-review.md` requires
`request-changes` when a protected-path change is not justified; this override
instead caps the verdict at `comment`. That is intentional: this skill has no
app identity here and posts as a human collaborator on someone else's PR, so the
conservative direction is to flag and let a human decide, never to block. Keep
the cap. If upstream's protected-path text changes, this paragraph is the thing
to re-read, not a bug to reconcile.

### O4. Model mapping

Sub-agent frontmatter uses Vertex model IDs. Map them when calling the Agent
tool; the frontmatter itself stays unmodified.

| Frontmatter `model:` | Agent tool `model` | Sub-agents |
|---|---|---|
| `opus` | `opus` | correctness, security, challenger |
| `claude-sonnet-4-6@default` | `sonnet` | intent-coherence, style-conventions, docs-currency, cross-repo-contracts |
| `haiku` | `haiku` | security-triage |

Dispatch every sub-agent with `subagent_type: general-purpose`. The sub-agents
are **prompt content, not registered agents** — compose the prompt from the file
body exactly as the vendored step 4 describes. Do not look for them in the
agent registry.

The vendored constraint *"All sub-agents MUST be dispatched simultaneously —
include all Agent calls in a single message"* still applies and matters: issue
every parallel Agent call in one assistant turn.

### O5. Forge is always GitHub

`FULLSEND_FORGE` is unset. Treat it as `github` unconditionally and use
`vendor/github/SKILL.md` for all data fetching. Skip every GitLab branch.

Derive the required variables from the argument before step 1:

Take the number that follows `/pull/` — **never** the last number in the string.
Real GitHub URLs routinely end in some other number (`?w=1` is the hide-whitespace
toggle; `/commits/<sha>` and `#pullrequestreview-<id>` both end in digits), and in
an established repo that number is usually itself a valid PR. The existence check
then passes and the entire review silently runs against the wrong PR.

```bash
# $ARGUMENTS is a PR URL or a bare PR number. Branch on which, because a bare
# "#28" is all fragment — stripping #... unconditionally would empty it.
ARG="$ARGUMENTS"
case "$ARG" in
  *://*|*/pull/*)                           # URL: drop ?query and #fragment
    ARG="${ARG%%#*}"; ARG="${ARG%%\?*}"
    PR_NUMBER="$(printf '%s' "$ARG" | sed -nE 's@.*/pull/([0-9]+).*@\1@p')" ;;
  *)                                        # bare number, optionally "#28"
    PR_NUMBER="$(printf '%s' "$ARG" | tr -d '#[:space:]' | grep -xE '[0-9]+')" ;;
esac
# Refuse rather than guess: a URL with no /pull/ segment (an issue link, a repo
# root) is not a PR reference, even though it may well end in digits.
[ -n "$PR_NUMBER" ] || { echo "could not read a PR number from: $ARGUMENTS"; exit 1; }

# A full URL naming a different repo wins over the local checkout.
REPO_FULL_NAME="$(printf '%s' "$ARG" | sed -nE 's@^https?://[^/]+/([^/]+/[^/]+)/pull/.*@\1@p')"
[ -n "$REPO_FULL_NAME" ] || REPO_FULL_NAME="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
export PR_NUMBER REPO_FULL_NAME

PR_INFO="$(gh pr view "$PR_NUMBER" --repo "$REPO_FULL_NAME" \
  --json number,title,state --jq '"\(.state)\t\(.title)"' 2>/dev/null)"
[ -n "$PR_INFO" ] || { echo "PR #$PR_NUMBER not found in $REPO_FULL_NAME"; exit 1; }
PR_STATE="${PR_INFO%%$'\t'*}"; PR_TITLE="${PR_INFO#*$'\t'}"

# Always show what was resolved — this is the only chance to notice a bad parse.
echo "reviewing $REPO_FULL_NAME#$PR_NUMBER — $PR_TITLE ($PR_STATE)"
[ "$PR_STATE" = "OPEN" ] \
  || { echo "PR #$PR_NUMBER is $PR_STATE — this skill reviews open PRs only."; exit 1; }
```

Report the resolved `owner/repo#number — title (state)` line to the user before
dispatching anything. Do not proceed on a non-`OPEN` PR.

### O6. Repo context injected into every sub-agent

Append this to the **Context package** (vendored step 3d, Part 4) for every
sub-agent. It is repo truth the upstream prompts cannot know.

Read `$(git rev-parse --show-toplevel)/AGENTS.md` and include it, then add this
verbatim block:

```markdown
### Repo invariants (migtools/crane-plugin-buildconfig-to-shipwright)

- Go module `github.com/migtools/crane-plugin-buildconfig-to-shipwright`.
- **CI parity:** CI builds this module standalone. The local `go.work` resolves
  dependencies across sibling modules and hides breakage. The authoritative
  check is `GOWORK=off go test ./... -count=1`. A finding that only reproduces
  under the workspace is local noise — do not report it.
- **controller-runtime skew:** crane-lib pins v0.21.0 while the workspace
  resolves v0.23.x. Any use of v0.22+ API (notably `client.Client.Apply` and
  `runtime.ApplyConfiguration`) compiles locally and breaks CI. Flag it as
  **high**.
- crane-lib is pinned to a published pseudo-version and there is **no**
  `replace` directive in `go.mod`. A PR adding `replace => ../crane-lib` is a
  **high** finding.
- **`AGENTS.md` is stale on this point.** It states in two places that a
  `replace` directive exists and must be maintained. That has not been true
  since the pin moved to a published pseudo-version containing `NewResources`.
  `go.mod` is authoritative; do not raise a finding that the code contradicts
  `AGENTS.md` here. Flagging the stale `AGENTS.md` text itself is valid and
  belongs to `docs-currency`.
- `crane-lib/convert/` is legacy and frozen for this effort. New conversion
  logic belongs in `buildconfig/`, not `convert/`.
```

**`style-conventions` specifically:** its job here is to audit against
`AGENTS.md` and the conventions actually visible in the surrounding code — not
to apply generic Go style opinions. State that explicitly in its context
package. Do not report lint-class nits that `gofmt` or `go vet` would catch.

### O7. Sub-agent selection

Follow the vendored step 3c selection rules. Two additions:

- `cross-repo-contracts` is **high value in this repo** — the crane-lib
  dependency boundary is exactly its remit. Dispatch it whenever `go.mod`,
  `go.sum`, or anything under `buildconfig/` that crosses the crane-lib API
  changes, not only on public-API changes.
- `--only=a,b,c` restricts dispatch to the named sub-agents. `challenger` still
  runs afterwards unless explicitly excluded. Use this to test cheaply.

### O8. Re-review context

Upstream pre-fetches a prior review via `pre-fetch-prior-review.sh` and passes
both `PRIOR_REVIEW_SHA` and `PRIOR_REVIEW_PROVENANCE`, discarding any prior
review it cannot attribute to the expected author
(`vendor/agent-review.md:39-50`, `vendor/SKILL.md:193-195`). There is no
pre-script here, so do **both** halves inline. The provenance half is not
optional: prior findings feed severity anchoring in `vendor/meta-prompt.md`, so
mistaking someone else's comment for a prior run corrupts this run's severities.

**Only a review this skill wrote counts as a prior review.** The newest review by
*anyone* does not — a human "LGTM" posted after our review would become the most
recent one. We have no app identity to check (posting goes through `gh pr review`
as the user), so the hidden head-SHA marker from vendored step 7 is the
discriminator: it is on the first line of every review this skill produces.

```bash
# Newest review carrying our marker — not simply the newest review.
PRIOR_REVIEW="$(gh pr view "$PR_NUMBER" --repo "$REPO_FULL_NAME" --json reviews \
  --jq '[.reviews[] | select(.body | test("^<!-- \\*\\*Head SHA:\\*\\*"))] | last | .body // empty' \
  2>/dev/null | head -200)"
PRIOR_REVIEW_SHA="$(printf '%s' "$PRIOR_REVIEW" \
  | sed -nE '1s/.*Head SHA:\*\* ([0-9a-f]{7,40}).*/\1/p')"
```

If `PRIOR_REVIEW` is empty — no marker found — treat every "prior findings" slot
as `"none — first review"` and continue. Never fall back to the newest review by
an arbitrary author.

### O9. Reporting

End with a summary the user can act on:

```
DEEP REVIEW — <repo>#<pr>  <title>
──────────────────────────────────────────────
Dispatched   : <sub-agents, with model tier>
Raw findings : N     After challenger: M   (removed: N-M)
Verdict      : approve | comment | request-changes | reject
Protected    : <paths, or "none">
──────────────────────────────────────────────
<findings, critical → info, each with file:line and remediation>
```

Always print what the challenger **removed** and why. That log is the main
signal for whether the review is over- or under-firing, and it is the first
thing to tune.

### O10. Severity threshold — report everything

`vendor/agent-review.md:54-57` marks `$REVIEW_FINDING_SEVERITY_THRESHOLD` as
**required**, supplied by `harness/review.yaml`, and says callers running outside
that harness must set it themselves. We are such a caller, and nothing else here
sets it — which would leave both the severity filter and the
`request-changes` → `comment` downgrade it triggers undefined.

> Treat `$REVIEW_FINDING_SEVERITY_THRESHOLD` as **`info`**, the lowest severity
> in upstream's `info < low < medium < high < critical` order. Suppress nothing.
> Because nothing is ever filtered out, the rule that downgrades a
> `request-changes` or `reject` verdict when filtering empties the findings array
> can never fire — ignore it.

Report-only is already the default (O3), so the useful failure mode here is
showing too much rather than too little. If that becomes noisy, raise this to
`low` rather than reintroducing an unset variable.

---

<!-- ══════════════════════════════════════════════════════════════════════ -->
<!-- BELOW THIS LINE: vendored verbatim from fullsend-ai/agents             -->
<!-- skills/pr-review/SKILL.md @ ee30be60 — Apache-2.0, see vendor/NOTICE   -->
<!-- Do not edit here. Edit src/header.md or re-run bin/sync.               -->
<!-- ══════════════════════════════════════════════════════════════════════ -->

