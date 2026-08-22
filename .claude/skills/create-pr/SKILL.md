---
name: create-pr
description: Commit, push, and create or amend a PR with this repo's conventions enforced, then update the linked Jira story. Trigger on "create pr", "create-pr", "push this", "open a pr".
allowed-tools: [Bash, Read, AskUserQuestion, Skill]
user_invocable: true
---

# Create PR

Commit, push, and create (or amend) a pull request for
`crane-plugin-buildconfig-to-shipwright`, then update the linked Jira story.

## Repo Conventions (hardcoded)

- **Push target:** the remote named **`fork`**. NEVER push to `origin`
  (origin is the shared upstream `migtools/...` — pushing there is forbidden).
- **Base branch:** `main`.
- **Branch model:** one branch per Jira story. If the user gives `BUILD-XXXX`,
  the branch is named for it.
- **Jira prefix:** if a Jira issue is linked, commit subjects and the PR title
  start with `[BUILD-XXXX]`. The Jira project code is `BUILD`.
- **Commit flags:** always `-s` (sign-off) and `-S` (GPG sign).
- **Co-author trailer:** every commit ends with `Co-Authored-By: Claude`.
- **Voice:** run the `unslop` skill over the commit body, PR title, and PR body
  before using them (strip AI tells, plain human voice).

## Arguments

The user may pass a Jira key (e.g., `BUILD-2046`). If not passed, ask in Step 2.

The skill auto-detects new-PR vs amend mode from the branch state.

## Step 1 — Pre-flight

```bash
git reset HEAD          # clear anything already staged
gh auth status 2>&1     # verify GitHub auth
```

If `gh auth status` fails, stop and tell the user to run `gh auth login`.

Confirm a `fork` remote exists:

```bash
git remote get-url fork 2>&1
```

If there is no `fork` remote, stop and tell the user to add one pointing at
their fork. Do not fall back to `origin`.

## Step 2 — Jira story

If the user passed a Jira key, use it. Otherwise ask via AskUserQuestion with
**three** options:

- **Existing story** → the user gives `BUILD-XXXX`. Use it for the prefix,
  branch name, and the post-PR Jira updates in Step 9.
- **No Jira** → proceed with no prefix, no `Resolves:` line, and skip Step 9.
- **Create a story** → create one now using the `jira` skill's conventions.
  Ask the user for the epic key and story points, then:

  ```bash
  jira issue create --type Story --summary "<summary>" --body "<body>

  _Co-Authored-By: Claude Code._" --no-input
  jira epic add <EPIC-KEY> <NEW-KEY>
  curl -s -X PUT "https://redhat.atlassian.net/rest/api/2/issue/<NEW-KEY>" \
    -H "Content-Type: application/json" --netrc \
    -d '{"fields": {"customfield_10028": <POINTS>}}'
  ```

  Run the `unslop` skill over the summary and body first, and show them to the
  user before creating. Use the new key for the rest of the flow.

Capturing a key here means Step 9 runs. No key means Step 9 is skipped.

## Step 3 — Detect mode

```bash
BRANCH=$(git branch --show-current)
```

- **On `main`** → new-PR mode. Go to Step 4 to create a branch.
- **On a feature branch** → check for an open PR:

  ```bash
  gh pr list --head "$BRANCH" --state open --json number,title,url
  ```

  - **Open PR found** → amend mode (default). Show it and confirm via
    AskUserQuestion. The user may override to add a separate commit instead.
  - **No open PR** → new-PR mode on the existing branch. Skip Step 4.

**Amend-mode principle:** rewrite the commit message, PR title, and PR body to
describe the entire diff from `main` as one coherent unit of work. Never
reference "added later", "fixed after review", or incremental steps.

## Step 4 — Create branch (new-PR mode, only when on `main`)

If a Jira key exists, name the branch for the story
(`BUILD-XXXX-<short-kebab-summary>`). Otherwise generate a descriptive
kebab-case name (3-5 words, no `feat/` prefixes).

```bash
git checkout -b <branch-name>
```

## Step 5 — Stage files

```bash
git status --short
```

Present the changed files via AskUserQuestion (multiSelect):
- **Suggested** — files changed in this conversation
- **Other changes** — additional files the user can opt into

Then:

```bash
git add <file1> <file2> ...
```

## Step 6 — Commit

### 6a. Analyze the diff

- Amend mode: `git diff main...HEAD`
- New-PR mode: `git diff --cached`

