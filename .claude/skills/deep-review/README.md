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

Upstream normally runs this inside its own harness. We run only the review
orchestrator and its sub-agents, via the *interactive mode* upstream already
supports when `$FULLSEND_OUTPUT_DIR` is unset. See §6 for the adaptations.

It fills a specific gap: **depth on a single open PR** — six independent
reviewers that never see each other's output, followed by a skeptic whose only
job is to throw findings away. It reviews **open pull requests only**, reading
file contents from the PR head commit via the GitHub API, never your working
tree. It is not a pre-push tool.

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

## 3. The sub-agent roster

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

Every sub-agent has an explicit `Own:` / `Do not own:` boundary. That is what
stops six reviewers all reporting the same naming nit, and it is why dedup is
tractable.

**Tier decides the cost of silence.** If an **opus**-tier sub-agent times out or
returns nothing, that absence is itself recorded as a `high` finding, forcing
the verdict to at minimum `request-changes`. A sonnet-tier miss is only `info`.
Silence cannot be mistaken for a clean bill of health.

**The challenger** runs last, with fresh context — raw findings and the diff,
never the orchestrator's reasoning — and is forbidden from generating new
findings. It tags each one `kept | downgraded | merged | removed` with a reason.
The report always prints what it removed: **that log is the primary tuning
signal.** Deleting a lot means the dimension prompts are too eager; deleting
nothing means it is not doing its job.

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
│   └── header.md        ← OURS. Frontmatter + local overrides O1–O10.
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

All nine live in [`src/header.md`](src/header.md) — that file is authoritative;
this table is only an index. They are restated at the top of the generated
`SKILL.md`, where they explicitly supersede the vendored text below them.

| # | Override | Why it exists |
|---|---|---|
| **O1** | Path remapping (`sub-agents/…` → `vendor/sub-agents/…`) | Our layout differs from upstream's |
| **O2** | Always interactive mode; skip `fullsend-check-output` | No harness, no `$FULLSEND_OUTPUT_DIR`, that binary is not installed |
| **O3** | Report-only default; protected-path approve→comment cap | Replaces `post-review.sh`, which we do not have |
| **O4** | Model mapping `claude-sonnet-4-6@default` → `sonnet` | Upstream uses Vertex model IDs |
| **O5** | Forge is always GitHub; derive `PR_NUMBER`/`REPO_FULL_NAME` inline | No `FULLSEND_FORGE`, no pre-script |
| **O6** | Inject this repo's `AGENTS.md` plus the CI-parity, controller-runtime-skew, no-`replace`, and frozen-`convert/` invariants into every sub-agent | Repo knowledge upstream cannot have |
| **O7** | `cross-repo-contracts` dispatches on crane-lib boundary changes; `--only` flag | Tuned for this repo |
| **O8** | Fetch prior review inline via `gh`, accepting only a review carrying our head-SHA marker | Replaces `pre-fetch-prior-review.sh` *and* its provenance check |
| **O9** | Report format, always printing challenger removals | Tuning signal |
| **O10** | `$REVIEW_FINDING_SEVERITY_THRESHOLD` = `info` — suppress nothing | Upstream requires it; no harness to supply it |

---

## 7. Updating from upstream

Upstream is **very** active — `fullsend-ai/agents` took ~880 commits in its
first two months, including one breaking relocation of the entire agent
directory. Assume this bundle goes stale.

```bash
bin/sync --check                     # verify hashes, then ask GitHub what changed
bin/sync --update [--ref <sha>]      # diff upstream's changes, rewrite vendor/, rebuild
```

`--check` distinguishes "upstream moved but our files did not" from `CHANGED` /
`GONE` on a file we actually vendor; `GONE` means resolve it by hand first.
`--update` never touches `src/header.md`. Run `bin/sync --build` after editing
`src/header.md` yourself.

**After any update, re-read the O1–O10 overrides.** They reference upstream
concepts by name — step numbers, `$FULLSEND_OUTPUT_DIR`, `post-review.sh`,
sub-agent filenames. If upstream renames a step or drops a sub-agent, an
override can silently stop applying.

**Never edit anything under `vendor/`.** `bin/sync` hashes every file listed in
`.manifest` and reports an edit as `EDITED`, a deletion as `MISSING`, and a file
present under `vendor/` that the manifest does not list as `EXTRA` — so adding a
file is caught too, not only changing one. Any of the three makes `bin/sync` exit
non-zero, which is what makes it usable as a pre-commit or CI gate. Put the change
in `src/header.md` as a new override instead.

---

## 8. Gotchas

- **Run from the repo root.** The skill is discovered at
  `.claude/skills/deep-review/`; it will not be found from a parent directory.
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
from [`fullsend-ai/agents`](https://github.com/fullsend-ai/agents) — see
[`vendor/NOTICE`](vendor/NOTICE) for the operative license text and attribution.
Local integration (`src/header.md`, `bin/sync`, this README) is part of this
repository.
