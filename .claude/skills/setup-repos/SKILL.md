---
name: setup-repos
description: Create or update repo.md with local repository paths for the BuildConfig-to-Shipwright migration repos, and report which optional review tools are installed. Use when repo.md is missing, a new repo is added, or a skill reports a missing tool. Auto-invoked by /tech-design and /tech-review when repo.md is not found.
argument-hint: [update]
allowed-tools: [Bash, Read, Write, Edit, AskUserQuestion]
user_invocable: true
---

# /setup-repos — Local Repository Configuration

Every migration skill needs to know where your clones live. Those paths differ per machine,
so they are never committed. This skill writes them once to `repo.md` at the project root.

`repo.md` is gitignored. `repo_example.md`, beside this file, is the committed template.

## Arguments

The user invoked this with: $ARGUMENTS

- No arguments: full setup.
- `update`: re-scan using the stored work directory and append only missing entries.

## Repositories

| Label | Match on `origin` remote | Required |
|-------|--------------------------|----------|
| Upstream Shipwright Build Repo | `shipwright-io/build` | yes |
| Upstream Shipwright Triggers Repo | `shipwright-io/triggers` | no |
| Strategy Catalog Repo | `redhat-openshift-builds/strategy-catalog` | yes |
| Downstream Operator Repo | `redhat-openshift-builds/operator` | yes |
| Downstream OpenShift Builds Repo | `redhat-openshift-builds/shipwright-io` | no |
| Crane Plugin Repo | `migtools/crane-plugin-buildconfig-to-builds` | yes |
| Crane Repo | `migtools/crane` | no |
| Crane Lib Repo | `migtools/crane-lib` | no — legacy, read-only |

Two entries are not repositories and have no remote:

- **Work Directory** — the root the search starts from.
- **Designs Directory** — where design docs are written. Defaults to
  `<Crane Plugin Repo>/designs`, which `.gitignore` covers.

  A custom value **must live outside any repository checkout**. `.gitignore` only ignores
  `designs/`; a custom directory elsewhere inside the repo would not be ignored and the
  author's notes would be committed. Reject such a path and ask again.

`Crane Lib Repo` is the frozen pre-2026-08-13 home of the conversion code. It is read as a
prior-art archive only and is never a PR target. Leaving it unset is fine.

These four are checked on the web and are never cloned, so they need no path:
`containers/buildah`, `openshift/source-to-image`, `openshift/api`, `tektoncd/pipeline`.

## Tooling

`/tech-review` runs several reviewers. Two are built into Claude Code and are always
there. The rest are optional: when one is absent that reviewer is skipped and the review
says so, rather than failing.

| Tool | Kind | Required | Used by |
|-------|------|----------|---------|
| `/code-review` | Built into Claude Code | yes | `/tech-review` |
| `/simplify` | Built into Claude Code | yes | `/tech-review` |
| `coderabbit` | External CLI | no | `/tech-review` |
| `qodo` | External CLI, `@qodo/command` | no | `/tech-review` |
| `compound-engineering` | Claude Code plugin | no | `/tech-review` escalation |

Tool availability is **not** stored in `repo.md`. Paths are stable for the life of a
clone; an install happens in one command and changes without warning. `repo.md` holds
paths only, and each skill probes for tools when it runs. What this skill gives you is
the onboarding picture: what is here, what is not, and what each absence costs.

## Step 1: Check existing repo.md

Read `repo.md` at the project root.

- `update` and the file exists → go to Step 4.
- `update` and it does not exist → treat as full setup, continue to Step 2.
- No arguments and the file exists → say "repo.md already exists. Run `/setup-repos update`
  to add missing repos." and stop.
- No arguments and it does not exist → continue to Step 2.

## Step 2: Ask setup mode

Ask the user:

> How would you like to set up repo.md?
>
> 1. **Auto-discover** — give me your work directory and I will find the repos
> 2. **Manual** — copy the template and fill it in yourself

For **Manual**, tell them:

> ```bash
> cp .claude/skills/setup-repos/repo_example.md repo.md
> ```
> Then edit `repo.md` with your real paths.

The template ships `/path/to/...` placeholders, so a partly-edited file looks configured
but is not. Before reporting success, read the file back and check that:

