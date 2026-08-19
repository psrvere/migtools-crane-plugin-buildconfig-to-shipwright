# Migrating BuildConfig volumes to Shipwright

For users whose BuildConfig declared strategy volumes
(`spec.strategy.dockerStrategy.volumes` / `spec.strategy.sourceStrategy.volumes`).
The converter copies these to the Shipwright `Build` as `spec.volumes` under
their **original names** — but that alone is not enough for the Build to run.

## Symptom

The converted `Build` fails validation on the cluster:

```
status:
  registered: "False"
  reason: UndefinedVolume
  message: Volume "my-npm-secret" is not defined in the Strategy
```

A `BuildRun` referencing it fails the same way (`Succeeded=False`) before any
build pod is created.

## Why

Shipwright volume semantics (upstream `shipwright/build`):

- A `Build`/`BuildRun` may only reference volumes that the build strategy
  declares, matched by **exact name**. Unknown names fail validation with
  `UndefinedVolume`.
- A strategy volume can only be overridden when it sets `overridable: true`
  (defaults to false; otherwise `VolumeNotOverridable`).
- Mount paths are fixed by the strategy's step `volumeMounts`. A Build volume
  carries only a name and a volume source (Secret/ConfigMap) — never a path.

The shipped `buildah` / `source-to-image` ClusterBuildStrategies do not
declare your BuildConfig's volume names, so the converted Build cannot
register until you provide a strategy copy that does.

## Fix, step by step

1. **Copy the shipped strategy** and rename it (strip `status` and managed
   fields):

   ```sh
   oc get clusterbuildstrategy buildah -o yaml > buildah-with-volumes.yaml
   # edit metadata.name -> buildah-with-volumes
   ```

2. **Declare the volume** with the *same name* the converted Build uses, as
   overridable, with a placeholder source (the Build's override supplies the
   real Secret/ConfigMap at run time):

   ```yaml
   spec:
     volumes:
       - name: my-npm-secret
         overridable: true
         emptyDir: {}
   ```

3. **Mount it in the build step** at the path your build expects. The
   converter warning echoes the original BuildConfig destination paths:

   ```yaml
   # inside the step that runs the build (e.g. the buildah step)
   volumeMounts:
     - name: my-npm-secret
       mountPath: /etc/npm
   ```

4. **Expose the mount to the Dockerfile's `RUN` steps.** The step
   `volumeMount` from step 3 only makes the files visible to the `buildah`
   *process* — they are **not** inside the container that `RUN` executes in.
   Add a bind to the `bud` invocation in your strategy copy, and it **must end
   in `:ro`**:

   ```sh
   # in the build step's script (the `buildah` step for source-to-image)
   buildah --storage-driver=$(params.storage-driver) \
     bud "${budArgs[@]}" \
     --volume /etc/npm:/etc/npm:ro \
     --registries-conf=/tmp/registries.conf \
     --tag="${image}" \
     --file="${dockerfile}" \
     .
   ```

   Without `:ro` the build fails at the first `RUN` that touches the path:

   ```
   remounting "/var/tmp/buildah.../mnt/rootfs/etc/npm" in mount namespace
   with flags [] instead of [ST_RDONLY]: permission denied
   ```

   Secret and ConfigMap volumes are mounted read-only (tmpfs) by Kubernetes.
   Unprivileged `buildah` (OpenShift `pipelines-scc`) cannot remount a
   read-only source read-write, so the bind flags have to match the source.
   Both shipped strategies end in `buildah bud`, so this applies to `buildah`
   and `source-to-image` alike.

5. **Point the Build at your copy**:

   ```yaml
   spec:
     strategy:
       kind: ClusterBuildStrategy
       name: buildah-with-volumes
   ```

6. **Re-apply.** Existing Builds only re-validate on a spec change
   (generation-based reconcile) — touch the Build spec or recreate it after a
   strategy update.

## Notes

- If both `Build` and `BuildRun` override the same volume, the `BuildRun`
  value wins.
- Docker builds: the files are visible to `RUN` only at the mounted path (see
  step 4) and are not part of the build context — have the Dockerfile
  `RUN cp`/read from that path instead of `ADD`/`COPY`. As with BuildConfig
  build volumes, they are not persisted into the output image.
- Volumes that many users need can get first-class support in the shipped
  strategies instead — precedent: the trusted-CA volume (BUILD-2342).
