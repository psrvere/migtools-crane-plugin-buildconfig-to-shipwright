# Decide what to do with each Jira issue in Review from the user's PRs that name it.
# Input:  {issues: [{key, summary}], prs: [{number, title, headRefName, state, url, mergedAt}]}
# Output: [{key, summary, action, prs}]. An open PR always wins: the story is not done
# while any of its PRs is still open.
.prs as $prs
| [ .issues[] | . as $i
    | ($prs | map(select((.title + " " + .headRefName) | test("\\b" + $i.key + "\\b")))) as $m
    | { key, summary,
        prs: ($m | map({number, state, url, mergedAt})),
        action: (if ($m | any(.state == "OPEN")) then "skip-open"
                 elif ($m | any(.state == "MERGED")) then "close"
                 elif ($m | length) > 0 then "leave-unmerged"
                 else "no-pr" end) } ]
