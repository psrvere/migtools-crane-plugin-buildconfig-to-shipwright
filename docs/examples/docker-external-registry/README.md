# Docker build pushed to an external registry

A Docker-strategy BuildConfig that builds `ruby-hello-world` from its own Dockerfile and
pushes to an ImageStream. On the target cluster there is no ImageStream, so the image goes
to `quay.io/myorg` instead.

## The input

[`buildconfig.yaml`](buildconfig.yaml). The parts that matter:

- `strategy.type: Docker` with `dockerfilePath: Dockerfile`.
- `source.git` pointing at a public repository.
- `output.to` of kind `ImageStreamTag`, named `ruby-hello-world:latest`.
- `output.pushSecret` naming `quay-push`, the credential for the external registry.
- One `ConfigChange` trigger.

## The command

The plugin binary is in `./plugins`. The flags are in
[`optional-flags.json`](optional-flags.json): one registry mapping that rewrites the
internal registry prefix to `quay.io/myorg`.

```bash
crane export -n buildconfig-test

crane transform KubernetesPlugin BuildConfigToBuildsPlugin \
  --optional-flags "$(cat optional-flags.json)"

crane apply
```

## The output

[`expected/Build_ruby-hello-world-docker.yaml`](expected/Build_ruby-hello-world-docker.yaml)
is the Build the plugin generated. The original BuildConfig is removed from the output.
What to look at:

| In the Build | Where it came from |
|---|---|
| `spec.strategy.name: buildah` | the Docker strategy type. `buildah` is the strategy-catalog name the Builds for Red Hat OpenShift operator installs. Point the Build at a copy of it with the `default-build-strategy` flag |
| `spec.paramValues[dockerfile]: Dockerfile` | `dockerStrategy.dockerfilePath` |
| `spec.source.git` with `url` and `revision` | `source.git.uri` and `source.git.ref` |
| `spec.output.image: quay.io/myorg/buildconfig-test/ruby-hello-world:latest` | the ImageStreamTag output, resolved to the internal-registry form `image-registry.openshift-image-registry.svc:5000/buildconfig-test/ruby-hello-world:latest`, then rewritten by the registry mapping. The namespace stays in the path |
| `spec.output.pushSecret: quay-push` | `output.pushSecret` |
| annotation `conversion-outcome: converted-with-warnings` | something was dropped or needs review; the warnings say what |
| annotation `original-triggers` | the `ConfigChange` trigger, kept for the day triggers exist in Shipwright |

The `configMapValue: null` and `secretValue: null` lines under `paramValues` are
serialisation noise from the Shipwright types. Shipwright accepts them.

## The warnings

The same text is in the `conversion-warnings` annotation and in the plugin log.

| Warning | Meaning |
|---|---|
| `Output ImageStreamTag "ruby-hello-world:latest" resolved to fallback URL: …` | no `imagestream-mapping` matched the output, so the plugin built the internal-registry form and let the registry mapping rewrite it. That is what we wanted here |
| `Output image … was redirected off the internal registry …` | nothing on the source cluster will see new images any more. Anything that watched the ImageStream to roll out must be repointed |
| `uses runPolicy "Serial", which is dropped` | BuildRuns run concurrently. Serialise them in your pipeline if two runs pushing the same tag matters |
| `ConfigChange trigger is dropped` | the first build will not start on its own |
| `Found 1 trigger(s) … none work in Shipwright today` | the summary line every BuildConfig with triggers gets |

## What to do next

1. The Build references `quay-push` by name, so that Secret must exist in the target
   namespace. The plugin leaves Secrets alone. `crane export` picks up the source
   cluster's `quay-push` and `crane apply` creates it on the target, unless another
   transform plugin you selected filters it out. If you applied only the Build from
   `expected/`, create the Secret yourself:

   ```bash
   kubectl create secret docker-registry quay-push -n buildconfig-test \
     --docker-server=quay.io --docker-username=… --docker-password=…
   ```

2. Apply the Build and check that Shipwright accepted it:

   ```bash
   kubectl apply -f expected/Build_ruby-hello-world-docker.yaml
   kubectl wait --for=jsonpath='{.status.registered}'=True \
     build.shipwright.io/ruby-hello-world-docker -n buildconfig-test --timeout=120s
   ```

   Write `build.shipwright.io`, not `build`. On OpenShift the short name resolves to the
   OpenShift Build API and reports the object as not found.

3. Start the first build yourself, since the `ConfigChange` trigger is gone. On OpenShift
   with the Builds operator, leave `serviceAccount` unset: the BuildRun runs as the
   `pipeline` account, which the operator has already allowed to run build pods.

   ```bash
   cat <<'YAML' | kubectl create -f -
   apiVersion: shipwright.io/v1beta1
   kind: BuildRun
   metadata:
     generateName: ruby-hello-world-docker-
     namespace: buildconfig-test
   spec:
     build:
       name: ruby-hello-world-docker
   YAML
   ```

4. If a Deployment or DeploymentConfig on the source cluster rolled out on the ImageStream,
   point it at `quay.io/myorg/buildconfig-test/ruby-hello-world:latest`.

Rows in the [support matrix](../../support-matrix.md) for each field above: Strategy, Source,
Output, Build settings (run policy), Triggers.
