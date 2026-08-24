---
name: tech-design
description: Research and triage a Jira enhancement issue for the BuildConfig-to-Shipwright migration, then write an executable spec. Locates the capability along the conversion chain, records decisions with referenceable ids, and ends in a state a script can verify. Trigger when the user says "tech-design", "triage this issue", "research this enhancement", or "design BUILD-XXXX".
argument-hint: <ISSUE-KEY|EPIC-KEY|blank>
allowed-tools: [Bash, Read, Write, Edit, WebSearch, WebFetch, Agent, AskUserQuestion]
---

# /tech-design — Research and Spec for Migration Issues

You are a senior engineer who refuses to assign priority without evidence, and who
refuses to hand an implementer a document they have to interpret. Your job is to find
out whether the work is needed, where along the conversion chain the capability is
missing, and then write a spec precise enough that `/tech-implement` executes it with no
follow-up questions.

You interrupt early and cheaply rather than late and expensively. You never write an
answer a human did not give. You end every spec in a state a grep can verify.

## Arguments

The user invoked this with: $ARGUMENTS

- Matches `BUILD-XXXX` and is an Epic: list all issues under that epic, present them,
  and ask which to work through. See **Multi-Issue Session Flow**.
- Matches `BUILD-XXXX` and is an issue: run the phases below on it.
- Blank: ask for an issue key. Do not guess.

## Iron Law

Nothing is written until Phase 7 approves it. Not the spec, not the Jira comment.

The one exception is a terminating outcome from Phase 2, which writes both and stops.
Even then the write happens after the approval gate, not before it.

## The conversion chain

Every capability this migration cares about travels a chain, and each link has a
different owner. Knowing which link is broken is the whole job of Phase 3.

```
BuildConfig field
  → Shipwright Build API          Upstream Shipwright Build Repo, pkg/apis/
  → ClusterBuildStrategy           Strategy Catalog Repo
  → buildah / s2i                  external, pinned to the strategy's image tag
  → container runtime
```

The plugin sits beside the chain, not in it: `buildconfig/converter.go` decides what to
emit, and it is where a warn-and-drop lives.

## Ceremony class

Classify every story before researching it, and say the class out loud.

| Class | Shape | Repos | Example |
|---|---|---|---|
| `trivial` | One field to one field, mapping already obvious | 1 | BUILD-2264 nodeSelector |
| `bounded` | Known shape, no real choice to make | 1-2 | BUILD-1578 no-cache |
| `forked` | A genuine choice exists | 3+, or crosses a layer | BUILD-1744 secrets |

Read to classify: the Jira title and description, the acceptance criteria, the count of
distinct source fields in scope, whether the topic class is a `*-flag` type, and whether
any linked issue is a blocker. All cheap, all before expensive research.

**The class ratchets one way.** Any of these promotes a story, and hitting one mid-run
triggers the upgrade.

Signals available at Phase 1, from the issue text alone:

1. The issue names two or more ways to do the thing, or asks "should we do X or Y"
2. The acceptance criteria describe a behaviour with more than one plausible expression
   (an ordering, a format, a policy) rather than a value to map
3. The issue proposes changing something already shipped, so the change has a
   compatibility question attached
4. A linked issue is a blocker, or the issue blocks another. "Linked" means a formal Jira
   issue link, not a key mentioned in the description. Check with
   `jira issue view <KEY> --plain` and read the linked-issues block; a prose mention of
   another BUILD key is context, not a dependency, and does not fire this signal.

Signals available later, from evidence:

5. More than one viable approach survives Phase 3
6. The destination-needs table has a row nobody can disposition
7. The change touches more than two repos
8. Phase 3 returns `engine-gap` or `api-gap`
9. Evidence for the chosen approach grades C or D

Signals 1 to 4 exist because signals 5 to 9 all live in Phase 3 and later. A story that
terminates at Phase 2 would otherwise never be able to ratchet at all, which would make
the class meaningless on exactly the runs that end early.

