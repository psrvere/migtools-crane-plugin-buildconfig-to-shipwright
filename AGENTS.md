# AGENTS.md

## What is this

A [crane](https://github.com/migtools/crane) transform plugin that converts OpenShift `BuildConfig` resources (`build.openshift.io/v1`) to Shipwright `Build` CRs (`shipwright.io/v1beta1`). It runs as a standalone binary communicating over stdin/stdout JSON, following the crane plugin protocol.

## Enhancement proposal

https://github.com/konveyor/enhancements/pull/300

## Development skills

This repo ships eleven Claude Code skills under `.claude/skills/` that take a Jira BUILD issue
from triage to a merged pull request, plus a twelfth, `plain-words`, that they all use for
text a person reads. [`development.md`](development.md) is the map: the
workflow, what each skill needs and leaves behind, and a walkthrough. Each skill's full
instructions live in its own `SKILL.md`.

## Related repositories

- **crane-lib** (`github.com/konveyor/crane-lib`) — provides the plugin interface (`transform.Plugin`), CLI harness (`transform/cli`), and types (`PluginRequest`, `PluginResponse`). This plugin requires an unreleased version of crane-lib that includes the `NewResources` field in `PluginResponse` (pinned to pseudo-version `v0.1.6-0.20260807130033-222a325c7cee` in `go.mod`).
- **crane-plugin-openshift** (`github.com/migtools/crane-plugin-openshift`) — the reference crane transform plugin this project follows architecturally.
- **crane-lib/convert/** — the original `crane convert` implementation this plugin ports from. It required live cluster access; this plugin works offline.

## How it works

The plugin fits into crane's multi-stage transform pipeline. For each resource in the export:

1. If the resource is not a `BuildConfig` (apiGroup `build.openshift.io`), it is passed through unchanged.
2. If it is a BuildConfig with a Docker or Source strategy and an output image, the plugin returns `IsWhiteOut: true` (marks the original for deletion) and generates a new Shipwright `Build` resource via `NewResources`, plus a `ServiceAccount` or `ConfigMap` when needed.
3. A BuildConfig with a Custom or JenkinsPipeline strategy, no output image, or one the plugin cannot convert passes through unchanged with two annotations saying it was skipped or failed, and why. The migration continues.
4. Docker strategy maps to `buildah` ClusterBuildStrategy, Source (S2I) strategy maps to `source-to-image`.

The full picture, step by step, is in [`docs/architecture.md`](docs/architecture.md). What happens to every BuildConfig field is in [`docs/support-matrix.md`](docs/support-matrix.md).

## ImageStream resolution

The original `crane convert` resolved ImageStreamTag/ImageStreamImage references by calling the live cluster API. This plugin works offline and uses flags instead:

- `--imagestream-mapping` (`ns/name:tag=registry/image:tag`) — explicit mapping
- `--registry-mapping` (`old-registry=new-registry`) — rewrite image registry paths
- Fallback: constructs `image-registry.openshift-image-registry.svc:5000/<ns>/<name>:<tag>` with a warning

## Building

```
GOTOOLCHAIN=auto go build -o crane-plugin-buildconfig-to-builds .
```

A binary built that way reports its version to crane as `devel`. The release workflow is
what stamps a real one, with
`-ldflags "-X github.com/migtools/crane-plugin-buildconfig-to-builds/buildconfig.PluginVersion=<tag>"`,
so `crane plugin-manager list --installed` saying `devel` means a local build, not a bug.

Requires Go 1.26.4+, forced by `shipwright-io/build v0.21.4`, which pulls in k8s v0.36 and declares `go 1.26.4`. The module sat on Shipwright v0.19.0 / Go 1.25.6 for a while so it could be built alongside mta-crane, which is still on Go 1.25.6; v0.21.0 is the first release carrying the `omitempty` tags on `SingleValue` (BUILD-1743), so consuming it costs the Go 1.25 toolchain. What that costs mta-crane, and the decision still open there, is [ADR-0015](docs/adr/0015-go-toolchain-parity-with-mta-crane.md). The pinned crane-lib pseudo-version (`v0.1.6-0.20260807130033-222a325c7cee`) provides the unreleased `NewResources` API — update this when crane-lib publishes a new release.

## Development tools (`hack/`)

The `hack/` directory contains development and testing utility scripts for setting up E2E test environments. These are **developer tools**, not production-grade end-user programs. They prioritize simplicity and iteration speed over comprehensive error handling, input validation, and edge-case coverage.

When working with `hack/` scripts:
- Expect them to be opinionated and narrowly scoped for their specific use case (e.g., setting up Minikube with Shipwright)
- They may fail fast rather than gracefully handle all error conditions
- They are designed for developers who understand the underlying tools (kubectl, minikube, etc.)
- User-facing documentation in `hack/README.md` provides usage examples, but the scripts themselves are not hardened against all misuse scenarios

This is intentional — `hack/` scripts trade robustness for maintainability and developer velocity. For production cluster setup, users should follow upstream documentation for Kubernetes, Tekton, and Shipwright.

## Testing

The project uses a three-level testing strategy, plus a documentation suite that runs on its own build tag:

### 1. Unit Tests
Standard Go tests at the method level. Every functional test file carries `//go:build !documentation`, so the default build runs this suite and leaves the documentation tests out:

```bash
GOTOOLCHAIN=auto go test ./...
```

### Documentation Tests
The doc-consistency tests (support matrix, architecture page, examples, README, ADRs) carry `//go:build documentation`, so the run above skips them. Run them with the tag, the way [`.github/workflows/documentation.yml`](.github/workflows/documentation.yml) does:

```bash
GOTOOLCHAIN=auto go test -tags documentation ./buildconfig
```

The two suites never overlap: a red documentation check names a doc to fix and does not turn the functional run red, and a functional failure does not block the documentation run.

### 2. Plugin conversion tests
Every fixture under `tests/testdata/NN-*` runs through `plugin.Run()` and is compared with
its `expected_<Kind>.yaml` goldens. No crane binary, no cluster; details in
[`tests/README.md`](tests/README.md):

```bash
(cd tests && GOTOOLCHAIN=auto GOWORK=off go test ./e2e -count=1)
```

### 3. Cluster E2E Tests
Full end-to-end validation on real Minikube clusters. See [`hack/README.md`](hack/README.md) for detailed setup instructions and troubleshooting and [`.github/workflows/test-e2e-minikube-pr.yml`](.github/workflows/test-e2e-minikube-pr.yml) for example test flow.

**CI/CD:**

Pull requests run automated E2E tests on Minikube via [`.github/workflows/test-e2e-minikube-pr.yml`](.github/workflows/test-e2e-minikube-pr.yml).

## Releasing

Two workflows, run by hand from the Actions tab, in this order.

**Create release branch**
([`.github/workflows/release-branch.yml`](.github/workflows/release-branch.yml)) takes a
major and minor version, `0.1`, and opens `release-0.1` off main. crane's naming, so no
leading `v`. Patch releases reuse the branch: `v0.1.0`, `v0.1.1` and the rest all come off
`release-0.1`.

**Release** ([`.github/workflows/release.yml`](.github/workflows/release.yml)) runs on that
branch and takes the full version, `v0.1.0`. It refuses to run
anywhere but a `release-*` branch, and refuses a version whose series does not match the
branch, so `v0.1.3` cannot be tagged on `release-0.2`. It builds the five platforms crane
itself publishes, stamps the version, checks each binary carries it, writes checksums, and
opens a **draft** release. Major versions are refused: this ships `0.x` until someone
decides otherwise and edits the check.

A draft creates no tag and serves no assets. Publishing it is what does both, and nothing
downstream works until you do:

- The entry in [migtools/crane-plugins](https://github.com/migtools/crane-plugins) is what
  makes `crane plugin-manager add` work. Nothing here writes it: open that PR by hand, with a
  manifest naming this release's assets, after the release is published.
  **Do not merge it before the release is published.** `plugin-manager add` does not
  check the HTTP status of its download, so against an unpublished release it writes
  GitHub's 404 page into the plugins directory as the plugin binary and reports success.
- mta-crane pins this plugin in its `go.mod`. That bump needs the tag to exist, so it comes
  after publishing too.

The release runs unit tests, not the cluster suite. `tests/e2e-cluster.sh` needs a cluster
and nothing in the release job has one, so what the release proves is what `go test ./...`
proves. Run the cluster tests on the branch before you cut from it.

## Before you change behaviour

Read, in this order: the record in [`docs/adr/`](docs/adr/README.md) for the area you are
touching; the rules table in [`docs/architecture.md`](docs/architecture.md); that step's row
in the same page; and the rows in [`docs/support-matrix.md`](docs/support-matrix.md) for the
field. Quote code by running grep or sed, never from memory.

## After you change behaviour

Update the support-matrix row if a warning changed (the matrix test tells you which), the
steps table in the architecture page if the pipeline order changed, and add a record under
`docs/adr/` if you decided a new rule. There is no changelog.

## Files you may own fully

`main.go`, `tests/testdata/export/*`, `buildconfig/postcommit.go`,
and two more conversion steps that write nothing to the Build, `processRunPolicy` and
`processChainCandidates` with its `chainInputs`. Propose and ship; the maintainer reads the
result, not the diff.

Not the rest of `buildconfig/chain.go`. `chainRunOrderSentence` and `chainCandidate` live
there but `processTriggers` and `processSource` use them to build warnings, so their text
reaches the `conversion-warnings` annotation on the Build, and both of those callers are
read line by line. The architecture page's file table says the same thing: `chain.go` is
"read every changed line".

Not `buildconfig/names.go` either, since BUILD-2439. Its `commandArg` decides which names
reach the commands the W8, W72 and W73 warnings tell the operator to paste into a shell,
so a change there can put shell text into the `conversion-warnings` annotation.

Nothing else. In particular this list does not cover `hack/*` or `buildconfig/*_test.go`,
because CI executes both: `.github/workflows/test-e2e-minikube-pr.yml` runs the `hack/`
setup scripts, `.github/workflows/go.yml` runs `go test ./...` for the functional suite,
and `.github/workflows/documentation.yml` runs `go test -tags documentation ./buildconfig`
for the doc suite. Between the two Go workflows every test file compiles and runs. Code
that runs on the CI runner is read line by line.

## Files where the maintainer reads your diff line by line

**Everything not named in the list above.** That is the default, so a file in neither list
is read line by line rather than left undecided. The ones worth calling out:

`plugin.go`, `disposition.go`, `outcome.go`, `imagestream.go`, `triggers.go`,
`dockerfile.go`, and all of `converter.go`, including the strategy switch and both
handlers, the output gate, `processOutput`, `processSource`, `uniqueName`, the outcome
block, serialization, and every remaining `process*` step. Five of those steps write to
the generated Build and two of the five decide identity or mount Secrets, so none of them
are low-stakes: `processStrategyVolumes` (writes `b.Spec.Volumes`, and
`convertBuildVolumeSource` sets `volumeSource.Secret`), `processResources` (writes the
BuildRun-template annotation, including `spec.serviceAccount`), `processCompletionDeadline`
(`b.Spec.Timeout`), `processNodeSelector` (`b.Spec.NodeSelector`),
`processBuildsHistoryLimits` (`b.Spec.Retention`), `processGitProxyConfig` (copies proxy
URLs, which can embed credentials, into `Build.spec.env`) and `processOutputImageLabels`.

Also `hack/*`, `buildconfig/*_test.go`, `tests/e2e-*.sh`, the golden files under
`tests/testdata/e2e-*/`, `.github/workflows/*`, `go.mod` and `go.sum`, this file, and
`.claude/skills/**`. The last two decide what an agent is allowed to do and what a review
looks for, so widening your own grant is never a change you land unreviewed. Where the two
lists could both match a path, the line-by-line list wins.

On all of these: explain your reasoning before merge, name the rule you relied on, and do
not reword a warning without saying which matrix row moves.

## When a documentation test fails

These tests guard the docs. A red one means a doc to update, not a test to weaken.

They carry `//go:build documentation`, so `go test ./...` skips them; run them with
`go test -tags documentation ./buildconfig`, which is what `.github/workflows/documentation.yml`
does on every PR.

Every test in the table exists on `main`, or arrives with the PR that adds its row. Six
landed with the documentation PRs (#64, #65, #66 to #68, #70).

| Test | Guards | Fix |
|---|---|---|
| `TestSupportMatrixCoversEveryWarning` | every warning template has a row in the matrix, and every quoted warning still exists | add or reword the row in `docs/support-matrix.md`. To retire a warning, keep its row and start the cell with `Retired by BUILD-` and the story number. Put no backticks anywhere in that cell: a backtick-quoted string is read as a live warning template, and the row then fails the doc-to-code check instead |
| `TestArchitectureDocNamesEveryFileAndStage` | every non-test Go file and every `process*` method is named in the architecture page | add the line |
| `TestInvariantsCiteRealTests` | every test the architecture page cites exists | rename it in the page, or restore the test |
| `TestExamplesMatchCommittedOutput` | each `docs/examples/*/expected/` matches the plugin's output | `go test -tags documentation ./buildconfig -run TestExamplesMatchCommittedOutput -update` (once #66 to #68 land; the flag does not exist before that), then re-read that example's README. A regenerated expectation is a changed assertion, so it is read line by line like any other golden file |
| `TestReadmeOptionalFlagsAreValidJSON`, `TestReadmeVersionsMatchPins` | README flag examples are JSON; the Shipwright version in the README, the Go version in this file, and the crane commit in `hack/README.md` match `go.mod`, the Minikube script and the CI workflow | fix whichever page the failure names |
| `TestTriggerRunbookYAMLParses` | every `yaml` block in `docs/trigger-migration.md` is a document `kubectl apply` could read | fix the block; the failure names its line |
| `TestADRsAreWellFormed` | every record has its parts and is in the index | fix the record |
| `TestNoDirectWarnLoggingInConverter` | no `c.Log.Warn*` in a Converter method other than `warnf`, which is the single recording path | record the drop through `c.warnf` (ADR-0003) |

## Gotchas

- A crane released before `v0.11.0-alpha.1` silently produces no Builds with this plugin:
  `NewResources` landed in crane commit `24eafd8` on 13 August 2026, and that tag is the
  first to carry it. For testing a branch, build crane from the commit
  `.github/workflows/test-e2e-minikube-pr.yml` pins and put it first on `PATH` before running
  `tests/e2e-cluster.sh`. Users install a crane release and add the plugin with
  `crane plugin-manager add`; the README covers that. The symptom of an old crane is quiet:
  the transform logs `converted-with-warnings`, the BuildConfig is whited out, and no Build
  appears under `transform/` or `output/`. Check `crane version` before trusting an empty
  output. The current `crane apply` takes no `--export-dir`: it reads `export/` from the
  working directory, so run it from the directory that holds `export/`. `--transform-dir`
  and `--output-dir` still exist and default to `transform/` and `output/` there.
- Run the Go suite as CI does: `GOWORK=off go test ./... -count=1` for the functional suite,
  and `GOWORK=off go test -tags documentation ./buildconfig -count=1` for the doc suite. The
  workspace `go.work` outside this repo can resolve different dependency versions.
- On OpenShift, `kubectl get build/<name>` is the OpenShift Build API. Write
  `build.shipwright.io/<name>`.
- A Build with a Local source (what a binary BuildConfig becomes) starts only with
  `shp build upload <build> <directory> -F`. A BuildRun created any other way waits out
  `spec.source.local.timeout` and fails. `shp` has no `--context` flag; to target a cluster
  that is not the current context, write a minimal kubeconfig with
  `oc config view --minify --flatten --raw --context <ctx>` under `umask 077` (it carries
  a token, so no other local user may read it), pass it as `KUBECONFIG`, and delete the
  file afterwards.
- The Builds for Red Hat OpenShift operator needs an `openshift-builds` namespace even when
  both operators are subscribed into `openshift-operators`. Without it `OpenShiftBuild/cluster`
  reports `SharedResourceReconcileFailed` and no ClusterBuildStrategy appears. Create the
  namespace first.
- Fixtures under `tests/testdata/` and worked examples under `docs/examples/` use generic
  names only. Material that came from a customer is reshaped before it lands here: no
  customer name, namespace, application, image or hostname, in the files, the commit message
  or the pull request. The repository is public.
- A BuildRun with `serviceAccount` unset runs as the namespace `pipeline` account. That is
  right only when the BuildConfig named no ServiceAccount and the plugin generated none.
  Otherwise the `buildconfig-to-shipwright/buildrun-template` annotation names the account
  the BuildRun must use, so point the BuildRun at it: a generated account carries the
  BuildConfig's pull secret and the builder image will not pull without it. Grant a
  generated account the SCC scoped to that one account,
  `oc adm policy add-scc-to-user pipelines-scc -z <generated-sa> -n <namespace>`. Never bind
  it with `-g system:serviceaccounts` or to the namespace default account.

## Commit policy

Every commit in this repo is signed off:

```
git commit -s
```

- `-s` adds the DCO `Signed-off-by` trailer.

Only two skills create commits, amend them or push: `/create-pr`, which opens a PR, and
`/edit-pr`, which changes an open one. Every other skill, and any agent working without a
skill, leaves its changes uncommitted in the working tree and hands over to `/create-pr`
when the branch has no PR yet, or to `/edit-pr` when it has one.

Everything a person reads is written with the `plain-words` skill
(`.claude/skills/plain-words/SKILL.md`). That covers every commit message, PR title and
body, PR or review comment, thread reply and Jira comment, and anything a skill or agent
shows the user. No exceptions. The skill carries `/unslop`'s rules, so it works without
`/unslop` installed.

End every commit message with exactly this line, then the `Signed-off-by` line that `-s`
adds:

```
Co-Authored-By: Claude
```

No model name and no email address. This holds for every commit, including the fixes an
agent makes after a `/deep-review` pass.

