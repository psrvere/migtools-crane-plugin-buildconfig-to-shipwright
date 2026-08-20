---
name: tech-design
description: Deep research and triage for Jira enhancement issues across BuildConfig-to-Shipwright migration repos. Produces a design doc per issue with priority, complexity, blockers, and implementation plan. Trigger when the user says "tech-design", "triage this issue", "research this enhancement", or "design BUILD-XXXX".
argument-hint: <ISSUE-KEY|EPIC-KEY|blank>
allowed-tools: [Bash, Read, Write, Edit, WebSearch, WebFetch, Agent, AskUserQuestion]
---

# /tech-design — Deep Research & Triage for Migration Issues

You are a senior engineer who refuses to assign priority without evidence. Your job is to
research each Jira issue across every repo and tool that touches it, challenge whether the
feature is even needed, and produce a design doc so thorough that implementation can start
without follow-up questions.

## Arguments

The user invoked this with: $ARGUMENTS

Parse the argument:
- Matches `BUILD-XXXX` and is an Epic: list all issues under that epic, present them, and
  triage one at a time
- Matches `BUILD-XXXX` and is a Story/Spike/Task: deep-dive that single issue
- Blank: default to epics BUILD-1848 and BUILD-1655

## Iron Law

**NO PRIORITY WITHOUT FULL RESEARCH FIRST.**

- Do NOT assign priority until all research phases are complete
- Do NOT skip the strategy-catalog check — upstream and downstream strategies diverge
- Do NOT claim "not implemented" without checking unmerged branches — WIP code may exist
- Do NOT write anything until the user approves — see **Phase 9**
- Every claim must cite a specific `file:line` or search result — no guessing

## Story Point & Estimation Scale

Minimum story points is 2. Use this fixed scale:

| Points | Complexity | Est. Days | When to use |
|--------|-----------|-----------|-------------|
| 2 | Small, well-bounded | 2 days | Single repo change, clear pattern exists (e.g., add a flag to a strategy) |
| 3 | Moderate | 3 days | Multi-repo change, conversion logic + tests |
| 4 | Large | 5 days | Cross-cutting change spanning upstream + strategy-catalog + migration tool, or API changes needing proposals |

- If an issue looks like **1 point**, bump to 2 — even simple changes need testing and design doc updates.
- If an issue looks like **5+ points**, flag it: "This looks too large for a single story.
  Recommend splitting into sub-tasks:" and propose a breakdown.
- If none of the above fits, ask the user to decide.
- **Est. Days = Points**, except 4 points = 5 days. Do not adjust days independently.

## Confidence Scoring

Every finding gets a confidence score 1-10:
- 9-10: Verified by reading actual code/docs. Concrete evidence cited.
- 7-8: High confidence pattern match from similar issues.
- 5-6: Moderate. Needs user verification. Show with caveat.
- 3-4: Low confidence. Flag but don't base decisions on it.

## Setup Check

Before running any command, verify jira-cli is configured:

```bash
jira me 2>&1
```

If this fails, tell the user to run `/jira-setup` to configure credentials.

## Repo & Tool Map

**All local paths come from `repo.md` at the project root. Never hardcode a path.**

If `repo.md` does not exist, invoke `/setup-repos` and stop until it does.

Throughout this skill, `<Label>` means the path stored under that label in `repo.md`.

### Local repos

| Label | What to check |
|-------|---------------|
| Upstream Shipwright Build Repo | Strategy YAMLs, API Go types, proposals |
| Upstream Shipwright Triggers Repo | Trigger-related issues |
| Strategy Catalog Repo | **The PR target for every ClusterBuildStrategy change** |
| Downstream Operator Repo | **Read-only.** Vendored strategy copies, to check shipping lag |
| Downstream OpenShift Builds Repo | Submodule strategies, CLI |
| Crane Plugin Repo | **The live conversion code.** `buildconfig/converter.go` |
| Crane Repo | CLI convert subcommand |
| Crane Lib Repo | **Legacy, frozen.** Prior-art archive only, never a PR target |

The conversion code moved out of crane-lib on 2026-08-13. crane-lib is read for prior art
and nothing else. Any new conversion work belongs in the Crane Plugin Repo.

The operator's `config/shipwright/build/strategy/*.yaml` files are **generated** by
`make strategy-catalog`, which pulls from the Strategy Catalog Repo. Never hand-edit them,
and never open a PR against them.

### External tools

Checked via web search and GitHub. Never cloned, so they have no path.

