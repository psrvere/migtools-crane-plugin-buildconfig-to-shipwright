# AGENTS.md

## What is this

A [crane](https://github.com/konveyor/crane) transform plugin that converts OpenShift `BuildConfig` resources (`build.openshift.io/v1`) to Shipwright `Build` CRs (`shipwright.io/v1beta1`). It runs as a standalone binary communicating over stdin/stdout JSON, following the crane plugin protocol.

## Enhancement proposal

https://github.com/konveyor/enhancements/pull/300

## Related repositories

- **crane-lib** (`github.com/konveyor/crane-lib`) — provides the plugin interface (`transform.Plugin`), CLI harness (`transform/cli`), and types (`PluginRequest`, `PluginResponse`). This plugin needs the `NewResources` field in `PluginResponse`, which is pinned in `go.mod` to the published pseudo-version `v0.1.6-0.20260807130033-222a325c7cee`. There is no `replace` directive — the module builds standalone.
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

Requires Go 1.26+ (forced by transitive dependencies). crane-lib is pinned to a published pseudo-version for the `NewResources` API; no sibling `../crane-lib` checkout is required, so `GOWORK=off go build ./...` works from a clean clone.

## Testing

```
GOTOOLCHAIN=auto go test ./...
```
