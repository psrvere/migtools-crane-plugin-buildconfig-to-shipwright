# BuildConfig to Shipwright Plugin Tests

Focused unit test suite for validating BuildConfig → Shipwright Build conversion.

## Approach

**Direct plugin execution + Golden file comparison** - No crane binary or cluster needed!

```
BuildConfig YAML → Parse → plugin.Run() → Compare with Expected Output → ✅
```

Tests call the plugin directly as a Go library and compare output against expected golden files.

## Quick Start

**Note:** Tests are run manually/locally on demand (not in CI).

```bash
cd tests

# Run all tests
go test ./e2e -v

# Run single test
go test ./e2e -v -ginkgo.focus="webapp-docker"

# Run only tests with golden files
go test ./e2e -v -ginkgo.focus="docker|s2i|webapp"
```

**Speed:** ~0.012 seconds for 10 tests (with golden files)  
**Requirements:** Go 1.22+ only (no crane, no cluster)

## Structure

```
tests/
├── framework/
│   ├── plugin.go           # runs plugin.Run() directly on a parsed BuildConfig
│   └── validation.go       # compares generated resources with the golden files
├── e2e/
│   ├── e2e_suite_test.go   # Ginkgo setup
│   └── conversion_test.go  # DescribeTable, one Entry per testdata directory (25 today)
├── testdata/
│   ├── 01-datagrid-hotrod/         # one directory per case
│   │   ├── buildconfig.yaml        # the input
│   │   ├── flags.json              # optional: crane --optional-flags for this case
│   │   └── expected_Build.yaml     # golden, one expected_<Kind>.yaml per generated resource
│   ├── 06-jenkins-pipeline/
│   │   └── expected_annotations.json   # instead of a golden: the outcome annotations of a passthrough
│   ├── ...
│   └── e2e-*/                      # cluster cases for e2e-cluster.sh, not read by this suite
└── e2e-cluster.sh          # cluster-based integration tests
```

## Test Coverage

### 25 test cases

