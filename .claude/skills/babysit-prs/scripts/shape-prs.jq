# Shape the raw data gather-prs collects into one decision record per PR.
# Input:  {prs: [gh pr list objects], main: {workflowName: conclusion}, feedback: {"<number>": count}}
# Output: array sorted by number; only failing checks are kept, each with a kind.
def failing: ["FAILURE","TIMED_OUT","CANCELLED","STARTUP_FAILURE","ACTION_REQUIRED","ERROR"];
def infra: ["TIMED_OUT","CANCELLED","STARTUP_FAILURE"];
def local_workflows: ["Go build and tests","Documentation tests"];

. as $in
| [ $in.prs[]
    | { number, title, branch: .headRefName, url, draft: (.isDraft // false),
        jira_key: ([.title, .headRefName] | map(capture("(?<k>BUILD-[0-9]+)")? | .k) | .[0]),
        merge_state: .mergeStateStatus,
        freshness: (if .mergeStateStatus == "DIRTY" then "conflict"
                    elif .mergeStateStatus == "BEHIND" then "behind"
                    else "current" end),
        feedback: ($in.feedback[(.number | tostring)] // 0),
        checks: [ (.statusCheckRollup // [])[]
                  | { name: (.name // .context),
                      workflow: (.workflowName // null),
                      conclusion: ((.conclusion // .state // "") | ascii_upcase),
                      run_id: ((.detailsUrl // .targetUrl // "") | (capture("/runs/(?<id>[0-9]+)")? | .id) // null) }
                  | select(.conclusion as $c | failing | index($c))
                  | .kind = (if .workflow != null and ((($in.main[.workflow] // "") | ascii_upcase) as $m | failing | index($m)) then "main-red"
                             elif (.conclusion as $c | infra | index($c)) then "infra"
                             elif .workflow != null and (.workflow | test("E2E")) then "e2e"
                             elif (.workflow as $w | local_workflows | index($w)) then "reproducible"
                             else "other" end) ] } ]
| sort_by(.number)