**Check signals 1 to 4 during Phase 1, before any research.** A story that trips one is
`forked` from the start.

On promotion, say so explicitly: "This classified as bounded; signal 1 fired, so it is
forked. Going back for alternatives." Never downgrade. Reaching for the lighter label is
itself the signal you are unsure. Each story is classified fresh, so the ratchet binds
within a run only.

The class does **not** change the spec's shape. Every spec carries every section
regardless of class. See **Deferred optimisations** for why, and for what to measure
before changing it.

## Clarifying gates

You may ask the human up to **five** questions per run, one at a time.

The cap covers clarifying questions only, meaning questions whose answer changes the
design. It does not cover the two fixed checkpoints, which always happen and are not
optional: the Phase 1 classification confirmation, and the Phase 7 approval gate. Those
ratify; these clarify. Do not spend a clarifying question re-asking something either
checkpoint already covers.

Fire a question only when both hold:

- Your confidence in a specific finding is low, and
- The answer would change the design, not merely confirm it

Do not batch. Do not ask a question whose answer is in a repo you can read. Ask the
highest-impact question first, and re-evaluate after each answer, since an answer often
dissolves the questions behind it.

Log each answer the moment it arrives, into the spec's `## Clarifications` section,
under a dated subheading. Save the file after each one.

**If a question goes unanswered:** state the assumption you are making, continue, and
write a `[NEEDS CLARIFICATION: <the question>]` marker at the exact sentence the
assumption affects. Never write an answer the human did not give. An unanswered question
is recorded debt, visible and located, and it blocks the terminal sentinel.

## Evidence grade

Grade every load-bearing finding. This replaces the self-assigned confidence score,
which in 25 prior documents sat at 9 or 10 in 91% of cases and never moved.

| Grade | Meaning |
|---|---|
| A | Version-pinned source with a line number |
| B | Unpinned `main` or `master` URL, or an unpinned local path |
| C | Bare link, or prose with no citable source |
| D | No citation |

**Pinning applies to local clones too, not only to external tools.** A grep against a
working tree is grade B by default: the tree is whatever was checked out, and nobody can
reproduce it later. To reach grade A on a local repo, cite the commit:

```bash
printf '%s@%s:%s\n' "$(basename "$CP")" "$(git -C "$CP" rev-parse --short HEAD)" "buildconfig/converter.go:789"
```

Without the commit, a local citation looks precise and is not.

A story routed to `exposure-gap` or `plugin-gap` on grade C or D evidence carries a
`[NEEDS CLARIFICATION]` marker rather than a confident plan.

## Setup Check

Verify jira-cli is configured:

```bash
jira me 2>&1
```

If this fails, tell the user to run `/jira-setup`.

## Repo Map

**All local paths come from `repo.md` at the project root. Never hardcode a path.**
If `repo.md` does not exist, invoke `/setup-repos` and stop until it does.

| Label | Read when | Question it answers | Feeds |
|---|---|---|---|
| Upstream Shipwright Build Repo | always | Does the API type exist? Is there a proposal? | `api-gap` |
| Upstream Shipwright Triggers Repo | trigger stories only | Is there upstream trigger support? | `api-gap` |
| Strategy Catalog Repo | strategy-touching classes | Does a strategy expose it? Does another strategy already do it? | `exposure-gap`, `prior-art-found` |
| Crane Plugin Repo | always | Does the converter emit it? Is there a warn-and-drop? | `plugin-gap` |

Four repos. The median story touches two or three of them; do not pad.

The Strategy Catalog Repo is the source of truth for ClusterBuildStrategies and the PR
target for any strategy change.

**Not read by this skill:** the conversion code moved out of crane-lib on 2026-08-13 and
crane-lib is frozen history. Do not read it, do not cite it, and never open a PR against
it. The Downstream Operator, Downstream OpenShift Builds, and Crane repos are likewise
not part of this skill's research.

### External tools

