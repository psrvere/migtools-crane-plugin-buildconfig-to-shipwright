# Source-to-Image build with an ImageStream builder

A Source-to-Image BuildConfig for a Node.js app. Its builder image is `nodejs:18-ubi8` from
the `openshift` namespace's ImageStreams, which the target cluster does not have. The
`imagestream-mapping` flag names the real image. The output stays on the target cluster's
internal registry, which is the shape of an OpenShift-to-OpenShift move.

## The input

[`buildconfig.yaml`](buildconfig.yaml). The parts that matter:

- `strategy.type: Source` with `sourceStrategy.from` of kind `ImageStreamTag`, name
  `nodejs:18-ubi8`, namespace `openshift`.
- `sourceStrategy.env` with one variable.
- `source.git` plus a `contextDir`.
- `output.to` of kind `ImageStreamTag`, no push secret.
- Three triggers: `ConfigChange`, `ImageChange`, and a `GitHub` webhook with a secret
  reference.

## The command

The flags are in [`optional-flags.json`](optional-flags.json): one ImageStream mapping for the
builder. The key is `<namespace>/<name>:<tag>` exactly as the BuildConfig writes it. There is
no registry mapping, so the output image keeps the internal-registry form.

```bash
crane export -n my-app

crane transform BuildConfigPlugin \
  --plugin-dir ./plugins \
  --optional-flags "$(cat optional-flags.json)"

crane apply
```

## The output

[`expected/Build_sample-nodejs.yaml`](expected/Build_sample-nodejs.yaml). What to look at:

| In the Build | Where it came from |
|---|---|
| `spec.strategy.name: source-to-image` | the Source strategy type |
| `spec.paramValues[builder-image]: registry.access.redhat.com/ubi9/nodejs-18:latest` | `sourceStrategy.from`, resolved through the ImageStream mapping. Without the flag it would be `image-registry.openshift-image-registry.svc:5000/openshift/nodejs:18-ubi8`, which exists on no target cluster |
| `spec.env` | `sourceStrategy.env` |
| `spec.source.contextDir: source-build` | `source.contextDir` |
| `spec.output.image: image-registry.openshift-image-registry.svc:5000/my-app/sample-nodejs:latest` | the ImageStreamTag output in its internal-registry form. No mapping touched it |
| annotation `original-triggers` | all three triggers. The webhook keeps only the secret's name; an inline secret value would have been left out |

## The warnings

| Warning | Meaning |
|---|---|
| `Output ImageStreamTag "sample-nodejs:latest" resolved to fallback URL: …` | no mapping matched the output, so the plugin used the internal-registry form. Right for an OpenShift target |
| `No explicit pushSecret found for ImageStreamTag output …` | the push to the internal registry works only if the BuildRun runs as a ServiceAccount with push rights. On OpenShift with the Builds operator that is the default `pipeline` account. See step 1 below |
| `uses runPolicy "Serial", which is dropped` | BuildRuns run concurrently |
| `ConfigChange trigger is dropped` | start the first build yourself |
| `ImageChange trigger is dropped — builds will no longer start when the strategy image nodejs:18-ubi8 changes` | a new builder image no longer rebuilds the app |
| `GitHub webhook trigger is dropped — the old OpenShift webhook URL will stop working …` | remove or repoint the webhook in GitHub; use Pipelines-as-Code or Tekton Triggers for pushes |
| `Found 3 trigger(s) …` | the summary line |

## What to do next

1. Apply the Build and start the first build. On OpenShift with the Builds for Red Hat
   OpenShift operator, leave `serviceAccount` unset: the BuildRun runs as the `pipeline`
   account, which can push to the internal registry and is allowed to run build pods. The
   `builder` account that OpenShift builds used is refused by the cluster's security policy
   for Shipwright pods, so do not name it here.

   ```bash
   kubectl apply -f expected/Build_sample-nodejs.yaml
   kubectl wait --for=jsonpath='{.status.registered}'=True \
     build.shipwright.io/sample-nodejs -n my-app --timeout=120s

   cat <<'YAML' | kubectl create -f -
   apiVersion: shipwright.io/v1beta1
   kind: BuildRun
   metadata:
     generateName: sample-nodejs-
     namespace: my-app
   spec:
     build:
       name: sample-nodejs
   YAML
   ```

   Write `build.shipwright.io`, not `build`. On OpenShift the short name resolves to the
   OpenShift Build API.

2. Delete the GitHub webhook that pointed at the old cluster, or repoint it at whatever
   you set up to create BuildRuns on push.

3. If anything rebuilt when the builder image changed, arrange that in your pipeline.

Rows in the [support matrix](../../support-matrix.md): Source (S2I) strategy, Image references,
Output, Triggers.
