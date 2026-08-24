---
name: tech-design
description: Research and triage a Jira enhancement issue for the BuildConfig-to-Shipwright migration, then write an executable spec. Locates the capability along the conversion chain, records decisions with referenceable ids, and ends in a state a script can verify. Trigger when the user says "tech-design", "triage this issue", "research this enhancement", or "design BUILD-XXXX".
argument-hint: <ISSUE-KEY|EPIC-KEY|blank>
allowed-tools: [Bash, Read, Write, Edit, WebSearch, WebFetch, Agent]
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

```text
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

1. The issue names two or more ways to **build** the thing, or asks "should we do X or Y",
   and the choice changes what gets written. Naming two tools a document will describe is
   not a fork; naming two mechanisms one of which we must pick is.
2. The acceptance criteria describe a behaviour with more than one plausible expression
   (an ordering, a format, a policy) rather than a value to map
3. The issue changes already-shipped behaviour **in a way something else depends on**: an
   output format, a warning string, an exit code, a default, or a public contract.
   Narrow this deliberately. "Touches existing code" describes nearly all maintenance
   work and would make `forked` the default; the question is whether a consumer can
   notice the change.
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
`forked` from the start. The one read allowed before the checkpoint is Phase 2's scope
check, which is a grep and not research; see Phase 1.

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

Lead with the answer. The first line of any question or checkpoint is the
recommendation, or the fact the human asked for, in one sentence; context and options
follow for a reader who wants them. A three-option brief with a paragraph of setup got
interrupted on BUILD-2334 with "super short answer". The sentence it was missing was
"yes, the bump waits on a tag, and the noise can be fixed without it".

Ask in plain text, and keep the whole question under fifteen lines. No `file:line`, no
citations, no option cards: AskUserQuestion is not in this skill's tool list, because
its option descriptions invite exactly the density that keeps getting cut. Run the
question through `/unslop` before sending it. This applies to the Phase 1 and Phase 7
checkpoints as much as to clarifying questions. On BUILD-2315 a three-option card with
a paragraph of setup was rejected and rewritten on request as twelve short lines: a
two-sentence problem, three one-line options, "which one: A, B, or C?". That version
got a one-word answer.

That version is the shape. Write every question and checkpoint to it:

```text
Q<N> — <one-line title>
<What the thing is and what is at stake, in one or two plain sentences that a reader
who has not seen the research can follow.>
Recommendation: <option> because <one plain reason>.
A) <option, one line, an outcome not a mechanism>
B) <option, one line>
C) <option, one line, only when a real third exists>
Reply with a letter.
```

`Q<N>` counts questions within a run, starting at Q1, and keeps them apart from the
spec's `D-N` decision ids; the two fixed checkpoints use the same shape without a
number. This is gstack's prose decision brief with the completeness scores and the
per-option pros and cons removed, because those bullets are the density the cards were
rejected for. The BUILD-2340 question that got the one-word answer, for the record:

> Q1 — Inline Dockerfile
> Some BuildConfigs have the Dockerfile typed directly into them instead of in the git
> repo. Today we print an error and throw that text away. The buildah strategy cannot
> take Dockerfile text, so the build will not run either way; the only question is
> whether to keep the text.
> Recommendation: A because nothing is lost and the user can copy it into the repo later.
> A) Put the text in a ConfigMap next to the Build.
> B) Keep throwing it away and warn.
> C) Put the text in an annotation on the Build.
> Reply with a letter.

Log each answer the moment it arrives, to
`<Designs Directory>/.clarifications-BUILD-XXXX.md`, under a dated subheading, as
`- Q<N>: <question> → A: <answer>`. Save that file after every answer.

This is a scratch file, not the spec, and the distinction is what keeps the Iron Law
intact. An answer a human gave is the one thing in a run that cannot be recovered by
re-running it, so it is written immediately. The spec is still written only at Phase 7,
and the `## Clarifications` section is copied from this file when the spec is written.
Delete the scratch file once the spec exists.

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