**18 from real-world scenarios (issues #833-#850):**
- Docker + S2I combinations
- Environment variables and volumes
- Pull secrets and proxies
- Post-commit hooks
- Service account overrides
- No-cache builds
- ImageSource cross-namespace

**2 from PR#60 cluster tests:**
- Docker + ImageStream (Ruby)
- S2I + ImageStream (Node.js)

### Validation Approach

**Golden File Comparison:**
- Tests with expected outputs in `expected_output/` compare complete YAML
- Exact field-by-field comparison
- 10 tests currently have golden files

**Tests without golden files:**
- Skipped (incomplete BuildConfigs or Templates)
- Can be added later as needed

### What's Validated

When golden files exist, tests validate:

1. **API Version** - Must be `shipwright.io/v1beta1`
2. **Strategy Mapping**
   - Docker → buildah
   - Source → source-to-image
   - JenkinsPipeline/Custom → skipped
3. **Annotations**
   - `crane.konveyor.io/converted-from`
   - Conversion outcome tracking
4. **Field Mappings**
   - Git source (URI, ref, contextDir)
   - Output image
   - Dockerfile path
   - Timeouts and retention
5. **Labels Preservation**
6. **Triggers** - Preserved in annotations
7. **Environment Variables** - Preserved
8. **Volumes** - Preserved

## How Tests Work

Each test specifies its expected outcome:

1. **`"pass"`** - Expects Build generated and matching golden file
   - Parses BuildConfig YAML from `testdata/buildconfig_yamls/`
   - Calls `plugin.Run()` directly (no crane binary)
   - Compares actual vs expected YAML (exact match)
   - Fails if no Build generated or if it differs from golden file

2. **`"empty"`** - Expects NO Build generated (correct plugin behavior)
   - Used for JenkinsPipeline strategy (unsupported)
   - Used for BuildConfigs missing required fields (e.g., spec.output.to)
   - Asserts plugin correctly returns empty (not an error)
   - Fails if Build IS generated

3. **`"skip"`** - Skips test (incomplete test data)
   - Used for Templates/Lists that need unwrapping
   - Used for tests with known issues
   - Does not fail the test suite

**Test outcomes:**
- ✅ **Pass** - Build matches expected (or correctly empty)
- ❌ **Fail** - Build differs from expected (or unexpected outcome)
- ⊘ **Skip** - Test explicitly skipped

## Example Output

```
Running Suite: BuildConfig to Shipwright Conversion Suite
==========================================================

✓ [#835] docker-and-s2i [PASSED] [0.001s]
✓ [#836] webapp-docker [PASSED] [0.001s]
✓ [#837] api-s2i [PASSED] [0.001s]
✓ [#838] jenkins-pipeline (skipped) [PASSED] [0.001s]
✓ [PR#60] docker-imagestream-ruby [PASSED] [0.001s]
✓ [PR#60] s2i-imagestream-nodejs [PASSED] [0.001s]
...

Ran 20 of 20 Specs in 0.012 seconds
SUCCESS! -- 12 Passed | 8 Failed | 0 Pending | 0 Skipped
```

## Adding New Tests

### 1. Add BuildConfig YAML

```bash
cp my-buildconfig.yaml testdata/21-my-test.yaml
```

### 2. Add Test Entry

Edit `e2e/conversion_test.go`:

```go
Entry("[#851] my-test", "21-my-test.yaml", "851", "description"),
```

### 3. Run Test

```bash
go test ./e2e -v -ginkgo.focus="my-test"
```

## Extending Tests

### Add New Golden File Test

1. Create golden file: `tests/testdata/expected_output/21-my-test-expected.yaml`
2. Add test entry:
   ```go
   Entry("[#851] my-test", "21-my-test.yaml", "851", "description", "pass")
   ```

### Add Test Expecting Empty Result

For unsupported strategies or incomplete BuildConfigs:
```go
Entry("[#852] custom", "22-custom.yaml", "852", "Custom strategy", "empty")
```

## CI Integration

E2E tests run automatically on every PR and push to main via GitHub Actions.

**Workflow:** `.github/workflows/go.yml`

```yaml
- name: E2E plugin conversion tests
  env:
    GOPROXY: "https://proxy.golang.org"
  run: |
    cd tests
    go test ./e2e -v
    echo "## E2E Test Results" >> "$GITHUB_STEP_SUMMARY"
    echo "✅ Plugin conversion tests passed" >> "$GITHUB_STEP_SUMMARY"
```

**Runs on:**
- All pull requests (gates merging)
- Push to main branch (post-merge validation)

**No dependencies needed in CI** - just Go!

## Benefits

✅ **Fast** - 0.011s for all 20 tests  
✅ **Simple** - No crane binary, no cluster, no rule engine  
✅ **Focused** - Tests plugin logic, not crane workflow  
✅ **Maintainable** - Golden file comparison only (~270 LOC framework)  
✅ **Explicit** - Each test declares expected outcome (pass/empty/skip)  
✅ **Comprehensive** - 20 test cases covering all scenarios  

## What's NOT Tested

This framework focuses on **plugin conversion correctness**. It does NOT test:

- ❌ crane CLI workflow (export/transform/apply)
- ❌ Plugin loading mechanism  
- ❌ Actual image builds on cluster
- ❌ BuildRun execution

**For integration testing:** Use crane's E2E tests or the bash scripts in this repo (`e2e-cluster.sh`, `e2e-transform.sh`).

## Relationship to PR#60

PR#60 added cluster-based integration tests (Bash scripts). This framework:
- **Complements** those tests (unit vs integration)
- **Includes** their test cases (#19, #20) as unit tests
- **Validates** conversion logic they depend on
- **Runs faster** for development iteration

Both are valuable:
- **Unit tests (this):** Fast feedback on conversion logic
- **Cluster tests (PR#60):** Full workflow validation

## Test Results

Every directory under `tests/testdata/NN-*` is one Entry, and every Entry runs; nothing is
skipped. 25 cases as of this file, in three groups by what the directory holds.

**Golden comparison (20 cases).** The generated resources must match the
`expected_<Kind>.yaml` files byte for byte:

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

**Passthrough with outcome annotations (3 cases).** No Build is generated; the
BuildConfig comes back with the annotations in `expected_annotations.json`:

- ✅ 06-jenkins-pipeline — JenkinsPipeline rejected
- ✅ 07-custom-strategy — Custom strategy rejected
- ✅ 12-pullsecret-nodejs — Missing output rejected

**No BuildConfig extracted (2 cases).** The input is a Template or a List, and the
suite expects nothing to be generated:

- ✅ 21-template-negative — Template ignored (negative test)
- ✅ 22-list-negative — List ignored (negative test)

Regenerate a golden from the plugin, never by hand: drive the built plugin over the
`buildconfig.yaml` (see `/tech-test`, the stdin drive) and save what it emitted as
`expected_<Kind>.yaml`.

## Troubleshooting

### Tests Fail

1. Check error message for which rule failed
2. Compare expected vs actual values
3. Fix plugin code or update rule definition

### Rule Definition Issues

- Verify YAML syntax in `rules.yaml`
- Check field paths use dot notation correctly
- Ensure rule type exists in `rule_evaluator.go`

### Plugin Build Issues

```bash
# Ensure plugin builds
cd ..
go build .
```