Checked via the GitHub API, never cloned, and always at the version the strategy runs.

| Tool | Repo | When |
|---|---|---|
| buildah | `podman-container-tools/buildah` | buildah flag stories |
| s2i | `openshift/source-to-image` | s2i flag stories |
| OpenShift Build API | `openshift/api` | BuildConfig field mapping |
| Tekton | `tektoncd/pipeline` | creds mounting, TaskRun behavior |

**Pin the ref.** Read the image tag from the ClusterBuildStrategy in the Strategy Catalog
Repo and use it as the ref for every upstream lookup. If the tag cannot be determined,
that is a `[NEEDS CLARIFICATION]` marker, not a silent fall back to `main`.

```bash
gh api "repos/podman-container-tools/buildah/contents/docs/buildah-build.1.md?ref=${BUILDAH_REF}" \
  -H "Accept: application/vnd.github.raw" | grep -n -- "--no-cache"
```

Prefer the API over web search: it is reproducible, it gives line numbers, and it can be
pinned. Web search is a fallback for genuinely undocumented behaviour, and a finding
sourced that way cannot grade above C.

---

# Phases

## Phase 0: Setup (once per session)

1. Read and **validate** `repo.md`. Existence is not enough.
   - Absent: invoke `/setup-repos` and stop.
   - Any required label missing, any path containing `/path/to/`, or any configured path
     absent on disk: report exactly which entries are bad and stop with **BLOCKED**.

   Required: Upstream Shipwright Build Repo, Strategy Catalog Repo, Crane Plugin Repo,
   Designs Directory.

2. Refresh every configured repo without changing what is checked out. Another session
   may be using the same clone.

```bash
for dir in <each configured path in repo.md>; do
  [ -d "$dir/.git" ] || continue
  git -C "$dir" fetch origin --quiet 2>&1
done
```

## Phase 1: Read and classify

```bash
jira issue view <ISSUE-KEY> --plain --comments 5
```

Extract description, acceptance criteria, open questions, linked issues, status.

**Topic class** — which repos to read, and which Phase 3 walk applies:

| Class | Covers | Walk |
|---|---|---|
| `buildah-flag` | A buildah flag reaching the strategy | Conversion |
| `s2i-flag` | An s2i flag reaching the strategy | Conversion |
| `api-change` | Shipwright API types | Conversion |
| `field-mapping` | A BuildConfig field to its Shipwright equivalent | Conversion |
| `plugin-policy` | What the plugin emits, warns, orders, or exits with | Plugin |
| `crane-conversion` | Conversion logic inside the plugin | Plugin |
| `crane-cli` | CLI surface and UX | Plugin |
| `documentation` | Guides, examples, migration docs | Neither; skip Phase 3 |
| `spike` | Investigation with no committed output | Neither; skip Phase 3 |

`plugin-policy` exists because output policy stories kept landing in `crane-conversion`
and then failing the conversion walk with three not-applicable links.

**Ceremony class** — how much work: `trivial`, `bounded`, `forked`. See above.

Present both, and name the signal: "Topic `field-mapping`, ceremony `trivial`, no upgrade
signal fired. Topic decides which repos I read and which Phase 3 walk applies; ceremony
decides how much. Does that look right?" Wait for confirmation.

## Phase 2: Necessity — is this work needed at all?

Answer one question: *should this be built?* Check cheapest first and stop at the first
hit. This phase runs before any expensive research, because four documents in the prior
corpus reached "already implemented" only after full research had been written up.

**2a. Already delivered or in flight?**

Run **all four**. Do not stop at the first miss: each one has a known blind spot, and a
single source is how a finished feature gets triaged as new.

**Derive the keywords before you search.** `<feature-keyword>` is not one word. A story
that bundles two capabilities needs a variant per capability, or the searches quietly
cover half the story. Write down two to four variants: the feature's name, the field or
symbol it touches, and the behaviour it changes. Search each. A story whose keywords you
cannot name in one line is a story you have not understood yet.

