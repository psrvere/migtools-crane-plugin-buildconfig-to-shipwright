# AGENTS.md

## What is this

A [crane](https://github.com/konveyor/crane) transform plugin that converts OpenShift `BuildConfig` resources (`build.openshift.io/v1`) to Shipwright `Build` CRs (`shipwright.io/v1beta1`). It runs as a standalone binary communicating over stdin/stdout JSON, following the crane plugin protocol.

## Enhancement proposal

https://github.com/konveyor/enhancements/pull/300

## Related repositories

- **crane-lib** (`github.com/konveyor/crane-lib`) — provides the plugin interface (`transform.Plugin`), CLI harness (`transform/cli`), and types (`PluginRequest`, `PluginResponse`). This plugin requires an unreleased version of crane-lib that includes the `NewResources` field in `PluginResponse` (pinned to pseudo-version `v0.1.6-0.20260807130033-222a325c7cee` in `go.mod`).
- **crane-plugin-openshift** (`github.com/migtools/crane-plugin-openshift`) — the reference crane transform plugin this project follows architecturally.
- **crane-lib/convert/** — the original `crane convert` implementation this plugin ports from. It required live cluster access; this plugin works offline.

## How it works

The plugin fits into crane's multi-stage transform pipeline. For each resource in the export:

1. If the resource is not a `BuildConfig` (apiGroup `build.openshift.io`), it is passed through unchanged.
2. If it is a BuildConfig, the plugin returns `IsWhiteOut: true` (marks the original for deletion) and generates a new Shipwright `Build` resource via `NewResources`.
3. Docker strategy maps to `buildah` ClusterBuildStrategy, Source (S2I) strategy maps to `source-to-image`.

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

## Commit policy

Every commit in this repo is signed off:

```
git commit -s
```

- `-s` adds the DCO `Signed-off-by` trailer.

Write the commit message through the `/unslop` skill before committing, so it
reads like a person wrote it. This holds for every commit, including the fixes an
agent makes after a `/deep-review` pass.