**Pin once, in a header table, not on every citation.** A spec that repeats
`repo@sha:path:line` on thirty claims is the citation wall this skill's readers cut.
List each repo and the commit it was read at in a single table under `## Context`, then
cite `path:line` alone in the body. Grade A asks that the commit be recoverable for
every claim, not that it sit adjacent to every line number, and one table satisfies that
while keeping the prose readable.

A story routed to `exposure-gap` or `plugin-gap` on grade C or D evidence carries a
`[NEEDS CLARIFICATION]` marker rather than a confident plan.

**A code comment is not evidence for what the code does.** A claim about behaviour that
rests on a comment, a docstring, or a field name is grade C at best — comments drift from
the code they describe. To assert behaviour at grade A, cite the line that actually
implements it (the write, the branch taken, the call), or build the binary and cite its
output. This is the one gap a resolved sentinel cannot catch: `NO UNRESOLVED MARKERS` means
no open questions remain, not that every asserted mechanism was traced. BUILD-2316's spec
claimed `warnf` populates the `conversion-warnings` annotation, quoting the `warnf` doc
comment; the annotation is in fact written only from build-args warnings, so a wrong
mechanism shipped under a clean sentinel. When a load-bearing claim says "the code does X,"
trace the line that does X — or run it — before the spec asserts it.

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

Two more repos are read for a narrow purpose only:

| Label | Read when | Never |
|---|---|---|
| Crane Lib Repo | To check the plugin **interface contract** this repo compiles against: `transform.Plugin`, `PluginRequest`, `PluginResponse`, and the stdin/stdout protocol. It is a live dependency pinned in `go.mod`. | As a prior-art archive for old conversion code, and never a PR target. |
| Downstream Operator Repo | When a story's acceptance criteria name it. | As a source of truth for strategies. Its files are generated from the Strategy Catalog. |
| Crane Repo | For the Plugin walk's consumer-tolerance link only: what crane does with the plugin's response once it has it, in `internal/transform/writer.go` and `internal/apply/`. | As a PR target, and never for how conversion used to work. |

The distinction on crane-lib matters. It is frozen as the *former home of the conversion
code*, and that history is not research input. It is current as the *library defining the
plugin protocol*, and a question about that protocol is answerable nowhere else. Citing
it for the contract is correct; citing it for how conversion used to work is not.

**Not read at all:** the Downstream OpenShift Builds repo. No phase reads it, and it
appears in no design document produced to date. The Crane repo used to sit in this
sentence. It left because the fact that decided BUILD-2315 lives there: crane's writer
keeps the last of two resources sharing a kind, namespace and name
(`internal/transform/writer.go`), which is what turned a plugin-emitted ServiceAccount
into silent credential loss. crane-lib defines what the plugin sends; crane decides what
happens to it, and the second question is the consumer-tolerance link.

### External tools

Checked via the GitHub API, never cloned, and always at the version the strategy runs.

| Tool | Repo | When |
|---|---|---|
| buildah | `podman-container-tools/buildah` | buildah flag stories |
| s2i | `openshift/source-to-image` | s2i flag stories |
| OpenShift Build API | `openshift/api` | BuildConfig field mapping |
| Tekton | `tektoncd/pipeline` | creds mounting, TaskRun behavior |
| Tekton Triggers | `tektoncd/triggers` | webhook and trigger stories |
| Pipelines as Code | `openshift-pipelines/pipelines-as-code` | trigger stories on OpenShift |

This list is not closed. Any upstream project a story genuinely depends on can be read
the same way, pinned to a ref, and cited at grade A. Add a row when you use one, rather
than downgrading real evidence because the table did not anticipate it.

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

Say one line at the start of each phase, naming the phase and what it is about to read.
Research is silent by nature, and on BUILD-2315 the harness had to prompt five times for
a sign of life during Phase 3.

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

Run Phase 2's scope check before presenting, and carry its answer into the question. It
is one grep of the target repo for what the story says exists, and it changes what the
human is asked. On BUILD-2315 that grep showed the function items 1 to 4 harden does not
exist in this repo, so the checkpoint could say "items 1 to 4 have nowhere to land here;
item 5 is the story" instead of asking whether a seven-item story is forked.
Classification still comes from the issue text; the grep only says where the text
points.