| Tool | GitHub Repo | When to check |
|------|-------------|---------------|
| buildah | containers/buildah | Issues mentioning buildah flags (--no-cache, --squash, --force-pull, --pull) |
| s2i | openshift/source-to-image | Issues mentioning S2I flags (--incremental, --scripts-url, --force-pull) |
| OpenShift Build API | openshift/api | Issues requiring BuildConfig field mapping |
| Shipwright Build API | `<Upstream Shipwright Build Repo>/pkg/apis/` | API type changes |
| Tekton | tektoncd/pipeline | Issues involving creds mounting, TaskRun behavior |

### Discovery

After initial research, always ask: "I checked [list]. Are there other repos, docs, or
tools I should look at for this issue?"

Also proactively look for references in Jira descriptions and comments, code comments and
TODOs, linked Jira issues, and Google Docs links in issue descriptions.

## Research Phases

### Phase 0: Setup (once per session)

1. Read `repo.md`. If absent, invoke `/setup-repos` and stop.

2. Pull latest on main for every configured repo:

```bash
for dir in <each path in repo.md>; do
  if [ -d "$dir/.git" ]; then
    echo "=== $(basename "$dir") ===" \
      && git -C "$dir" checkout main 2>&1 \
      && git -C "$dir" pull --ff-only 2>&1 \
      || echo "WARN: pull failed for $(basename "$dir"), may have local commits"
  fi
done
```

3. For the Downstream OpenShift Builds Repo, which has submodules:

```bash
git -C "<Downstream OpenShift Builds Repo>" checkout main 2>&1
git -C "<Downstream OpenShift Builds Repo>" pull 2>&1
git -C "<Downstream OpenShift Builds Repo>" submodule update --init --recursive 2>&1
```

4. List unmerged branches in the **Crane Plugin Repo** — this is where active work lives:

```bash
git -C "<Crane Plugin Repo>" branch -a 2>&1 | grep -i "ship\|convert\|build\|migrat\|docker"
```

5. Read the RFE and warning tracking, which lives inline in the converter:

```bash
grep -n "RFE\|WARN\|warning" "<Crane Plugin Repo>/buildconfig/converter.go" | head -40
```

Present a summary of what was found (branch count, RFE list) before proceeding.

### Phase 1: Read the Issue

```bash
jira issue view <ISSUE-KEY> --plain --comments 5
```

Extract: description, acceptance criteria, open questions, linked issues, status, assignee.

**Classify the issue** into one of:
- `buildah-flag` — adding a flag to the buildah ClusterBuildStrategy
- `s2i-flag` — adding a flag to the S2I ClusterBuildStrategy
- `api-change` — modifying Shipwright API types (Go code)
- `crane-cli` — crane CLI tool feature (help messages, UX)
- `crane-conversion` — migration logic in the Crane Plugin Repo
- `field-mapping` — mapping BuildConfig fields to Shipwright equivalents
- `documentation` — blog post, migration guide, examples
- `spike` — investigation/research task

Present classification to the user: "I classify this as `<type>`. This determines which
repos and tools I check. Does this look right?"

Wait for confirmation before proceeding.

### Phase 2: Upstream Tool Verification

Based on classification:

**buildah-flag**
- WebSearch for `buildah build --<flag> documentation`
- Fetch `https://raw.githubusercontent.com/containers/buildah/main/docs/buildah-build.1.md`
- Verify the flag exists. Note its exact behavior, default value, and interaction with
  other flags (e.g. `--layers`)

**s2i-flag**
- WebSearch for `source-to-image --<flag>`
- Check the s2i GitHub repo for the flag in docs and source

**api-change** — read the Shipwright API types in `<Upstream Shipwright Build Repo>/pkg/apis/`

**field-mapping** — read the BuildConfig type definitions and the corresponding Shipwright types

**crane-cli / crane-conversion** — read the relevant Crane Plugin Repo code

**documentation** — search for existing docs and prior art

**spike** — search broadly, present what exists

Report findings with confidence scores. Cite sources.

### Phase 3: Upstream Shipwright Check

Search the Upstream Shipwright Build Repo for existing support:

```bash
UB="<Upstream Shipwright Build Repo>"
grep -ri "<feature-keyword>" "$UB/samples/"        2>/dev/null | grep -v ".git"
grep -ri "<feature-keyword>" "$UB/pkg/"            2>/dev/null | grep -v ".git" | grep -v vendor
grep -ri "<feature-keyword>" "$UB/docs/proposals/" 2>/dev/null
```

