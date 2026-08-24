---
name: create-pr
description: Commit, push, and create or amend a PR with this repo's conventions enforced, with optional Jira story updates. Trigger on "create pr", "create-pr", "push this", "open a pr".
argument-hint: [BUILD-XXXX]
allowed-tools: [Bash, Read, AskUserQuestion, Skill]
user_invocable: true
---

# /create-pr — Commit, Push, and Open PRs

Commit, push, and create (or amend) a pull request for
`crane-plugin-buildconfig-to-shipwright`, and optionally update the linked Jira
story.

## Repo Conventions (hardcoded)

- **Push target:** the remote named **`fork`**. NEVER push to `origin`
  (origin is the shared upstream `migtools/...` — pushing there is forbidden).
- **Base branch:** `main`.
- **Branch model:** one branch per Jira story. If the user gives `BUILD-XXXX`,
  the branch is named for it.
- **Jira prefix:** if a Jira issue is linked, commit subjects and the PR title
  start with `[BUILD-XXXX]`. The Jira project code is `BUILD`.
- **Commit flags:** always `-s` (sign-off), and `-S` (GPG sign) when a signing key
  is configured — drop `-S` if this machine has none.
- **Co-author trailer:** every commit **message** and the PR body end with
  `Co-Authored-By: Claude` — **no email address**, everywhere. This is a bare
  marker, not GitHub's attributed-co-author form (which would need an email); the
  project chose the plain line for consistency across all skills.
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

  Put the free-text summary and body in shell variables so a stray quote or
  `$(...)` cannot break out of the command. Validate the points are numeric.

  ```bash
  SUMMARY='<summary>'
  BODY='<body>

  _Co-Authored-By: Claude Code._'
  POINTS='<points>'
  [[ "$POINTS" =~ ^[0-9]+$ ]] || { echo "story points must be a number"; exit 1; }

  jira issue create --type Story --summary "$SUMMARY" --body "$BODY" --no-input
  jira epic add <EPIC-KEY> <NEW-KEY>
  curl -s -X PUT "https://redhat.atlassian.net/rest/api/2/issue/<NEW-KEY>" \
    -H "Content-Type: application/json" --netrc \
    -d "$(jq -n --argjson points "$POINTS" '{fields: {customfield_10028: $points}}')"
  ```

  Run the `unslop` skill over the summary and body first, and show them to the
  user before creating. Use the new key for the rest of the flow.

Capturing a key here means Step 9 runs. No key means Step 9 is skipped.

## Step 3 — Locate the work: branch, worktree, and mode

The change may not live in the current checkout. `/tech-implement` commits on the story
branch inside a **dedicated worktree** while the main checkout stays on `main`, so
`git branch --show-current` here can read `main` and miss the work entirely. Resolve the
branch first, then run every later git command against the directory that actually holds it.

1. **Resolve the branch.**
   - With a `BUILD-XXXX` key, find its branch across local refs:

     ```bash
     git for-each-ref --format='%(refname:short)' refs/heads \
       | grep -E "BUILD-XXXX" | grep -vE '(^|/)main$' | sort -u
     ```

     Exactly one match → use it. Several → list and ask. None → fall back to the current
     branch, or Step 4 (create a branch) if the checkout is on `main`.
   - No key → use the current branch (`git branch --show-current`).

2. **Find that branch's working directory.** If it is checked out in a worktree, operate
   there; otherwise operate in the current checkout. Every git command in Steps 5–8 runs
   with `git -C "$WORK"` (or `cd "$WORK"` once). Never `git checkout` the branch in the main
   checkout — another session may share it.

   ```bash
   WORK=$(git worktree list --porcelain \
     | awk -v b="refs/heads/$BRANCH" '/^worktree /{w=$2} $0=="branch "b{print w}')
   WORK=${WORK:-$(pwd)}
   ```

3. **Detect mode.** Derive the fork owner (Step 8) and check for an open PR:

   ```bash
   gh pr list --head "<fork-owner>:$BRANCH" --state open --json number,title,url
   ```

   - **Open PR found** → amend mode. Show it and confirm via AskUserQuestion.
   - **No open PR, but the branch already has a commit ahead of `main`** → new-PR mode on
     the **existing commit**. `/tech-implement` wrote and unslopped that commit; do not
     re-commit or rewrite its message. Skip Step 4 and Steps 5–6; go to Step 7 (push).
   - **No open PR and no commit ahead** (on `main`, or an empty branch) → new-PR mode: create
     the branch (Step 4), stage (Step 5), and commit (Step 6).

   Check for an existing commit with:

   ```bash
   git -C "$WORK" rev-list --count "main..$BRANCH"    # >0 means a commit already exists
   ```

**Amend-mode principle:** the PR is always one commit. When you *do* author the message,
describe the entire diff from `main` as one coherent unit — never "added later" or "fixed
after review". If the branch has several commits **and none is an already-finished
`/tech-implement` commit you are preserving**, squash them (Step 6c, `git reset --soft
main`) so one message matches the history. A single finished commit is left as-is.

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