## Phase 2: Necessity — is this work needed at all?

**Scope check, first, before anything else.** Where would the fix actually land?

If the answer is a repository this skill does not cover, stop. Record
`necessity: out-of-scope`, name the repo that owns the work and the evidence for that,
and go to Phase 7. Do not route it into `plugin-gap` and write a plan whose PR target
cannot contain the fix. A confident spec pointing at the wrong repo is worse than no
spec, because it reports the story handled while the bug stays live.

Watch for the lexical trap: a story can say "plugin" and mean something else entirely.
`crane-lib`'s `transform/kubernetes` has its own plugin type unrelated to this repo.
Confirm which codebase the words point at before assuming it is ours.

The same goes for any claim about what exists today. A description written before the
2026-08-13 repo move describes crane-lib, and a function it names may never have reached
this repo. Grep the target repo at HEAD for every symbol, file and behaviour the
description says is there before building on it. BUILD-2334 said `cleanNullFields`
strips nulls today and must be deleted. It only ever existed on an unmerged crane-lib
branch, and the plugin was emitting the nulls with no workaround at all. The story's
scope item read "delete the workaround"; the true scope item was "there is no
workaround".

Answer one question: *should this be built?* Check cheapest first and stop at the first
hit. This phase runs before any expensive research, because four documents in the prior
corpus reached "already implemented" only after full research had been written up.

**2a. Already delivered or in flight?**

**Search the repo the work would land in, not the plugin by default.** The searches below
say `<Target Repo>`, and choosing it wrong is fatal: a strategy-side story searched
against the plugin returns four clean misses while a finished PR sits in the catalog.
Pick from the topic class:

| Topic class | `<Target Repo>` |
|---|---|
| `buildah-flag`, `s2i-flag` | Strategy Catalog Repo |
| `api-change` | Upstream Shipwright Build Repo |
| `field-mapping`, `crane-conversion`, `plugin-policy`, `crane-cli` | Crane Plugin Repo |
| `documentation` | Crane Plugin Repo, plus wherever the docs live |
| anything touching a strategy | Strategy Catalog Repo **and** Crane Plugin Repo, both |

When two repos are plausible, search both. Two extra `gh` calls cost seconds; a false
"not started" costs a duplicated implementation.

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
#    Exclude .claude: the plugin repo keeps git worktrees under .claude/worktrees/, and
#    each one is a full copy of the tree. Without the exclusion a single real hit arrives
#    buried in one duplicate per worktree, and the file list alone can run past a hundred
#    lines. .gstack holds the browse tool's untracked network and console logs at the
#    repo root, and any string a visited page served can turn up in them.
grep -rn --exclude-dir=.claude --exclude-dir=.gstack --exclude-dir=vendor --exclude-dir=.git \
  "<feature-keyword>" "<Target Repo>"

# 3. Merged PRs. This is the one that finds delivered work. --state open cannot: a
#    merged PR is closed, so searching open PRs alone reports "not started" on finished
#    features.
cd "<Target Repo>" && gh pr list --state all --search "<feature-keyword>" --limit 20

# 4. Open PRs by issue key, for work in flight under a different description.
cd "<Target Repo>" && gh pr list --state all --search "<ISSUE-KEY>"

# 5. Sibling design specs. The strongest dependency evidence often sits in another
#    story's spec, not in Jira or code. Nothing else looks here.
grep -rln "<feature-keyword>\|<ISSUE-KEY>" "<Designs Directory>"/BUILD-*.md
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

A closed blocker is not a shipped blocker. Jira closes a story when its PR merges, and
this story may need what that PR produces, which is a later event. Name the artifact
this story consumes and check that it exists. For an upstream code change that is a tag
containing the commit, not a merged PR, and `git tag --contains <sha>` on the upstream
clone answers it in one line. BUILD-2334 arrived with BUILD-1743 Closed and its PR merged
on Shipwright `main`, while every tag cut since came from a release branch without it.
Read as "blocker closed", the story looked ready. Read as "artifact missing", it was a
wait, and `blocked-on` below is where it goes.

