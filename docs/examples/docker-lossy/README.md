# A lossy Docker build

A Docker-strategy BuildConfig that carries five things Shipwright cannot hold as they are:
a pull secret with no ServiceAccount, a Secret strategy volume, resource requests, a
post-commit test hook, and an inline Dockerfile. It still converts. The plugin generates
three resources for it and eight warnings, and every one of them asks something of you.
This is the example to read before a real migration.

## The input

[`buildconfig.yaml`](buildconfig.yaml). The parts that matter:

- `dockerStrategy.pullSecret: registry-pull`, and no `spec.serviceAccount`.
- `dockerStrategy.volumes`: one Secret volume, `npmrc`, mounted at `/root/.npmrc`.
- `source.dockerfile`: an inline Dockerfile that overrides the one in the repository.
- `spec.resources` with requests and limits.
- `spec.postCommit.script: npm test`.
- `output.to` of kind `ImageStreamTag` with `pushSecret: quay-push`.

## The command

```bash
crane export -n shop

crane transform BuildConfigPlugin \
  --plugin-dir ./plugins \
  --optional-flags "$(cat optional-flags.json)"

crane apply
```

## The output

Three resources, in [`expected/`](expected/):

| File | Why it exists |
|---|---|
| [`Build_webapp.yaml`](expected/Build_webapp.yaml) | the Build. Note `spec.volumes` with the `npmrc` Secret under its original name, and the four annotations below |
| [`ServiceAccount_webapp.yaml`](expected/ServiceAccount_webapp.yaml) | the pull secret has to hang off a ServiceAccount, and the BuildConfig named none, so the plugin made one carrying `registry-pull` |
| [`ConfigMap_webapp-dockerfile.yaml`](expected/ConfigMap_webapp-dockerfile.yaml) | the inline Dockerfile, preserved. The buildah strategy cannot build from it yet, so this keeps the content from being lost |

The four annotations on the Build:

| Annotation | What it holds |
|---|---|
| `buildconfig-to-shipwright/buildrun-template` | a complete BuildRun with `stepResources` for the `build-and-push` step carrying the requests and limits, and `serviceAccount: webapp` so the pull secret is used. Not a live object; nothing runs until you apply it |
| `buildconfig-to-shipwright/inline-dockerfile-configmap` | the ConfigMap's name |
| `buildconfig-to-shipwright/conversion-outcome` | `converted-with-warnings` |
| `crane.konveyor.io/conversion-warnings` | the eight warnings |

## The warnings

| Warning | Meaning |
|---|---|
| `Volume "npmrc" was converted, but the Build will fail validation (reason: UndefinedVolume) until you …` | Shipwright only accepts volumes the strategy declares. The warning spells out the three edits and carries the original mount path, `/root/.npmrc`, which the Build itself cannot hold |
| `Volumes were converted to Build spec volumes, but the shipped Buildah ClusterBuildStrategy does not declare them …` | the once-per-BuildConfig summary of the same fact |
| `Inline Dockerfile on BuildConfig shop/webapp cannot be consumed by the buildah strategy; its content was preserved in ConfigMap …` | logged at ERROR. The Build will use the Dockerfile in the git repository, not this one, until you commit it |
| `Output ImageStreamTag "webapp:latest" resolved to fallback URL …` and `… redirected off the internal registry …` | as in the [external registry example](../docker-external-registry/) |
| `uses runPolicy "Serial", which is dropped` | BuildRuns run concurrently |
| `PostCommit hook (script) has no Shipwright equivalent and was dropped …` | `npm test` no longer runs between build and push. Anything you put after the BuildRun runs after the image is already in the registry |
| `Resource requirements are not supported on Shipwright Build. Apply the BuildRun template …` | the requests and limits live only in the template annotation |

## What to do next

1. The generated resources reference three Secrets by name: `registry-pull`, `npmrc`,
   and `quay-push`. They must exist in the target namespace. The plugin leaves Secrets
   alone. `crane export` picks them up and `crane apply` creates them on the target,
   unless another transform plugin you selected filters them out. If you applied only
   the resources from `expected/`, create them yourself.

2. Apply all three generated resources. The Build will not register yet:

   ```bash
   kubectl apply -f expected/
   kubectl get build webapp -n shop -o jsonpath='{.status.reason}: {.status.message}'
   # UndefinedVolume: Volume "npmrc" is not defined in the Strategy
   ```

3. Follow [`docs/volume-migration.md`](../../volume-migration.md): copy the `buildah`
   ClusterBuildStrategy as `buildah-with-volumes`, declare an overridable volume named
   `npmrc`, and mount it on the `build-and-push` step. Two details the runbook's snippet
   makes easy to miss: the mount must be `readOnly: true`, or Shipwright rejects the BuildRun
   with `volume mount "npmrc" must be read only`, and `subPath: .npmrc` puts the secret's key
   at the file path the BuildConfig used:

   ```yaml
   volumeMounts:
     - name: npmrc
       mountPath: /root/.npmrc
       subPath: .npmrc
       readOnly: true
   ```

   Then point the Build at the copy and confirm it registers:

   ```bash
   kubectl patch build.shipwright.io webapp -n shop --type merge \
     -p '{"spec":{"strategy":{"kind":"ClusterBuildStrategy","name":"buildah-with-volumes"}}}'
   kubectl wait --for=jsonpath='{.status.registered}'=True \
     build.shipwright.io/webapp -n shop --timeout=120s
   ```

   Write `build.shipwright.io`, not `build`. On OpenShift the short name resolves to the
   OpenShift Build API.

4. Commit the Dockerfile from the ConfigMap to the repository, or accept that the build uses
   whatever Dockerfile the repository has:

   ```bash
   kubectl get configmap webapp-dockerfile -n shop -o jsonpath='{.data.Dockerfile}'
   ```

5. Let the generated ServiceAccount run build pods. On OpenShift with the Builds operator
   only the `pipeline` account is allowed to by default; a ServiceAccount the plugin generated
   is refused with `PodAdmissionFailed` until it gets the same policy:

   ```bash
   oc adm policy add-scc-to-user pipelines-scc -z webapp -n shop
   ```

   The pull secret on that account is what buildah uses to pull the `FROM` image, so it must
   be a valid credential for that registry. A placeholder secret that names the registry
   makes the pull fail with `403 Forbidden`.

6. Start the first build from the template, which carries the resources and the generated
   ServiceAccount. The copy from step 3 keeps the `build-and-push` step name, so the
   `stepResources` entry still matches:

   ```bash
   kubectl get build webapp -n shop \
     -o jsonpath='{.metadata.annotations.buildconfig-to-shipwright/buildrun-template}' \
     | kubectl create -f -
   ```

7. Put `npm test` somewhere else. A Tekton task after the BuildRun is the usual place, and it
   runs after the push, so it gates deployment rather than the registry.

Rows in the [support matrix](../../support-matrix.md): Strategy (pull secret), Strategy
volumes, Source (inline Dockerfile), Output, Build settings (resources, post-commit hook).
