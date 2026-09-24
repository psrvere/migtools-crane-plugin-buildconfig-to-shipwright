# Development skills

This repo ships ten [Claude Code](https://claude.com/claude-code) workflow skills under
`.claude/skills/`, plus a writing helper they all share. Together the ten automate the path
from a Jira BUILD issue to a merged pull request: research and triage, implementation, unit
and cluster testing, a pre-PR review gate, a docs sync, opening the PR, changing the open
PR, multi-agent review of the published PR, and addressing the feedback that comes back.

Each one is invoked as a slash command from inside a clone of this repo. Every skill's
full instructions live in its own `SKILL.md`. This page is the map, not the manual.

They are development tooling only. Nothing here is needed to *use* the plugin; if you are
migrating BuildConfigs, [Usage with crane](README.md#usage-with-crane) is the section you
want.

## Workflow

```
  Setup    ┌────────────────────────────────────────────┐
           │  /setup-repos                              │  ◄── run once per machine
           │  finds your local clones, writes their     │
           │  paths to repo.md. Every skill reads it    │
           └─────────────────────┬──────────────────────┘
                                 │
  ═════════════════════════════════════════════════════════════
   Everything below runs once per Jira issue
  ═════════════════════════════════════════════════════════════
                                 │
                                 ▼
  Phase 1  ┌────────────────────────────────────────────┐
           │  /tech-design BUILD-XXXX                   │  ◄── priority comes last, and
           │  should this be built at all? Checks       │      only with a file:line
           │  upstream, shipped code and open PRs       │      behind every claim
           │  → design doc, once you approve it         │
           └─────────────────────┬──────────────────────┘
                                 │
  ═════════════════════════════════════════════════════════════
   DECISION: is the change needed, and is it unblocked?
     No  → record the finding in Jira, close it, done
     Yes → continue below
  ═════════════════════════════════════════════════════════════
                                 │
                                 ▼
  Phase 2  ┌────────────────────────────────────────────┐
           │  /tech-implement BUILD-XXXX                │  ◄── refuses to start
           │  turns the design doc into code, in its    │      without a design doc
           │  own worktree so a shared clone is safe    │
           │  catalog first, then converter, then tests │
           └─────────────────────┬──────────────────────┘
                                 │
                                 ▼
  Phase 3  ┌────────────────────────────────────────────┐
           │  /tech-test BUILD-XXXX unit                │  ◄── no cluster — runs on
           │  compiles the branch, runs the Go suite    │      any clone
           │  and the offline conversion checks         │
           └─────────────────────┬──────────────────────┘
                                 │
                                 ▼
  Phase 4  ┌────────────────────────────────────────────┐
           │  /tech-review BUILD-XXXX                   │  ◄── plus five checks a
           │  the gate before a branch becomes a PR:    │      general reviewer
           │  reviewers in parallel, then an agent      │      does not perform
           │  paid to disprove every blocker            │
           └─────────────────────┬──────────────────────┘
                                 │
                                 ▼
  Phase 5  ┌────────────────────────────────────────────┐
           │  /tech-test BUILD-XXXX cluster             │  ◄── needs a real cluster.
           │  runs the original BuildConfig first,      │      Succeeding is not the
           │  then the converted one, and compares      │      same as doing the same job
           │  the output images by digest and labels    │
           └─────────────────────┬──────────────────────┘
                                 │
                                 ▼
  Phase 6  ┌────────────────────────────────────────────┐
           │  /tech-document BUILD-XXXX                 │  ◄── before the branch
           │  brings the docs in step with the code:    │      becomes a PR
           │  support matrix, architecture page, ADRs   │
           └─────────────────────┬──────────────────────┘
                                 │
                                 ▼
  Phase 7  ┌────────────────────────────────────────────┐
           │  /create-pr BUILD-XXXX                     │  ◄── commit, push to the
           │  commits signed, pushes to your fork, and  │      fork, open upstream.
           │  opens the PR with this repo's conventions │      Never pushes to origin
           └─────────────────────┬──────────────────────┘
                                 │
                                 ▼
  Phase 8  ┌────────────────────────────────────────────┐
           │  /deep-review <PR#>                        │  ◄── open PRs only — yours
           │  up to six reviewers with non-overlapping  │      or anyone else's.
           │  beats, then a challenger that can only    │      Silence counts as a finding
           │  delete findings, never add them           │
           └─────────────────────┬──────────────────────┘
                                 │  the run writes verdict.json and prints its path;
                                 │  hand that path to the next phase as --from
                                 ▼
  Phase 9  ┌────────────────────────────────────────────┐
           │  /address-review <PR#> [--from <path>]     │  ◄── triages every thread
           │  reads every thread, fixes what is valid,  │      into fix, answer or
           │  hands the commit to /edit-pr, replies,    │      push back
           │  resolves, and re-checks                   │
           └────────────────────────────────────────────┘

  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─

  Anytime:
    /setup-repos update   — re-scan after cloning a new repo
    /deep-review <PR#>    — review any open PR, no local branch needed
    /edit-pr <PR#>        — commit and push new work onto an open PR
```

Phases 8 and 9 hand over on disk, not through GitHub. `/deep-review` writes its
adjudicated findings to `verdict.json` in its run directory and prints the path;
`/address-review --from <that path>` reads them as items already settled and does not
triage them a second time. So reviewing your own PR never means posting a review to
yourself: the findings go straight into the fix round, and the human and bot comments
already on the PR are picked up in the same run.

Only `/create-pr` and `/edit-pr` commit, amend or push (`AGENTS.md` › Commit policy).
Every other skill leaves its changes uncommitted on the story branch. `/tech-review` and
`/tech-test` copy that uncommitted work into their own throwaway worktrees, so they test
and review what is actually there. `/create-pr` commits it and opens the PR; after that,
`/edit-pr` folds each new change into the PR's commits, five at most, and rewrites the
messages, the title and the body so each reads as one change.

Each phase is its own command. Review sits between the two test stages on purpose: the
unit stage is cheap and catches what review should not waste time on, while the cluster
stage is slow enough that repeating it after review-driven changes is the largest
avoidable cost in the loop.

## The skills

| Command | What it does | Needs first | Leaves behind |
|---------|--------------|-------------|---------------|
| `/setup-repos [update]` | Finds your local clones of the repos this work touches and writes their paths to `repo.md`. Every other skill reads that file, so no skill hardcodes a path that only exists on your machine | — | `repo.md` at the project root |
| `/tech-design <ISSUE-KEY>` | Works out whether an issue should be built at all, before working out how. Checks whether the feature already exists upstream, already shipped, or is sitting in someone's open PR. Then it asks whether Shipwright's design makes it unnecessary anyway. Priority and story points come last, and every claim has to cite a file and line | `repo.md` | A design doc under `designs/`, plus a Jira comment. Both are written only after you approve them |
| `/tech-implement <ISSUE-KEY>` | Turns the approved design doc into code, and refuses to start without one. Works in its own throwaway worktree rather than your checkout, because two sessions sharing a clone share one index and one HEAD. Strategy catalog changes go first, then the converter, then the tests. A conversion cannot be tested against a strategy parameter that does not exist yet | A design doc | The change, uncommitted, on the story branch in its worktree; test results under `designs/test-results/` |
| `/tech-test <ISSUE-KEY or branch> unit` | Compiles the branch and runs the Go suite plus the offline conversion checks. No cluster, so it runs on any clone. It goes before review, because reviewing code that does not compile wastes the reviewer. A branch name stands in for the key when the work has no story yet; the report then says the key is owed before a PR | A branch | A run report |
| `/tech-review [<ISSUE-KEY>] [--fix]` | The gate a branch passes before it becomes a PR. Runs general reviewers in parallel, then hands every blocking finding to a separate agent whose only job is to disprove it. A false blocker stops a good branch, and that is the expensive failure. Then adds five checks no general reviewer performs; the sharpest compares the parameter names the converter emits against those the strategy YAML actually defines, since a mismatch compiles cleanly and only fails on the cluster with `UndefinedParameter` | A branch whose unit tests pass | Findings in the terminal. Commits nothing |
| `/tech-test <ISSUE-KEY or branch> cluster` | Runs the original BuildConfig on a real cluster first, then the converted Build, and compares the two output images by digest and labels. A converted Build that merely succeeds proves nothing. The question is whether it did the same job as the one it replaced. That comparison is what makes it a test rather than a smoke check | A reviewed branch, `oc`, and a cluster | A run report; fixtures archived, then only what it created is deleted |
| `/tech-document [<ISSUE-KEY>]` | Brings the docs in step with a code change before the branch becomes a PR: the support-matrix row when a warning moved, the architecture page when the pipeline order changed, an ADR when a new rule was decided. Can also audit the whole doc map against the code | A branch whose code changed | Doc edits on the branch, uncommitted |
| `/create-pr [<ISSUE-KEY>]` | Commits signed-off, tests with `GOWORK=off`, pushes to your fork, and opens the PR against upstream with this repo's conventions enforced, updating the Jira story when asked. Aims for one commit and asks before squashing several. Never pushes to `origin`, and hands over to `/edit-pr` when the branch already has a PR | A branch ready to publish | A commit, a fork push, and an open PR |
| `/edit-pr [<pr-number\|url>]` | Puts new work onto an open PR. Amends the commit a change fixes or finishes, or adds a well-scoped one, and keeps the PR at five commits or fewer. Rewrites each touched commit message, the PR title and the body so each reads as one change, tests with `GOWORK=off`, and after you approve the planned commits pushes to your fork with an archive tag and `--force-with-lease`. Leaves Jira alone unless asked | An open PR and uncommitted changes on its branch | Rewritten commits on the fork, and a PR title and body that describe the PR as it stands |
| `/deep-review <pr-number\|url>` | Up to six reviewers read an open PR in parallel, each with an explicit list of what it does and does not own, so they do not all report the same naming nit. A challenger then runs as its own stage: it reads the findings and the diff, but never the orchestrator's reasoning, and can only delete findings, never add them. If a top-tier reviewer returns nothing, that silence is recorded as a finding rather than passing as a clean bill of health | An open PR | Findings in the terminal, and `verdict.json` plus `verdict.md` in the run directory, which is what `/address-review --from` reads. Posts nothing unless asked |
| `/address-review [<pr-number\|url>] [--from <verdict.json>]` | Reads every inline thread, review write-up and PR comment, the bots and your own `/deep-review` verdict included, and triages each into fix, answer or push back. `--from` takes that verdict from `/deep-review`'s run directory instead of from a review posted on the PR, and those findings skip triage because deep-review's challenger already adjudicated them. After you approve the table it fixes the code, tests with `GOWORK=off`, hands the commit, the push and the PR body to `/edit-pr`, replies where each comment was left, and resolves the threads | An open PR with feedback, a `/deep-review` verdict file, or both | Fixes committed and pushed through `/edit-pr`; replies and resolved threads on the PR |

### Shared by every skill

| File | What it does |
|------|--------------|
| `.claude/skills/plain-words/SKILL.md` | The writing rules for every question, checkpoint and summary a skill shows you: plain words, the finding first, short sentences, no skill vocabulary. It carries the `/unslop` rules, so it works without that skill installed. Commit messages, PR titles and bodies, posted comments, thread replies and Jira comments are written with it too |
| `.claude/skills/decision-kinds.md` | The kinds of decision a skill may ask you to make: scope, architecture, mapping, policy, compatibility, interface, security, code structure and testing. Every decision question opens with its kind, and gives each option one gain and one cost. `/tech-design`'s Clarifying gates holds the template |

## Getting started

You need [Claude Code](https://claude.com/claude-code), `gh` authenticated against GitHub,
and `jira-cli` configured. `/tech-design` checks `jira me` before it does anything else.
The `/tech-test` cluster stage also needs `oc` and a reachable OpenShift cluster.

Then, once per machine:

```
/setup-repos
```

It scans your work directory for the clones the other skills read and writes their paths
to `repo.md`. Those paths differ per machine, so `repo.md` is gitignored and never
committed; `.claude/skills/setup-repos/repo_example.md` is the template it follows. Run
`/setup-repos update` after cloning a new repo rather than editing the file by hand.

`designs/` is gitignored for the same reason. Design docs and test results are working
notes, not deliverables.

## Two review skills, two scopes

`/tech-review` and `/deep-review` sound alike. They do not overlap, and neither calls the
other.

|  | `/tech-review` | `/deep-review` |
|--|----------------|----------------|
| **When** | Before the PR exists, on a local branch | On an open PR |
| **Scope** | The branch and its paired strategy change | The PR as published |
| **Unique value** | Cross-repo consistency, test evidence | Adversarial multi-agent depth |
| **Reviews others' work** | No | Yes |

Neither checks out your branch or writes to Jira, and both are report-only by default.

Every sub-agent a skill dispatches names its model, and the ceiling is Opus. The ceiling
caps, it never raises: a stage that ran on Sonnet stays on Sonnet. A built-in Skill forks
on the model of whoever invokes it, so `/tech-review` never invokes one from the session:
it forks `/code-review` at low effort from inside an Opus sub-agent, runs its docs pass as
an Opus sub-agent, and its simplify pass as a Sonnet one.

`/deep-review` is not original work: its review logic is vendored verbatim from the
[fullsend](https://github.com/fullsend-ai/fullsend) agent bundle under Apache-2.0. See
[`.claude/skills/deep-review/README.md`](.claude/skills/deep-review/README.md) for the
attribution, the pinned upstream commit, and the local adaptations.

## Walkthrough

Taking one issue from triage to a merged pull request:

```
/setup-repos                     # once per machine, writes repo.md

/tech-design BUILD-2269          # research → designs/BUILD-2269-*.md
                                 # stop here if the issue turns out to be
                                 # unnecessary, already done, or blocked

/tech-implement BUILD-2269       # branch and code, left uncommitted

/tech-test BUILD-2269 unit       # compile gate + Go suite, no cluster
/tech-review BUILD-2269          # reviewers, challenger, cross-repo checks
/tech-test BUILD-2269 cluster    # real OpenShift, baseline vs converted
/tech-document BUILD-2269        # bring the docs in step with the code
```

Then publish and review the PR, and address what comes back:

```
/create-pr BUILD-2269            # commit, push to your fork, open the PR
/deep-review 32                  # multi-agent review of the published PR
                                 # prints the verdict.json path at the end

/address-review 32 --from /tmp/deep-review-32/verdict.json
                                 # those findings plus every thread on the PR:
                                 # fix, hand to /edit-pr, reply, resolve

/edit-pr 32                      # any later change to the open PR
```

Push branches to your fork, never to `origin`. Upstream changes land through pull
requests only.