### Terminal outcomes

This phase may end the run. Record one of:

| Outcome | Meaning | Evidence required |
|---|---|---|
| `already-done` | Delivered already | The commit, PR, or story that did it |
| `not-needed` | Should not be built | A stated reason |
| `superseded-by BUILD-XXXX` | Another story covers it | The story key |
| `out-of-scope` | The fix belongs to a repo this skill does not cover | The owning repo, and why |
| `blocked-on <artifact>` | Needed, but an external artifact does not exist yet | The artifact by name, and the check that showed it missing |
| `proceed` | Continue to Phase 3 | — |

**A bundled story gets one outcome per scope item, not one for the story.** Real stories
arrive carrying three requests wearing one issue key. Forcing a single value throws away
the two answers that were not chosen. Write:

```text
necessity: proceed
necessity_by_scope:
  - param validation: proceed
  - golden test: N/A — covered by BUILD-2328
  - pre-flight checks: not-needed — closed won't-do on BUILD-2039, offline plugin
  - version bump: blocked-on shipwright-io/build tag containing 3a4da666
```

The top-level `necessity` is the outcome that governs the run: `proceed` if any item
proceeds. If no item proceeds, the run terminates on whichever outcome the items share.

On a terminating outcome, skip to Phase 7, write the Jira comment and the spec, and
stop. The spec is still written: a story that should not be built deserves a record of
why, so nobody re-triages it in three months.

See **What a spec must contain** below for what a terminated run writes.

`blocked-on` is the one terminal outcome that expects a second run. Its Jira comment
names the artifact and the command that will show it has arrived, so the wait has a
concrete end. Its spec carries the Phase 0 to 2 evidence and stops there, and the next
run starts from that file rather than from the Jira text. The run itself is DONE, not
BLOCKED: the spec and the comment were written. BLOCKED is for the skill being unable
to run at all.

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
4. **Plugin** — two questions, in this order. First, can the plugin *obtain* what it
   would need: is the input in the BuildConfig itself, or does it require another
   resource, a cluster lookup, or a flag that does not exist? Only then: does
   `converter.go` emit it, or warn-and-drop?

The Plugin link asks two questions because a mapping can be impossible rather than
merely missing, and the difference decides whether the story has an implementation at
all. `PluginRequest` carries one resource plus a flat `Extras` map (Crane Lib Repo,
`transform/plugin.go`), and the plugin has no cluster access, so a story asking the
converter to read a ServiceAccount, a Secret, or any sibling resource has nowhere to
read it from no matter what the API and the strategy support. Answer availability
first: a `plugin-gap` recorded against an input the plugin cannot see is really a
not-implementable scope item, and belongs in `necessity_by_scope` with a `D-N`
recording the constraint and its escape clause.

**Plugin walk.** No upstream chain applies; the question is what the plugin does today
and what the target can accept.

1. **Current behaviour** — what does the plugin do now? Cite the exact code path,
   including the early return or branch that decides it. Then run it. A grep shows what
   the code says and the binary shows what it emits, and the two disagreed on
   BUILD-2334: no line of code mentioned null, and every emitted `paramValues` entry
   carried three of them. Build to the scratch directory, feed it a fixture, read the
   JSON:

   ```bash
   GOWORK=off go build -o "$SCRATCH/plugin" "$CP"
   python3 -c 'import yaml,json,sys; print(json.dumps(yaml.safe_load(open(sys.argv[1]))))' \
     "$CP/tests/testdata/export/resources/myapp/BuildConfig_build.openshift.io_v1_myapp_webapp-docker.yaml" \
     | "$SCRATCH/plugin" 2>/dev/null | python3 -m json.tool | grep -n "<field>"
   ```

   A fixture is already a `PluginRequest`: the exported resource inline, plus an optional
   `extras` map for flags. `yq -o=json` does the same conversion if PyYAML is missing.
   Ten seconds, grade A, and the citation is the commit the binary was built from.