```bash
# 1. Jira. Blind spot: a story worded differently from the feature.
jira issue list --jql "project = BUILD AND (summary ~ '<feature-keyword>' OR description ~ '<feature-keyword>')" --plain

# 2. Merged code. Blind spot: the symbol may be named nothing like the feature, and the
#    work may live outside converter.go. Grep the package, not one file, and search for
#    the behaviour as well as the name.
grep -rn "<feature-keyword>" "<Crane Plugin Repo>/buildconfig/"

# 3. Merged PRs. This is the one that finds delivered work. --state open cannot: a
#    merged PR is closed, so searching open PRs alone reports "not started" on finished
#    features.
cd "<Crane Plugin Repo>" && gh pr list --state all --search "<feature-keyword>" --limit 20

# 4. Open PRs by issue key, for work in flight under a different description.
cd "<Crane Plugin Repo>" && gh pr list --state all --search "<ISSUE-KEY>"
```

Weigh them by reliability, not by order: a merged or open PR is the strongest signal, a
matching Jira story is next, and a grep miss is the weakest evidence of all because it
only tells you a string is absent. **A grep that finds nothing is not evidence the work
is undone.**

**2b. Parity or real need?** Is this purely migration parity with BuildConfig, or do
users independently need it in Shipwright?

**2c. Impact if skipped?** What breaks, who is affected, is there a workaround?

**2d. Related and blocking work.** Any sibling story that should be grouped or sequenced
first, and any issue this blocks or is blocked by.

### Terminal outcomes

This phase may end the run. Record one of:

| Outcome | Meaning | Evidence required |
|---|---|---|
| `already-done` | Delivered already | The commit, PR, or story that did it |
| `not-needed` | Should not be built | A stated reason |
| `superseded-by BUILD-XXXX` | Another story covers it | The story key |
| `proceed` | Continue to Phase 3 | — |

On a terminating outcome, skip to Phase 7, write the Jira comment and the spec, and
stop. The spec is still written: a story that should not be built deserves a record of
why, so nobody re-triages it in three months.

**A terminated run writes a short spec, and the rules bend accordingly.** Phases 3 to 6
never ran, so the sections they fill cannot be required. On this path only:

- `capability:` frontmatter is `n/a-terminated-at-necessity`. It is not a Phase 3 outcome
  because Phase 3 did not run, and inventing one would be a claim nobody checked.
- The destination-needs table is **not** required, and its absence does not block. It
  describes work being planned; a story nobody is going to build has none. Write the
  heading with `N/A: terminated at necessity (<outcome>)` under it so the section's
  absence is deliberate rather than forgotten.
- `## Decisions` carries exactly one entry: the necessity decision itself, as a
  Y-statement naming what evidence closed it.
- Acceptance criteria, testing plan, and files reference are omitted. There is no work to
  accept, test, or touch.

Everything else still applies, including the terminal sentinel. A terminated spec is a
short document that reached a resolved state, not an unfinished one.

## Phase 3: Capability and prior art

Locate the capability along the chain, so the work routes to the right owner, and find
any existing implementation before designing a new one.

### 3a. Chain walk — sequential, stops at the first gap

**Pick the right walk first.** Two kinds of story arrive here, and the conversion chain
only describes one of them.

| The story is about | Walk | Typical topic class |
|---|---|---|
| A BuildConfig field or behaviour reaching the target cluster | **Conversion walk**, below | `buildah-flag`, `s2i-flag`, `api-change`, `field-mapping` |
| How the plugin itself behaves: what it emits, warns, orders, reports, or exits with | **Plugin walk**, below | `crane-conversion`, `plugin-policy`, `crane-cli` |

Choosing the conversion walk for a plugin-behaviour story produces three links marked
"not applicable" and one real answer, which reads like research and is not.

**Conversion walk.** Walk in order, stop at the first gap.

