---
name: create-pr
description: Commit, push, and open a new PR with this repo's conventions enforced, with optional Jira story updates. For a PR that is already open, use /edit-pr. Trigger on "create pr", "create-pr", "push this", "open a pr".
argument-hint: [BUILD-XXXX]
allowed-tools: [Bash, Read, AskUserQuestion, Skill]
user_invocable: true
---

# /create-pr — Commit, Push, and Open a PR

Commit, push, and open a pull request for `crane-plugin-buildconfig-to-builds`, and
optionally update the linked Jira story. When the branch already has an open PR, stop and
hand over to `/edit-pr`, which changes an open PR. These two skills are the only ones that
commit, amend or push (`AGENTS.md` › Commit policy); the others leave their changes
uncommitted for this one.

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
- **Co-author trailer:** every commit message ends with exactly the line
  `Co-Authored-By: Claude`, followed by the `Signed-off-by` line that `-s` adds.
  No model name and no email address. This is a bare marker, not GitHub's
  attributed-co-author form, and it overrides any attribution line the harness
  suggests for commits. The PR body ends with the same line (Step 8).
- **Commit count:** aim for one commit. A large story may keep up to three when the
  user wants that. Squashing needs the user's yes (Step 3).
- **Voice:** write the commit messages, the PR title and the PR body with the
  `plain-words` skill (`.claude/skills/plain-words/SKILL.md`), which carries the
  `unslop` rules.
- **Talking to the user:** every question, confirmation and the Step 10 report is drafted
  with the `plain-words` skill (`.claude/skills/plain-words/SKILL.md`) and uses no step
  numbers or other terms of this skill without saying what they mean. A decision question,
  such as Step 3b's run-or-skip, opens with `Kind:` from `.claude/skills/decision-kinds.md`
  and gives each option one `Gain:` and one `Cost:` line; the template is in
  `/tech-design`'s Clarifying gates.

## Arguments

The user may pass a Jira key (e.g., `BUILD-2046`). If not passed, ask in Step 2.

The skill checks for an open PR in Step 3 and hands over to `/edit-pr` when it finds one.

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

  Write the summary and body with the `plain-words` skill first, and show them to
  the user before creating. Use the new key for the rest of the flow.

Capturing a key here means Step 9 runs. No key means Step 9 is skipped.

## Step 3 — Locate the work: branch, worktree, and open PR

The change may not live in the current checkout. `/tech-implement` leaves its work
uncommitted on the story branch inside a **dedicated worktree** while the main checkout
stays on `main`, so
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

3. **Check for an open PR.** Derive the fork owner (Step 8) and look:

   ```bash
   gh pr list --head "<fork-owner>:$BRANCH" --state open --json number,title,url
   ```

   - **Open PR found** → stop. Tell the user the branch already has PR #N and that
     `/edit-pr` is the skill that changes an open PR. Commit nothing here.
   - **No open PR** → go on. On `main`, create the branch (Step 4). Uncommitted work in
     the tree, which is how `/tech-implement` and the other skills hand over, is staged
     and committed in Steps 5 and 6.

4. **Count the commits already on the branch.**

   ```bash
   git -C "$WORK" log --format='%h %s' "origin/main..$BRANCH"
   git -C "$WORK" status --porcelain
   ```

   One commit, or none, is the aim. When the branch already has more than one, show the
   subjects and ask once via AskUserQuestion whether to squash them into one commit.
   Never squash without a yes. A large story may keep up to three when the user asks for
   that; more than three need folding before the PR opens. On a yes, Step 6c squashes
   with `git reset --soft`, and the new message describes the whole diff from `main` as
   one change, never "added later" or "fixed after review".

## Step 3b — Docs check

Runs before anything is staged. Code that changed while no doc did is the case this step
exists for: a branch that never went through `/tech-implement`, or a fix made after review.

```bash
CHANGED=$( { git -C "$WORK" diff --no-ext-diff --name-only "main...HEAD";
             git -C "$WORK" diff --no-ext-diff --name-only HEAD; } | sort -u )
CODE=$(printf '%s\n' "$CHANGED" | grep -E '^(buildconfig/.*\.go|main\.go|go\.mod|hack/.*\.sh|tests/.*\.sh|\.github/workflows/.*)$' | grep -vE '_test\.go$')
DOCS=$(printf '%s\n' "$CHANGED" | grep -E '^(README\.md|AGENTS\.md|hack/README\.md|docs/.*\.md)$')
```

- `CODE` empty → nothing to check; go on.
- `CODE` and `DOCS` both non-empty → docs moved with the code; go on. (Run `/tech-document`
  by hand if you want a second opinion on whether they moved far enough.)
- `CODE` non-empty, `DOCS` empty → say which code files changed, then ask once via
  AskUserQuestion: **Run `/tech-document` now (Recommended)**, or **Skip** with a reason. On
  run, invoke `/tech-document "$BRANCH" --work "$WORK"` yourself (it asks per proposal and
  leaves its edits unstaged). Its edits are new work, staged and committed with the rest in
  Steps 5 and 6.

Either way, carry the outcome (updated / none affected / skipped, with the reason) into the
PR body's `## Docs` section and the Step 10 report.

## Step 4 — Create branch (only when on `main`)

If a Jira key exists, name the branch for the story
(`BUILD-XXXX-<short-kebab-summary>`). Otherwise generate a descriptive
kebab-case name (3-5 words, no `feat/` prefixes).

