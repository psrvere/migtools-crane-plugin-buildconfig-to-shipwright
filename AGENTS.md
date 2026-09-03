# AGENTS.md

## What is this

A [crane](https://github.com/migtools/crane) transform plugin that converts OpenShift `BuildConfig` resources (`build.openshift.io/v1`) to Shipwright `Build` CRs (`shipwright.io/v1beta1`). It runs as a standalone binary communicating over stdin/stdout JSON, following the crane plugin protocol.

## Enhancement proposal

https://github.com/konveyor/enhancements/pull/300

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
GOTOOLCHAIN=auto go build -o crane-plugin-buildconfig-to-shipwright .
```

Requires Go 1.25.6+ (forced by transitive dependencies, notably `shipwright-io/build v0.19.0`). Newer Shipwright releases (v0.20+) pull in k8s v0.36 and require Go 1.26; this module stays on Shipwright v0.19.0 / k8s v0.34 to remain buildable with the Go 1.25 toolchain. The pinned crane-lib pseudo-version (`v0.1.6-0.20260807130033-222a325c7cee`) provides the unreleased `NewResources` API — update this when crane-lib publishes a new release.

## Development tools (`hack/`)

The `hack/` directory contains development and testing utility scripts for setting up E2E test environments. These are **developer tools**, not production-grade end-user programs. They prioritize simplicity and iteration speed over comprehensive error handling, input validation, and edge-case coverage.

When working with `hack/` scripts:
- Expect them to be opinionated and narrowly scoped for their specific use case (e.g., setting up Minikube with Shipwright)
- They may fail fast rather than gracefully handle all error conditions
- They are designed for developers who understand the underlying tools (kubectl, minikube, etc.)
- User-facing documentation in `hack/README.md` provides usage examples, but the scripts themselves are not hardened against all misuse scenarios

This is intentional — `hack/` scripts trade robustness for maintainability and developer velocity. For production cluster setup, users should follow upstream documentation for Kubernetes, Tekton, and Shipwright.

## Testing

The project uses a three-level testing strategy:

### 1. Unit Tests
Standard Go tests at the method level:

```bash
GOTOOLCHAIN=auto go test ./...
```


### 2. Plugin E2E Tests
Tests the plugin binary with crane, processing input YAMLs and asserting output transformations:

```bash
./tests/e2e-transform.sh
```

### 3. Cluster E2E Tests
Full end-to-end validation on real Minikube clusters. See [`hack/README.md`](hack/README.md) for detailed setup instructions and troubleshooting and [`.github/workflows/test-e2e-minikube-pr.yml`](.github/workflows/test-e2e-minikube-pr.yml) for example test flow.

**CI/CD:**

Pull requests run automated E2E tests on Minikube via [`.github/workflows/test-e2e-minikube-pr.yml`](.github/workflows/test-e2e-minikube-pr.yml).

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

`main.go`, `tests/testdata/export/*`, `buildconfig/names.go`, `buildconfig/postcommit.go`,
and `processRunPolicy`, the one conversion step that writes nothing to the Build. Propose
and ship; the maintainer reads the result, not the diff.

Nothing else. In particular this list does not cover `hack/*` or `buildconfig/*_test.go`,
because CI executes both: `.github/workflows/test-e2e-minikube-pr.yml` runs the `hack/`
setup scripts, and `.github/workflows/go.yml` runs `go test ./...`, which compiles every
test file. Code that runs on the CI runner is read line by line.

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

Six of the eight land with the sibling documentation PRs (#64, #65, #66 to #68, #70) and do
not exist on `main` yet. They are listed here so the table is complete when those merge.

| Test | Guards | Fix |
|---|---|---|
| `TestSupportMatrixCoversEveryWarning` | every warning template has a row in the matrix, and every quoted warning still exists | add or reword the row in `docs/support-matrix.md`; to retire a warning, keep its row and make the text `Retired by BUILD-<n>` |
| `TestArchitectureDocNamesEveryFileAndStage` | every non-test Go file and every `process*` method is named in the architecture page | add the line |
| `TestInvariantsCiteRealTests` | every test the architecture page cites exists | rename it in the page, or restore the test |
| `TestExamplesMatchCommittedOutput` | each `docs/examples/*/expected/` matches the plugin's output | `go test ./buildconfig -run TestExamplesMatchCommittedOutput -update` (once #66 to #68 land; the flag does not exist before that), then re-read that example's README. A regenerated expectation is a changed assertion, so it is read line by line like any other golden file |
| `TestReadmeOptionalFlagsAreValidJSON`, `TestReadmeVersionsMatchPins` | README flag examples are JSON; README versions match `go.mod`, the Minikube script, and the CI crane pin | fix the README |
| `TestADRsAreWellFormed` | every record has its parts and is in the index | fix the record |
| `TestNoDirectWarnLoggingInConverter` | no `c.Log.Warn*` in a Converter method other than `warnf`, which is the single recording path | record the drop through `c.warnf` (ADR-0003) |

## Gotchas

- The released crane (v0.0.5) silently produces no Builds with this plugin. Build crane from
  the commit `.github/workflows/test-e2e-minikube-pr.yml` pins, and put it first on `PATH`
  before running `tests/e2e-transform.sh`.
- Run the Go suite as CI does: `GOWORK=off go test ./... -count=1`. The workspace `go.work`
  outside this repo can resolve different dependency versions.
- On OpenShift, `kubectl get build/<name>` is the OpenShift Build API. Write
  `build.shipwright.io/<name>`.
- A BuildRun with `serviceAccount` unset runs as the namespace `pipeline` account. That is
  right only when the plugin generated no ServiceAccount. When it did, the generated account
  carries the BuildConfig's pull secret and the plugin names it in the
  `buildconfig-to-shipwright/buildrun-template` annotation, so point the BuildRun at it or
  the builder image will not pull. Grant it the SCC scoped to that one account,
  `oc adm policy add-scc-to-user pipelines-scc -z <generated-sa> -n <namespace>`. Never bind
  it with `-g system:serviceaccounts` or to the namespace default account.

## Commit policy

Every commit in this repo is signed off:

```
git commit -s
```

- `-s` adds the DCO `Signed-off-by` trailer.

Write the commit message through the `/unslop` skill before committing, so it
reads like a person wrote it. This holds for every commit, including the fixes an
agent makes after a `/deep-review` pass.

