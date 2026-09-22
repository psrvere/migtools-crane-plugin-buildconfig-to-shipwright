# BuildConfig to Shipwright Plugin Tests

Offline conversion suite for the plugin. Every fixture under `testdata/` goes through
`plugin.Run()` as a Go library call, and what comes back is compared with the golden files
committed beside it. No crane binary, no cluster.

## Approach

```
testdata/NN-<case>/buildconfig.yaml → plugin.Run() → compare with expected_<Kind>.yaml → ✅
```

## Quick Start

```bash
cd tests

# Run every case
GOWORK=off go test ./e2e -count=1

# Run one case, by its Entry label
GOWORK=off go test ./e2e -v -ginkgo.focus="webapp-docker"
```

`GOWORK=off` runs the suite the way CI does; a `go.work` outside this repo can resolve
different dependency versions. **Requirements:** the Go version in `tests/go.mod`
(`GOTOOLCHAIN=auto` fetches it). Nothing else.

## Structure

```
tests/
├── framework/
│   ├── plugin.go           # runs plugin.Run() directly on a parsed BuildConfig
│   └── validation.go       # compares generated resources with the golden files
├── e2e/
│   ├── e2e_suite_test.go   # Ginkgo setup
│   └── conversion_test.go  # DescribeTable, one Entry per testdata directory (27 today)
├── testdata/
│   ├── 01-datagrid-hotrod/         # one directory per case
│   │   ├── buildconfig.yaml        # the input
│   │   ├── flags.json              # optional: crane --optional-flags for this case
│   │   └── expected_Build.yaml     # golden, one expected_<Kind>.yaml per generated resource
│   ├── 06-jenkins-pipeline/
│   │   └── expected_annotations.json   # instead of a golden: the outcome annotations of a passthrough
│   ├── ...
│   └── e2e-*/                      # cluster cases for e2e-cluster.sh, not read by this suite
├── go.mod                  # its own module; `replace` points at the plugin one level up
└── e2e-cluster.sh          # cluster-based integration tests
```

## How a case is checked

Each `testdata/NN-<slug>/` directory is one `Entry` in `e2e/conversion_test.go`. For it,
the suite:

1. Parses `buildconfig.yaml`, a multi-document file. Only documents whose kind is
   `BuildConfig` reach the plugin; a Template or a List is left alone, its objects are not
   unwrapped, and nothing is generated (cases 21 and 22). A file with several BuildConfigs
   converts each one (case 03).
2. Reads `flags.json` when it exists and passes its keys as crane `--optional-flags`, the
   way `imagestream-mapping` and `registry-mapping` reach the plugin.
3. Calls `plugin.Run()` and collects every resource it returns: `Build`, `ServiceAccount`,
   `ConfigMap`.
4. Compares each kind with `expected_<Kind>.yaml`:
   - the file exists and has content: the generated resources of that kind, joined with
     `---` when there are several, must equal it after YAML normalization. Both sides are
     parsed and re-marshalled, so key order and formatting do not matter, and every value
     does;
   - the file is missing or empty: nothing of that kind may be generated.
5. When `expected_annotations.json` exists, checks that the patch the plugin returns for
   the first BuildConfig sets every listed annotation to the listed value. This is how a
   passthrough (Custom strategy, no output image) is asserted: no golden, an outcome and a
   reason.

Every Entry runs; there is no skip outcome. A case that must produce no Build has no
`expected_Build.yaml`, and the suite fails if one appears.

## Adding a case

1. Create `testdata/NN-<slug>/` with the next free number and put the input in
   `buildconfig.yaml`. Add `flags.json` when the case needs plugin flags.
2. Generate the golden from the plugin, never by hand: drive the built plugin over the
   fixture (the stdin drive in `/tech-test`) and save each emitted resource as
   `expected_<Kind>.yaml`. A hand-written golden encodes what you expect; a generated one
   encodes what the code does. For a passthrough, write `expected_annotations.json`
   instead, holding the outcome annotations the plugin sets.