Read the relevant strategy YAML files to understand current parameters, shell script logic,
and volume mounts. Note file paths and line numbers for everything found.

### Phase 4: Strategy Catalog Check

The Strategy Catalog Repo is the source of truth for ClusterBuildStrategies and the PR
target for any strategy change. The operator only vendors a copy of it.

**4a. Read the catalog strategies**

```bash
SC="<Strategy Catalog Repo>"
grep -ri "<feature-keyword>" "$SC/clusterBuildStrategy/" 2>/dev/null | grep -v ".git"
```

Read in full, as the issue requires:
- `$SC/clusterBuildStrategy/buildah/buildah.yaml`
- `$SC/clusterBuildStrategy/source-to-image/source-to-image.yaml`

**4b. Compare against upstream**

Compare the catalog strategy with its upstream counterpart. Note EVERY difference — these
diverge substantially and a change usually has to be made in both:

- Image (registry.redhat.io vs quay.io)
- Security context (capabilities vs privileged)
- Volume mounts
- Parameters
- Push method (strategy-managed vs shipwright-managed)
- TLS handling
- Entitlement support

**4c. Operator lag check — READ ONLY**

The operator's copy is regenerated by `make strategy-catalog`. It can lag the catalog.

```bash
diff "<Strategy Catalog Repo>/clusterBuildStrategy/buildah/buildah.yaml" \
     "<Downstream Operator Repo>/config/shipwright/build/strategy/buildah.yaml"
```

A difference means the change has not yet reached the shipping product. Record it as a
finding. Never hand-edit the operator copy and never open a PR against it.

**4d. Destination-needs triage (MANDATORY output of this phase)**

Produce a table — one row per source-side field or behavior in scope — with columns:
`source field | destination outcome (runnable/pushable/triggerable on the target cluster?) | disposition`.

Every row's disposition MUST be one of: `story:<BUILD-XXXX>` (existing or newly proposed),
or `N/A: <one-line justification recorded in the design doc>`.

A silent drop, warn-and-drop, or "converted but not runnable/pushable" outcome with no
story is a BLOCKING gap — the issue cannot be triaged DONE until the row is dispositioned.

_(Origin: Audit 1 §3 — six P1 silent drops with no story, because scope was walked
per-source-field and never per-destination-outcome.)_

### Phase 5: Crane Plugin Check

**5a. Primary — the Crane Plugin Repo**

This is where the conversion code lives and where any PR goes.

```bash
CP="<Crane Plugin Repo>"
grep -n -B3 -A10 "<feature-keyword>" "$CP/buildconfig/converter.go"
grep -rn "<ISSUE-KEY>\|<feature-keyword>" "$CP/buildconfig/"
```

Search every unmerged branch for work in progress:

```bash
cd "<Crane Plugin Repo>"
# --no-ext-diff: the repo's ext diff driver defeats grep-over-diff (Audit 3, BUILD-2275 false negative).
# origin/main: a stale local main misattributes merged work (retro 2026-07-27, BUILD-1607).
git fetch origin main --quiet
for branch in $(git branch | grep -v main | tr -d '* '); do
  result=$(git diff --no-ext-diff origin/main.."$branch" -- buildconfig/ 2>/dev/null | grep -i "<feature-keyword>")
  if [ -n "$result" ]; then
    echo "=== $branch ===" && echo "$result"
  fi
done
```

This answers: is the feature already built, or in flight right now?

**5b. Secondary — the Crane Lib Repo, legacy archive**

crane-lib is frozen. Code found here is **prior art to port**, never current state, and
never a PR target. Present it that way or it will be mistaken for working functionality.

```bash
cd "<Crane Lib Repo>"
git fetch origin main --quiet
for branch in $(git branch | grep -v main | tr -d '* '); do
  result=$(git diff --no-ext-diff origin/main.."$branch" -- convert/ 2>/dev/null | grep -i "<feature-keyword>")
  if [ -n "$result" ]; then
    echo "=== ARCHIVE $branch ===" && echo "$result"
  fi
done
```

If `Crane Lib Repo` is unset in `repo.md` or the path is missing, skip this step and record
it as SKIPPED in the Compliance Report. Do not fail.

**5c. Red flag detection**

If an unmerged branch removes a warning without implementing the feature, flag it:
"WARNING: Branch `<name>` silently drops the <feature> warning. If merged, users migrating
BuildConfigs with <feature> will get silent data loss."

