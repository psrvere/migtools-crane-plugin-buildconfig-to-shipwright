# BuildConfig to Shipwright Build: support matrix

This page lists every field of an OpenShift `BuildConfig`, what the plugin does with it, where
it lands in the Shipwright `Build`, what you have to do by hand afterwards, and the warning you
will see in the log. Read it before a migration to know what will and will not carry over, and
after one to understand a warning you got.

Rows that lose something come first in every section. Clean conversions come last.

Contents

1. [How to read this page](#how-to-read-this-page)
2. [What stops a BuildConfig from converting](#what-stops-a-buildconfig-from-converting)
3. [Field by field](#field-by-field)
4. [Plugin flags](#plugin-flags)
5. [What the plugin writes](#what-the-plugin-writes)
6. [Warning reference](#warning-reference)

## How to read this page

Every BuildConfig ends in exactly one of four outcomes.

| Outcome | What it means | What you get |
|---|---|---|
| `converted` | a Build was generated and nothing was dropped | a Build, and sometimes a ServiceAccount or ConfigMap next to it |
| `converted-with-warnings` | a Build was generated, but at least one field was dropped, changed, or needs work on your side | the same, plus the warnings in an annotation on the Build |
| `skipped` | the plugin chose not to convert this BuildConfig | the original BuildConfig, unchanged, with an annotation saying why |
| `failed` | the plugin could not convert this BuildConfig | the same as skipped, with a different reason |

A skipped or failed BuildConfig never stops the migration. The other resources in the export
still convert. Look for `buildconfig-to-shipwright/conversion-outcome` on each object to find
out what happened to it.

The "What happens" column uses these words:

- **Converted.** The value is carried into the Build. Nothing to do.
- **Converted, with a warning.** The value is carried, but the Build will not run correctly
  until you do something. The warning says what.
- **Dropped.** The value is not carried. The rest of the BuildConfig still converts. The
  warning says what you lost.
- **Dropped silently.** The value is not carried and there is no warning. These are listed so
  you can check for them yourself.
- **Skipped** and **Failed.** The whole BuildConfig is not converted. See the next section.

Warnings are quoted in the [Warning reference](#warning-reference) at the end, keyed W1 to W62.
In the quotes, `…` marks a value the plugin fills in, such as a BuildConfig name.

## What stops a BuildConfig from converting

| Condition | Outcome | What you do by hand | Reason recorded |
|---|---|---|---|
| `spec.strategy.type: Custom` | skipped | Rewrite the build as a Shipwright ClusterBuildStrategy or a Tekton Task | W4 |
| `spec.strategy.type: JenkinsPipeline` | skipped | Move the pipeline to Tekton Pipelines | W5 |
| `spec.output.to` missing, or its `name` empty | skipped | Add an output image, or accept that this build has no target and leave it behind | W7 |
| `spec.strategy.type` empty or unrecognised | failed | Fix the BuildConfig on the source cluster first | W6 |
| a strategy `from` or image-source `from` whose `kind` is not `ImageStreamTag`, `ImageStreamImage` or `DockerImage`. For a Docker strategy an empty `kind` counts as unrecognised | failed | Set `kind` on the reference | `unknown image reference kind … for …`, wrapped as `error resolving Docker strategy From field: …`, `error resolving Source strategy From field: …` or `failed to resolve image source (BuildConfig …): …` depending on which reference failed |
| more than one of `source.git`, `source.binary`, `source.images` set | failed | Split into one BuildConfig per source | `multiple source types are not supported in a single build in Shipwright (BuildConfig …)` |
| `source.binary` without `asFile` (an extracted archive) | failed | Shipwright's local source takes a directory upload, not an archive. Switch to a git or single-file source | `binary archive source (extracted archive) is not supported in Shipwright, only single-file binary sources (asFile) (BuildConfig …)` |
| more than one entry in `source.images` | failed | One image source per Build. Split the BuildConfig | `multiple image sources are not supported in Shipwright (BuildConfig …)` |
| the Build, ServiceAccount, ConfigMap or BuildRun template cannot be serialised | failed | Report it. This should not happen on a valid export | `error converting Build to unstructured: …`, `error converting ServiceAccount to unstructured: …`, `error converting inline-Dockerfile ConfigMap to unstructured: …`, `error marshaling BuildRun spec for BuildConfig …: …`, `error unmarshaling BuildRun spec for BuildConfig …: …` or `error marshaling BuildRun template for BuildConfig …: …` |
| the plugin cannot read its flags or decode the BuildConfig | none. The plugin returns an error and crane aborts the whole transform, not just this BuildConfig | Fix the flag value. A BuildConfig that does not decode should be reported | `error parsing optional fields: …`, `error marshaling BuildConfig to JSON: …` or `error decoding BuildConfig: …` |

A BuildConfig skipped for its strategy type has not been looked at any further, so nothing else
is reported for it. One skipped for a missing output image has already had its strategy block
processed: warnings about the strategy image, build args or volumes may appear in the log for
it, and a failure in that block (an unresolvable `from`) is reported as failed, not skipped.
Either way the BuildConfig itself stays exactly as it was.

## Field by field

### Metadata

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `metadata.name` with characters or length not allowed in a DNS-1123 label (63 chars, lowercase, digits, hyphens) | Converted, with a warning. The name is lowercased, invalid runs become `-`, and an 8-character hash of the original is appended | `metadata.name` of the Build | Update anything that refers to the build by its old name | W1 |
| two BuildConfigs whose sanitised names collide, such as `my.app` and `my_app`, which both become `my-app` | Converted, with W1 for each but nothing about the collision. Each BuildConfig is converted on its own, so both Builds get the same name and the later one overwrites the earlier when the output is applied | `metadata.name` | Rename one of them before migrating, or check the output for duplicate Build names | W1 only. W2 and W3 describe this case but cannot fire from the plugin today; see [Warning reference](#warning-reference) |
| `metadata.labels` starting with `openshift.io/build`, or the deprecated `buildconfig` label | Dropped silently (logged at INFO only) | | Nothing. These describe OpenShift build machinery | none |
| `metadata.annotations` starting with `openshift.io/` or `kubectl.kubernetes.io/` | Dropped silently (logged at INFO only) | | Nothing | none |
| `metadata.name`, `metadata.namespace` | Converted | `metadata.name`, `metadata.namespace` | Nothing | none |
| other `metadata.labels` and `metadata.annotations` | Converted | `metadata.labels`, `metadata.annotations`, plus the plugin's own annotations | Nothing | none |

### Strategy

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `strategy.type: Docker` | Converted | `spec.strategy: {kind: ClusterBuildStrategy, name: buildah}`. The name changes with `--default-build-strategy docker=…` | Make sure the `buildah` ClusterBuildStrategy exists on the target | none |
| `strategy.type: Source` | Converted | `spec.strategy: {kind: ClusterBuildStrategy, name: source-to-image}`. The name changes with `--default-build-strategy s2i=…` | Make sure the `source-to-image` ClusterBuildStrategy exists on the target | none |
| `strategy.type: Custom`, `JenkinsPipeline`, empty, unknown | Skipped or failed | | See [What stops a BuildConfig from converting](#what-stops-a-buildconfig-from-converting) | W4, W5, W6 |
| `dockerStrategy.pullSecret` or `sourceStrategy.pullSecret`, with `spec.serviceAccount` also set | Dropped. The plugin does not touch a named ServiceAccount | | Link the secret to that ServiceAccount on the target: `oc -n <ns> secrets link <sa> <secret> --for=pull,mount` | W8 |
| `dockerStrategy.pullSecret` or `sourceStrategy.pullSecret`, no `spec.serviceAccount` | Converted. A new ServiceAccount carrying the secret is generated | a `ServiceAccount` named after the BuildConfig, listed in both `imagePullSecrets` and `secrets`. The BuildRun template, if any, names it | Migrate the secret itself; the plugin only references it | none |
| `spec.serviceAccount` | Converted, with a warning. The name is carried only into the BuildRun template, if one is generated | `spec.serviceAccount` of the BuildRun template | Recreate the account's secrets, image pull secrets and role bindings on the target | W9 |

### Docker strategy

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `dockerStrategy.buildArgs[]` with an invalid name (empty, or containing `=`, `$`, `{`, `}`, whitespace, control characters) | Dropped | | Rename the arg | W12 |
| `dockerStrategy.buildArgs[].valueFrom.configMapKeyRef` or `secretKeyRef` with an empty name or key | Dropped | | Fix the reference | W14, W16 |
| `dockerStrategy.buildArgs[].valueFrom.configMapKeyRef` or `secretKeyRef` with `optional: true` | Converted, with a warning. Shipwright has no optional lookup | `spec.paramValues[build-args]` | Make sure the key exists on the target, or the BuildRun fails | W15, W17 |
| `dockerStrategy.buildArgs[].valueFrom.fieldRef` or `resourceFieldRef` | Dropped | | Set the value directly in the Build | W18 |
| `dockerStrategy.buildArgs[].valueFrom` of any other shape | Dropped | | Set the value directly in the Build | W19 |
| `dockerStrategy.buildArgs[]` with both `value` and `valueFrom` | Converted, with a warning. `valueFrom` wins | `spec.paramValues[build-args]` | Nothing, unless you meant the literal | W13 |
| `dockerStrategy.from` | Converted. The reference is resolved through the mapping flags | `spec.paramValues[runtime-stage-from]` | See [Image references](#image-references) | W11 or W20 when it cannot be resolved |
| `dockerStrategy.volumes[]` | Converted, with a warning | `spec.volumes[]` | See [Strategy volumes](#strategy-volumes) | W24 to W28 |
| `dockerStrategy.buildArgs[]` with a literal `value` | Converted | `spec.paramValues[build-args]` as `NAME=VALUE` | Nothing | none |
| `dockerStrategy.buildArgs[].valueFrom.configMapKeyRef` | Converted | `spec.paramValues[build-args]` as a ConfigMap value reference, resolved at BuildRun time | Migrate the ConfigMap | none |
| `dockerStrategy.buildArgs[].valueFrom.secretKeyRef` | Converted | `spec.paramValues[build-args]` as a Secret value reference | Migrate the Secret | none |
| `dockerStrategy.dockerfilePath` | Converted | `spec.paramValues[dockerfile]` | Nothing | none |
| `dockerStrategy.env[]` | Converted | `spec.env[]` | Nothing | none |
| `dockerStrategy.forcePull: true` | Converted | `spec.paramValues[pull] = always` | Nothing | none |
| `dockerStrategy.noCache: true` | Converted | `spec.paramValues[no-cache] = true` | Nothing | none |
| `dockerStrategy.imageOptimizationPolicy: SkipLayers` or `SkipLayersAndWarn` | Converted | `spec.paramValues[squash] = true` | Nothing | none |
| `dockerStrategy.imageOptimizationPolicy: None` | Converted. No param is written | | Nothing | none |

### Source (S2I) strategy

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `sourceStrategy.scripts` | Dropped | | Bake the scripts into the builder image, or wait for the linked RFE | W21 |
| `sourceStrategy.incremental: true` | Dropped | | Builds start from scratch. Wait for the linked RFE | W22 |
| `sourceStrategy.forcePull: true` | Dropped | | Wait for the linked RFE | W23 |
| `sourceStrategy.from` | Converted. The reference is resolved through the mapping flags. An empty `kind` is treated as `ImageStreamTag` | `spec.paramValues[builder-image]` | See [Image references](#image-references) | W11 or W20 when it cannot be resolved |
| `sourceStrategy.volumes[]` | Converted, with a warning | `spec.volumes[]` | See [Strategy volumes](#strategy-volumes) | W24 to W28 |
| `sourceStrategy.env[]` | Converted | `spec.env[]` | Nothing | none |

### Strategy volumes

Applies to `dockerStrategy.volumes[]` and `sourceStrategy.volumes[]`.

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| a volume with an empty `name` | Dropped | | Name it | W24 |
| a second volume with the same `name` | Dropped | | Remove the duplicate | W25 |
| a volume whose `source.type` is not `Secret` or `ConfigMap` | Dropped | | Only Secret and ConfigMap volumes exist in Shipwright | W26 |
| a `Secret` or `ConfigMap` volume | Converted, with a warning. The Build will not register (`Registered=False`, reason `UndefinedVolume`) until the strategy declares the volume | `spec.volumes[]`, under the same name | Copy the ClusterBuildStrategy, add an overridable volume with that name and a mount at the original path, and point the Build at the copy. See `docs/volume-migration.md` | W27 per volume, W28 once |
| `volumes[].mounts[].destinationPath` | Carried into the warning only. Shipwright takes mount paths from the strategy, not the Build | | Use the path when you edit the strategy copy | in W27 |

### Source

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `source.dockerfile` (inline Dockerfile) with a Docker strategy | Converted, with a warning. The content is saved in a ConfigMap, but the Build cannot build from it yet | a `ConfigMap` named `<buildconfig>-dockerfile` with key `Dockerfile`, and the annotation `buildconfig-to-shipwright/inline-dockerfile-configmap` on the Build | Commit the Dockerfile to the source repository before running the Build | W59 (logged at ERROR) |
| `source.dockerfile` with a Source strategy | Dropped | | Nothing, unless you meant a Docker strategy | W60 |
| `source.configMaps[]` | Dropped | | Add an overridable volume to the strategy, a volume override on the Build, and change `ADD`/`COPY` to `RUN cp` in the Dockerfile | W34 per entry |
| `source.secrets[]` | Dropped | | Same as above | W35 per entry |
| `source.sourceSecret` when `source.git` is not set | Dropped | | Nothing. It did nothing on OpenShift either | W29 |
| no `git`, `binary` or `images` at all | Converted, with a warning. The Build has no `spec.source` | | Add a source, or delete the Build | W30. `contextDir`, `configMaps` and `secrets` are then dropped silently |
| `source.images[].as` | Dropped | | No equivalent | W31 |
| `source.images[].paths` | Dropped. The whole image becomes the source | | Adjust the Dockerfile to the image's layout | W32 |
| `source.binary.asFile` | Converted, with a change. The file name is not carried | `spec.source: {type: Local, local: {name: local-copy, timeout: 10m}}` | Upload the file with `shp build upload` or an equivalent when you run the build | none |
| `source.git.uri`, `source.git.ref` | Converted | `spec.source.git.url`, `spec.source.git.revision` | Nothing | none |
| `source.sourceSecret` with `source.git` | Converted | `spec.source.git.cloneSecret` | Migrate the secret | none |
| `source.git.httpProxy`, `httpsProxy`, `noProxy` | Converted | `spec.env[]` as `HTTP_PROXY` and `http_proxy`, and likewise for the other two | Nothing | none |
| `source.contextDir` | Converted | `spec.source.contextDir` | Nothing | none |
| `source.images[]` with exactly one entry | Converted. The image reference is resolved through the mapping flags | `spec.source: {type: OCIArtifact, ociArtifact: {image, pullSecret}}` | See [Image references](#image-references) | W33 when it cannot be resolved |
| `source.images[].pullSecret` | Converted | `spec.source.ociArtifact.pullSecret` | Migrate the secret | none |
| `source.type` | Ignored. The plugin looks at which of `git`, `binary`, `images` is set | | Nothing | none |

### Output

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `output.to` of kind `ImageStreamTag` with no matching `--imagestream-mapping` | Converted, with a warning. The image becomes `image-registry.openshift-image-registry.svc:5000/<ns>/<name>:<tag>`, then `--registry-mapping` is applied. A name without a tag gets `:latest` | `spec.output.image` | Pass `--imagestream-mapping <ns>/<name>:<tag>=<registry/image:tag>` if the fallback is wrong | W36 |
| `output.to` of kind `ImageStreamTag` whose resolved image is not on the internal registry | Converted, with a warning. The ImageStream on the source cluster will no longer update | `spec.output.image` | Repoint any Deployment or DeploymentConfig that watched the ImageStream | W37 |
| no `output.pushSecret` | Converted, with a warning | | Internal registry: give the BuildRun a ServiceAccount with push access. External registry: set `spec.output.pushSecret` to a registry credential | W38 for ImageStreamTag, W39 for anything else |
| `output.imageLabels[]` with an empty name | Dropped | | Name it | W40 |
| `output.imageLabels[]` with a duplicate name | Converted, with a warning. The last value wins | `spec.output.labels` | Remove the duplicate | W41 |
| `output.to` of kind `ImageStreamTag` with a matching `--imagestream-mapping` | Converted | `spec.output.image`, after `--registry-mapping` | Nothing | none |
| `output.to` of any other kind, including `DockerImage` and `ImageStreamImage` | Converted. The name is copied as written. No mapping flag is applied and there is no warning | `spec.output.image` | Check the registry in the name is reachable from the target | none |
| `output.pushSecret` | Converted | `spec.output.pushSecret` | Migrate the secret | none |
| `output.imageLabels[]` | Converted | `spec.output.labels` | Nothing | none |

### Build settings

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `spec.resources` (requests or limits) | Converted, with a warning. The Build cannot hold resources, so a BuildRun template is generated instead | the annotation `buildconfig-to-shipwright/buildrun-template`, holding a BuildRun with `stepResources` for the strategy's steps and the ServiceAccount | Review the template, then apply it to start a build. With `--default-build-strategy` the step names are unknown and `stepResources` is left out | W50, or W49 with a custom strategy |
| `spec.postCommit` (script, command or args) | Dropped | | Add a test step after the BuildRun in a Tekton Pipeline. It runs after the push, so it cannot block a bad image | W61, and W62 if both script and command are set |
| `spec.runPolicy: Serial` (or unset) | Dropped. BuildRuns run concurrently | | Serialise runs in your pipeline if ordering matters | W45 |
| `spec.runPolicy: SerialLatestOnly` | Dropped | | Serialise and cancel superseded runs in your pipeline | W46 |
| `spec.runPolicy` unrecognised | Dropped | | Nothing | W47 |
| `spec.runPolicy: Parallel` | Converted. Nothing to write, because this is already how BuildRuns behave | | Nothing | none |
| `spec.completionDeadlineSeconds` of 0 or less | Dropped | | Fix the value | W42 |
| `spec.completionDeadlineSeconds` larger than about 9.2 billion | Dropped | | Fix the value | W43 |
| `spec.completionDeadlineSeconds` | Converted | `spec.timeout` | Nothing | none |
| `spec.nodeSelector` with any invalid key or value | Dropped whole. A partial selector would schedule the build somewhere you did not ask for | | Fix the selector | W44 |
| `spec.nodeSelector` | Converted | `spec.nodeSelector` | Nothing | none |
| `spec.successfulBuildsHistoryLimit`, `spec.failedBuildsHistoryLimit` outside 1 to 10000, including 0 | Dropped. BuildRuns for that state are not pruned | | Set a value in range | W48 |
| `spec.successfulBuildsHistoryLimit`, `spec.failedBuildsHistoryLimit` | Converted | `spec.retention.succeededLimit`, `spec.retention.failedLimit` | Nothing | none |

### Triggers

No trigger type works after migration. Neither upstream Shipwright nor the Builds for OpenShift
operator provides webhook or image-change triggering today. Every trigger is dropped with a
warning, and the sanitised list is kept on the Build so it can be rebuilt later.

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `triggers[]` of type `GitHub`, `GitLab`, `Bitbucket` | Dropped | | Remove or repoint the webhook in your Git provider, then use Pipelines-as-Code or Tekton Triggers to create BuildRuns | W52 |
| `triggers[]` of type `Generic` | Dropped | | Same. `allowEnv` has no equivalent | W53 |
| `triggers[]` of type `ImageChange` | Dropped | | Start builds from your own automation when the image changes | W54 |
| `triggers[]` of type `ConfigChange` | Dropped | | Create the first BuildRun yourself. If the Build carries a BuildRun template, apply that | W56 (W55 is not reachable today, see the note below) |
| `triggers[]` of any other type | Dropped | | | W57 |
| any triggers at all | One summary warning, and the triggers are preserved | the annotation `buildconfig-to-shipwright/original-triggers`: type, secret reference name, `allowEnv`, `imageChange.from`, `paused`. Inline secret values and `lastTriggeredImageID` are never included | Keep the annotation until triggers exist in Shipwright | W58, and W51 if the list cannot be encoded |

Note: the plugin has two wordings for the ConfigChange warning. The one that mentions the BuildRun
template (W55) is never used today, because triggers are processed one step before the template
is written. You always get W56, even when the Build carries a template.

### Fields the plugin never reads

These are dropped silently. Check for them yourself.

| Field | Why it matters |
|---|---|
| `spec.mountTrustedCA` | A build that mounted the cluster's trusted CA bundle loses it. No warning. Work in progress: [PR #23](https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/23) (BUILD-2265) maps this field to the `trusted-ca` overridable volume of the shipped strategies, backed by a per-Build ConfigMap that the Cluster Network Operator fills with the CA bundle. Not merged; this row moves to [Build settings](#build-settings) when it lands |
| `spec.revision` | Recorded source revision. Rarely set by hand on a BuildConfig |
| `spec.strategy.customStrategy`, `spec.strategy.jenkinsPipelineStrategy` | The whole BuildConfig is skipped, so the contents are never read |
| `status` | Runtime state of the source cluster. Not configuration |

### Image references

`dockerStrategy.from`, `sourceStrategy.from` and `source.images[].from` all resolve the same way.
The output image resolves slightly differently; see [Output](#output).

| Reference | Result |
|---|---|
| `kind: ImageStreamTag` or `ImageStreamImage`, with `--imagestream-mapping <ns>/<name>=…` | the mapped image, then `--registry-mapping` |
| `kind: ImageStreamTag` or `ImageStreamImage`, no mapping | `image-registry.openshift-image-registry.svc:5000/<ns>/<name>`, then `--registry-mapping`. Warning W20 if the registry mapping changed nothing |
| `kind: DockerImage`, name contains `/` | the name, then `--registry-mapping` |
| `kind: DockerImage`, bare name, with `--imagestream-mapping <ns>/<name>=…` | the mapped image, then `--registry-mapping` |
| `kind: DockerImage`, bare name, no imagestream mapping, but a `--registry-mapping` prefix matches it | the name after `--registry-mapping`, with no warning |
| `kind: DockerImage`, bare name, no mapping of either kind | the name as written. Warning W11, because a bare name may have relied on an ImageStream with `lookupPolicy.local` |
| any other `kind` | the conversion fails |

The `<ns>` in a mapping key is the reference's own namespace, or the BuildConfig's namespace when
the reference has none. For strategy and source references the key uses the name as written. For
the output image a name without a tag is looked up with `:latest` appended.

## Plugin flags

| Flag | Format | What it changes |
|---|---|---|
| `--imagestream-mapping` | `ns/name:tag=registry/image:tag,…` | Replaces ImageStream references, and bare DockerImage names, with a concrete image |
| `--registry-mapping` | `old-registry=new-registry,…` | Rewrites the registry prefix of every resolved image reference, except an output image whose kind is not `ImageStreamTag`, which is copied as written (see [Output](#output)). The longest matching prefix wins |
| `--default-build-strategy` | `docker=name,s2i=name` | Uses a different ClusterBuildStrategy name. With a custom name the BuildRun template omits `stepResources` (W49) |
| `--search-registries` | `registry,…` | `spec.paramValues[registries-search]` |
| `--insecure-registries` | `registry,…` | Keyed on the strategy name written to the Build, not on the BuildConfig's strategy type. `source-to-image`: `spec.output.insecure: true` when the output image is on one of them. Any other name, including a `--default-build-strategy` override for S2I: `spec.paramValues[registries-insecure]` |
| `--block-registries` | `registry,…` | `spec.paramValues[registries-block]` |

These are not top-level flags. Pass them as `name=value` pairs, comma separated, inside crane's
`--optional-flags`. The warnings quote them with a leading `--`; the names are the same.

```bash
crane transform BuildConfigPlugin \
  --plugin-dir ./plugins \
  --optional-flags "registry-mapping=image-registry.openshift-image-registry.svc:5000=quay.io/myorg,imagestream-mapping=myns/mybuilder:latest=quay.io/myorg/builder:latest"
```

`crane transform optionals` prints the same list with examples.

## What the plugin writes

Generated resources, in the order they appear in the output:

| Resource | When |
|---|---|
| `Build` | always, for a converted BuildConfig |
| `ServiceAccount` | the strategy has a pull secret and the BuildConfig names no ServiceAccount |
| `ConfigMap` | an inline Dockerfile on a Docker strategy |

Annotations:

| Annotation | On | When |
|---|---|---|
| `crane.konveyor.io/converted-from` | Build, ConfigMap | always. Value `build.openshift.io/v1/BuildConfig/<name>` |
| `buildconfig-to-shipwright/conversion-outcome` | Build, or the passed-through BuildConfig | always. One of `converted`, `converted-with-warnings`, `skipped`, `failed` |
| `crane.konveyor.io/conversion-warnings` | Build | at least one warning. Cut at 32 KiB, keeping whole warnings; the annotation then ends with `... … more conversion warning(s) omitted to stay within the Kubernetes annotation size limit — see the crane plugin logs for the full list.` and W10 is logged |
| `buildconfig-to-shipwright/conversion-reason` | passed-through BuildConfig | skipped or failed. Cut at 4 KiB |
| `buildconfig-to-shipwright/buildrun-template` | Build | `spec.resources` is set |
| `buildconfig-to-shipwright/original-triggers` | Build | `spec.triggers` is not empty |
| `buildconfig-to-shipwright/inline-dockerfile-configmap` | Build | inline Dockerfile on a Docker strategy |

If the outcome annotations cannot be patched onto a passed-through BuildConfig, the plugin logs
`could not record the … disposition on the passed-through BuildConfig: …` with the cause
(`marshaling disposition patch: …` or `decoding disposition patch: …`) and passes it through
without them. This should not happen on a valid export.

Every warning is also written to the plugin log, prefixed with `[namespace/name]` of the
BuildConfig it came from. The log always has the full text, even when the annotation was cut.

## Warning reference

Verbatim, with `…` where the plugin fills in a value. W4 and W7 share the template
`… — passing BuildConfig … through unchanged`, and W5 the template
`… — passing BuildConfig … through unchanged. Consider migrating to Tekton Pipelines directly.`,
where the first `…` is the reason shown below. W2 and W3 fire when two resources converted by
the same converter collide; the plugin builds a new converter for every resource, so neither can
fire today (see [Metadata](#metadata)).

| # | Text |
|---|---|
| W1 | `Generated … name … is not a valid DNS-1123 label of at most … characters — using … instead` |
| W2 | `Generated … name for … collides with the name already generated for … — using … instead` |
| W3 | `Hash-suffixed … name … for … still collides with the name already generated for … — resources may overwrite each other` |
| W4 | `Custom build strategy is not supported for conversion — passing BuildConfig … through unchanged` |
| W5 | `JenkinsPipeline build strategy is not supported for conversion — passing BuildConfig … through unchanged. Consider migrating to Tekton Pipelines directly.` |
| W6 | `unknown build strategy type … for BuildConfig …` (a failure reason, written to the `conversion-reason` annotation, not a warning) |
| W7 | `BuildConfig has no output image (spec.output.to is missing or empty); a Shipwright Build requires spec.output.image — passing BuildConfig … through unchanged` |
| W8 | `BuildConfig …/… names ServiceAccount … and pull secret …. crane migrates that ServiceAccount as-is and this conversion does not modify it, so attach the pull secret on the target cluster before running the BuildRun: oc -n … secrets link … … --for=pull,mount` |
| W9 | `The original ServiceAccount … on BuildConfig …/… may carry additional secrets, imagePullSecrets, and RBAC bindings. Verify these associations are available in the target cluster for the Shipwright BuildRun.` |
| W10 | `Conversion warnings exceeded … bytes — … of … warnings were omitted from annotation …; the full list is in the warnings logged above.` |
| W11 | `DockerImage reference … in namespace … has no registry or path. On OpenShift a name like this may have resolved to an ImageStream with lookupPolicy.local, which Shipwright cannot do. If it is an ImageStream, pass --imagestream-mapping …/…=<registry/image:tag>; if it is a public image, qualify the name with its registry, or list that registry in --search-registries so buildah can resolve it at build time.` |
| W12 | `Build arg with invalid name … was skipped — names must be non-empty and must not contain '=', '$', '{', '}', whitespace, or control characters (BuildConfig …).` |
| W13 | `Build arg … sets both value and valueFrom; using valueFrom and ignoring the literal value (BuildConfig …).` |
| W14 | `Build arg … references a ConfigMap with an empty name or key and was skipped (BuildConfig …).` |
| W15 | `Build arg … references ConfigMap … key … with optional: true — Shipwright has no 'optional' equivalent; a missing key will fail the BuildRun (BuildConfig …).` |
| W16 | `Build arg … references a Secret with an empty name or key and was skipped (BuildConfig …).` |
| W17 | `Build arg … references Secret … key … with optional: true — Shipwright has no 'optional' equivalent; a missing key will fail the BuildRun (BuildConfig …).` |
| W18 | `Build arg … uses fieldRef/resourceFieldRef which has no Shipwright equivalent. This build arg was skipped — set it manually in the generated Build (BuildConfig …).` |
| W19 | `Build arg … has an empty or unsupported valueFrom source. This build arg was skipped — set it manually in the generated Build (BuildConfig …).` |
| W20 | `ImageStream reference … in namespace … could not be resolved — no --imagestream-mapping provided. Using fallback: …. Provide --imagestream-mapping to set the correct image reference.` |
| W21 | `Custom scripts are not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: …` |
| W22 | `Incremental build is not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: …` |
| W23 | `ForcePull flag is not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: …` |
| W24 | `Skipping volume with empty name for BuildConfig …: the Shipwright Build API requires volumes to be named` |
| W25 | `Skipping duplicate volume … for BuildConfig …: a volume with this name was already converted` |
| W26 | `Skipping volume … for BuildConfig …: …` where the reason is `unsupported volume source type …; supported types are Secret and ConfigMap`, `secret volume source is nil` or `configMap volume source is nil` |
| W27 | `Volume … was converted, but the Build will fail validation (reason: UndefinedVolume) until you: (1) add an overridable volume named '…' to your ClusterBuildStrategy copy — volumes: [{name: …, overridable: true, emptyDir: {}}] (placeholder source; the converted Build's override supplies the real Secret/ConfigMap), (2) add a volumeMount for '…' on the strategy build step (…), (3) point the Build at the strategy copy via spec.strategy.name. See ….` |
| W28 | `Volumes were converted to Build spec volumes, but the shipped … ClusterBuildStrategy does not declare them: Shipwright will reject the Build (Registered=False, reason: UndefinedVolume) until a matching volume with 'overridable: true' is added to a copy of the strategy. See ….` |
| W29 | `BuildConfig …/… sets sourceSecret … but has no git source; sourceSecret only authenticates git clones and was not migrated.` |
| W30 | `No source type specified for BuildConfig: …` |
| W31 | `Image source 'As' field is not supported in Shipwright. BuildConfig: …` |
| W32 | `Image source 'Paths' field is not supported in Shipwright. BuildConfig: …` |
| W33 | W11 or W20, for the image source reference |
| W34 | `BuildConfig '…' mounts ConfigMap '…' to '…' during build. Shipwright uses BuildVolume to mount ConfigMaps, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '…' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the ConfigMap, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for ConfigMap files.` |
| W35 | `BuildConfig '…' mounts secret '…' to '…' during build. Shipwright uses BuildVolume to mount secrets, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '…' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the secret, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for secret files.` |
| W36 | `Output ImageStreamTag … resolved to fallback URL: …` |
| W37 | `Output image for ImageStreamTag … was redirected off the internal registry to …; the ImageStream will no longer be updated, so any Deployment or DeploymentConfig watching it to roll out will stop firing.` |
| W38 | `No explicit pushSecret found for ImageStreamTag output. Ensure the BuildRun uses a ServiceAccount with internal registry push access.` |
| W39 | `No explicit pushSecret found for DockerImage output. Set spec.output.pushSecret to a registry credential secret, or ensure the BuildRun ServiceAccount carries credentials for the target registry; otherwise the push will fail.` |
| W40 | `Skipping output imageLabel with empty name` |
| W41 | `Duplicate output imageLabel …: overriding value … with …` |
| W42 | `completionDeadlineSeconds … on BuildConfig … is not positive; leaving Build timeout unset` |
| W43 | `completionDeadlineSeconds … on BuildConfig … exceeds the maximum representable timeout of … seconds; leaving Build timeout unset` |
| W44 | `nodeSelector on BuildConfig …/… is invalid: …; dropping the whole nodeSelector — migrated builds will not be pinned to any node` where the reason is `key … is not a valid label key (…)` or `value … for key … is not a valid label value (…)` |
| W45 | `BuildConfig … uses runPolicy …, which is dropped: OpenShift queued its builds and ran them one at a time, but Shipwright BuildRuns run concurrently. Serialize the runs in your CI/CD pipeline if build ordering matters, for example when several BuildRuns push the same image tag` |
| W46 | `BuildConfig … uses runPolicy …, which is dropped: OpenShift queued its builds and cancelled superseded ones so that only the latest ran, but Shipwright BuildRuns run concurrently and are never auto-cancelled. Serialize the runs and cancel superseded ones in your CI/CD pipeline if you depend on this` |
| W47 | `BuildConfig … uses unrecognized runPolicy …, which is dropped: Shipwright has no build scheduling policy and BuildRuns run concurrently` |
| W48 | `… … on BuildConfig …/… is outside the Shipwright … range […,…]; leaving retention unset — migrated BuildRuns will not be auto-pruned` |
| W49 | `Build strategy … is a custom mapping with unknown step names — stepResources were omitted from the BuildRun template in annotation …. Add stepResources entries matching the strategy's step names to carry over the BuildConfig resource requirements (requests: …, limits: …).` |
| W50 | `Resource requirements are not supported on Shipwright Build. Apply the BuildRun template from annotation … (after review) or set stepResources on each BuildRun you create.` |
| W51 | `BuildConfig …: could not preserve original triggers in annotation …: …` |
| W52 | `BuildConfig …: … webhook trigger is dropped — the old OpenShift webhook URL will stop working after migration, and Shipwright provides no replacement URL. Remove or repoint the webhook in your Git provider, then set up Pipelines-as-Code or Tekton Triggers to create BuildRuns on push events.` |
| W53 | W52, followed by ` Note: webhook-injected environment variables (allowEnv) have no equivalent in Shipwright.` when `allowEnv` is set |
| W54 | `BuildConfig …: ImageChange trigger is dropped — builds will no longer start when … changes. Shipwright has no equivalent of image change triggers today.` |
| W55 | `BuildConfig …: ConfigChange trigger is dropped — the automatic first build will not happen. The generated Build carries a BuildRun template (annotation …); apply it once after review to start the first build.` |
| W56 | `BuildConfig …: ConfigChange trigger is dropped — the automatic first build will not happen; create a BuildRun manually once to start the first build.` |
| W57 | `BuildConfig …: unsupported trigger type … is dropped during migration.` |
| W58 | `Found … trigger(s) (…) on BuildConfig … — none work in Shipwright today; builds must be started manually or by your own automation.` |
| W59 | `Inline Dockerfile on BuildConfig …/… cannot be consumed by the buildah strategy; its content was preserved in ConfigMap …/… (key …). Commit it to the source repository as the Dockerfile, or see …, before running the Build.` |
| W60 | `BuildConfig …/… has an inline Dockerfile set on a Source strategy. Inline Dockerfiles are not used by Source-to-Image and were not migrated. If this was intended for a Docker strategy build, reconfigure the BuildConfig strategy type.` |
| W61 | `PostCommit hook (…) has no Shipwright equivalent and was dropped from BuildConfig '…'. In OpenShift this ran inside the built image before the push, and a failure failed the build. To replicate, add a test step after the BuildRun in a Tekton Pipeline — note this runs after the image is pushed, so it can no longer block a bad image from reaching the registry.` |
| W62 | `BuildConfig '…' sets both script and command in spec.postCommit, which the BuildConfig API does not allow. The script form was assumed for the warning above.` |

## Pending changes

Open pull requests that will change rows on this page. Each row is rewritten when its PR merges.

| PR | Story | What changes |
|---|---|---|
| [#23](https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/23) | BUILD-2265 | `spec.mountTrustedCA` becomes converted: a `trusted-ca` volume on the Build and a generated ConfigMap labelled `config.openshift.io/inject-trusted-cabundle: "true"`. Two new warnings, one for clusters without the injector and one for a custom strategy |
