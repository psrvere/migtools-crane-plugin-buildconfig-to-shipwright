# Architecture

This page is for people and agents who change the plugin. It explains what happens to a
BuildConfig inside the plugin, which parts of the code are risky to touch, and which rules
must stay true. Read it before editing anything under `buildconfig/`. If you run the tool
rather than change it, start with the README and `docs/support-matrix.md` instead.

Two tests in `buildconfig/` keep this page honest. See [What keeps this page true](#what-keeps-this-page-true).

Contents

1. [How the plugin runs](#how-the-plugin-runs)
2. [The conversion, step by step](#the-conversion-step-by-step)
3. [Steps that depend on each other](#steps-that-depend-on-each-other)
4. [Outcomes, and where they are recorded](#outcomes-and-where-they-are-recorded)
5. [What the plugin generates](#what-the-plugin-generates)
6. [The files, and how carefully to read a change to each](#the-files)
7. [Rules that must stay true](#rules-that-must-stay-true)
8. [Where to add things](#where-to-add-things)
9. [What keeps this page true](#what-keeps-this-page-true)

## How the plugin runs

The plugin is a separate program that crane starts once per resource. crane writes one
JSON object to the plugin's standard input, reads one JSON object back from standard
output, and the process exits. There is no loop inside the plugin. A migration with two
hundred BuildConfigs runs the plugin two hundred times.

`main.go` wires the plugin type into crane-lib's command-line harness. Everything else is
in the `buildconfig` package. The entry point is `Run` in `plugin.go`, and it does one of
three things:

- **The object is not a BuildConfig.** `Run` returns an empty response. crane passes the
  object through untouched. This is how every Deployment, Service and Secret in the export
  goes past this plugin.
- **The object is a BuildConfig and it converts.** `Run` returns "delete the original"
  (`IsWhiteOut`) plus the new resources (`NewResources`): one Shipwright Build and, when
  needed, a ServiceAccount and a ConfigMap.
- **The object is a BuildConfig and it does not convert.** `Run` returns a small JSON patch
  that adds two annotations to the original BuildConfig saying it was skipped or failed,
  and why. The BuildConfig stays in the migration output so an auditor can find it.

`Run` returns a Go error only for three problems that happen before conversion starts: the
flags cannot be parsed, the object cannot be re-encoded, or it cannot be decoded as a
BuildConfig. crane treats any plugin error as fatal for the whole run, so `Convert` itself
never returns one. A BuildConfig that cannot be converted is a `failed` outcome, not an
error.

```mermaid
flowchart TD
    A[crane writes one object to stdin] --> B{Is it a BuildConfig?}
    B -- no --> C[Empty response: pass through untouched]
    B -- yes --> D[Parse flags, decode into a typed BuildConfig]
    D --> E[Convert]
    E --> F{Outcome}
    F -- converted, converted-with-warnings --> G[Whiteout the original, emit the new resources]
    F -- skipped, failed --> H[Pass through with an annotation patch saying why]
    C --> Z[One response on stdout, exit]
    G --> Z
    H --> Z
```

`Metadata` declares the six flags and `ParseOptionalFields` parses them, both in
`plugin.go`. `crane transform optionals` prints them. They are: `registry-mapping`,
`imagestream-mapping`, `default-build-strategy`, `search-registries`,
`insecure-registries`, `block-registries`.

## The conversion, step by step

`Convert` in `converter.go` builds one Shipwright Build from one BuildConfig. It walks the
BuildConfig field group by field group in a fixed order. At each step it writes what
Shipwright can express and records a warning for anything it drops. Three steps can end
the conversion early. The rest only add to the Build or warn.

| # | Step | Function | Reads | Writes | Can end the conversion |
|---|---|---|---|---|---|
| 1 | Skeleton | inline in `Convert` | name, namespace, labels, annotations | a new Build with the same name (sanitized by `uniqueName`), copied labels and annotations, and the `converted-from` annotation | no |
| 2 | Strategy | inline switch, then `processDockerStrategy` or `processSourceStrategy` | `spec.strategy` | strategy name (`buildah` or `source-to-image`, or the override from `default-build-strategy`), the strategy params, `spec.env`, `spec.volumes` | Custom and JenkinsPipeline: skipped. Unknown type: failed. A `from` image that cannot be resolved: failed |
| 3 | Output-image gate | inline in `Convert` | `spec.output.to` | nothing | Missing or empty: skipped. Shipwright requires an output image |
| 4 | Pull secret | inline, `getPullSecret`, `generateServiceAccount` | the strategy's `pullSecret`, `spec.serviceAccount` | a new ServiceAccount carrying the secret, only when the BuildConfig names no service account | serialization error: failed |
| 5 | Named service account | inline in `Convert` | `spec.serviceAccount` | nothing; warns that its secrets and RBAC must be recreated by hand | no |
| 6 | Inline Dockerfile | `processInlineDockerfile` in `dockerfile.go` | `spec.source.dockerfile` | Docker strategy: a ConfigMap holding the Dockerfile, plus a pointer annotation on the Build. Source strategy: dropped with a warning | serialization error: failed |
| 7 | Source | `processSource`, `processGitProxyConfig` | `spec.source` | `spec.source` as Git, Local (single-file binary) or OCIArtifact (one image); `contextDir`; proxy env vars | more than one source type, an archive binary, more than one image, or a bad image reference: failed |
| 8 | Output | `processOutput`, `processOutputImageLabels` | `spec.output` and the two mapping flags | `spec.output.image`, `pushSecret`, `labels` | no |
| 9 | Completion deadline | `processCompletionDeadline` | `completionDeadlineSeconds` | `spec.timeout`; out-of-range values are dropped | no |
| 10 | Node selector | `processNodeSelector` | `nodeSelector` | `spec.nodeSelector`; if any key is invalid the whole map is dropped | no |
| 11 | Run policy | `processRunPolicy` | `runPolicy` | nothing; warns for anything other than Serial | no |
| 12 | Post-commit hook | `processPostCommit` in `postcommit.go` | `postCommit` | nothing; warns that the hook is dropped | no |
| 13 | History limits | `processBuildsHistoryLimits` | the two history limits | `spec.retention`; values outside 1 to 10000 are dropped | no |
| 14 | Registries | `addRegistries` | the three registry flags | strategy params for search, insecure and block lists. For source-to-image the insecure list sets `spec.output.insecure` instead, because Shipwright does the push there | no |
| 15 | Triggers | `processTriggers` in `triggers.go` | `spec.triggers` | the original triggers as an annotation, secrets removed; one warning per trigger and one summary | no |
| 16 | Resources | `processResources` | `spec.resources` | a BuildRun template with the requests and limits, stored as an annotation. Never a live BuildRun | template cannot be marshalled: failed |
| 17 | Outcome | inline in `Convert` | the warnings recorded since step 1 | the `conversion-outcome` annotation, and the `conversion-warnings` annotation when any warning fired | no |
| 18 | Serialize | `toUnstructured`, `stripSerializationNoise` | the Build, ServiceAccount, ConfigMap | the resources crane will write | conversion error: failed |

```mermaid
flowchart TD
    S1[1 Skeleton] --> S2{2 Strategy type}
    S2 -- Custom, JenkinsPipeline --> SK[skipped]
    S2 -- unknown --> FA[failed]
    S2 -- Docker --> S2a[processDockerStrategy]
    S2 -- Source --> S2b[processSourceStrategy]
    S2a --> S3{3 Has output image?}
    S2b --> S3
    S3 -- no --> SK
    S3 -- yes --> S4[4 Pull secret to ServiceAccount]
    S4 --> S5[5 Named service account warning]
    S5 --> S6[6 Inline Dockerfile to ConfigMap]
    S6 --> S7[7 Source]
    S7 -- bad source --> FA
    S7 --> S8[8 Output]
    S8 --> S9[9 to 13 Deadline, node selector, run policy, post-commit, history limits]
    S9 --> S14[14 Registries]
    S14 --> S15[15 Triggers]
    S15 --> S16[16 Resources]
    S16 --> S17{17 Any warnings?}
    S17 -- no --> C1[converted]
    S17 -- yes --> C2[converted-with-warnings]
    C1 --> S18[18 Serialize]
    C2 --> S18
```

Every warning goes through one function, `warnf` in `outcome.go`. It prefixes the
message with the BuildConfig's namespace and name, appends it to the converter's list, and
logs it. Two places log at ERROR instead of WARN but still record through the same list:
a generated name that still collides after hash-suffixing, and an inline Dockerfile on a
Docker strategy. The step 17 decision, the annotation on the Build, and the `Warnings`
field on the outcome all read that one list.

## Steps that depend on each other

Most steps only read the BuildConfig and write their own part of the Build, so their order
does not matter. Five pairs do depend on order:

| Later step | Needs from an earlier step |
|---|---|
| 14 Registries | the strategy name from step 2, to decide between a param and `output.insecure`; the output image from step 8 |
| 16 Resources | the strategy name from step 2, to fill in step names; the generated ServiceAccount name from step 4 |
| 17 Outcome | every warning, so it must run after every other step |
| 18 Serialize | the annotations written in step 17 |
| 3 Output gate | must run before steps 4 to 18, which all assume an output image exists |

Two ordering problems exist in the code today. Both are known and tracked; neither breaks
a build on the cluster.

- **Step 15 reads what step 16 writes.** The ConfigChange trigger warning checks for the
  BuildRun-template annotation, which step 16 adds one step later. Through `Convert` the
  annotation is never there yet, so the warning always says "create a BuildRun by hand",
  even when the Build is about to carry a template. A unit test seeds the annotation and
  calls `processTriggers` directly, so it passes without exercising the real order. The fix
  is to run step 16 before step 15.
- **Step 2 runs before the gate in step 3.** A BuildConfig with no output image goes
  through the whole strategy step, raises its warnings about build args or volumes, and is
  then skipped. Skipped and failed outcomes carry no warnings, so those warnings survive
  only in the log. Nothing is wrong on the cluster; the audit trail is incomplete.

## Outcomes, and where they are recorded

Every BuildConfig ends in exactly one of four states, defined in `outcome.go`.

| State | Meaning | Recorded on | As |
|---|---|---|---|
| `converted` | a Build was generated and nothing was dropped | the new Build | annotation `buildconfig-to-shipwright/conversion-outcome` |
| `converted-with-warnings` | a Build was generated, but something was dropped or needs review | the new Build | the same annotation, plus `crane.konveyor.io/conversion-warnings` holding the warning text |
| `skipped` | the plugin chose not to convert: Custom or JenkinsPipeline strategy, or no output image | the original BuildConfig, passed through | annotations `conversion-outcome` and `conversion-reason`, added by a JSON patch built in `disposition.go` |
| `failed` | conversion hit an error: an unknown strategy, a bad source or image reference, or a serialization problem | the original BuildConfig, passed through | the same two annotations |

The only transition is `converted` to `converted-with-warnings`, made in step 17 when any
warning was recorded. Skipped and failed are final the moment they are returned.

Two size limits protect the output from being rejected by Kubernetes, which caps all
annotations on an object at 256 KiB. `boundedWarnings` cuts the warnings annotation at
32 KiB, keeps only whole warnings, and adds a line saying how many it left out.
`truncateReason` cuts the reason annotation at 4 KiB. The log always has the full text.

## What the plugin generates

| Resource | When | Name |
|---|---|---|
| Shipwright `Build` | always, for a converted BuildConfig | the BuildConfig's name, sanitized to a valid DNS label |
| `ServiceAccount` | the strategy has a pull secret and the BuildConfig names no service account | the BuildConfig's name, sanitized |
| `ConfigMap` | an inline Dockerfile on a Docker strategy | the BuildConfig's name plus `-dockerfile`, sanitized |
| BuildRun template | `spec.resources` has requests or limits | not a resource: YAML text in the `buildconfig-to-shipwright/buildrun-template` annotation |

Names go through `uniqueName` in `converter.go`. It lowercases, replaces invalid characters,
trims to 63 characters, and, if the sanitized name would collide with another original name
of the same kind in the same namespace, adds a hash suffix. 63 is the DNS label limit rather
than the 253-character name limit because Shipwright appends its own suffixes. The same input
always produces the same output, so converting twice is safe.

The annotations the plugin writes, and where:

| Annotation | On | Written when |
|---|---|---|
| `crane.konveyor.io/converted-from` | Build, ConfigMap | always |
| `crane.konveyor.io/conversion-warnings` | Build | at least one warning fired |
| `buildconfig-to-shipwright/conversion-outcome` | Build, or the passed-through BuildConfig | always |
| `buildconfig-to-shipwright/conversion-reason` | passed-through BuildConfig | skipped or failed |
| `buildconfig-to-shipwright/buildrun-template` | Build | `spec.resources` is set |
| `buildconfig-to-shipwright/original-triggers` | Build | `spec.triggers` is not empty |
| `buildconfig-to-shipwright/inline-dockerfile-configmap` | Build | inline Dockerfile on a Docker strategy |

## The files

Each file carries a label that says how a change to it should be reviewed.

- **Read every changed line.** A mistake here pushes the wrong image, deletes the original
  with nothing usable in its place, or reports a lossy conversion as clean. The reviewer
  reads the diff, and the author explains which rule from the next section they relied on.
- **Trust the tests.** A mistake here fails loudly on the cluster or leaves a warning the
  user can act on. The reviewer reads the result, not the diff.

| File | What lives there | Review |
|---|---|---|
| `main.go` | wires the plugin into crane-lib's CLI harness | trust the tests |
| `buildconfig/plugin.go` | `Run`, `Metadata`, the flag names, flag parsing, the pass-through with disposition | read every changed line |
| `buildconfig/converter.go` | `Convert`, the `Converter` type, `uniqueName`, and most of the steps | see below |
| `buildconfig/outcome.go` | the four outcome states and `warnf` | read every changed line |
| `buildconfig/disposition.go` | the JSON patch that annotates a skipped or failed BuildConfig | read every changed line |
| `buildconfig/imagestream.go` | resolves `from` and image-source references through the mapping flags or the internal-registry fallback | read every changed line |
| `buildconfig/triggers.go` | step 15: preserve triggers as an annotation, strip webhook secrets, warn | read every changed line |
| `buildconfig/dockerfile.go` | step 6: inline Dockerfile to ConfigMap | read every changed line |
| `buildconfig/names.go` | DNS-1123 sanitizing and hash suffixing | trust the tests |
| `buildconfig/postcommit.go` | step 12: post-commit hook warning | trust the tests |
| `buildconfig/*_test.go` | the unit tests | trust the tests |

Inside `converter.go`, read every changed line in the strategy switch and both strategy
handlers, the output gate, `processSource`, `processOutput`, `addRegistries`, `uniqueName`,
the outcome block, and `toUnstructured`. Trust the tests for `processCompletionDeadline`,
`processNodeSelector`, `processRunPolicy`, `processBuildsHistoryLimits`,
`processResources` and `processStrategyVolumes`.

Outside the Go code, read every changed line in `tests/e2e-cluster.sh`,
`tests/e2e-transform.sh`, the two `expected-build.yaml` golden files, and the workflows
under `.github/`. Each of those can turn CI green without proving anything.

## Rules that must stay true

Each rule names the test that fails if it is broken. Where no test pins a rule, the table
says so. Rules marked ADR have a decision record in `docs/adr/` that explains the
reasoning. Those records do not exist yet; the numbers below are the planned ones.

| # | Rule | Why | Pinned by |
|---|---|---|---|
| 1 | Every dropped or degraded field is recorded through `warnf` (or `recordWarning` for the two ERROR-level drops). A direct `Log.Warn` for a drop is a review error | a warning that bypasses the list makes a lossy conversion look clean. This happened once, for every trigger type | `attribution_test.go` (`TestConvertSilentDropsAreRecorded`); otherwise convention and review. ADR-0003 |
| 2 | The plugin never contacts a cluster. Image references resolve from flags or the documented fallback | the reason this plugin exists instead of the old `crane convert` | convention only. ADR-0001 |
| 3 | One BuildConfig that cannot convert never aborts the crane run. `Run` returns no error for it; the object passes through annotated | crane aborts the whole migration on any plugin error | `outcome_test.go` (`TestRunDoesNotAbortOnConversionFailure`). ADR-0002 |
| 4 | Field processing and its warnings run only after the three early exits (Custom, JenkinsPipeline, missing output) | otherwise an unconverted BuildConfig gets false "field dropped" warnings | `postcommit_test.go` (`TestPostCommitSilentOnPassThroughPaths`) |
| 5 | Strategy parameter names are a wire contract with the ClusterBuildStrategy. Renaming one without a matching catalog change drops the value on the cluster | Shipwright refuses to register a Build whose params it does not know | string assertions in `converter_test.go`; `tests/e2e-cluster.sh` checks `registered=True`. ADR-0004 |
| 6 | Out-of-range or invalid values are warned about and dropped whole. Never clamped, never partly applied | clamping rewrites user intent silently | `converter_test.go` (retention), `nodeselector_test.go` |
| 7 | Never emit a ServiceAccount with the same name as one the BuildConfig names | crane migrates that account separately; a same-named emit overwrites its pull secrets | `converter_test.go` (`TestNamedServiceAccountWithPullSecretIsNotGenerated`). ADR-0006 |
| 8 | Never guess a push-secret name. Warn instead | a guessed name gives a Build that Shipwright marks as missing its secret | `converter_output_credentials_test.go`. ADR-0006 |
| 9 | The BuildRun template is inert text in an annotation, written only when `spec.resources` has requests or limits | a live BuildRun in the migration stream would start a build on apply | `converter_test.go` (resources tests). ADR-0005 |
| 10 | Converted volumes keep their exact BuildConfig names | Shipwright matches volumes by name; a rename hides the cluster's error | `volumes_test.go`. ADR-0007 |
| 11 | The warnings annotation stays under 32 KiB and says when it was cut | Kubernetes rejects an object whose annotations exceed 256 KiB; warning text contains user-controlled names | `attribution_test.go` (`TestWarningsAnnotationStaysBounded`). ADR-0003 |
| 12 | Generated names are valid DNS labels, collision-proof, and stable across runs. Output YAML is byte-stable for the same input | converting twice must give the same result | `names_test.go`, `serialization_test.go` |
| 13 | Triggers are preserved and warned about, never converted. The preservation annotation never carries webhook secrets | no trigger type works after migration today | `triggers_test.go`. ADR-0008 |
| 14 | Never generate a volume for a source secret or ConfigMap | the Dockerfile also needs an edit the plugin cannot make; half the job produces builds that fail silently | `converter_test.go` (source secrets and ConfigMaps tests) |
| 15 | A convertible BuildConfig always produces a Build. Degraded and warned, never blocked | the migration's job is to get resources onto the target and report gaps | `outcome_test.go` (`TestConvertOutcomeConvertedWithWarnings`) |

## Where to add things

- **A new warning.** Call `warnf`. Nothing else. Then add the field's row to
  `docs/support-matrix.md`; the matrix test fails until you do.
- **A new flag.** Add the constant and its `OptionalFields` entry in `Metadata`, the field on
  `PluginOptionalFields`, and the parsing line in `ParseOptionalFields`. All three are in
  `plugin.go`.
- **A new step.** Write a `process*` method on `Converter` and call it from `Convert` after
  the output gate and before the outcome block. If it can fail, return an error and let
  `Convert` turn it into a `failed` outcome. Add its row to the step table above.
- **A new generated resource.** Name it through `uniqueName`, append it to `newResources`
  in `Convert`, and set the `converted-from` annotation on it.
- **A new annotation.** Add the constant next to the others at the top of `converter.go` and
  a row to the annotations table above.
- **A new build strategy.** Add a case to the switch in `Convert` and a handler like
  `processDockerStrategy`. The handler must set the strategy name before returning, because
  steps 14 and 16 read it.

## What keeps this page true

Two tests in `buildconfig/architecture_doc_test.go`:

- `TestArchitectureDocNamesEveryFileAndStage` fails if a non-test Go file, or a
  `process*` method on `Converter`, is missing from this page. A new file or step forces a
  line here.
- `TestInvariantsCiteRealTests` fails if a test named in the rules table no longer exists.

They check that names appear, not that descriptions are right. Review keeps the wording
true. When a change touches the order of steps, an outcome, or a rule, the PR updates this
page first and the reviewer reads the doc diff before the code diff.

## Pending changes

Open pull requests that will change this page. Each section is rewritten when its PR merges.

| PR | Story | What changes |
|---|---|---|
| [#23](https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/23) | BUILD-2265 | Adds a step, `processMountTrustedCA`, that appends a `trusted-ca` volume and emits a third generated resource, a CA-bundle ConfigMap. Adds four constants for it. The branch predates the outcome model and records its warnings with `Log.Warnf` instead of `warnf`, so it needs a rebase and a port to rule 1 before it lands |
| [#63](https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/63) | | Adds a Go E2E framework under `tests/e2e` and `tests/framework`, driven by `tests/rules.yaml` and YAML fixtures. Those files need a review label in [The files](#the-files) |
| [#62](https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/62) | | Downgrades the Go toolchain to 1.25. No effect on this page beyond the build instructions in the README |