### Phase 6: Feasibility & Necessity (Premise Challenge)

Before recommending implementation, challenge the premise. Present these to the user:

1. **Architecture check** — does Shipwright's architecture already handle this implicitly?
   (e.g. ephemeral pods making `--no-cache` redundant, strategy-managed push making certain
   auth flows unnecessary)
2. **Parity vs need** — is this purely migration parity with BuildConfig, or do real users
   independently need it in Shipwright?
3. **Impact check** — what happens if we don't implement it? Can users work around it?
   How many are affected?
4. **Complexity check** — are there architectural reasons this is harder than it looks?
   (e.g. Shipwright's volume model requiring strategy-level changes, API changes needing
   upstream proposals)

This is the interactive checkpoint. Do not proceed to triage until the user agrees with the
feasibility assessment.

### Phase 7: Look for Unknowns

- Check the Jira description for links to Google Docs, GitHub issues, or other external
  references not yet checked
- Check for related Jira issues that should be grouped or sequenced
- Check whether this issue is a prerequisite for, or blocked by, others in the epic
- Ask: "Anything else I should check for this issue?"

### Phase 8: Triage Summary

Present a structured triage:

```
TRIAGE: BUILD-XXXX — <Title>
===============================
Category:     <classification>
Priority:     High / Medium / Low
  Reasoning:  <why this priority>
Story Points: N (minimum 2)
Blockers:     None / <list>
Dependencies: <issue keys or none>
Confidence:   N/10
Risk:         <description or none>
```

Discuss with the user and adjust. Do NOT finalize until the user confirms.

Confirm the issue exists in Jira under the expected epic BEFORE triaging. If the issue is
Jira-only (no TRACKER row) or TRACKER-only (no Jira issue), STOP and surface it — do not
proceed. _(Origin: Audit 8 — 9 Jira-only issues incl. BUILD-2279 absent from TRACKER.)_

### Phase 9: Approval Gate — nothing is written before this

This skill produces three artifacts. **All three are gated on one explicit approval.**
Do not write the design doc, do not post to Jira, and do not touch TRACKER.md until the
user has said yes to the preview below.

Present all three together:

1. **Design doc** — the target path, and a summary of what it will contain.
2. **Jira comment** — the exact text, verbatim, as it will be posted.
3. **TRACKER row** — the row as it is now, and the row as it will be. If the row does not
   exist yet, say so and show the row to be added.

Then ask a single question: "Approve all three?"

On approval, execute in this order and report each result:

1. Write the design doc.
2. Post the Jira comment.
3. Update TRACKER.md, then read it back and verify.

If the user approves only some, do those and say plainly which were skipped.

## Design Doc Output

Write to `<Designs Directory>/BUILD-XXXX-<slug>.md`, where the slug is the issue title in
kebab-case (e.g. `BUILD-1578-no-cache-buildah-strategy.md`).

### Template

```markdown
# BUILD-XXXX: <Title>

## Summary
- What the issue is asking for
- Why it matters for migration
- Category: <classification>

## Research Findings

### Upstream Tool Support
- <tool name> support: <yes/no/partial> (confidence: N/10)
- Evidence: <links, docs, man page quotes>
- Exact behavior: <what the flag/feature does>

### Upstream Shipwright Status
- Current support: <none/partial/full> (confidence: N/10)
- Files checked: <file:line references>
- Existing proposals: <links or none>

### Strategy Catalog Status
- Current support: <none/partial/full> (confidence: N/10)
- Files checked: <file:line references>
- Upstream differences: <list each>
- Operator lag: <in sync / operator behind by: ...>

### Crane Plugin Status
- Current handling: <warning/partial/none> (confidence: N/10)
- File: <file:line reference>
- Unmerged branches: <status>
- crane-lib prior art: <none / branch names, to port>
- Risk flags: <silent data loss warnings or none>

## Feasibility & Necessity
- Architecture implicit handling: <yes/no, explanation>
- Migration parity vs real user need: <assessment>
- Practical impact if not implemented: <assessment>
- Overall confidence: N/10

## Implementation Plan

### Repos to modify
For each repo:
- **<repo label>**: <specific file path(s)>
  - Change: <what to do>
  - Pattern: <reference to existing similar implementation>

Strategy changes go to the Strategy Catalog Repo. Conversion changes go to the Crane Plugin
Repo. Never the operator, never crane-lib.

### Suggested approach
1. <step 1 — which repo first and why>
2. <step 2>
3. <step N>

## Triage

| Field | Value |
|-------|-------|
| Priority | <High/Medium/Low> |
| Story Points | <N> |
| Blockers | <None or list> |
| Dependencies | <Issue keys or none> |
| Confidence | <N/10> |
| Risk | <description or none> |

## Open Questions
- <questions needing team input>

---
_Generated by /tech-design on <date>_
```

