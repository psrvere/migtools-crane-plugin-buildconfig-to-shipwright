# Binary build fed by a directory upload

A Docker-strategy BuildConfig with no git source. Each build was started with
`oc start-build binary-app --from-dir=.`, which streamed the current directory into the
build context. The Dockerfile in that directory downloads the application artifact from
Nexus, using a URL from `dockerStrategy.env`, a CA bundle copied in from a ConfigMap, and
credentials copied in from a Secret. This is the shape most of the client BuildConfigs we
have seen take.

## The input

[`buildconfig.yaml`](buildconfig.yaml). The parts that matter:

- `source.type: Binary` with `binary: {}`. No `asFile`, so the input was a directory.
- `source.configMaps` copying `cluster-ca-certs` into `certs/`, and `source.secrets` copying
  `nexus` into `nexusSecret/`, both inside the build context.
- `dockerStrategy.env` with `artifact_url` and `artifact_name`, which the Dockerfile's
  `RUN` steps read.
- `dockerStrategy.from` and `output.to` both on a private registry that the target cluster
  can also reach, so no registry mapping is needed.
- `runPolicy: Serial` and history limits of 2.

## The command

The plugin binary is in `./plugins`. [`optional-flags.json`](optional-flags.json) is empty:
the images already point at a registry the target cluster reaches.

```bash
crane export -n buildconfig-test

crane transform KubernetesPlugin BuildConfigToBuildsPlugin

crane apply
```

## The output

[`expected/Build_binary-app.yaml`](expected/Build_binary-app.yaml) is the Build the plugin
generated. The original BuildConfig is removed from the output. What to look at:

| In the Build | Where it came from |
|---|---|
| `spec.source: {type: Local, local: {name: local-copy, timeout: 10m0s}}` | `source.binary`. A Local source is a directory upload, the same thing `--from-dir` did, and nothing feeds it until you run `shp build upload` |
| `spec.strategy.name: buildah` | the Docker strategy type |
| `spec.paramValues[runtime-stage-from]` | `dockerStrategy.from`; the strategy replaces the last `FROM` with it |
| `spec.paramValues[dockerfile]: Dockerfile` | `dockerStrategy.dockerfilePath` |
| `spec.env` with `artifact_url` and `artifact_name` | `dockerStrategy.env`. See the caveat under "What to do next" |
| `spec.output.image: registry.example.internal/binary-app:1.2.2` | `output.to`, unchanged |
| `spec.retention.succeededLimit: 2`, `failedLimit: 2` | the two history limits |
| annotation `conversion-outcome: converted-with-warnings` | the warnings below say what needs you |

Nothing in the Build carries the ConfigMap or the Secret. That is what the first two
warnings are about.

## The warnings

The same text is in the `conversion-warnings` annotation and in the plugin log.

| Warning | Meaning |
|---|---|
| `has a binary source with no asFile … start each build with 'shp build upload binary-app <directory>'` | a Local source waits for an upload. A BuildRun created any other way waits 10 minutes and fails |
| `mounts ConfigMap 'cluster-ca-certs' to 'certs' during build …` | the file no longer reaches the build. The strategy has to declare a volume for it |
| `mounts secret 'nexus' to 'nexusSecret' during build …` | same, for the Secret |
| `No explicit pushSecret found for DockerImage output …` | OpenShift pushed with the builder account. Shipwright needs the credential named on the Build or on the BuildRun's ServiceAccount |
| `uses runPolicy "Serial", which is dropped` | BuildRuns run concurrently. Serialise them in your pipeline if two runs pushing the same tag matters |

## What to do next

1. Give the build its files. Follow [volume-migration.md](../../volume-migration.md): copy
   the `buildah` strategy, declare an overridable volume named `cluster-ca-certs` and one
   named `nexus`, mount them read-only in the build step, add the two volume overrides to the
   Build, point `spec.strategy.name` at the copy, and change the Dockerfile's `COPY certs/`
   and `COPY nexusSecret/` into `RUN cp` from the mount paths.

2. Put a push credential on the Build. Create a `docker-registry` Secret for
   `registry.example.internal` in the namespace and set `spec.output.pushSecret` to its
   name, or link it to the `pipeline` ServiceAccount the BuildRun runs as.

3. Check the env caveat. OpenShift inserted `dockerStrategy.env` as an `ENV` instruction
   right after `FROM`, so `RUN curl "$artifact_url"` worked. Shipwright's `spec.env` only
   reaches the buildah step, and `RUN` sees an empty value. Until BUILD-2476 lands, declare
   `ARG artifact_url` and `ARG artifact_name` in the Dockerfile and pass the values as
   build args on the Build:

   ```yaml
   spec:
     paramValues:
     - name: build-args
       values:
       - value: artifact_url=https://nexus.example.internal/repository/deploy/app.jar
       - value: artifact_name=app.jar
   ```

4. Apply the Build and check that Shipwright accepted it:

   ```bash
   kubectl apply -f expected/Build_binary-app.yaml
   kubectl wait --for=jsonpath='{.status.registered}'=True \
     build.shipwright.io/binary-app -n buildconfig-test --timeout=120s
   ```

   Write `build.shipwright.io`, not `build`. On OpenShift the short name resolves to the
   OpenShift Build API and reports the object as not found.

5. Start each build from the directory you used to start it on OpenShift. `shp` is the
   Shipwright CLI; `-F` follows the log.

   ```bash
   shp build upload binary-app ./context -n buildconfig-test -F
   ```

   The CLI creates a BuildRun, waits for its pod, streams the directory into it, and the
   build proceeds. There is no `oc start-build` equivalent that works without the upload.

## Verified

Run on a ROSA cluster with the Builds for Red Hat OpenShift operator 1.9.0 on 2026-09-16,
with the two placeholder registries replaced by ones the cluster reaches and a Dockerfile
that echoes the two env values, copies the two files and copies `app.jar`. The baseline
`oc start-build --from-dir` completed. The plugin produced the Build in `expected/`. With
steps 1 to 3 applied, a strategy copy carrying the two volumes, the two overrides on the
Build and the values as build args, the Build registered and `shp build upload` produced a
BuildRun that printed both values, listed `ca.crt`, `username` and `password` from the
mounts, and pushed. Without step 3 the same `RUN echo` prints empty strings, which is the
caveat.

Rows in the [support matrix](../../support-matrix.md) for each field above: Source
(`source.binary`, `source.configMaps`, `source.secrets`), Docker strategy, Output, Build
settings (run policy, history limits).
