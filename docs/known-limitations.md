# Known limitations

What does not migrate, what you do instead, and what is planned. Read this page before a
migration. [support-matrix.md](support-matrix.md) has the field-by-field detail, and every
row here points into it. Every `W` number links to its entry in the
[Warning reference](support-matrix.md#warning-reference).

Last checked against `main` on 2026-09-10.

## The short list

Six things to know before you run the migration.

1. **Custom and JenkinsPipeline builds do not convert.** The plugin skips them and passes the
   BuildConfig through unchanged. Rewrite a Custom build as a ClusterBuildStrategy or a Tekton
   Task, and a Jenkins pipeline as a Tekton Pipeline.
2. **No trigger fires after migration.** Webhook, ImageChange and ConfigChange triggers are all
   dropped. The plugin keeps them in an annotation on the Build. You start BuildRuns yourself,
   or from Pipelines-as-Code or Tekton Triggers.
3. **Builds that feed other builds are not ordered.** Shipwright runs BuildRuns concurrently.
   Run the producer to completion before you start the consumer.
4. **A binary archive source, or more than one source, fails the conversion.** Shipwright takes
   one source per Build, and its local source is a directory upload. Split the BuildConfig, or
   move the source to git.
5. **Files from `source.secrets` and `source.configMaps` do not reach the build.** The
   strategy has to declare the volume first. [volume-migration.md](volume-migration.md) has
   the steps.
6. **The trusted CA bundle is dropped, with no warning.** A fix is in review, see
   [Planned](#planned).

## Not supported

None of these has a story on the board. Shipwright has no equivalent, or the plugin cannot see
enough of the cluster to do the work. The last column says why, and names the warning you will see.

| BuildConfig feature | What happens | What you do instead | Why |
|---|---|---|---|
| `strategy.type: Custom` | skipped, passed through unchanged | Rewrite the build as a ClusterBuildStrategy or a Tekton Task | A Custom build runs an image you supply. Shipwright has no strategy that takes one. [W4](support-matrix.md#w4) |
| `strategy.type: JenkinsPipeline` | skipped, passed through unchanged | Move the pipeline to Tekton Pipelines | The strategy needs a Jenkins server. Shipwright builds images, it does not run pipelines. [W5](support-matrix.md#w5) |
| `source.binary` without `asFile` (an extracted archive) | failed | Use a git source, or a single-file binary source | Shipwright's local source uploads a directory, not an archive. [W63](support-matrix.md#w63) |
| More than one of `source.git`, `source.binary`, `source.images` | failed | Split into one BuildConfig per source. For git plus an image, rewrite as a multi-stage Dockerfile | A Shipwright Build has one `spec.source`. [W62](support-matrix.md#w62) |
| More than one `source.images` entry | failed | Rewrite as a multi-stage Dockerfile | One OCI artifact source per Build. [W64](support-matrix.md#w64) |
| Webhook triggers: `GitHub`, `GitLab`, `Bitbucket`, `Generic` | dropped, kept in the `buildconfig-to-shipwright/original-triggers` annotation | Remove or repoint the webhook in your Git provider. Use Pipelines-as-Code or Tekton Triggers to create BuildRuns on push. [trigger-migration.md](trigger-migration.md#webhooks-github-gitlab-bitbucket-generic) has the listener to apply | Shipwright serves no webhook URL. The Triggers component that would is not shipped with the operator. [W50](support-matrix.md#w50), [W51](support-matrix.md#w51) |
| `ImageChange` trigger | dropped, kept in the same annotation | Start a BuildRun from your own automation when the image changes. [trigger-migration.md](trigger-migration.md#imagechange) has a Pipeline for chained Builds and a CronJob for external images | Nothing in Shipwright watches an image for changes. [W52](support-matrix.md#w52) |
| `ConfigChange` trigger | dropped, kept in the same annotation | Create the first BuildRun yourself. When the Build carries a BuildRun template, apply that. Both commands are in [trigger-migration.md](trigger-migration.md#configchange) | Creating a Build starts nothing. Only a BuildRun starts a build. [W54](support-matrix.md#w54) |
| Run ordering for chained builds, where one build's output is another's input | both Builds convert, with a warning or an info line on the consumer. Nothing orders the runs | Run the producer's BuildRun to completion, then the consumer's | crane hands the plugin one resource at a time, and Shipwright does not order BuildRuns. [Chained builds](support-matrix.md#chained-builds) |
| `runPolicy: Serial` (or unset) and `SerialLatestOnly` | dropped. BuildRuns run concurrently | Serialise runs in your pipeline | Shipwright has no build queue. Every BuildRun starts as soon as it is created. [W43](support-matrix.md#w43), [W44](support-matrix.md#w44) |
| `postCommit` hooks | dropped | Add a test step after the BuildRun in a Tekton Pipeline. It runs after the push, so it cannot block a bad image | OpenShift ran the hook inside the built image before the push. Shipwright has no step between build and push. [W59](support-matrix.md#w59) |
| `source.secrets[]` and `source.configMaps[]` | dropped | Add an overridable volume to the strategy, a volume override on the Build, and change `ADD` or `COPY` to `RUN cp` in the Dockerfile. See [volume-migration.md](volume-migration.md) | Shipwright mounts files through strategy volumes, and the shipped strategies declare none. [W32](support-matrix.md#w32), [W33](support-matrix.md#w33) |
| `source.images[].as` | dropped | No equivalent | The OCI artifact source cannot rename what it unpacks. [W29](support-matrix.md#w29) |
| `source.images[].paths` | dropped. The whole image becomes the source | Adjust the Dockerfile to the image's layout | The OCI artifact source unpacks the whole image at the context root. It cannot pick paths. [W30](support-matrix.md#w30) |
| Inline Dockerfile (`source.dockerfile`) on a Docker strategy | converted, with a warning. The Dockerfile is saved to a ConfigMap the Build cannot use | Commit the Dockerfile to the source repository. Point `dockerStrategy.dockerfilePath` at it when it is not at the context root | The buildah strategy reads the Dockerfile from the source checkout. It cannot take one from a ConfigMap, and it will stay that way. [W57](support-matrix.md#w57) |
| `buildArgs[].valueFrom.fieldRef` and `resourceFieldRef` | dropped | Set the value directly in the Build | Shipwright build args take a literal, a ConfigMap key or a Secret key. Nothing else. [W18](support-matrix.md#w18) |
| A strategy volume with a `CSI` source | dropped | Add the volume to `spec.volumes` on the Build yourself | The plugin maps Secret and ConfigMap volume sources only. Shipwright itself accepts any pod volume source. [W24](support-matrix.md#w24) |
| `pullSecret` on a BuildConfig that names a `serviceAccount` | not linked. crane migrates the account as-is | Run the `oc secrets link` command from the warning on the target | The plugin never edits an account you own, so it cannot attach the secret. [W8](support-matrix.md#w8) |
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
| Strategy volumes with a Secret or ConfigMap source | The shipped strategies do not declare the volume. Copy the strategy and add it, per [volume-migration.md](volume-migration.md) | Shipwright checks every Build volume against the strategy. A volume the strategy does not declare fails validation with `UndefinedVolume`. [W25](support-matrix.md#w25), [W26](support-matrix.md#w26) |
| `sourceStrategy.incremental: true` | The first BuildRun fails unless the output image already exists on the target. Run it once with `incremental=false`, or push the image by hand | An incremental build starts `FROM` the previous output image to reuse its artifacts. On a fresh target that image is not there yet. [W21](support-matrix.md#w21) |
| `resources` (CPU and memory) | Shipwright puts resources on the BuildRun, not the Build. Apply the BuildRun template from the annotation | A Shipwright Build has no field for resource requirements. Only a BuildRun does, so the plugin writes one into an annotation for you to apply. [W48](support-matrix.md#w48) |
| Output to a registry with no `pushSecret` | A ServiceAccount with push credentials, or `spec.output.pushSecret` on the Build | OpenShift pushed with the builder account's credentials. Shipwright needs them named on the Build, or on the ServiceAccount the BuildRun uses. [W36](support-matrix.md#w36), [W37](support-matrix.md#w37) |

## Planned

Work with a story on the board. Nothing here has a release date.

| BuildConfig feature | What happens today | Planned change | Tracking |
|---|---|---|---|
| `mountTrustedCA` | dropped, no warning | A `trusted-ca` volume on the Build and a ConfigMap the cluster fills with the CA bundle | BUILD-2265, [PR #23](https://github.com/migtools/crane-plugin-buildconfig-to-shipwright/pull/23) in review |
| `serviceAccount` with its secrets and role bindings | crane migrates the account object. The plugin carries the name into the BuildRun template and warns you to verify the rest | Migrate the account's secrets, image pull secrets and RBAC with it | BUILD-2402 |
