# Known limitations

What does not migrate, what you do instead, and what is planned. Read this page before a
migration. [support-matrix.md](support-matrix.md) has the field-by-field detail, and every
row here points into it. Every `W` number links to its entry in the
[Warning reference](support-matrix.md#warning-reference).

Last checked against `main` on 2026-09-23.

## The short list

Five things to know before you run the migration.

1. **Custom and JenkinsPipeline builds do not convert.** The plugin skips them and passes the
   BuildConfig through unchanged. Rewrite a Custom build as a ClusterBuildStrategy or a Tekton
   Task, and a Jenkins pipeline as a Tekton Pipeline.
2. **No trigger fires after migration.** Webhook, ImageChange and ConfigChange triggers are all
   dropped. The plugin keeps them in an annotation on the Build. You start BuildRuns yourself,
   or from Pipelines-as-Code or Tekton Triggers.
3. **Builds that feed other builds are not ordered.** Shipwright runs BuildRuns concurrently.
   Run the producer to completion before you start the consumer.
4. **More than one source fails the conversion.** Shipwright takes one source per Build. Split
   the BuildConfig, or move the source to git.
5. **Files from `source.secrets` and `source.configMaps` do not reach the build.** The
   strategy has to declare the volume first. [volume-migration.md](volume-migration.md) has
   the steps.

## Not supported

None of these has a story on the board. Shipwright has no equivalent, or the plugin cannot see
enough of the cluster to do the work. The last column says why, and names the warning you will see.

| BuildConfig feature | What happens | What you do instead | Why |
|---|---|---|---|
| `strategy.type: Custom` | skipped, passed through unchanged | Rewrite the build as a ClusterBuildStrategy or a Tekton Task | A Custom build runs an image you supply. Shipwright has no strategy that takes one. [W4](support-matrix.md#w4) |
| `strategy.type: JenkinsPipeline` | skipped, passed through unchanged | Move the pipeline to Tekton Pipelines | The strategy needs a Jenkins server. Shipwright builds images, it does not run pipelines. [W5](support-matrix.md#w5) |
| More than one of `source.git`, `source.binary`, `source.images` | failed | Split into one BuildConfig per source. For git plus an image, rewrite as a multi-stage Dockerfile | A Shipwright Build has one `spec.source`. [W62](support-matrix.md#w62) |
| More than one `source.images` entry | failed | Rewrite as a multi-stage Dockerfile | One OCI artifact source per Build. [W64](support-matrix.md#w64) |
| Webhook triggers: `GitHub`, `GitLab`, `Bitbucket`, `Generic` | dropped, kept in the `buildconfig-to-shipwright/original-triggers` annotation | Remove or repoint the webhook in your Git provider. Use Pipelines-as-Code or Tekton Triggers to create BuildRuns on push. [trigger-migration.md](trigger-migration.md#webhooks-github-gitlab-bitbucket-generic) has the listener to apply | Shipwright serves no webhook URL. The Triggers component that would is not shipped with the operator. [W50](support-matrix.md#w50), [W51](support-matrix.md#w51) |
| `ImageChange` trigger | dropped, kept in the same annotation | Start a BuildRun from your own automation when the image changes. [trigger-migration.md](trigger-migration.md#imagechange) has a Pipeline for chained Builds and a CronJob for external images | Nothing in Shipwright watches an image for changes. [W52](support-matrix.md#w52) |
| `ConfigChange` trigger | dropped, kept in the same annotation | Create the first BuildRun yourself. When the Build carries a BuildRun template, apply that. Both commands are in [trigger-migration.md](trigger-migration.md#configchange) | Creating a Build starts nothing. Only a BuildRun starts a build. [W54](support-matrix.md#w54) |
| Run ordering for chained builds, where one build's output is another's input | both Builds convert, with a warning or an info line on the consumer. Nothing orders the runs | Run the producer's BuildRun to completion, then the consumer's | crane hands the plugin one resource at a time, and Shipwright does not order BuildRuns. [Chained builds](support-matrix.md#chained-builds) |
| `runPolicy: Serial` (or unset) and `SerialLatestOnly` | dropped. BuildRuns run concurrently | Serialise runs in your pipeline | Shipwright has no build queue. Every BuildRun starts as soon as it is created. [W43](support-matrix.md#w43), [W44](support-matrix.md#w44) |
| `postCommit` hooks | dropped | Add a test step after the BuildRun in a Tekton Pipeline. It runs after the push, so it cannot block a bad image | OpenShift ran the hook inside the built image before the push. Shipwright has no step between build and push. [W59](support-matrix.md#w59) |
| `source.secrets[]` and `source.configMaps[]` | dropped | Add an overridable volume to the strategy, a volume override on the Build, and change `ADD` or `COPY` to `RUN cp` in the Dockerfile. See [volume-migration.md](volume-migration.md) | Shipwright mounts files through strategy volumes, and the shipped strategies declare none under a name of yours. [W32](support-matrix.md#w32), [W33](support-matrix.md#w33) |
| `source.images[].as` | dropped | No equivalent | The OCI artifact source cannot rename what it unpacks. [W29](support-matrix.md#w29) |
| `source.images[].paths` | dropped. The whole image becomes the source | Adjust the Dockerfile to the image's layout | The OCI artifact source unpacks the whole image at the context root. It cannot pick paths. [W30](support-matrix.md#w30) |
| Inline Dockerfile (`source.dockerfile`) on a Docker strategy | converted, with a warning. The Dockerfile is saved to a ConfigMap the Build cannot use | Commit the Dockerfile to the source repository. Point `dockerStrategy.dockerfilePath` at it when it is not at the context root | The buildah strategy reads the Dockerfile from the source checkout. It cannot take one from a ConfigMap, and it will stay that way. [W57](support-matrix.md#w57) |
| `buildArgs[].valueFrom.fieldRef` and `resourceFieldRef` | dropped | Set the value directly in the Build | Shipwright build args take a literal, a ConfigMap key or a Secret key. Nothing else. [W18](support-matrix.md#w18) |
| A strategy volume with a `CSI` source | dropped | Add the volume to `spec.volumes` on the Build yourself | The plugin maps Secret and ConfigMap volume sources only. Shipwright itself accepts any pod volume source. [W24](support-matrix.md#w24) |
| `pullSecret` on a BuildConfig that names a `serviceAccount` | not linked. crane carries the account itself, except `default`, and except `builder` and `deployer` when crane-plugin-openshift is one of the stages `crane transform` runs | Run the `oc secrets link` command from the warning on the target. For those three accounts, check the target holds the account first; W72 gives the command | The plugin never edits an account you own, so it cannot attach the secret. [W8](support-matrix.md#w8), and [W72](support-matrix.md#w72) for the three accounts the migration can drop |
| A dry-run flag, or a check that the target runs Shipwright | not a feature | The transform runs offline and writes files. Read them before you apply | The plugin never talks to a cluster, so it cannot check one. [architecture.md](architecture.md) |

A note on triggers. Shipwright's Build API has a `spec.trigger` field for GitHub webhooks and
Tekton Pipeline runs, served by the separate [Triggers](https://github.com/shipwright-io/triggers)
component. That component calls itself a work in progress, and the Builds for OpenShift
operator does not ship it. Nothing implements the `Image` trigger type. GitLab, Bitbucket
and Generic webhooks are outside its stated scope. If that changes, the webhook and
`ImageChange` rows above are the two that would move.

## Converted, but needs a step from you

These migrate, and the Build will not run until you act. The warning on the Build says what.
The last column says why, and names the warning.

| BuildConfig feature | What the Build needs | Why |
|---|---|---|
| Strategy volumes with a Secret or ConfigMap source | The shipped strategies declare no volume under a name of yours. Copy the strategy and add it, per [volume-migration.md](volume-migration.md) | Shipwright checks every Build volume against the strategy. A volume the strategy does not declare fails validation with `UndefinedVolume`. [W25](support-matrix.md#w25), [W26](support-matrix.md#w26) |
| `sourceStrategy.incremental: true` | The first BuildRun fails unless the output image already exists on the target. Run it once with `incremental=false`, or push the image by hand | An incremental build starts `FROM` the previous output image to reuse its artifacts. On a fresh target that image is not there yet. [W21](support-matrix.md#w21) |
| `dockerStrategy.env` | Add `ENV <name>=<value>` after each `FROM` in the Dockerfile | OpenShift wrote the entries into the Dockerfile for you. On Shipwright they only reach the build container, so a `RUN` step that reads one gets an empty value and the build carries on. [W69](support-matrix.md#w69) |
| `sourceStrategy.env` | Set each entry as `NAME=VALUE` in the Build's `build-env` parameter | The source-to-image strategy passes `build-env` to s2i and ignores `spec.env`, so the assemble script does not see the values. [W70](support-matrix.md#w70) |
| `resources` (CPU and memory) | Shipwright puts resources on the BuildRun, not the Build. Apply the BuildRun template from the annotation | A Shipwright Build has no field for resource requirements. Only a BuildRun does, so the plugin writes one into an annotation for you to apply. [W48](support-matrix.md#w48) |
| Output to a registry with no `pushSecret` | A ServiceAccount with push credentials, or `spec.output.pushSecret` on the Build | OpenShift pushed with the builder account's credentials. Shipwright needs them named on the Build, or on the ServiceAccount the BuildRun uses. [W36](support-matrix.md#w36), [W37](support-matrix.md#w37) |
| `mountTrustedCA: true` | The `ca-bundle.crt` key filled in the generated `<buildconfig>-trusted-ca-migrated` ConfigMap, by the Cluster Network Operator on OpenShift or by you anywhere else, and a `trusted-ca` volume the target strategy declares. The shipped `buildah` and `source-to-image` strategies declare it from strategy-catalog commit `cb2432c` onward; an older catalog, or a strategy of your own named through `--default-build-strategy`, needs the overridable volume added | The build asked for the cluster's trust material, so the mount is made to fail visibly rather than let the build run without it. Only the `ca-bundle.crt` key is projected, so nothing else in the ConfigMap can enter the trust store. [W76](support-matrix.md#w76), [W77](support-matrix.md#w77) |
| A binary source, with or without `asFile` | Start each build with `shp build upload <build> <directory>`. With `asFile`, put the file in that directory under that name. The BuildRun template cannot start a Local-source Build, so what it carries has to go on the upload instead: the account as `--sa-name`, and `spec.resources` nowhere at all, since `shp build upload` has no flag for step resources and each build runs with the strategy's defaults | OpenShift took the input from `oc start-build` at each start. A Shipwright Local source is also a directory upload, but nothing feeds it on its own: a BuildRun started without the upload waits out the timeout and fails. `shp` also sends fewer files than `oc` did; see [What `shp build upload` leaves out](#what-shp-build-upload-leaves-out). [W67](support-matrix.md#w67), [W68](support-matrix.md#w68), [W71](support-matrix.md#w71) |

### What `shp build upload` leaves out

`oc start-build --from-dir` sent every file in the directory except `.git/`. `shp build upload`
sends less. Checked on a ROSA cluster with Builds for Red Hat OpenShift 1.9.0 on 2026-09-17,
with `shp` built from `main` and from each open fix. Only the first two rows are in the W67
warning, because only those will stay true; [ADR-0011](adr/0011-binary-upload-differences-warn-only-what-lasts.md)
records why.

| What `shp` does | Upstream status | What you do |
|---|---|---|
| Skips every file the directory's top-level `.gitignore` lists | By design, and there is no switch to turn it off ([SHIP-0021](https://github.com/shipwright-io/community/blob/main/ships/0021-local-source-upload.md), `shp build upload --help`). No request upstream to change it | Remove the entry from `.gitignore`, or upload a copy of the directory without it. A build output such as `target/app.jar` is the usual casualty |
| Drops a symlink that points outside the directory. Once the symlink fix below merges, the upload fails instead | By design, for security ([maintainer review on cli#355](https://github.com/shipwright-io/cli/pull/355#discussion_r3110712038)) | Copy the file into the directory |
| Drops files and directories whose names start with `.git`, such as `.github/`, `.gitignore` and `.gitattributes` | Bug: [cli#408](https://github.com/shipwright-io/cli/issues/408), fix in review in [cli#409](https://github.com/shipwright-io/cli/pull/409) | Until it merges, rename or copy what the build needs |
| Drops symlinks inside the directory | Bug, fix in review in [cli#355](https://github.com/shipwright-io/cli/pull/355). No issue was filed | Until it merges, replace the symlink with a copy of its target |
| Drops empty directories | Not reported, and not planned to be | Create the directory in the Dockerfile with `RUN mkdir` |

## Planned

Work with a story on the board. Nothing here has a release date.

| BuildConfig feature | What happens today | Planned change | Tracking |
|---|---|---|---|
| `dockerStrategy.env` | copied to `spec.env`, which `RUN` does not see. Warned | The buildah strategy takes an `env` parameter, and the plugin maps the entries to it | BUILD-1491 (strategy), BUILD-2499 (plugin) |
| `sourceStrategy.env` | copied to `spec.env`, which s2i does not see. Warned | The plugin maps the entries to the strategy's `build-env` parameter | BUILD-2500 |
| The `secrets` list on a migrated `serviceAccount` | crane carries the account, its RoleBindings and the cluster RBAC that names it, and the plugin names it in the BuildRun template. crane-lib clears the account's `secrets` list, so a secret linked `--for=mount` has to be re-linked by hand | Carry the `secrets` list across, so no re-link is needed | BUILD-2343 |