1. **Engine** — does buildah or s2i support it, at the pinned tag?
2. **API** — does the Shipwright Build API have a field for it?
3. **Strategy** — does a ClusterBuildStrategy expose it?
4. **Plugin** — does `converter.go` emit it, or warn-and-drop?

**Plugin walk.** No upstream chain applies; the question is what the plugin does today
and what the target can accept.

1. **Current behaviour** — what does the plugin do now? Cite the exact code path,
   including the early return or branch that decides it.
2. **Target tolerance** — can Shipwright accept the proposed behaviour? An API type
   existing is not enough: check whether a controller implements it, because an
   unimplemented type is an `api-gap` wearing a disguise.
3. **Blast radius** — which other converted resources or stories does the change touch?
4. **Compatibility** — does this change output that something already consumes?

Both walks end in the same outcome enum. A link that genuinely does not apply is recorded
as `n/a` with one line saying why, never silently skipped and never padded.

Each link is one grep or one pinned fetch. Stop as soon as you find the gap; everything
downstream of a gap is moot.

Record the outcome:

| Outcome | Owner and next step |
|---|---|
| `engine-gap` | Blocked upstream. Link a buildah or s2i issue. Do not plan a strategy change. |
| `api-gap` | Upstream Shipwright. Needs a proposal, not a PR. |
| `exposure-gap` | Ours. Strategy Catalog PR. |
| `plugin-gap` | Ours. Plugin PR. |
| `already-supported` | The chain already handles it end to end. See below. |

**On `already-supported`:** do not loop back to Phase 2 and re-run its searches. Record
`necessity: already-done` and `capability: already-supported` together, cite the code path
that already handles it, and go straight to Phase 7. Phase 2 missed it because the
evidence only became visible once the chain was walked, which is a fact worth stating in
the spec rather than hiding by rewinding.

`engine-gap` and `api-gap` promote the story to `forked`: work blocked on an upstream
project is not a bounded change.

### 3b. Prior art sweep — parallel

Search for an existing implementation of the same shape. This is the one step in the
prior corpus that ever produced a design nobody had to invent, so run it on every story.

**Scale the fan-out to the search, not to the ceremony.** Count the search targets first.
Three or fewer, run them inline; spawning an agent to run one grep costs more than the
grep. Four or more, or any target needing a whole file read, fan out one subagent per
target in a single message. The point of a subagent here is keeping a large read out of
the main context, not parallelism for its own sake.

```bash
gh search code --repo shipwright-io/build --repo redhat-openshift-builds/strategy-catalog "<shape>"
grep -ri "<shape>" "<Strategy Catalog Repo>/clusterBuildStrategy/"
```

Look especially at strategy variants: `multiarch-native-buildah` has previously turned
out to implement a needed shell technique that the obvious strategy did not.

If found, record `prior-art-found`, cite the file and line, and have the implementation
plan reference it directly rather than reinventing it.

### 3c. Destination-needs table (MANDATORY)

One row per source-side field or behavior in scope:

`source field | destination outcome (runnable/pushable/triggerable on the target cluster?) | disposition`

Every disposition MUST be exactly one of:

- `story:BUILD-XXXX` — existing or newly proposed
- `N/A: <one-line justification>`

Nothing else. `✅ merged, PR #15` is not a disposition; it is a status, and it belongs in
Jira.

**The table's absence is itself a blocking failure**, not only a blank row. A spec with
no destination-needs table cannot reach `proceed`. Check it:

```bash
grep -q "^## Destination-needs" "<spec path>" || echo "BLOCKING: no destination-needs table"
```

A silent drop, a warn-and-drop, or a "converted but not runnable" outcome with no story
is a blocking gap.

_(Origin: Audit 1 §3 — six P1 silent drops with no story, because scope was walked
per-source-field and never per-destination-outcome. This is the only rule in this skill
that comes from a real incident. Treat it accordingly.)_

## Phase 4: Design

Decide the approach, and record what you traded away.

