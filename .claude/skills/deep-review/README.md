# deep-review

Multi-agent pull request review for `crane-plugin-buildconfig-to-shipwright`.

One orchestrator triages the PR, fans out to up to six specialised reviewer
sub-agents **in parallel**, runs an adversarial "challenger" pass that deletes
false positives, and prints a severity-ranked verdict. It is **report-only by
default** — it posts nothing and edits nothing unless you explicitly ask.

```bash
/deep-review 24            # review PR #24, print findings, post nothing
```

---

## 1. Where this came from

This skill is **not original work**. The review logic is vendored verbatim from
the [fullsend](https://github.com/fullsend-ai/fullsend) project's agent bundle:

| | |
|---|---|
| **Upstream repo** | [`fullsend-ai/agents`](https://github.com/fullsend-ai/agents) |
| **Pinned commit** | `ee30be6030f8e6fbe4ba5fc5638eb483dbffeb84` |
| **Upstream path** | `skills/pr-review/`, `skills/docs-review/`, `agents/review.md` |
| **License** | Apache-2.0 — see [`vendor/NOTICE`](vendor/NOTICE) |
| **Local modifications to vendored files** | **None.** Byte-identical. |

fullsend is an agentic SDLC framework: GitHub events trigger sandboxed agents
that triage issues, write code, and review PRs. Its review agent normally runs
inside that harness — in a network-less sandbox with a read-only repo token,
writing a JSON verdict that a separate shell script posts.

We are not running the harness. We run **only** the review orchestrator and its
sub-agents, locally, against our own PRs. Upstream anticipated this: the
orchestrator has a documented *interactive mode* that activates when the
harness environment variable `$FULLSEND_OUTPUT_DIR` is unset. That is the mode
we use. See §6 for the small set of local adaptations that makes it work.

### Why we added it

The gap it fills is **depth on a single open PR**: six independent reviewers
that never see each other's output, followed by a skeptic whose only job is to
throw findings away.

That is a different job from a linter or a standards check. It is most valuable
on changes that will be read by upstream maintainers, on changes large enough
that no single pass holds the whole thing in view, and on PRs you did not write
yourself.

It reviews **open pull requests only** — it fetches file contents from the PR
head commit via the GitHub API and never reads your working tree. It is not a
pre-push tool.

---

## 2. How to run it

Run from **inside the plugin repo** so the skill is discoverable.

```bash
cd ~/Desktop/work-repos/migtools/crane-plugin-buildconfig-to-shipwright

/deep-review 24                                  # by PR number
/deep-review https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/24
/deep-review 24 --only=correctness               # one sub-agent — cheap smoke test
/deep-review 24 --only=correctness,security      # subset
/deep-review 24 --post                           # post to GitHub (asks first)
```

| Flag | Effect |
|---|---|
| *(none)* | Print the review to the terminal. **Posts nothing, edits nothing.** |
| `--only=a,b` | Restrict dispatch to the named sub-agents. `challenger` still runs. |
| `--post` | Post via `gh pr review` — shows the exact body and asks for confirmation first. Never posts silently. |

**Start here on a new checkout:**

```bash
.claude/skills/deep-review/bin/sync      # verify vendored files, rebuild SKILL.md
/deep-review 24 --only=correctness       # confirm dispatch works before a full run
```

### Safety behaviour you should know about

- **Report-only is the default.** `origin` is the shared upstream
  `migtools/crane-plugin-buildconfig-to-shipwright`; posting a review there is
  an outward-facing act, so it requires `--post` *and* a confirmation prompt.
- **Protected paths can never be approved.** If the PR touches `.claude/`,
  `.github/`, `AGENTS.md`, `CLAUDE.md`, `Makefile`, `go.mod`, `go.sum`, or
  `LICENSE`, the verdict is capped at `comment` even if every sub-agent came
  back clean. Upstream enforces this in a post-script we do not have, so it is
  reimplemented as a hard rule in our overlay (override **O3**).
- It never commits, pushes, or edits files.

---

## 3. How it works

```
  /deep-review <pr>
        │
        ├─ 1  identify PR, fetch metadata + diff + file contents at PR HEAD
        │      (deliberately NOT from disk — disk holds base-branch code)
        ├─ 2  fetch prior review, if any, for severity anchoring
        ├─ 3  triage: classify change domains → select sub-agents (usually 3–6)
        │      └─ 3c-1  security-triage pre-pass (haiku) on large PRs only
        │
        ├─ 4  ══ DISPATCH IN PARALLEL — all Agent calls in one turn ══
        │        correctness · security · intent-coherence
        │        style-conventions · docs-currency · cross-repo-contracts
        │
        ├─ 5  collect findings
        │      (a sub-agent that times out is itself recorded as a finding)
        ├─ 6a-c  group by file, merge same-category, keep distinct-category
        ├─ 6d  ══ CHALLENGER (opus, sequential) ══
        │        fresh context: raw findings + diff ONLY, never the synthesis.
        │        Its job is to DELETE findings, not add them.
        ├─ 6e  PR-level checks: body injection defense, metadata verification,
        │        scope authorization, protected paths
        ├─ 6f  verdict + self-consistency check
        └─ 7  print report
```

### The sub-agent roster

Six review **dimensions**, plus two helpers that are not dimensions. Model tier
comes from each sub-agent's own frontmatter.

| Sub-agent | Model | Dispatched when | Owns |
|---|---|---|---|
| `correctness` | **opus** | always | Logic errors, nil handling, edge cases, race conditions, API contracts, test adequacy **and test integrity** |
| `security` | **opus** | auth, permissions, secrets, data handling, config touched | Vulnerabilities, access control, data exposure, injection, privilege escalation |
| `intent-coherence` | sonnet | linked issue exists, or change is non-trivial | Architectural fit, intent alignment, PR scope, scope authorization |
| `style-conventions` | sonnet | always | Repo conventions — pointed at this repo's `AGENTS.md`, **not** generic Go opinions |
| `docs-currency` | sonnet | repo has docs | Documentation staleness (runs the `docs-review` skill inline) |
| `cross-repo-contracts` | sonnet | `go.mod`/`go.sum`/crane-lib boundary changes | Contract breakage affecting other repos |
| `security-triage` | haiku | large PRs, pre-pass | Ranks which files are security-critical so context budget goes there first |
| `challenger` | **opus** | always, **after** the others | False-positive removal, cross-dimension dedup, severity calibration |

Two design details worth internalising:

**Every sub-agent has an explicit `Own:` / `Do not own:` boundary.** That is
what stops six reviewers all reporting the same naming nit, and it is why
dedup is tractable.

**Tier decides the cost of silence.** If an **opus**-tier sub-agent times out or
returns nothing, that absence is itself recorded as a `high` finding, which
forces the verdict to at minimum `request-changes`. A sonnet-tier miss is only
`info`. Upstream's reasoning: *"an approval that skipped security or
correctness review is worse than no review at all."* Silence cannot be mistaken
for a clean bill of health.

### The challenger is the interesting part

Most review tools fail by crying wolf. The challenger exists purely to fight
that. It runs **after** everything else, with **fresh context** — it sees the
raw findings and the diff, but never the orchestrator's reasoning — and it is
explicitly forbidden from generating new findings. It returns each finding
tagged `kept | downgraded | merged | removed` with a reason, plus a separate
list of what it deleted.

The report always prints what the challenger removed. **That log is the primary
signal for tuning** — if it is deleting a lot, the dimension prompts are too
eager; if it never deletes anything, it is not doing its job.

---

## 4. Repository layout

The single most important thing to understand:

> **`vendor/` is theirs and is never edited. `src/` is ours. `SKILL.md` is
> generated from both.**

```
.claude/skills/deep-review/
├── README.md            ← you are here
├── SKILL.md             ← GENERATED — do not edit. This is what Claude loads.
├── src/
│   └── header.md        ← OURS. Frontmatter + local overrides O1–O9.
├── bin/
│   └── sync             ← verify / check / update / build
└── vendor/              ← THEIRS. Byte-identical to upstream @ ee30be60.
    ├── .manifest        ← sha256 of every vendored file
    ├── NOTICE           ← Apache-2.0 attribution
    ├── SKILL.md         ← the orchestrator (1252 lines)
    ├── agent-review.md  ← agent definition: prohibitions + output schema
    ├── meta-prompt.md   ← wrapper injected into every sub-agent prompt
    ├── github/SKILL.md  ← gh CLI command reference
    ├── docs-review/SKILL.md
    └── sub-agents/      ← the 8 sub-agent prompts
```

`SKILL.md` is built by concatenation:

```
SKILL.md  =  src/header.md  +  vendor/SKILL.md (YAML frontmatter stripped)
```

Because our text and upstream's text never share a file, a `git merge`-style
conflict between our changes and theirs is **structurally impossible**. That is
the entire point of the layout — see §7.

> The sub-agents are **prompt content, not registered Claude Code agents.** The
> orchestrator reads each `.md` file, strips its frontmatter, and passes the
> body as the `prompt` argument to the Agent tool. Nothing needs installing into
> an agent registry.

---

## 5. Prerequisites

- `gh` authenticated with read access to `migtools/crane-plugin-buildconfig-to-shipwright`
- `curl`, `shasum`, `awk` (macOS/Linux defaults)
- Claude Code, run from the repo root

No fullsend installation, no sandbox, no cloud credentials, no Vertex AI.

---

## 6. Local overrides

All nine live in [`src/header.md`](src/header.md) and are stated at the top of
the generated `SKILL.md`, where they explicitly supersede the vendored text
below them.

| # | Override | Why it exists |
|---|---|---|
| **O1** | Path remapping (`sub-agents/…` → `vendor/sub-agents/…`) | Our layout differs from upstream's |
| **O2** | Always interactive mode; skip `fullsend-check-output` | No harness, no `$FULLSEND_OUTPUT_DIR`, that binary is not installed |
| **O3** | Report-only default; protected-path approve→comment cap | Replaces `post-review.sh`, which we do not have |
| **O4** | Model mapping `claude-sonnet-4-6@default` → `sonnet` | Upstream uses Vertex model IDs |
| **O5** | Forge is always GitHub; derive `PR_NUMBER`/`REPO_FULL_NAME` inline | No `FULLSEND_FORGE`, no pre-script |
| **O6** | Inject repo invariants into every sub-agent context | See below |
| **O7** | `cross-repo-contracts` dispatches on crane-lib boundary changes; `--only` flag | Tuned for this repo |
| **O8** | Fetch prior review inline via `gh` | Replaces `pre-fetch-prior-review.sh` |
| **O9** | Report format, always printing challenger removals | Tuning signal |

### O6 — the repo knowledge upstream cannot have

Every sub-agent receives this repo's `AGENTS.md` plus these invariants:

- **CI parity.** CI builds this module standalone; the local `go.work` hides
  breakage. The authoritative check is `GOWORK=off go test ./... -count=1`.
  A finding that only reproduces under the workspace is local noise and must
  not be reported.
- **controller-runtime skew.** crane-lib pins v0.21.0 while the workspace
  resolves v0.23.x. Any v0.22+ API use (notably `client.Client.Apply`,
  `runtime.ApplyConfiguration`) compiles locally and breaks CI → **high**.
- **No `replace` directive.** crane-lib is pinned to a published
  pseudo-version. A PR adding `replace => ../crane-lib` is a **high** finding.
- **`crane-lib/convert/` is frozen.** New conversion logic belongs in
  `buildconfig/`.

---

## 7. Updating from upstream

Upstream is **very** active. `fullsend-ai/agents` was itself split out of the
main fullsend repo on 2026-06-26 and has taken ~880 commits in under two
months. The parent project deleted and relocated its entire embedded agent
directory in a single breaking commit during that window. Assume this bundle
goes stale, and re-check before you rely on a review.

### Check whether an update is even needed

```bash
.claude/skills/deep-review/bin/sync --check
```

This verifies our vendored files still match their recorded hashes, then asks
GitHub what changed. It distinguishes the two cases that matter:

```
upstream main is 2c3e23f4; you are pinned to ee30be60

Files in this bundle that changed upstream:
  (none — main moved but this bundle is unaffected)
```

↑ Upstream moved, but none of *our* 13 files did. **Nothing to do.**

```
  CHANGED   skills/pr-review/sub-agents/correctness.md
  GONE      skills/pr-review/meta-prompt.md
```

↑ Now it matters. `GONE` means upstream moved or deleted the file — resolve
that by hand before running `--update`.

### Apply an update

```bash
bin/sync --update                    # move to upstream main
bin/sync --update --ref <sha>        # or pin to a specific commit
```

This prints a unified diff of **upstream's changes only** (their old vs their
new), rewrites `vendor/`, updates `.manifest` and `NOTICE`, and rebuilds
`SKILL.md`. It never touches `src/header.md`.

**After any update, re-read the O1–O9 overrides.** They reference upstream
concepts by name — step numbers, `$FULLSEND_OUTPUT_DIR`, `post-review.sh`,
sub-agent filenames. If upstream renames a step or drops a sub-agent, an
override can silently stop applying. `--update` prints a reminder; take it
seriously.

### All commands

| Command | Does |
|---|---|
| `bin/sync` | Verify vendored files against `.manifest`, rebuild `SKILL.md` |
| `bin/sync --check` | The above, plus a read-only upstream comparison |
| `bin/sync --update [--ref SHA]` | Fetch, diff, rewrite `vendor/`, rebuild |
| `bin/sync --build` | Rebuild `SKILL.md` only — run after editing `src/header.md` |

### If you need to change behaviour

**Never edit anything under `vendor/`.** `bin/sync` hashes every file and will
report your edit as drift:

```
EDITED   sub-agents/correctness.md   (local edit under vendor/ — move it to src/header.md)
```

…and the next `--update` will overwrite it. Put the change in `src/header.md`
as a new override, then `bin/sync --build`.

---

## 8. Gotchas

- **Run from the repo root.** The skill is discovered at
  `.claude/skills/deep-review/`; it will not be found from a parent directory.
- **`.claude/` is excluded from git** in this repo via `.git/info/exclude`, so
  these files are currently untracked and will not survive a fresh clone.
- **A full run is expensive** — up to eight model calls, three on opus. Use
  `--only=` while iterating.
- **Findings cite the PR head, not your working tree.** The orchestrator
  deliberately fetches file contents from the PR HEAD commit because local disk
  holds base-branch code. Do not "correct" a finding by checking your checkout.
- **This reviews PRs, not branches.** There is no local-diff mode; open the PR
  first, then review it.

---

## 9. Credits

Review logic © the fullsend-ai contributors, Apache-2.0, vendored unmodified
from [`fullsend-ai/agents`](https://github.com/fullsend-ai/agents). Full license
text and attribution in [`vendor/NOTICE`](vendor/NOTICE).

Local integration (`src/header.md`, `bin/sync`, this README) is part of this
repository.