3. Add the Entry to `e2e/conversion_test.go`:

   ```go
   Entry("my-case", "NN-my-case", "what the case covers"),
   ```

4. Run it: `GOWORK=off go test ./e2e -v -ginkgo.focus="my-case"`.
5. Add the case to the list under Test Results below.

Fixtures carry generic names only. The repository is public, so nothing that came from a
customer lands here as it arrived.

## CI

`.github/workflows/go.yml` runs the suite on every pull request and on every push to
`main`, as the "E2E plugin conversion tests" step after the unit tests. Nothing beyond Go
is installed for it.

## What is not tested here

- the crane workflow (`export`, `transform`, `apply`) and plugin loading
- image builds and BuildRuns on a cluster

`e2e-cluster.sh` covers those on Minikube. `.github/workflows/test-e2e-minikube-pr.yml`
runs it on every pull request, and `../hack/README.md` explains the setup.

## Test Results

Every directory under `tests/testdata/NN-*` is one Entry, and every Entry runs; nothing is
skipped. 27 cases as of this file, in three groups by what the directory holds.

**Golden comparison (22 cases).** The generated resources must match the
`expected_<Kind>.yaml` files after YAML normalization:

- ✅ 01-datagrid-hotrod — S2I with triggers
- ✅ 02-cakephp-mysql — S2I with postCommit
- ✅ 03-docker-and-s2i — Multi-BuildConfig (2 Builds)
- ✅ 04-webapp-docker — Docker strategy
- ✅ 05-api-s2i — S2I strategy
- ✅ 08-docker-with-envvars — Docker with envvars
- ✅ 09-s2i-with-envvars — S2I with envvars
- ✅ 10-docker-with-volumes — Docker with volumes
- ✅ 13-generic-test-build — S2I with Generic trigger
- ✅ 14-docker-postcommit — Docker with postCommit
- ✅ 15-build-with-proxy — S2I with proxy
- ✅ 16-imagesource-cross-namespace — Docker with cross-namespace ImageStream
- ✅ 17-docker-nocache — Docker nocache
- ✅ 18-serviceaccount-override — ServiceAccount override
- ✅ 19-docker-imagestream-ruby — Docker with ImageStream
- ✅ 20-s2i-imagestream-nodejs — S2I with ImageStream
- ✅ 23-imagechange-trigger — S2I with ImageChange trigger
- ✅ 11-s2i-with-volumes — S2I with binary directory source and volumes
- ✅ 24-binary-docker-certs — Docker with binary directory source, source configMaps and secrets
- ✅ 25-binary-asfile — Docker with a single-file binary source (asFile)
- ✅ 26-binary-docker-resources — Docker with binary directory source and resources
- ✅ 27-s2i-forbidden-env — S2I with env names Shipwright forbids

**Passthrough with outcome annotations (3 cases).** No Build is generated; the
BuildConfig comes back with the annotations in `expected_annotations.json`:

- ✅ 06-jenkins-pipeline — JenkinsPipeline rejected
- ✅ 07-custom-strategy — Custom strategy rejected
- ✅ 12-pullsecret-nodejs — Missing output rejected

**No BuildConfig extracted (2 cases).** The input is a Template or a List, and the
suite expects nothing to be generated:

- ✅ 21-template-negative — Template ignored (negative test)
- ✅ 22-list-negative — List ignored (negative test)

## Troubleshooting

- **`<Kind> mismatch (see expected_<Kind>.yaml)`**, followed by `Line N` with `Expected`
  and `Actual`: the plugin's output moved. Decide whether the code or the golden is wrong;
  when the code is right, regenerate the golden from the plugin.
- **`Unexpected <Kind> generated`**: the plugin now emits a kind the case has no golden
  for. Add `expected_<Kind>.yaml` if that is intended.
- **`Expected <Kind> (from expected_<Kind>.yaml), but none generated`**: the conversion
  now fails or skips this input. Read the outcome annotations with the stdin drive.
- **Build errors under `tests/`**: this is its own module. Run `go build ./...` here, and
  `go mod tidy` when a plugin package it imports has moved.