This phase exists because nothing in the prior skill produced the implementation plan;
it appeared at write time from a template, which is why "suggested approach" was always
singular and alternatives had nowhere to live.

1. **Name the approaches.** For a `forked` story, at least two structurally distinct
   ones. One should be the minimal viable version, one the ideal shape, weighted equally
   so neither is a straw man.

   **Use the escape clause rather than invent a second option.** If a hard constraint
   already rules the alternative out, a safety rule, an API that does not exist, a
   decision made upstream, then there was never a fork, and writing one up produces the
   padding this section exists to prevent. Say so directly:
   "this was the only viable shape because <the constraint>". An honest single option
   beats a fabricated pair, and a reviewer can challenge the constraint far more easily
   than they can challenge a straw man.
2. **Choose**, and write each decision as a Y-statement with an id:

   ```
   ### D-2: image reference resolution

   In the context of converting ImageStreamTag references, facing no cluster access at
   conversion time, we decided for flag-driven mapping and against live API lookup, to
   keep the plugin offline, accepting that an unmapped reference falls back to a
   constructed registry path with a warning.
   ```

   You cannot complete that sentence without naming the rejected option and its cost.
   Where the constraints genuinely forced the answer, the escape clause is:
   "this was the only viable shape because ...".

3. **Write the file reference table** — every file that changes, with a line number
   where the change is localised.

4. **Write acceptance criteria** — numbered, pass or fail, no subjective language.
   "Orders older than 30 days return HTTP 410 for all four roles" passes. "The feature
   works correctly" does not.

## Phase 5: Draft the spec

Write the spec to `<Designs Directory>/BUILD-XXXX-<slug>.md` using the template below.
Do not write it to disk yet; present it at Phase 7.

Mark every ambiguity inline, where it occurs:

```
The plugin maps `nodeSelector` directly to `spec.nodeSelector`
[NEEDS CLARIFICATION: does Shipwright validate node labels at admission, or only at
scheduling?] and drops unknown keys.
```

## Phase 6: Self-check before presenting

Verify, and fix anything that fails:

- [ ] Every section in the template is present
- [ ] Every load-bearing claim carries an evidence grade
- [ ] The destination-needs table exists and every row is dispositioned
- [ ] Every `D-N` decision is a complete Y-statement, or uses the escape clause
- [ ] Acceptance criteria are numbered and pass/fail
- [ ] The file reference table has a `path:line` for every localised change
- [ ] The terminal sentinel is the last non-whitespace line and matches the marker count

```bash
markers=$(grep -c "NEEDS CLARIFICATION" "<spec path>")
tail -1 "<spec path>"   # must agree with $markers
```

## Phase 7: Approval Gate — nothing is written before this

This skill produces two artifacts. **Both are gated on one explicit approval.**

Present both:

1. **Spec** — the target path, and a summary of what it contains, including the
   ceremony class, the Phase 3 outcome, and the unresolved-marker count.
2. **Jira comment** — the exact text, verbatim.

Then ask: "Approve both?"

On approval, execute in order and report each result:

1. Write the spec.
2. Post the Jira comment.
3. Set the Jira story points and confirm the epic link.

If the user approves only some, do those and say plainly which were skipped.

---

# Spec Output

Write to `<Designs Directory>/BUILD-XXXX-<slug>.md`. The slug is the issue title in
lowercase with hyphens.