2. **Consumer tolerance** — who consumes this output, and can they accept the change?
   Identify the consumer first, because it is not always a cluster:
   - Shipwright, for anything applied to a cluster. An API type existing is not enough:
     check whether a controller implements it, since an unimplemented type is an
     `api-gap` wearing a disguise.
   - crane, for anything crossing the plugin protocol. The contract lives in crane-lib;
     what crane then does with the response lives in the Crane Repo, and that is the
     half that bites. `internal/transform/writer.go` resolves two resources with one
     kind, namespace and name by keeping the last, which on BUILD-2315 meant a
     plugin-emitted ServiceAccount could silently replace the migrated one.
   - a human or a script, for CLI output, warnings, and exit codes. Here the question is
     whether anything parses it today.

   Name the consumer explicitly. "Shipwright accepts it" on a story that never touches a
   cluster is the hollow answer this link is prone to.

   When the consumer chain crosses three or more projects, fan out one subagent per
   project in a single message, each pinned to the version the plugin or strategy
   actually runs, and keep only the answers. BUILD-2315's chain ran crane, kustomize,
   the Kubernetes API types, Shipwright, Tekton and `oc`, and walking it inline took
   about thirty sequential reads. The fan-out rule in 3b applies here for the same
   reason.
3. **Blast radius** — which other converted resources or stories does the change touch?
4. **Compatibility** — does this change output that something already consumes?

Both walks end in the same outcome enum. A link that genuinely does not apply is recorded
as `n/a` with one line saying why, never silently skipped and never padded.

**A dependency bump gets a trial in a scratch copy.** When a scope item says "bump X",
copy the repo to the scratch directory, run `go get X@<ref>`, `go mod tidy`,
`go build ./...` and `go test ./...`, and read the resulting `go.mod` diff. Do it before
designing, because the result changes the design. On BUILD-2334 the fix commit resolved
to a pseudo-version below the current pin, Go reported the bump as a downgrade, and any
later `go get -u` would have reverted it. A spec written from the changelog alone would
have recommended it.

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
- `story:BUILD-XXXX (fold-in proposed)` — an existing story whose scope would have to
  grow to cover this row. The Jira comment names every proposed fold-in, so the other
  story's owner hears about it.
- `N/A: <one-line justification>`

Nothing else, and one per row. A row carrying `story:` and `N/A:` together passes the
Phase 6 grep and says two things; split it. `✅ merged, PR #15` is not a disposition; it
is a status, and it belongs in Jira.

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

```text
The plugin maps `nodeSelector` directly to `spec.nodeSelector`
[NEEDS CLARIFICATION: does Shipwright validate node labels at admission, or only at
scheduling?] and drops unknown keys.
```

## Phase 6: Self-check before presenting

Two kinds of check. Run the mechanical ones as commands; nothing about them is a matter
of opinion, and a failure is a failure.

```bash
SPEC="<spec path>"
# 1. Sentinel present, last, and agreeing with the marker count.
#    Count occurrences, not matching lines: grep -c returns 1 for a line carrying two
#    markers, which would under-report and let a spec claim it is resolved when it is not.
#    Read the last NON-BLANK line: tail -1 returns "" on a file ending in a newline.
markers=$(grep -o "NEEDS CLARIFICATION" "$SPEC" | wc -l | tr -d ' ')
sentinel=$(grep -v '^[[:space:]]*$' "$SPEC" | tail -1)
if [ "$markers" -eq 0 ]; then
  [ "$sentinel" = "NO UNRESOLVED MARKERS" ] || echo "FAIL: 0 markers but sentinel reads '$sentinel'"
else
  [ "$sentinel" = "UNRESOLVED MARKERS: $markers" ] || echo "FAIL: $markers markers but sentinel reads '$sentinel'"
fi
# 2. Every section its phases require is present (see "What a spec must contain")
grep -c "^## " "$SPEC"
# 3. Every destination-needs row dispositioned, when that table is required.
#    Strip the header and separator rows first. Both match "^|" and neither carries a
#    disposition, so counting them made this check report 2 on a perfectly clean table
#    and 0 was unreachable. The header match is case-insensitive: every spec written so
#    far capitalises it ("Source field / state"), and a case-sensitive match counted it.
sed -n '/^## Destination-needs/,/^## /p' "$SPEC" | grep "^|" \
  | grep -iv "^|[[:space:]]*source field" | grep -v "^|[-|[:space:]]*$" \
  | grep -vc "story:\|N/A:"   # want 0
# 4. Every D-N heading has a body
grep -c "^### D-[0-9]" "$SPEC"
```

