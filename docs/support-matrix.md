# BuildConfig to Shipwright Build: support matrix

This page lists every field of an OpenShift `BuildConfig`, what the plugin does with it, where
it lands in the Shipwright `Build`, what you have to do by hand afterwards, and the warning you
will see in the log. Read it before a migration to know what will and will not carry over, and
after one to understand a warning you got.

Rows that lose something come first in every section. Clean conversions come last. For the
short list of what does not migrate, read [known-limitations.md](known-limitations.md) first.

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

Warnings are quoted in the [Warning reference](#warning-reference) at the end, keyed [W1](#w1) to [W66](#w66). Each number is an anchor: `#w12` jumps to [W12](#w12).
In the quotes, `…` marks a value the plugin fills in, such as a BuildConfig name.

## What stops a BuildConfig from converting

| Condition | Outcome | What you do by hand | Why, and the reason recorded |
|---|---|---|---|
| `spec.strategy.type: Custom` | skipped | Rewrite the build as a Shipwright ClusterBuildStrategy or a Tekton Task | A Custom build runs an image you supply. Shipwright has no strategy that takes one. [W4](#w4) |
| `spec.strategy.type: JenkinsPipeline` | skipped | Move the pipeline to Tekton Pipelines | The strategy needs a Jenkins server. Shipwright builds images, it does not run pipelines. [W5](#w5) |
| `spec.output.to` missing, or its `name` empty | skipped | Add an output image, or accept that this build has no target and leave it behind | A Shipwright Build requires `spec.output.image`. [W7](#w7) |
| `spec.strategy.type` empty or unrecognised | failed | Fix the BuildConfig on the source cluster first | The plugin cannot choose a ClusterBuildStrategy. [W6](#w6) |
| a strategy `from` or image-source `from` whose `kind` is not `ImageStreamTag`, `ImageStreamImage` or `DockerImage`. For a Docker strategy an empty `kind` counts as unrecognised | failed | Set `kind` on the reference | The plugin resolves `ImageStreamTag`, `ImageStreamImage` and `DockerImage` references only. [W61](#w61) |
| more than one of `source.git`, `source.binary`, `source.images` set | failed | Split into one BuildConfig per source. When `source.images` is one of them, that was OpenShift's chained-build pattern: rewrite the Dockerfile as a multi-stage build (`COPY --from=<image>`) and remove `source.images`. The error says so | A Shipwright Build has one `spec.source`. [W62](#w62) |
| `source.binary` without `asFile` (an extracted archive) | failed | Shipwright's local source takes a directory upload, not an archive. Switch to a git or single-file source | Shipwright's local source uploads a directory, not an archive. [W63](#w63) |
| more than one entry in `source.images` | failed | One image source per Build. This was OpenShift's chained-build pattern with more than one producer: rewrite the Dockerfile as a multi-stage build, one `COPY --from=<image>` per image, and remove `source.images` | One OCI artifact source per Build. [W64](#w64) |
| the Build, ServiceAccount, ConfigMap or BuildRun template cannot be serialised | failed | Report it. This should not happen on a valid export | An internal error. The generated object could not be written out. [W65](#w65) |
| the plugin cannot read its flags or decode the BuildConfig | none. The plugin returns an error and crane aborts the whole transform, not just this BuildConfig | Fix the flag value. A BuildConfig that does not decode should be reported | The plugin could not start on this input. [W66](#w66) |

A BuildConfig skipped for its strategy type has not been looked at any further, so nothing else
is reported for it. One skipped for a missing output image has already had its strategy block
processed: warnings about the strategy image, build args or volumes may appear in the log for
it, and a failure in that block (an unresolvable `from`) is reported as failed, not skipped.
Either way the BuildConfig itself stays exactly as it was.

## Field by field

### Metadata

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `metadata.name` with characters or length not allowed in a DNS-1123 label (63 chars, lowercase, digits, hyphens) | Converted, with a warning. The name is lowercased, invalid runs become `-`, and an 8-character hash of the original is appended | `metadata.name` of the Build | Update anything that refers to the build by its old name | Shipwright names must be DNS-1123 labels. [W1](#w1) |
| two BuildConfigs whose names would collide after sanitising | Prevented for any name the plugin had to rewrite: the suffix is a hash of the original, so `my.app` and `my_app` become `my-app-<hash>` with different hashes. The one gap is a name that is already valid and happens to equal another BuildConfig's rewritten name, such as `foo-1cbec737` next to `Foo`. crane runs the plugin once per resource, so that is not detected: both Builds, and the ServiceAccount and `-dockerfile` ConfigMap generated for each, get the same name, and the later one overwrites the earlier when the output is applied | `metadata.name` | Nothing, unless you have names of the form `<name>-<8 hex characters>`; then check the output for duplicate Build, ServiceAccount and ConfigMap names. A duplicate ServiceAccount means one Build runs with the other's pull secret | Only a name the plugin rewrote is checked against others. [W1](#w1) for the rewritten name |
| `metadata.labels` starting with `openshift.io/build`, or the deprecated `buildconfig` label | Dropped silently (logged at INFO only) | | Nothing. These describe OpenShift build machinery | none |
| `metadata.annotations` starting with `openshift.io/` or `kubectl.kubernetes.io/` | Dropped silently (logged at INFO only) | | Nothing | none |
| `metadata.name`, `metadata.namespace` | Converted | `metadata.name`, `metadata.namespace` | Nothing | none |
| other `metadata.labels` and `metadata.annotations` | Converted | `metadata.labels`, `metadata.annotations`, plus the plugin's own annotations | Nothing | none |

### Strategy

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `strategy.type: Docker` | Converted | `spec.strategy: {kind: ClusterBuildStrategy, name: buildah}`. The name changes with `--default-build-strategy docker=…` | Make sure the `buildah` ClusterBuildStrategy exists on the target | none |
| `strategy.type: Source` | Converted | `spec.strategy: {kind: ClusterBuildStrategy, name: source-to-image}`. The name changes with `--default-build-strategy s2i=…` | Make sure the `source-to-image` ClusterBuildStrategy exists on the target | none |
| `strategy.type: Custom`, `JenkinsPipeline`, empty, unknown | Skipped or failed | | See [What stops a BuildConfig from converting](#what-stops-a-buildconfig-from-converting) | No ClusterBuildStrategy can take these. [W4](#w4), [W5](#w5), [W6](#w6) |
| `dockerStrategy.pullSecret` or `sourceStrategy.pullSecret`, with `spec.serviceAccount` also set | Dropped. The plugin does not touch a named ServiceAccount | | Link the secret to that ServiceAccount on the target: `oc -n <ns> secrets link <sa> <secret> --for=pull,mount` | The plugin never edits an account you own. [W8](#w8) |
| `dockerStrategy.pullSecret` or `sourceStrategy.pullSecret`, no `spec.serviceAccount` | Converted. A new ServiceAccount carrying the secret is generated | a `ServiceAccount` named after the BuildConfig, listed in both `imagePullSecrets` and `secrets`. The BuildRun template, if any, names it | Migrate the secret itself; the plugin only references it | none |
| `spec.serviceAccount` | Converted, with a warning. The name is carried only into the BuildRun template, if one is generated | `spec.serviceAccount` of the BuildRun template | Recreate the account's secrets, image pull secrets and role bindings on the target | Only the name travels. The account's bindings stay on the source cluster. [W9](#w9) |

### Docker strategy

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `dockerStrategy.buildArgs[]` with an invalid name (empty, or containing `=`, `$`, `{`, `}`, whitespace, control characters) | Dropped | | Rename the arg | Build args are passed as `NAME=VALUE`, which these characters would break. [W12](#w12) |
| `dockerStrategy.buildArgs[].valueFrom.configMapKeyRef` or `secretKeyRef` with an empty name or key | Dropped | | Fix the reference | A reference with no name or key points nowhere. [W14](#w14), [W16](#w16) |
| `dockerStrategy.buildArgs[].valueFrom.configMapKeyRef` or `secretKeyRef` with `optional: true` | Converted, with a warning. Shipwright has no optional lookup | `spec.paramValues[build-args]` | Make sure the key exists on the target, or the BuildRun fails | Shipwright has no optional lookup. A missing key fails the BuildRun. [W15](#w15), [W17](#w17) |
| `dockerStrategy.buildArgs[].valueFrom.fieldRef` or `resourceFieldRef` | Dropped | | Set the value directly in the Build | Build args take a literal, a ConfigMap key or a Secret key. Nothing else. [W18](#w18) |
| `dockerStrategy.buildArgs[].valueFrom` of any other shape | Dropped | | Set the value directly in the Build | Same reason: no other source shape is accepted. [W19](#w19) |
| `dockerStrategy.buildArgs[]` with both `value` and `valueFrom` | Converted, with a warning. `valueFrom` wins | `spec.paramValues[build-args]` | Nothing, unless you meant the literal | One arg, one value. `valueFrom` wins. [W13](#w13) |
| `dockerStrategy.from` | Converted. The reference is resolved through the mapping flags | `spec.paramValues[runtime-stage-from]` | See [Image references](#image-references) | No mapping flag covered the reference, or a bare name relied on ImageStream lookup. [W11](#w11) or [W20](#w20) when it cannot be resolved |
| `dockerStrategy.volumes[]` | Converted, with a warning | `spec.volumes[]` | See [Strategy volumes](#strategy-volumes) | Volumes convert, but the strategy has to declare them. [W22](#w22) to [W26](#w26) |
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
| `sourceStrategy.scripts` | Converted. The value is copied as written; no mapping flag applies | `spec.paramValues[scripts-url]` | Check that an `http(s)` URL is reachable from the target's build pods, or that an `image://` path exists in the builder image the Build resolves to. s2i fails the run with `could not download any scripts from URL` otherwise. See [Strategy parameters](#strategy-parameters) | none |
| `sourceStrategy.incremental: true` | Converted, with a warning. s2i builds `FROM <output image> as cached`, so the first BuildRun on a target that does not hold the output image yet fails at buildah | `spec.paramValues[incremental] = true` | Run the first BuildRun with `paramValues` `incremental=false`, or push the output image once by hand. Restrict write access to the output tag on the target registry; see [Strategy parameters](#strategy-parameters) | s2i builds `FROM` the previous output image, which a fresh target does not have. [W21](#w21) |
| `sourceStrategy.forcePull: true` | Converted | `spec.paramValues[pull-policy] = always` | Nothing on the Build. See [Strategy parameters](#strategy-parameters) | none |
| `sourceStrategy.from` | Converted. The reference is resolved through the mapping flags. An empty `kind` is treated as `ImageStreamTag` | `spec.paramValues[builder-image]` | See [Image references](#image-references) | No mapping flag covered the reference, or a bare name relied on ImageStream lookup. [W11](#w11) or [W20](#w20) when it cannot be resolved |
| `sourceStrategy.volumes[]` | Converted, with a warning | `spec.volumes[]` | See [Strategy volumes](#strategy-volumes) | Volumes convert, but the strategy has to declare them. [W22](#w22) to [W26](#w26) |
| `sourceStrategy.env[]` | Converted | `spec.env[]` | Nothing | none |

### Strategy parameters

The three S2I params above, `scripts-url`, `incremental` and `pull-policy`, exist only on a
`source-to-image` strategy that declares them. Builds for Red Hat OpenShift 1.9 (operator
commit 150298a3) and strategy-catalog cb2432c do. The upstream Shipwright sample strategy
and Builds 1.8 do not, and a Build that carries one of these params lands on them with
`Registered=False`, reason `UndefinedParameter`. A strategy copy named through
`--default-build-strategy` must declare them too. The plugin cannot see the target cluster,
so it emits no warning for this; check the strategy before you apply.

`incremental` also decides who can run code in the build. s2i builds `FROM <output image> as
cached` and runs `save-artifacts` in a container from that image, as root, in a pod holding the
BuildRun ServiceAccount's registry credentials. Push access to the output tag is therefore
execution inside the build. That was true on OpenShift too, but migration can move the tag:
`--registry-mapping` sends the output image to a registry whose write access may be wider than
the internal one it replaces ([W35](#w35) fires when it does). Restrict writes to the output tag on the
target registry before you enable `incremental` there.

### Strategy volumes

Applies to `dockerStrategy.volumes[]` and `sourceStrategy.volumes[]`.

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| a volume with an empty `name` | Dropped | | Name it | The Build API requires a volume name. [W22](#w22) |
| a second volume with the same `name` | Dropped | | Remove the duplicate | Volume names must be unique. [W23](#w23) |
| a volume whose `source.type` is not `Secret` or `ConfigMap`, that is `CSI` | Dropped | | The plugin maps only Secret and ConfigMap sources. Shipwright itself takes any pod volume source on `spec.volumes[]`, so add the volume by hand as the next row describes, with a `csi` source on the Build | The plugin maps Secret and ConfigMap sources only. [W24](#w24) |
| a `Secret` or `ConfigMap` volume | Converted, with a warning. The Build will not register (`Registered=False`, reason `UndefinedVolume`) until the strategy declares the volume | `spec.volumes[]`, under the same name | Copy the ClusterBuildStrategy, add an overridable volume with that name and a mount at the original path, and point the Build at the copy. See `docs/volume-migration.md` | The shipped strategies declare no volumes. [W25](#w25) per volume, [W26](#w26) once |
| `volumes[].mounts[].destinationPath` | Carried into the warning only. Shipwright takes mount paths from the strategy, not the Build | | Use the path when you edit the strategy copy | Mount paths belong to the strategy, not the Build. Quoted in [W25](#w25) |

### Source

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `source.dockerfile` (inline Dockerfile) with a Docker strategy | Converted, with a warning. The content is saved in a ConfigMap, but the Build cannot build from it | a `ConfigMap` named `<buildconfig>-dockerfile` with key `Dockerfile`, and the annotation `buildconfig-to-shipwright/inline-dockerfile-configmap` on the Build | Commit the Dockerfile to the source repository before running the Build | Buildah reads the Dockerfile from the source checkout, not from a ConfigMap. [W57](#w57) (logged at ERROR) |
| `source.dockerfile` with a Source strategy | Dropped | | Nothing, unless you meant a Docker strategy | S2I never uses a Dockerfile. [W58](#w58) |
| `source.configMaps[]` | Dropped | | Add an overridable volume to the strategy, a volume override on the Build, and change `ADD`/`COPY` to `RUN cp` in the Dockerfile | Files reach a build through strategy volumes only. [W32](#w32) per entry |
| `source.secrets[]` | Dropped | | Same as above | Files reach a build through strategy volumes only. [W33](#w33) per entry |
| `source.sourceSecret` when `source.git` is not set | Dropped | | Nothing. It did nothing on OpenShift either | `sourceSecret` authenticates git clones only. [W27](#w27) |
| no `git`, `binary` or `images` at all | Converted, with a warning. The Build has no `spec.source` | | Add a source, or delete the Build | A Build with no source cannot run. [W28](#w28). `contextDir`, `configMaps` and `secrets` are then dropped silently |
| `source.images[].as` | Dropped | | No equivalent | The OCI artifact source cannot alias an image for `COPY --from`. [W29](#w29) |
| `source.images[].paths` | Dropped. The whole image filesystem lands at the context root, so a `COPY` that expects the copied files fails | | Rewrite the Dockerfile as a multi-stage build (`COPY --from=<image> <sourcePath> <destination>`) and remove `source.images`. If another BuildConfig builds the image, run its BuildRun to completion first | The OCI artifact source unpacks the whole image at the context root. [W30](#w30) |
| `source.binary.asFile` | Converted, with a change. The file name is not carried | `spec.source: {type: Local, local: {name: local-copy, timeout: 10m}}` | Upload the file with `shp build upload` or an equivalent when you run the build | none |
| `source.git.uri`, `source.git.ref` | Converted | `spec.source.git.url`, `spec.source.git.revision` | Nothing | none |
| `source.sourceSecret` with `source.git` | Converted | `spec.source.git.cloneSecret` | Migrate the secret | none |
| `source.git.httpProxy`, `httpsProxy`, `noProxy` | Converted | `spec.env[]` as `HTTP_PROXY` and `http_proxy`, and likewise for the other two | Nothing | none |
| `source.contextDir` | Converted | `spec.source.contextDir` | Nothing | none |
| `source.images[]` with exactly one entry | Converted. The image reference is resolved through the mapping flags | `spec.source: {type: OCIArtifact, ociArtifact: {image, pullSecret}}` | See [Image references](#image-references) | No mapping flag covered the reference. [W31](#w31) when it cannot be resolved |
| `source.images[].pullSecret` | Converted | `spec.source.ociArtifact.pullSecret` | Migrate the secret | none |
| `source.type` | Ignored. The plugin looks at which of `git`, `binary`, `images` is set | | Nothing | none |

### Output

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `output.to` of kind `ImageStreamTag` with no matching `--imagestream-mapping` | Converted, with a warning. The image becomes `image-registry.openshift-image-registry.svc:5000/<ns>/<name>:<tag>`, then `--registry-mapping` is applied. A name without a tag gets `:latest` | `spec.output.image` | Pass `--imagestream-mapping <ns>/<name>:<tag>=<registry/image:tag>` if the fallback is wrong | No mapping flag covered the output, so the internal registry URL is assumed. [W34](#w34) |
| `output.to` of kind `ImageStreamTag` whose resolved image is not on the internal registry | Converted, with a warning. The ImageStream on the source cluster will no longer update | `spec.output.image` | Repoint any Deployment or DeploymentConfig that watched the ImageStream | The ImageStream on the source cluster stops updating. [W35](#w35) |
| no `output.pushSecret` | Converted, with a warning | | Internal registry: give the BuildRun a ServiceAccount with push access. External registry: set `spec.output.pushSecret` to a registry credential | Shipwright needs push credentials named on the Build or on its ServiceAccount. [W36](#w36) for ImageStreamTag, [W37](#w37) for anything else |
| `output.imageLabels[]` with an empty name | Dropped | | Name it | An image label needs a name. [W38](#w38) |
| `output.imageLabels[]` with a duplicate name | Converted, with a warning. The last value wins | `spec.output.labels` | Remove the duplicate | Label names must be unique. The last value wins. [W39](#w39) |
| `output.to` of kind `ImageStreamTag` with a matching `--imagestream-mapping` | Converted | `spec.output.image`, after `--registry-mapping` | Nothing | none |
| `output.to` of any other kind, including `DockerImage` and `ImageStreamImage` | Converted. The name is copied as written. No mapping flag is applied and there is no warning | `spec.output.image` | Check the registry in the name is reachable from the target | none |
| `output.pushSecret` | Converted | `spec.output.pushSecret` | Migrate the secret | none |
| `output.imageLabels[]` | Converted | `spec.output.labels` | Nothing | none |

### Build settings

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `spec.resources` (requests or limits) | Converted, with a warning. The Build cannot hold resources, so a BuildRun template is generated instead | the annotation `buildconfig-to-shipwright/buildrun-template`, holding a BuildRun with `stepResources` for the strategy's steps and the ServiceAccount | Review the template, then apply it to start a build. With `--default-build-strategy` the step names are unknown and `stepResources` is left out | A Build has no resources field. A BuildRun does. [W48](#w48), or [W47](#w47) with a custom strategy |
| `spec.postCommit` (script, command or args) | Dropped | | Add a test step after the BuildRun in a Tekton Pipeline. It runs after the push, so it cannot block a bad image | Shipwright has no step between build and push. [W59](#w59), and [W60](#w60) if both script and command are set |
| `spec.runPolicy: Serial` (or unset) | Dropped. BuildRuns run concurrently | | Serialise runs in your pipeline if ordering matters | Shipwright has no build queue. [W43](#w43) |
| `spec.runPolicy: SerialLatestOnly` | Dropped | | Serialise and cancel superseded runs in your pipeline | Shipwright has no build queue, and nothing cancels a superseded run. [W44](#w44) |
| `spec.runPolicy` unrecognised | Dropped | | Nothing | An unknown policy has nothing to map to. [W45](#w45) |
| `spec.runPolicy: Parallel` | Converted. Nothing to write, because this is already how BuildRuns behave | | Nothing | none |
| `spec.completionDeadlineSeconds` of 0 or less | Dropped | | Fix the value | A timeout must be positive. [W40](#w40) |
| `spec.completionDeadlineSeconds` larger than about 9.2 billion | Dropped | | Fix the value | Larger than a duration can hold. [W41](#w41) |
| `spec.completionDeadlineSeconds` | Converted | `spec.timeout` | Nothing | none |
| `spec.nodeSelector` with any invalid key or value | Dropped whole. A partial selector would schedule the build somewhere you did not ask for | | Fix the selector | A partial selector would pin builds to the wrong node. [W42](#w42) |
| `spec.nodeSelector` | Converted | `spec.nodeSelector` | Nothing | none |
| `spec.successfulBuildsHistoryLimit`, `spec.failedBuildsHistoryLimit` outside 1 to 10000, including 0 | Dropped. BuildRuns for that state are not pruned | | Set a value in range | Shipwright's retention range is 1 to 10000. [W46](#w46) |
| `spec.successfulBuildsHistoryLimit`, `spec.failedBuildsHistoryLimit` | Converted | `spec.retention.succeededLimit`, `spec.retention.failedLimit` | Nothing | none |

### Triggers

The plugin translates no trigger. Every trigger is dropped with a warning, and the sanitised list
is kept on the Build so it can be rebuilt later. Shipwright's Build API has a `spec.trigger` field
for GitHub webhooks and Tekton Pipeline runs, served by the separate
[Triggers](https://github.com/shipwright-io/triggers) component. That component calls itself a
work in progress and the Builds for OpenShift operator does not ship it. Nothing implements the
`Image` trigger type.

| Field | What happens | Where it lands | What you do by hand | Warning |
|---|---|---|---|---|
| `triggers[]` of type `GitHub`, `GitLab`, `Bitbucket` | Dropped | | Remove or repoint the webhook in your Git provider, then use Pipelines-as-Code or Tekton Triggers to create BuildRuns. The listener to apply is in [trigger-migration.md](trigger-migration.md#webhooks-github-gitlab-bitbucket-generic) | Shipwright serves no webhook URL. [W50](#w50) |
| `triggers[]` of type `Generic` | Dropped | | Same, on the Generic listener in [trigger-migration.md](trigger-migration.md#webhooks-github-gitlab-bitbucket-generic). `allowEnv` has no equivalent | Shipwright serves no webhook URL, and `allowEnv` has no equivalent. [W51](#w51) |
| `triggers[]` of type `ImageChange` | Dropped | | Start builds from your own automation when the image changes. If another BuildConfig builds that image, run its BuildRun to completion before this one. [trigger-migration.md](trigger-migration.md#imagechange) has a Pipeline for that, and a CronJob for external images | Nothing in Shipwright watches an image for changes. [W52](#w52) |
| `triggers[]` of type `ConfigChange` | Dropped | | Create the first BuildRun yourself. If the Build carries a BuildRun template, apply that. Both commands are in [trigger-migration.md](trigger-migration.md#configchange) | Creating a Build starts nothing. Only a BuildRun does. [W54](#w54) ([W53](#w53) is not reachable today, see the note below) |
| `triggers[]` of any other type | Dropped | | | An unknown trigger type has nothing to map to. [W55](#w55) |
| any triggers at all | One summary warning, and the triggers are preserved | the annotation `buildconfig-to-shipwright/original-triggers`: type, secret reference name, `allowEnv`, `imageChange.from`, `paused`. Inline secret values and `lastTriggeredImageID` are never included | Keep the annotation until triggers exist in Shipwright | No trigger type works in Shipwright today. [W56](#w56), and [W49](#w49) if the list cannot be encoded |

Note: the plugin has two wordings for the ConfigChange warning. The one that mentions the BuildRun
template ([W53](#w53)) is never used today, because triggers are processed before the step that writes
the template. You always get [W54](#w54), even when the Build carries a template.

### Fields the plugin never reads

These are dropped silently. Check for them yourself.

| Field | Why it matters |
|---|---|
| `spec.mountTrustedCA` | A build that mounted the cluster's trusted CA bundle loses it. No warning. A fix is planned: see [known-limitations.md](known-limitations.md#planned) |
| `spec.revision` | Runtime state, not configuration. `BuildConfig` and `Build` share the same spec struct, so the field exists on a BuildConfig, but OpenShift only fills it in on the `Build` objects it creates. On the BuildConfig the plugin reads it is empty. Not the same thing as `source.git.ref`, which is migrated; see the note below |
| `spec.strategy.customStrategy`, `spec.strategy.jenkinsPipelineStrategy` | The whole BuildConfig is skipped, so the contents are never read |
| `status` | Runtime state of the source cluster. Not configuration |

Two fields involve the word "revision" and only one of them is migrated.

- `spec.source.git.ref` **is** migrated. It becomes `spec.source.git.revision` on the Shipwright
  Build, and is listed under [Source](#source).
- `spec.revision` is **not** migrated, and there is nothing to migrate. It records the commit hash,
  author, committer and message of the snapshot one build ran against, and OpenShift writes it on
  each `Build` object rather than on the BuildConfig template the plugin reads. Shipwright reports
  the equivalent per run at `BuildRun.status.source.git.commitSha`, `.commitAuthor` and
  `.branchName`. The committer and the commit message have no Shipwright equivalent.

The easy mistake is to see both entries and conclude the migration dropped your git ref. It did
not. Check `spec.source.git.revision` on the generated Build.

### Image references

`dockerStrategy.from`, `sourceStrategy.from` and `source.images[].from` all resolve the same way.
The output image resolves slightly differently; see [Output](#output).

| Reference | Result |
|---|---|
| `kind: ImageStreamTag` or `ImageStreamImage`, with `--imagestream-mapping <ns>/<name>=…` | the mapped image, then `--registry-mapping` |
| `kind: ImageStreamTag` or `ImageStreamImage`, no mapping | `image-registry.openshift-image-registry.svc:5000/<ns>/<name>`, then `--registry-mapping`. Warning [W20](#w20) if the registry mapping changed nothing |
| `kind: DockerImage`, name contains `/` | the name, then `--registry-mapping` |
| `kind: DockerImage`, bare name, with `--imagestream-mapping <ns>/<name>=…` | the mapped image, then `--registry-mapping` |
| `kind: DockerImage`, bare name, no imagestream mapping, but a `--registry-mapping` prefix matches it | the name after `--registry-mapping`, with no warning |
| `kind: DockerImage`, bare name, no mapping of either kind | the name as written. Warning [W11](#w11), because a bare name may have relied on an ImageStream with `lookupPolicy.local` |
| any other `kind` | the conversion fails |

The `<ns>` in a mapping key is the reference's own namespace, or the BuildConfig's namespace when
the reference has none. For strategy and source references the key uses the name as written. For
the output image a name without a tag is looked up with `:latest` appended.

#### Chained builds

One BuildConfig's output is often another's input: a builder image built in the namespace, or an
artifact image copied from with `source.images`. On OpenShift an ImageChange trigger ran the
consumer after the producer pushed. Shipwright does not order BuildRuns, and crane runs the plugin
once per resource, so the plugin never sees both BuildConfigs. What it does instead, on the
consumer, whenever a strategy `from`, a `source.images[].from` or an ImageChange trigger names an
`ImageStreamTag` in the BuildConfig's own namespace:

- [W52](#w52) and [W30](#w30) end with ` If another BuildConfig in namespace … builds that image, run its BuildRun to completion before starting this Build; Shipwright does not order BuildRuns.` Both warnings carry it when both fire on one image, since each stands on its own.
- an input no warning names gets one info line in crane's output, `BuildConfig … pulls … from its own namespace.` followed by the same sentence, and the conversion stays clean.

An imported ImageStream in the same namespace looks the same to the plugin, so the notice says
"if". Run the producer's BuildRun to completion, then the consumer's. An artifact chain also needs
the multi-stage rewrite [W30](#w30) describes, because the OCI artifact source unpacks the whole image.
More than one `source.images` entry fails the conversion and the error names the same rewrite.

## Plugin flags

| Flag | Format | What it changes |
|---|---|---|
| `--imagestream-mapping` | `ns/name:tag=registry/image:tag,…` | Replaces ImageStream references, and bare DockerImage names, with a concrete image |
| `--registry-mapping` | `old-registry=new-registry,…` | Rewrites the registry prefix of every resolved image reference, except an output image whose kind is not `ImageStreamTag`, which is copied as written (see [Output](#output)). The longest matching prefix wins |
| `--default-build-strategy` | `docker=name,s2i=name` | Uses a different ClusterBuildStrategy name. With a custom name the BuildRun template omits `stepResources` ([W47](#w47)) |
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
| `crane.konveyor.io/conversion-warnings` | Build | at least one warning. Cut at 32 KiB, keeping whole warnings; the annotation then ends with `... … more conversion warning(s) omitted to stay within the Kubernetes annotation size limit — see the crane plugin logs for the full list.` and [W10](#w10) is logged |
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

Verbatim, with `…` where the plugin fills in a value. [W4](#w4) and [W7](#w7) share the template
`… — passing BuildConfig … through unchanged`, and [W5](#w5) the template
`… — passing BuildConfig … through unchanged. Consider migrating to Tekton Pipelines directly.`,
where the first `…` is the reason shown below. A retired number keeps its row, so the numbers
never shift. Its cell starts with `Retired by BUILD-` and the story number, and carries no
backticks, because a backtick-quoted string in a row is read as a live warning template.

| # | Text |
|---|---|
| <a id="w1" name="w1"></a>W1 | `Generated … name … is not a valid DNS-1123 label of at most … characters — using … instead` |
| <a id="w2" name="w2"></a>W2 | Retired by BUILD-2438. Described a name collision between two BuildConfigs converted by one process, which crane never does |
| <a id="w3" name="w3"></a>W3 | Retired by BUILD-2438, as W2 |
| <a id="w4" name="w4"></a>W4 | `Custom build strategy is not supported for conversion — passing BuildConfig … through unchanged` |
| <a id="w5" name="w5"></a>W5 | `JenkinsPipeline build strategy is not supported for conversion — passing BuildConfig … through unchanged. Consider migrating to Tekton Pipelines directly.` |
| <a id="w6" name="w6"></a>W6 | `unknown build strategy type … for BuildConfig …` (a failure reason, written to the `conversion-reason` annotation, not a warning) |
| <a id="w7" name="w7"></a>W7 | `BuildConfig has no output image (spec.output.to is missing or empty); a Shipwright Build requires spec.output.image — passing BuildConfig … through unchanged` |
| <a id="w8" name="w8"></a>W8 | `BuildConfig …/… names ServiceAccount … and pull secret …. crane migrates that ServiceAccount as-is and this conversion does not modify it, so attach the pull secret on the target cluster before running the BuildRun: oc -n … secrets link … … --for=pull,mount` |
| <a id="w9" name="w9"></a>W9 | `The original ServiceAccount … on BuildConfig …/… may carry additional secrets, imagePullSecrets, and RBAC bindings. Verify these associations are available in the target cluster for the Shipwright BuildRun.` |
| <a id="w10" name="w10"></a>W10 | `Conversion warnings exceeded … bytes — … of … warnings were omitted from annotation …; the full list is in the warnings logged above.` |
| <a id="w11" name="w11"></a>W11 | `DockerImage reference … in namespace … has no registry or path. On OpenShift a name like this may have resolved to an ImageStream with lookupPolicy.local, which Shipwright cannot do. If it is an ImageStream, pass --imagestream-mapping …/…=<registry/image:tag>; if it is a public image, qualify the name with its registry, or list that registry in --search-registries so buildah can resolve it at build time.` |
| <a id="w12" name="w12"></a>W12 | `Build arg with invalid name … was skipped — names must be non-empty and must not contain '=', '$', '{', '}', whitespace, or control characters (BuildConfig …).` |
| <a id="w13" name="w13"></a>W13 | `Build arg … sets both value and valueFrom; using valueFrom and ignoring the literal value (BuildConfig …).` |
| <a id="w14" name="w14"></a>W14 | `Build arg … references a ConfigMap with an empty name or key and was skipped (BuildConfig …).` |
| <a id="w15" name="w15"></a>W15 | `Build arg … references ConfigMap … key … with optional: true — Shipwright has no 'optional' equivalent; a missing key will fail the BuildRun (BuildConfig …).` |
| <a id="w16" name="w16"></a>W16 | `Build arg … references a Secret with an empty name or key and was skipped (BuildConfig …).` |
| <a id="w17" name="w17"></a>W17 | `Build arg … references Secret … key … with optional: true — Shipwright has no 'optional' equivalent; a missing key will fail the BuildRun (BuildConfig …).` |
| <a id="w18" name="w18"></a>W18 | `Build arg … uses fieldRef/resourceFieldRef which has no Shipwright equivalent. This build arg was skipped — set it manually in the generated Build (BuildConfig …).` |
| <a id="w19" name="w19"></a>W19 | `Build arg … has an empty or unsupported valueFrom source. This build arg was skipped — set it manually in the generated Build (BuildConfig …).` |
| <a id="w20" name="w20"></a>W20 | `ImageStream reference … in namespace … could not be resolved — no --imagestream-mapping provided. Using fallback: …. Provide --imagestream-mapping to set the correct image reference.` |
| <a id="w21" name="w21"></a>W21 | `Incremental build enabled. The first BuildRun fails unless the output image already exists in the target registry, because s2i builds FROM it. Run the first BuildRun with paramValues incremental=false, or push the image once by hand.` |
| <a id="w22" name="w22"></a>W22 | `Skipping volume with empty name for BuildConfig …: the Shipwright Build API requires volumes to be named` |
| <a id="w23" name="w23"></a>W23 | `Skipping duplicate volume … for BuildConfig …: a volume with this name was already converted` |
| <a id="w24" name="w24"></a>W24 | `Skipping volume … for BuildConfig …: …` where the reason is `unsupported volume source type …; supported types are Secret and ConfigMap`, `secret volume source is nil` or `configMap volume source is nil` |
| <a id="w25" name="w25"></a>W25 | `Volume … was converted, but the Build will fail validation (reason: UndefinedVolume) until you: (1) add an overridable volume named '…' to your ClusterBuildStrategy copy — volumes: [{name: …, overridable: true, emptyDir: {}}] (placeholder source; the converted Build's override supplies the real Secret/ConfigMap), (2) add a volumeMount for '…' on the strategy build step (…), (3) point the Build at the strategy copy via spec.strategy.name. See ….` |
| <a id="w26" name="w26"></a>W26 | `Volumes were converted to Build spec volumes, but the shipped … ClusterBuildStrategy does not declare them: Shipwright will reject the Build (Registered=False, reason: UndefinedVolume) until a matching volume with 'overridable: true' is added to a copy of the strategy. See ….` |
| <a id="w27" name="w27"></a>W27 | `BuildConfig …/… sets sourceSecret … but has no git source; sourceSecret only authenticates git clones and was not migrated.` |
| <a id="w28" name="w28"></a>W28 | `No source type specified for BuildConfig: …` |
| <a id="w29" name="w29"></a>W29 | `Image source 'As' field is not supported in Shipwright. BuildConfig: …` |
| <a id="w30" name="w30"></a>W30 | `BuildConfig …: source.images copied … path(s) from … into the build context on OpenShift. Shipwright's OCI artifact source unpacks the whole image filesystem at the context root and has no paths, so COPY instructions that expect those files under their destinationDir will not find them. Rewrite the Dockerfile as a multi-stage build (COPY --from=… <sourcePath> <destination>) and remove source.images. Keep that Dockerfile in the git repository the Build clones (spec.source.git); a Build with neither git nor source.images has no source.` followed by the chained-build sentence (see [Chained builds](#chained-builds)) when the image is an `ImageStreamTag` in the BuildConfig's own namespace |
| <a id="w31" name="w31"></a>W31 | W11 or W20, for the image source reference |
| <a id="w32" name="w32"></a>W32 | `BuildConfig '…' mounts ConfigMap '…' to '…' during build. Shipwright uses BuildVolume to mount ConfigMaps, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '…' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the ConfigMap, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for ConfigMap files.` |
| <a id="w33" name="w33"></a>W33 | `BuildConfig '…' mounts secret '…' to '…' during build. Shipwright uses BuildVolume to mount secrets, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '…' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the secret, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for secret files.` |
| <a id="w34" name="w34"></a>W34 | `Output ImageStreamTag … resolved to fallback URL: …` |
| <a id="w35" name="w35"></a>W35 | `Output image for ImageStreamTag … was redirected off the internal registry to …; the ImageStream will no longer be updated, so any Deployment or DeploymentConfig watching it to roll out will stop firing.` |
| <a id="w36" name="w36"></a>W36 | `No explicit pushSecret found for ImageStreamTag output. Ensure the BuildRun uses a ServiceAccount with internal registry push access.` |
| <a id="w37" name="w37"></a>W37 | `No explicit pushSecret found for DockerImage output. Set spec.output.pushSecret to a registry credential secret, or ensure the BuildRun ServiceAccount carries credentials for the target registry; otherwise the push will fail.` |
| <a id="w38" name="w38"></a>W38 | `Skipping output imageLabel with empty name` |
| <a id="w39" name="w39"></a>W39 | `Duplicate output imageLabel …: overriding value … with …` |
| <a id="w40" name="w40"></a>W40 | `completionDeadlineSeconds … on BuildConfig … is not positive; leaving Build timeout unset` |
| <a id="w41" name="w41"></a>W41 | `completionDeadlineSeconds … on BuildConfig … exceeds the maximum representable timeout of … seconds; leaving Build timeout unset` |
| <a id="w42" name="w42"></a>W42 | `nodeSelector on BuildConfig …/… is invalid: …; dropping the whole nodeSelector — migrated builds will not be pinned to any node` where the reason is `key … is not a valid label key (…)` or `value … for key … is not a valid label value (…)` |
| <a id="w43" name="w43"></a>W43 | `BuildConfig … uses runPolicy …, which is dropped: OpenShift queued its builds and ran them one at a time, but Shipwright BuildRuns run concurrently. Serialize the runs in your CI/CD pipeline if build ordering matters, for example when several BuildRuns push the same image tag` |
| <a id="w44" name="w44"></a>W44 | `BuildConfig … uses runPolicy …, which is dropped: OpenShift queued its builds and cancelled superseded ones so that only the latest ran, but Shipwright BuildRuns run concurrently and are never auto-cancelled. Serialize the runs and cancel superseded ones in your CI/CD pipeline if you depend on this` |
| <a id="w45" name="w45"></a>W45 | `BuildConfig … uses unrecognized runPolicy …, which is dropped: Shipwright has no build scheduling policy and BuildRuns run concurrently` |
| <a id="w46" name="w46"></a>W46 | `… … on BuildConfig …/… is outside the Shipwright … range […,…]; leaving retention unset — migrated BuildRuns will not be auto-pruned` |
| <a id="w47" name="w47"></a>W47 | `Build strategy … is a custom mapping with unknown step names — stepResources were omitted from the BuildRun template in annotation …. Add stepResources entries matching the strategy's step names to carry over the BuildConfig resource requirements (requests: …, limits: …).` |
| <a id="w48" name="w48"></a>W48 | `Resource requirements are not supported on Shipwright Build. Apply the BuildRun template from annotation … (after review) or set stepResources on each BuildRun you create.` |
| <a id="w49" name="w49"></a>W49 | `BuildConfig …: could not preserve original triggers in annotation …: …` |
| <a id="w50" name="w50"></a>W50 | `BuildConfig …: … webhook trigger is dropped — the old OpenShift webhook URL will stop working after migration, and Shipwright provides no replacement URL. Remove or repoint the webhook in your Git provider, then set up Pipelines-as-Code or Tekton Triggers to create BuildRuns on push events.` |
| <a id="w51" name="w51"></a>W51 | W50, followed by ` Note: webhook-injected environment variables (allowEnv) have no equivalent in Shipwright.` when `allowEnv` is set |
| <a id="w52" name="w52"></a>W52 | `BuildConfig …: ImageChange trigger is dropped — builds will no longer start when … changes. Shipwright has no equivalent of image change triggers today.` followed by the chained-build sentence (see [Chained builds](#chained-builds)) when the watched image is an `ImageStreamTag` in the BuildConfig's own namespace |
| <a id="w53" name="w53"></a>W53 | `BuildConfig …: ConfigChange trigger is dropped — the automatic first build will not happen. The generated Build carries a BuildRun template (annotation …); apply it once after review to start the first build.` |
| <a id="w54" name="w54"></a>W54 | `BuildConfig …: ConfigChange trigger is dropped — the automatic first build will not happen; create a BuildRun manually once to start the first build.` |
| <a id="w55" name="w55"></a>W55 | `BuildConfig …: unsupported trigger type … is dropped during migration.` |
| <a id="w56" name="w56"></a>W56 | `Found … trigger(s) (…) on BuildConfig … — none work in Shipwright today; builds must be started manually or by your own automation.` |
| <a id="w57" name="w57"></a>W57 | `Inline Dockerfile on BuildConfig …/… cannot be consumed by the buildah strategy; its content was preserved in ConfigMap …/… (key …). Commit it to the source repository as the Dockerfile, or see …, before running the Build.` |
| <a id="w58" name="w58"></a>W58 | `BuildConfig …/… has an inline Dockerfile set on a Source strategy. Inline Dockerfiles are not used by Source-to-Image and were not migrated. If this was intended for a Docker strategy build, reconfigure the BuildConfig strategy type.` |
| <a id="w59" name="w59"></a>W59 | `PostCommit hook (…) has no Shipwright equivalent and was dropped from BuildConfig '…'. In OpenShift this ran inside the built image before the push, and a failure failed the build. To replicate, add a test step after the BuildRun in a Tekton Pipeline — note this runs after the image is pushed, so it can no longer block a bad image from reaching the registry.` |
| <a id="w60" name="w60"></a>W60 | `BuildConfig '…' sets both script and command in spec.postCommit, which the BuildConfig API does not allow. The script form was assumed for the warning above.` |
| <a id="w61" name="w61"></a>W61 | `unknown image reference kind … for …`, wrapped as `error resolving Docker strategy From field: …`, `error resolving Source strategy From field: …` or `failed to resolve image source (BuildConfig …): …` depending on which reference failed (a failure reason, written to the `conversion-reason` annotation, not a warning) |
| <a id="w62" name="w62"></a>W62 | `multiple source types are not supported in a single build in Shipwright (BuildConfig …)`, followed by `; source.images alongside another source was OpenShift's chained-build pattern, which Shipwright expresses as a multi-stage Dockerfile: COPY --from=<image> the files you need and remove source.images` when `source.images` is one of them (a failure reason) |
| <a id="w63" name="w63"></a>W63 | `binary archive source (extracted archive) is not supported in Shipwright, only single-file binary sources (asFile) (BuildConfig …)` (a failure reason) |
| <a id="w64" name="w64"></a>W64 | `multiple image sources are not supported in Shipwright (BuildConfig …); source.images was OpenShift's chained-build pattern, which Shipwright expresses as a multi-stage Dockerfile: COPY --from=<image> the files you need from each and remove source.images` (a failure reason) |
| <a id="w65" name="w65"></a>W65 | `error converting Build to unstructured: …`, `error converting ServiceAccount to unstructured: …`, `error converting inline-Dockerfile ConfigMap to unstructured: …`, `error marshaling BuildRun spec for BuildConfig …: …`, `error unmarshaling BuildRun spec for BuildConfig …: …` or `error marshaling BuildRun template for BuildConfig …: …` (a failure reason) |
| <a id="w66" name="w66"></a>W66 | `error parsing optional fields: …`, `error marshaling BuildConfig to JSON: …` or `error decoding BuildConfig: …` (the plugin returns this error and crane aborts the transform; nothing is recorded on the BuildConfig) |