- every required label is present,
- no value still contains `/path/to/`,
- every path exists on disk,
- `Designs Directory` is outside every repository checkout.

Report the exact failing lines and treat setup as incomplete until they are fixed. For **Auto-discover**, continue to Step 3.

## Step 3: Auto-discover

Ask: "What is your work directory? (e.g. `~/work`, `~/repos`)"

Store as `WORK_DIR` and expand `~` to the full home path.

### 3a. Find every git repo under it

```bash
find "$WORK_DIR" -maxdepth 4 -name ".git" -type d 2>/dev/null | while read -r gitdir; do
  repo_path="$(dirname "$gitdir")"
  remote="$(git -C "$repo_path" remote get-url origin 2>/dev/null)"
  [ -n "$remote" ] && echo "$remote|$repo_path"
done
```

### 3b. Match remotes to labels

Match each remote against the table above. Handle HTTPS and SSH forms, with or without a
`.git` suffix — `https://github.com/migtools/crane`, `git@github.com:migtools/crane.git`
and `https://github.com/migtools/crane.git` are the same repo.

Match on the **full** `org/repo` tail, never the bare repo name. `migtools/crane` and
`migtools/crane-lib` and `migtools/crane-plugin-buildconfig-to-builds` all begin with
`crane`; a prefix match assigns the wrong path.

If two clones match one label, show both and ask which to use. Never guess.

### 3c. Report

Show a table of label, resolved path, and how it was found. Then:

- For each **required** label with no match, print the clone command and ask whether to
  proceed without it. `/tech-design` cannot run a full pass while one is missing.
- For each optional label with no match, note it and move on.
- Ask about anything ambiguous one question at a time.

```bash
git clone https://github.com/<org>/<repo>.git "$WORK_DIR/<org>/<repo>"
```

### 3d. Write repo.md

Write `Label: /absolute/path`, one per line, in the order of `repo_example.md`. Omit labels
that were not resolved rather than writing a placeholder — a placeholder path looks valid
and fails later with a confusing error.

Set `Designs Directory` to `<Crane Plugin Repo>/designs` unless the user gives another path.

Then read the file back and run the same validation as manual mode — required labels
present, no `/path/to/` left, every path resolving on disk, Designs Directory outside any
checkout. Report the result, and remind the user that `repo.md` is gitignored.

## Step 4: Partial update

Read the stored `Work Directory` from the existing `repo.md`, re-run the Step 3a scan, and
append only labels that are absent. Never rewrite or reorder lines that already exist —
the user may have edited a path by hand on purpose.

Report what was added, or say plainly that nothing was missing.

## Step 5: Report tooling

Run this in both setup modes — including manual — and on `update`.

```bash
for tool in coderabbit qodo; do
  if command -v "$tool" >/dev/null; then
    echo "$tool: $(command -v "$tool")"
  else
    echo "$tool: not installed (optional)"
  fi
done
```

Report each result, and for anything missing say which reviewer it disables so the user
can judge whether they care. Two optional CLIs absent is a working setup, not a broken
one.

**Do not print an install command you have not verified.** Both CLIs are third-party and
their install methods change; a stale command wastes more time than no command. Name the
tool and let the user get it from its vendor.

Three tools cannot be probed from the shell, and should not be:

- `/code-review` and `/simplify` are built into Claude Code. They have no files on disk.
- The `compound-engineering` plugin does have files, under `~/.claude/plugins/`, but that
  path is version-stamped, is Claude Code internals, and tells you only that the plugin
  was downloaded — not that it is enabled. Looking there gives a confident wrong answer.

Skills report their own availability when something tries to use them. Say in the report
that these three are checked at run time, and move on.

Write nothing from this step into `repo.md`.

## Never do these

- Never write an absolute path into any committed file. `repo.md` only.
- Never add `repo.md` to git. It is gitignored; do not force-add it.
- Never write a `/path/to/...` placeholder into a real `repo.md`.
- Never infer a path from a directory name. Confirm with the `origin` remote.
- Never record tool availability in `repo.md`. It is stale the moment someone installs
  something, and a stale "absent" makes a working tool look missing.
- Never print an install command you have not verified against the tool itself.