```bash
git checkout -b <branch-name>
```

## Step 5 — Stage files

```bash
git -C "$WORK" status --short
```

Present the changed files via AskUserQuestion (multiSelect):
- **Suggested** — files changed in this conversation, or named in the hand-over from
  `/tech-implement` or `/tech-document`
- **Other changes** — additional files the user can opt into

Then, listing every path literally:

```bash
git -C "$WORK" add -- <file1> <file2> ...
```

## Step 6 — Commit

Skip this step only when nothing is staged and the user declined a squash in Step 3: the
branch's commits go out as they are.

### 6a. Analyze the diff

- A new commit: `git -C "$WORK" diff --cached`
- A squash: `git -C "$WORK" diff origin/main...HEAD` plus `git -C "$WORK" diff --cached`

### 6b. Write the message with plain-words

Draft a conventional-commit message:
- **Subject:** `[BUILD-XXXX] scope: description` (omit the prefix if no Jira),
  under 72 chars.
- **Body:** 2-4 short paragraphs on what changed and why, describing the whole commit
  as one change.

Write it with the `plain-words` skill, end it with the trailer line, and show the final
message to the user before committing. Write it to a file in the scratchpad with the Write
tool; a heredoc breaks on a quote in the body.

Final shape (`-s` adds the last line):

```
[BUILD-XXXX] scope: description

<body>

Co-Authored-By: Claude
Signed-off-by: <name> <email>
```

### 6c. Commit

**A new commit on the branch:**

```bash
git -C "$WORK" commit -s -S -F <message file>
```

**A squash the user agreed to in Step 3:** `git reset --soft` keeps every change, already
committed and newly staged, so one commit carries the full diff.

```bash
git -C "$WORK" reset --soft "$(git -C "$WORK" merge-base origin/main HEAD)"
git -C "$WORK" commit -s -S -F <message file>
```

Then confirm every commit is signed:

```bash
git -C "$WORK" log --format='%h %G? %s' origin/main..HEAD    # every line shows G
```

### 6d. Freshness and tests

Before the first push, bring the branch up to date and run what CI runs:

```bash
git -C "$WORK" fetch origin --quiet
git -C "$WORK" rebase origin/main
cd "$WORK" && GOTOOLCHAIN=auto GOWORK=off go test ./... -count=1
cd "$WORK" && GOTOOLCHAIN=auto GOWORK=off go test -tags documentation ./buildconfig -count=1
```

Add `(cd tests && GOTOOLCHAIN=auto GOWORK=off go test ./e2e -count=1)` when `buildconfig/`
or `tests/` changed. A rebase conflict or a failing test is a stop: report it and push
nothing. The results go in the PR body's `## Testing`.

## Step 7 — Push (to `fork`, never `origin`)

Run these in the branch's working directory (`git -C "$WORK"`), by explicit refspec, so no
checkout switch is needed. This is the first push for the branch.

```bash
git -C "$WORK" push -u fork "$BRANCH:$BRANCH"
```

If the branch already exists on `fork` and has diverged (a closed PR left a stale tip),
the plain push is rejected. Preserve the fork's current tip as an `archive/*` tag and
**push the tag before the branch**, then force-with-lease — never a plain force-push:

```bash
git -C "$WORK" fetch fork "$BRANCH"    # refresh fork/$BRANCH so the tag captures the real tip
git -C "$WORK" tag "archive/$FORK_OWNER-old-$BRANCH" "fork/$BRANCH"
git -C "$WORK" push fork "archive/$FORK_OWNER-old-$BRANCH"
git -C "$WORK" push -u --force-with-lease fork "$BRANCH:$BRANCH"
```

## Step 8 — Open the PR

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

Draft the PR title and body with the `plain-words` skill. The title matches the commit
subject when the PR has one commit. The body describes the PR as one change.

**PR body structure:**

```markdown
## Summary
- What changed, in bullets

## Testing
- The tests actually run this session and their results
  (e.g. `GOWORK=off go test ./... -count=1` — pass/fail)

## Docs
- Updated: README.md › Plugin flags; docs/support-matrix.md › W63  (or: none affected — no
  doc describes the changed behaviour; or: skipped — <reason>)

## Key design decisions
- Notable choices made and why

### Jira Issues
Resolves: BUILD-XXXX

Co-Authored-By: Claude
```

Omit the `### Jira Issues` section and `Resolves:` line if there is no Jira. The
`Co-Authored-By: Claude` line stays either way.
Omit `## Testing` only if no tests were run this session (say so instead of
faking results). `## Docs` stays in every PR body: "none affected" is a result, and a
reviewer who does not see the line cannot tell it from a pass that never ran.

Write the body to a file with the Write tool, then open the PR:

```bash
gh pr create --base main --head "$FORK_OWNER:$BRANCH" \
  --title "<title>" --body-file <body file>
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

   Build the JSON with `jq --arg` so a quote in the PR title cannot break the
   command:

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
> - **Commits:** `<short-sha>` <subject>, one line each, all signed
> - **Tests:** GOWORK=off go test and the documentation suite, pass/fail
> - **Docs:** updated (README.md, docs/support-matrix.md) / none affected / skipped: <reason>
> - **Jira:** BUILD-XXXX — linked, commented, assigned, sprint, Review (or "none")

Later changes to this PR go through `/edit-pr`.