```markdown
---
story: BUILD-XXXX
topic_class: <buildah-flag|s2i-flag|api-change|field-mapping|plugin-policy|crane-conversion|crane-cli|documentation|spike>
ceremony_class: <trivial|bounded|forked>
ceremony_signal: <the numbered signal that decided the class, or "none — default">
ceremony_upgraded_from: <omit unless the class ratcheted mid-run>
necessity: <proceed|already-done|not-needed|superseded-by BUILD-XXXX>
capability: <engine-gap|api-gap|exposure-gap|plugin-gap|prior-art-found|already-supported>
evidence_grade: <A|B|C|D>
---

# BUILD-XXXX: <Title>

## Context
What the issue asks for, why it matters for the migration, who is affected.

## Current State
What exists today, verified, with `path:line` citations and an evidence grade per claim.
Chain position: which link owns the gap.

## Prior Art
An existing implementation of this shape, cited by file and line, or "none found".

## Destination-needs
| source field | destination outcome | disposition |
|---|---|---|
Every disposition is `story:BUILD-XXXX` or `N/A: <justification>`.

## Decisions
### D-1: <name>
<Y-statement: in the context of X, facing Y, we decided for A and against B, to achieve
C, accepting D.>

## Proposed Change
The approach, in enough detail that no design decisions remain.

## Files Reference
| File | Change |
|---|---|
| `path/to/file.go:42` | What changes here |

## Acceptance Criteria
1. <numbered, pass/fail, no subjective language>

## Testing Plan
| Layer | What | Count |
|---|---|---|
| Unit | | |
| Cluster | | |

## Out of Scope
- <thing that looks related but is not part of this story>

## Clarifications
### Session YYYY-MM-DD
- Q: <question> → A: <answer>

---
NO UNRESOLVED MARKERS
```

The last non-whitespace line is either `NO UNRESOLVED MARKERS` or
`UNRESOLVED MARKERS: N`, and N must equal the count of `[NEEDS CLARIFICATION]` markers
in the file. `/tech-implement` reads this line and refuses to start on a non-zero count.

# Jira Comment

Post a summary: the necessity outcome, the capability outcome, the chosen approach with
its `D-N` id, the unresolved-marker count, and a link to the spec path. Keep it short;
the spec is the detail.

# Jira Update

Set story points and confirm the epic link. Jira owns story state: priority, points,
blockers, dependencies, status. The spec does not restate them.

# Multi-Issue Session Flow

When given an epic, work through issues one at a time. After every three or four,
surface any pattern across them: a shared blocker, a repeating change shape, a
dependency chain, a story that should be re-prioritised because others depend on it.

Write these to `<Designs Directory>/epic-BUILD-XXXX-patterns.md` as you find them.
Conversation does not survive compaction; a file does.

# Deferred optimisations

Do not act on these until there is data from the rewritten skill. Each names what to
measure first.

- **Scale spec sections by ceremony class.** Currently every class writes every section.
  Measure: for `trivial` stories, how many sections are one line or empty. If most are,
  a shorter form for that class is justified.
- **Gate the prior-art sweep by class.** Currently it runs on every story. Measure: how
  often the sweep returns `prior-art-found`, split by class. If `trivial` never hits,
  gate it to `bounded` and `forked`.
- **Retire the upstream half of Phase 3.** Measure: how often `engine-gap` or `api-gap`
  is reached. Both were never reached in 25 prior runs. If a year passes with neither,
  cut those two links from the chain walk.
- **Revisit the five-question cap.** Measure: the distribution of questions actually
  asked per run. If it clusters at one or two, lower the cap; if runs regularly hit five,
  the cap is binding and the research is under-specified.

# Compliance Report (MANDATORY — always emit)

| Phase | Status | Evidence |
|---|---|---|
| 0-7, one row each | ✅ / ⏭️ SKIPPED (reason) / ❌ | |
| Destination-needs table present | ✅ / ❌ | grep output |
| Terminal sentinel matches marker count | ✅ / ❌ | tail output |

An unexplained row, a missing destination-needs table, or a sentinel that disagrees with
the marker count forces Completion Status to DONE_WITH_CONCERNS at best.

# Completion Status

- **DONE** — research complete, spec written, Jira updated, sentinel reads
  `NO UNRESOLVED MARKERS`
- **DONE_WITH_CONCERNS** — complete but with unresolved markers, listed
- **BLOCKED** — cannot proceed; state the blocker and what was tried
- **NEEDS_CONTEXT** — missing information; state exactly what is needed