Then the judged ones. These need reading, and saying so is more honest than pretending a
checkbox settles them:

- Does every load-bearing claim carry an evidence grade, and is each grade the one the
  citation actually earns?
- Is every `D-N` a complete Y-statement, or an honest use of the escape clause rather
  than a fabricated pair?
- Are the acceptance criteria genuinely pass/fail, or do they contain a word like
  "correctly" that nobody can test?

**On `path:line` in the Files Reference table.** A line number is required only where one
exists. Use the forms below rather than inventing a number or leaving the row out:

| Situation | Write |
|---|---|
| Editing existing code | `path/to/file.go:412` |
| A new file | `path/to/new_file.go` (new) |
| A call site blocked on an open question | `path/to/file.go` [NEEDS CLARIFICATION: which call site] |

An invented line number is worse than an absent one: it looks verified.

## Phase 7: Approval Gate — nothing is written before this

Before presenting, re-run search 5 from Phase 2a, the sibling-spec grep for the issue
key. Another session may have written a spec since Phase 2 ran, and a spec written
during this run is the one kind that search cannot have seen. On BUILD-2315 the
BUILD-2316 spec, written the same day, handed over a residual with "that is BUILD-2315"
in its Out of Scope, and it surfaced only by accident, through the diff log. A hit here
is dispositioned before the gate, not after.

This skill produces two artifacts. **Both are gated on one explicit approval.**

Present both, and a third item when it applies:

1. **Spec** — the target path, and a summary of what it contains, including the
   ceremony class, the Phase 3 outcome, and the unresolved-marker count.
2. **Jira comment** — the exact text, verbatim, after `/unslop`.
3. **Retitle** — when the surviving scope no longer matches the Jira title, the proposed
   new title. Both runs on 2026-08-24 needed one: BUILD-2316 dropped `--dest-registry`,
   BUILD-2315 kept one of seven items. A title describing work that no longer exists
   misleads the next person who lists the epic.

Then ask: "Approve both?" (or "all three").

On approval, execute in order and report each result:

1. Write the spec.
2. Post the Jira comment from a file, not a positional argument:
   `jira issue comment add KEY --template <file> --no-input </dev/null`. The positional
   form opened an editor and hung for two minutes on BUILD-2315.
3. Set the Jira story points, confirm the epic link, and apply the retitle if approved:
   `jira issue edit KEY -s "<title>" --no-input </dev/null`.
4. Record what the human changed. See below.

This skill does not edit TRACKER.md. Jira owns story state, and no skill in this repo
writes that file.

If the user approves only some, do those and say plainly which were skipped.

### Record what the human changed

The moment before approval is the only place in this skill where the agent's judgment
and a human's judgment are both on the table for the same question. That difference is
the cheapest signal available about where the skill is wrong, and it is gone the instant
the conversation moves on.

Append one line to `<Designs Directory>/agent-human-diff.jsonl` — never to the spec,
which is a deliverable and should not carry telemetry:

```bash
jq -c -n \
  --arg story "BUILD-XXXX" \
  --arg date "$(date -u +%Y-%m-%d)" \
  --argjson changed '<the changed fields, as an object, {} when nothing changed>' \
  '{story:$story, date:$date, changed:$changed}' \
  >> "<Designs Directory>/agent-human-diff.jsonl"
```

Record each field the human altered, as `{"field": {"agent": "...", "human": "..."}}`.
The fields worth watching are the ones the skill decided rather than found:
`ceremony_class`, `necessity`, `capability`, `topic_class`, the chosen approach's `D-N`,
and story points.