**Skip this whole step if the branch already carries a finished `/tech-implement` commit**
(Step 3 found a commit ahead of `main` and no new unstaged work). That commit is already
signed, unslopped, and carries the canonical trailer — preserve it and go to Step 7. Run
Step 6 only when you are authoring the first commit or squashing several unfinished ones.

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

**Amend mode:** collapse the branch to a single commit off `main`, then commit.
`git reset --soft main` keeps every change (already-committed and newly staged)
staged, so one `git commit` produces a single commit for the full diff. This is
correct whether the branch had one commit or several.

```bash
git reset --soft main
git commit -s -S -m "$(cat <<'EOF'
[BUILD-XXXX] scope: description covering the full diff from main

<body covering ALL changes in the PR>

Co-Authored-By: Claude
EOF
)"
```

## Step 7 — Push (to `fork`, never `origin`)

Run these in the branch's working directory (`git -C "$WORK"`), by explicit refspec, so no
checkout switch is needed. This is the **first** push for a `/tech-implement` branch —
`/tech-implement` commits but never pushes.

**New-PR mode (branch not yet on `fork`):**

```bash
git -C "$WORK" push -u fork "$BRANCH:$BRANCH"
```

If the branch already exists on `fork` and has diverged (a previous push, or a closed PR
left a stale tip), the plain push is rejected. Preserve the fork's current tip as an
`archive/*` tag and **push the tag before the branch**, then force-with-lease — never a
plain force-push:

```bash
git -C "$WORK" tag "archive/fork-old-$BRANCH" "fork/$BRANCH"
git -C "$WORK" push fork "archive/fork-old-$BRANCH"
git -C "$WORK" push -u --force-with-lease fork "$BRANCH:$BRANCH"
```

**Amend mode** (the branch is on `fork`, you rewrote or the review amended the commit):
same archive-then-force-with-lease sequence as above.

## Step 8 — Create or update the PR

The PR always targets `main` on the upstream repo. `gh` uses `origin` (the
upstream) as the base repo, and we push the branch to `fork`, so pass
`--head <fork-owner>:<branch>` to open the PR from the fork.

Derive the fork owner from the remote URL. The `fork` remote is SSH
(`git@github.com:<owner>/<repo>.git`), so pull the owner from between `:` and
the last `/`:

```bash
FORK_OWNER=$(git remote get-url fork | sed -E 's#.*[:/]([^/]+)/[^/]+$#\1#')
[ -n "$FORK_OWNER" ] || { echo "could not derive fork owner"; exit 1; }
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

Omit the `### Jira Issues` section and `Resolves:` line if there is no Jira. The
`Co-Authored-By: Claude` line stays either way.
Omit `## Testing` only if no tests were run this session (say so instead of
faking results).

**New-PR mode:**

```bash
gh pr create --base main --head "$FORK_OWNER:<branch>" \
  --title "..." --body "$(cat <<'EOF'
...
EOF
)"
```

**Amend mode:** look up the PR with the same `$FORK_OWNER:<branch>` head used
to create it, and stop if nothing comes back rather than editing PR "".

```bash
PR_NUMBER=$(gh pr list --head "$FORK_OWNER:$BRANCH" --state open --json number --jq '.[0].number')
[ -n "$PR_NUMBER" ] || { echo "no open PR found for this branch"; exit 1; }
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

   Build the JSON with `jq --arg` so a quote in the PR title (it can come from
   GitHub in amend mode) cannot break the command:

   ```bash
   PR_URL='<pr-url>'
   PR_TITLE='<pr-title>'
   curl -s -X POST "https://redhat.atlassian.net/rest/api/2/issue/BUILD-XXXX/remotelink" \
     -H "Content-Type: application/json" --netrc \
     -d "$(jq -n --arg url "$PR_URL" --arg title "$PR_TITLE" '{object: {url: $url, title: $title}}')"
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

4. **Add to current sprint.** Resolve the active sprint from the **board's agile API**, not
   `jira sprint list --current` — that command lists the current sprint's *issue keys*
   (e.g. `BUILD-2401`), so `tr -dc '0-9'` yields an issue number, usually a *completed*
   sprint, and the add fails with `You must specify a sprint which has not been completed`.
   Find the board that owns the project, then its active sprint:

   ```bash
   # Board that holds this project's sprints (pick the scrum board for the team, e.g.
   # "Openshift Builds Sprint Board"); list boards with `jira board list`.
   BOARD_ID=<scrum-board-id>
   SPRINT_ID=$(curl -s --netrc \
     "https://redhat.atlassian.net/rest/agile/1.0/board/$BOARD_ID/sprint?state=active" \
     | jq -r '.values[0].id')
   [ -n "$SPRINT_ID" ] && [ "$SPRINT_ID" != "null" ] || { echo "could not resolve the active sprint ID"; exit 1; }
   curl -s -X POST "https://redhat.atlassian.net/rest/agile/1.0/sprint/$SPRINT_ID/issue" \
     -H "Content-Type: application/json" --netrc \
     -d '{"issues": ["BUILD-XXXX"]}' -o /dev/null -w "sprint add: %{http_code}\n"   # 204 = added
   ```

   Verify the issue's sprint field afterward (`customfield_10020`) rather than trusting the
   status code alone.

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