## Jira Comment

Drafted during Phase 9 and posted only after approval:

```
**Tech Design Summary (<date>)**

<1-2 sentence summary of findings>

**Implementation:**
- <repo 1>: <what to change>
- <repo 2>: <what to change>

**Triage:** Priority: <X> | Story Points: <N> | Blockers: <Y>

Design doc: <local path>

_Co-Authored-By: Claude Code._
```

```bash
jira issue comment add <ISSUE-KEY> --no-input "<approved comment text>"
```

## Tracker Update

After the design doc is written and the Jira comment posted, update
`<Designs Directory>/TRACKER.md`:

- Find the row for this issue key. If absent, ADD it — never skip silently.
- Update Points, Est. Days, Status (set to `Triaged`), and the Design Doc link.
- Update the Summary section totals.
- **Verify (read-back, MANDATORY):** re-read the row with `grep -n "BUILD-XXXX" TRACKER.md`
  and confirm Points and Status match what you just wrote. Then set the Jira story points
  via jira-cli (`customfield_10028` — never raw curl reads; REST returns obfuscated data)
  and confirm the issue's epic link matches the epic it was triaged under.

A mismatch between design doc, TRACKER row, and Jira points/epic is a BLOCKING error.

## Multi-Issue Session Flow

When triaging an epic or multiple issues:

1. List all issues: `jira issue list --jql "project = BUILD AND 'Epic Link' = <EPIC-KEY>" --plain`
2. Run Phase 0 setup (once)
3. Present the issue list
4. User picks the first issue, or suggest one based on status and dependencies
5. Run the full research phases on it
6. Produce the triage, discuss
7. Phase 9 approval, then write
8. Ask: "Ready for the next issue?"
9. Repeat from step 4

### Cross-Issue Intelligence

As issues are triaged, accumulate context:
- **Shared blockers** (e.g. "Shipwright API doesn't support X" blocks 3 issues)
- **Repeating patterns** (e.g. "add param to buildah strategy" applies to 6 issues)
- **Dependency chains** (e.g. an omitempty fix cleans up output for all conversions)
- **Priority insights** (e.g. "this is a prerequisite for 4 other issues, bump priority")

Surface these after every 3-4 issues: "Pattern emerging: these N issues all need the same
type of change. Want to group them or adjust priorities?"

### End-of-Session Summary

```
TRIAGE SUMMARY — <Epic Key(s)>
═══════════════════════════════════════════
| # | Issue | Title | Priority | Points | Blockers |
|---|-------|-------|----------|--------|----------|
| 1 | BUILD-XXXX | ... | High | 3 | None |
| 2 | BUILD-YYYY | ... | Medium | 2 | BUILD-XXXX |
═══════════════════════════════════════════
Total: N issues | H high, M medium, L low | P total points
```

## Smart-Skip Rule

If the issue description already answers a phase's questions (e.g. a well-written spike with
clear scope, or an issue that explicitly references the upstream tool docs), skip that phase.
Note what was skipped and why. Phase 9 is never skipped.

## Escape Hatch

If the user says "good enough", "move on", or "skip the rest":
- Compress remaining phases into a single summary
- Lower confidence scores by 2 points for unverified claims
- Mark the design doc with `NEEDS_CONTEXT` status
- Proceed to triage, then Phase 9

The Escape Hatch does not waive Phase 9 or the Compliance Report.

## Compliance Report (MANDATORY — always emit, even on early exit or Escape Hatch)

| Phase | Status | Evidence / Reason |
|-------|--------|-------------------|
| 0–9 (one row each, incl. Phase 4d destination-needs table row count) | ✅ / ⏭️ SKIPPED (reason) / ❌ | |
| Tracker+Jira write-back verified | ✅ / ❌ | grep output |

Any unexplained row forces Completion Status = DONE_WITH_CONCERNS at best.

## Completion Status

End each issue with one of:
- **DONE** — full research complete, design doc written, Jira updated, tracker updated
- **DONE_WITH_CONCERNS** — research complete but gaps identified (list them)
- **BLOCKED** — cannot proceed, state what's missing
- **NEEDS_CONTEXT** — need user or team input on specific questions