### 6b. Write the message, then unslop it

Draft a conventional-commit message:
- **Subject:** `[BUILD-XXXX] scope: description` (omit the prefix if no Jira),
  under 72 chars.
- **Body:** 2-4 lines on what changed and why.

Run the `unslop` skill over the body to remove AI tells. Then append the
trailer. Show the final message to the user before committing.

Final shape:

```
[BUILD-XXXX] scope: description

<unslopped body>

Co-Authored-By: Claude
```

### 6c. Commit

**New-PR mode:**

```bash
git commit -s -S -m "$(cat <<'EOF'
[BUILD-XXXX] scope: description

<body>

Co-Authored-By: Claude
EOF
)"
```

**Amend mode:**

```bash
git commit --amend -s -S -m "$(cat <<'EOF'
[BUILD-XXXX] scope: description covering the full diff from main

<body covering ALL changes in the PR>

Co-Authored-By: Claude
EOF
)"
```

## Step 7 — Push (to `fork`, never `origin`)

**New-PR mode:**

```bash
git push -u fork <branch>
```

**Amend mode:**

```bash
git push --force-with-lease fork <branch>
```

## Step 8 — Create or update the PR

The PR always targets `main` on the upstream repo. Because we push to `fork`,
pass `--head <fork-owner>:<branch>` so `gh` opens the PR from the fork.

```bash
FORK_URL=$(git remote get-url fork)   # derive the fork owner from this
```

Draft the PR title and body, run the `unslop` skill over both, then create or
edit. The title matches the commit subject.

**PR body structure:**

```markdown
## Summary
- What changed, in bullets

## Testing
- The tests actually run this session and their results
  (e.g. `GOWORK=off go test ./... -count=1` — pass/fail)

## Key design decisions
- Notable choices made and why

### Jira Issues
Resolves: BUILD-XXXX

Co-Authored-By: Claude
```

Omit the `### Jira Issues` section and `Resolves:` line if there is no Jira.
Omit `## Testing` only if no tests were run this session (say so instead of
faking results).

**New-PR mode:**

```bash
gh pr create --base main --head "<fork-owner>:<branch>" \
  --title "..." --body "$(cat <<'EOF'
...
EOF
)"
```

**Amend mode:**

```bash
PR_NUMBER=$(gh pr list --head "$(git branch --show-current)" --state open --json number --jq '.[0].number')
gh pr edit "$PR_NUMBER" --title "..." --body "$(cat <<'EOF'
...
EOF
)"
```

## Step 9 — Update the Jira story (only when a Jira key is present)

Skip this step entirely if there is no linked Jira.

**Confirm first — required.** Before touching Jira, show the user exactly what
will happen and get a yes via AskUserQuestion:

> "Update BUILD-XXXX? I'll link the PR, add a comment, assign it to you, add it
> to the current sprint, and move it to Review."

Only proceed on an explicit yes. If the user declines, skip the updates and
report the PR as-is. Run each action the user confirmed:

1. **Link the PR:**

   ```bash
   curl -s -X POST "https://redhat.atlassian.net/rest/api/2/issue/BUILD-XXXX/remotelink" \
     -H "Content-Type: application/json" --netrc \
     -d '{"object": {"url": "<pr-url>", "title": "<pr-title>"}}'
   ```

2. **Add a comment:**

   ```bash
   jira issue comment add BUILD-XXXX --no-input "Opened PR: <pr-url>

   _Co-Authored-By: Claude Code._"
   ```

3. **Assign to me:**

   ```bash
   jira issue assign BUILD-XXXX "$(jira me)"
   ```

4. **Add to current sprint:**

   ```bash
   SPRINT_ID=$(jira sprint list 2>&1 | grep "active" | head -n 1 | awk '{print $1}')
   curl -s -X POST "https://redhat.atlassian.net/rest/agile/1.0/sprint/$SPRINT_ID/issue" \
     -H "Content-Type: application/json" --netrc \
     -d '{"issues": ["BUILD-XXXX"]}'
   ```

5. **Move to Review:**

   ```bash
   jira issue move BUILD-XXXX "Review"
   ```

## Step 10 — Report

Print:

> **PR ready**
>
> - **URL:** <pr-url>
> - **Branch:** `<branch>`
> - **Commit:** `<short-sha>`
> - **Mode:** New PR / Amended
> - **Jira:** BUILD-XXXX — linked, commented, assigned, sprint, Review (or "none")