Two rules that keep this honest:

- **An unchanged approval is data too.** Write `{"changed": {}}`. A field that is never
  corrected is a candidate for the skill deciding alone; a run where nothing is ever
  corrected, over many runs, is a gate that has become a rubber stamp. Those two look
  identical here and are separated only by whether the specs later survive
  implementation.
- **Record the change, not a justification for it.** If the human moved `bounded` to
  `forked`, that is the entry. Do not also write why the agent was right.

What to do with it, and the bad-score shape for each, is in `scorecard.md` under
"Agent versus human diff". The short version: a field corrected most times is one the
skill should stop guessing at, and a narrowing gap over time is the only evidence that
any of this is working.

---

# What a spec must contain

**A section is required when the phase that fills it ran.** Not before. Earlier versions
of this skill demanded every section on every path, which made a documentation story
unable to reach `proceed`: it skips Phase 3, and the destination-needs table lives there.
The rule below replaces every per-path exception.

| Section | Required when |
|---|---|
| Context, Current State | Always |
| Decisions | Always. On a terminated run it holds one entry: the necessity decision. |
| Clarifications | Always, even if the answer is "none fired" |
| Terminal sentinel | **Always, no exceptions** |
| Prior Art | Phase 3 ran |
| Destination-needs | Phase 3 ran **and** the story changes conversion output |
| Proposed Change, Files Reference, Acceptance Criteria, Testing Plan | Phase 4 ran |
| Out of Scope | Phase 4 ran |

A section whose phase did not run is **omitted**, not stubbed with `N/A`. An omitted
section is legible; a stubbed one is noise that looks like work.

`capability:` frontmatter takes the value matching the path:

| Path | `capability:` |
|---|---|
| Phase 3 ran | its outcome |
| Terminated at Phase 2 | `n/a-terminated-at-necessity` |
| `documentation` or `spike`, Phase 3 skipped by class | `n/a-no-chain` |
| `out-of-scope` | `n/a-out-of-scope` |

Never invent a Phase 3 outcome for a run that did not walk one. It is a claim nobody
checked.

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
necessity: <proceed|already-done|not-needed|superseded-by BUILD-XXXX|out-of-scope|blocked-on <artifact>>
necessity_by_scope: <omit unless the story bundles several requests; otherwise one entry
  per scope item, each ending in its own terminal outcome — see Phase 2>
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
- Q1: <question> → A: <answer>

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

Run the comment through `/unslop` before posting, and post it from a file (Phase 7,
step 2). Jira is read by people who never open the spec, and the comment is the one
place this skill's prose reaches them unedited.

# Jira Update

Set story points and confirm the epic link. Jira owns story state: priority, points,
blockers, dependencies, status. The spec does not restate them.

# Multi-Issue Session Flow

When given an epic, work through issues one at a time. After every three or four,
surface any pattern across them: a shared blocker, a repeating change shape, a
dependency chain, a story that should be re-prioritised because others depend on it.

Write these to `<Designs Directory>/epic-BUILD-XXXX-patterns.md` as you find them.
Conversation does not survive compaction; a file does.

This file is session state about an epic, not an artifact about one story, so the Iron
Law does not cover it: there is no per-story approval it could wait for, and holding it
in memory loses exactly the cross-story pattern it exists to catch. It never contains a
spec, a triage conclusion, or anything posted to Jira.

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
- **Decide whether `bounded` is a real class.** Measure: the `ceremony_class`
  distribution. Nine dry runs across seven stories produced two `already-done` and five
  `forked`, and not one `bounded`. Signal 3 was narrowed in response. If twenty real runs
  still produce no `bounded` story, delete the class rather than keep tuning signals
  toward it.
- **Act on the agent-human diff.** Measure: `agent-human-diff.jsonl`. A field the human
  corrects in most runs is one the skill should stop deciding alone, and should instead
  raise as a clarifying question. A field never corrected in fifty runs is one the skill
  can stop presenting at the gate. Do not act on either before there are enough lines to
  tell a pattern from a run of bad luck.

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
