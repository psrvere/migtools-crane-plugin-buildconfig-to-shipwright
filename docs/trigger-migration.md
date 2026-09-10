# Getting builds to fire again after migration

For users whose BuildConfig had triggers, which is nearly every BuildConfig: the console adds
`ConfigChange` and `ImageChange` to each one it creates. The plugin drops every trigger, keeps
a copy in an annotation, and warns per type ([W50](support-matrix.md#w50) to
[W56](support-matrix.md#w56)). Nothing starts a build on the target cluster until you add it.
This page has the resources to add, one recipe per trigger type.

The webhook recipe needs OpenShift Pipelines with the Triggers component, which the
operator installs with its default `all` profile. The other two need Pipelines only. All of
them need Builds for OpenShift. The recipes use the cluster's own `openshift/cli` image, so
nothing is pulled from outside. Last run on
OpenShift 4.20 with OpenShift Pipelines 1.23.2 (Tekton Triggers v0.36.0) and Builds for
OpenShift 1.9.0, on 2026-09-10.

## Read the annotation first

The plugin writes the dropped triggers to `buildconfig-to-shipwright/original-triggers` on the
Build. It keeps the type, the webhook Secret's name, `allowEnv`, the `ImageChange` source and
`paused`, and never the Secret's value.

```bash
kubectl get build.shipwright.io BUILD -n NAMESPACE \
  -o jsonpath='{.metadata.annotations.buildconfig-to-shipwright/original-triggers}'
```

```json
[{"type":"GitHub","secretReference":{"name":"sample-nodejs-github-webhook"}},
 {"type":"ImageChange","imageChange":{}},
 {"type":"ConfigChange"}]
```

Each entry picks a section below. `secretReference.name` is the Secret the webhook recipe
points at. crane export carries that Secret across with the rest of the namespace, so it is
already on the target under the same name, with its value under the key `WebHookSecretKey`.

The second thing to read is which ServiceAccount the build runs as. When the BuildConfig had
a pull secret, the plugin generated a ServiceAccount that carries it, named after the
BuildConfig. A BuildRun that does not name that account runs as the namespace `pipeline`
account, and a private builder image will not pull. When the BuildConfig also set resources,
the Build's `buildconfig-to-shipwright/buildrun-template` annotation names the account:

```bash
kubectl get build.shipwright.io BUILD -n NAMESPACE \
  -o jsonpath='{.metadata.annotations.buildconfig-to-shipwright/buildrun-template}' \
  | grep serviceAccount
```

When there is no annotation, look for the account itself:

```bash
kubectl get serviceaccount -n NAMESPACE
```

Every recipe below has a `SERVICEACCOUNT` placeholder: the generated account when there is
one, `pipeline` otherwise. On OpenShift, grant a generated account the SCC buildah needs,
scoped to that one account:

```bash
oc adm policy add-scc-to-user pipelines-scc -z SERVICEACCOUNT -n NAMESPACE
```

## Webhooks: GitHub, GitLab, Bitbucket, Generic

On OpenShift, a webhook was a URL on the API server. The git host called it and a build
started. A Tekton Triggers EventListener gives you the URL back. It receives the POST, an
interceptor checks the signature against the exported Secret, and a TriggerTemplate creates
a BuildRun. Nothing changes in the source repository.

One listener carries every provider-validated trigger in the namespace, one trigger per
BuildConfig webhook. Generic webhooks get a second listener with its own URL. A Generic
trigger validates nothing, so on a shared listener every GitHub push, and any other POST
that reaches the URL, would start a build through it as well.

Replace `NAMESPACE`, `BUILD`, `SECRET` and `SERVICEACCOUNT`, and keep the triggers you had,
deleting the rest:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: buildrun-webhooks
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: buildrun-webhooks
rules:
  - apiGroups: ["shipwright.io"]
    resources: ["buildruns"]
    verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: buildrun-webhooks-creator
subjects:
  - kind: ServiceAccount
    name: buildrun-webhooks
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: buildrun-webhooks
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: buildrun-webhooks-eventlistener
subjects:
  - kind: ServiceAccount
    name: buildrun-webhooks
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tekton-triggers-eventlistener-roles
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: buildrun-webhooks-NAMESPACE-eventlistener
subjects:
  - kind: ServiceAccount
    name: buildrun-webhooks
    namespace: NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tekton-triggers-eventlistener-clusterroles
---
apiVersion: triggers.tekton.dev/v1beta1
kind: TriggerTemplate
metadata:
  name: start-buildrun
spec:
  params:
    - name: build
    - name: trigger
    - name: serviceAccount
  resourcetemplates:
    - apiVersion: shipwright.io/v1beta1
      kind: BuildRun
      metadata:
        generateName: $(tt.params.build)-
        labels:
          buildconfig-to-shipwright/trigger: $(tt.params.trigger)
      spec:
        build:
          name: $(tt.params.build)
        serviceAccount: $(tt.params.serviceAccount)
---
apiVersion: triggers.tekton.dev/v1beta1
kind: EventListener
metadata:
  name: buildconfig-webhooks
spec:
  serviceAccountName: buildrun-webhooks
  triggers:
    - name: BUILD-github
      interceptors:
        - ref:
            name: github
          params:
            - name: secretRef
              value:
                secretName: SECRET
                secretKey: WebHookSecretKey
            - name: eventTypes
              value: ["push"]
      bindings:
        - name: build
          value: BUILD
        - name: trigger
          value: github
        - name: serviceAccount
          value: SERVICEACCOUNT
      template:
        ref: start-buildrun
    - name: BUILD-gitlab
      interceptors:
        - ref:
            name: gitlab
          params:
            - name: secretRef
              value:
                secretName: SECRET
                secretKey: WebHookSecretKey
            - name: eventTypes
              value: ["Push Hook"]
      bindings:
        - name: build
          value: BUILD
        - name: trigger
          value: gitlab
        - name: serviceAccount
          value: SERVICEACCOUNT
      template:
        ref: start-buildrun
    - name: BUILD-bitbucket
      interceptors:
        - ref:
            name: bitbucket
          params:
            - name: secretRef
              value:
                secretName: SECRET
                secretKey: WebHookSecretKey
            - name: eventTypes
              value: ["repo:refs_changed"]
      bindings:
        - name: build
          value: BUILD
        - name: trigger
          value: bitbucket
        - name: serviceAccount
          value: SERVICEACCOUNT
      template:
        ref: start-buildrun
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: buildconfig-webhooks
spec:
  to:
    kind: Service
    name: el-buildconfig-webhooks
  port:
    targetPort: http-listener
  tls:
    termination: edge
---
apiVersion: triggers.tekton.dev/v1beta1
kind: EventListener
metadata:
  name: buildconfig-generic
spec:
  serviceAccountName: buildrun-webhooks
  triggers:
    - name: BUILD-generic
      bindings:
        - name: build
          value: BUILD
        - name: trigger
          value: generic
        - name: serviceAccount
          value: SERVICEACCOUNT
      template:
        ref: start-buildrun
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: buildconfig-generic
spec:
  to:
    kind: Service
    name: el-buildconfig-generic
  port:
    targetPort: http-listener
  tls:
    termination: edge
```

Save it as `webhooks.yaml` and apply it:

```bash
kubectl apply -n NAMESPACE -f webhooks.yaml
```

A second BuildConfig in the same namespace is a second trigger in the same file, not a
second apply. `kubectl apply` replaces the listener's trigger list, so a file holding only the
new BuildConfig's trigger removes the first one, whose URL then answers every POST and
creates nothing. Copy the trigger block, change `BUILD`, `SECRET` and `SERVICEACCOUNT` in
the copy, keep both, and apply once.

The URLs to register at the git host:

```bash
echo "https://$(kubectl get route buildconfig-webhooks -n NAMESPACE -o jsonpath='{.spec.host}')"
echo "https://$(kubectl get route buildconfig-generic -n NAMESPACE -o jsonpath='{.spec.host}')"
```

Give the provider the same secret value the BuildConfig used. It is still in the Secret:

```bash
kubectl get secret SECRET -n NAMESPACE -o jsonpath='{.data.WebHookSecretKey}' | base64 -d
```

What each trigger checks, and what is different from OpenShift:

- **GitHub** checks `X-Hub-Signature-256` against the Secret and accepts `push` events.
- **GitLab** checks `X-Gitlab-Token` against the Secret and accepts `Push Hook` events.
- **Bitbucket** is written for Bitbucket Server, which signs its payloads. Bitbucket Cloud
  does not sign, so a Cloud trigger validates nothing. Put it on the `buildconfig-generic`
  listener instead, with the `secretRef` param removed and `eventTypes` set to
  `["repo:push"]`, and treat its URL the way the Generic paragraph below says.
- **Generic** checks nothing. OpenShift hid the Generic URL behind the secret in its path; a
  listener has one path and no secret. Restrict who can reach the Route, or move the caller
  to one of the provider triggers.
- `allowEnv` has no equivalent. A webhook cannot set environment variables on the BuildRun
  ([W51](support-matrix.md#w51)).
- OpenShift built only when the pushed branch matched the BuildConfig's. The recipe builds
  the Build's configured revision on every push. To keep the branch check, add a `cel`
  interceptor after the provider one with the filter `body.ref == 'refs/heads/main'`.

To send a test event without the git host, sign a payload with the Secret's value:

```bash
TOKEN=$(kubectl get secret SECRET -n NAMESPACE -o jsonpath='{.data.WebHookSecretKey}' | base64 -d)
URL=https://$(kubectl get route buildconfig-webhooks -n NAMESPACE -o jsonpath='{.spec.host}')
BODY='{"ref":"refs/heads/main"}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$TOKEN" | sed 's/^.* //')

# GitHub
curl -sS -X POST "$URL" -H 'Content-Type: application/json' \
  -H 'X-GitHub-Event: push' -H "X-Hub-Signature-256: sha256=$SIG" -d "$BODY"
# GitLab
curl -sS -X POST "$URL" -H 'Content-Type: application/json' \
  -H 'X-Gitlab-Event: Push Hook' -H "X-Gitlab-Token: $TOKEN" -d "$BODY"
# Bitbucket Server
curl -sS -X POST "$URL" -H 'Content-Type: application/json' \
  -H 'X-Event-Key: repo:refs_changed' -H "X-Hub-Signature: sha256=$SIG" -d "$BODY"
# Generic, on its own URL
curl -sS -X POST "https://$(kubectl get route buildconfig-generic -n NAMESPACE -o jsonpath='{.spec.host}')" \
  -H 'Content-Type: application/json' -d "$BODY"

kubectl get buildrun -n NAMESPACE -l buildconfig-to-shipwright/trigger
```

Every call, accepted or not, answers with the listener's `eventID`. An accepted one creates
a BuildRun carrying the trigger's label. A wrong signature creates nothing, and the only
trace is `payload signature check failed` in the `tekton-triggers-core-interceptors` pod's
log in `openshift-pipelines`.

If you already run Pipelines-as-Code, use it instead: its
[webhook guide](https://pipelinesascode.com/docs/guides/) wants a `.tekton/pipelinerun.yaml`
in each source repository and the secret under the key `webhook.secret`, so copy the value
out of `WebHookSecretKey` into a new Secret. The Task in the next section creates the BuildRun
from that PipelineRun.

## ImageChange

Nothing in Shipwright watches an image. What replaces the trigger depends on where the image
came from.

### The image is built by another migrated Build

This is the chained case the plugin warns about ([Chained builds](support-matrix.md#chained-builds)):
the consumer's builder or base image was the producer's output ImageStreamTag, and OpenShift
rebuilt the consumer whenever the producer pushed. Shipwright starts every BuildRun the
moment it is created, so ordering is yours. A Tekton Pipeline runs the producer to
completion and then the consumer. Replace `NAMESPACE`:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: buildrun-pipeline
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: buildrun-pipeline
rules:
  - apiGroups: ["shipwright.io"]
    resources: ["buildruns"]
    verbs: ["create", "get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: buildrun-pipeline-creator
subjects:
  - kind: ServiceAccount
    name: buildrun-pipeline
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: buildrun-pipeline
---
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: shipwright-buildrun
spec:
  description: Create a BuildRun for a Build and wait for it to finish.
  params:
    - name: build
      type: string
    - name: service-account
      type: string
      default: pipeline
    - name: poll-seconds
      type: string
      default: "10"
  steps:
    - name: run
      image: image-registry.openshift-image-registry.svc:5000/openshift/cli:latest
      script: |
        #!/bin/sh
        set -e
        cat > /tmp/buildrun.yaml <<EOF
        apiVersion: shipwright.io/v1beta1
        kind: BuildRun
        metadata:
          generateName: $(params.build)-
        spec:
          build:
            name: $(params.build)
          serviceAccount: $(params.service-account)
        EOF
        name=`kubectl create -f /tmp/buildrun.yaml -o jsonpath='{.metadata.name}'`
        echo "created buildrun/$name"
        while true; do
          status=`kubectl get buildrun/$name -o jsonpath='{.status.conditions[?(@.type=="Succeeded")].status}'`
          case "$status" in
            True)
              echo "buildrun/$name succeeded"
              exit 0 ;;
            False)
              kubectl get buildrun/$name -o jsonpath='{.status.conditions[?(@.type=="Succeeded")].message}'
              exit 1 ;;
          esac
          sleep $(params.poll-seconds)
        done
---
apiVersion: tekton.dev/v1
kind: Pipeline
metadata:
  name: rebuild-chain
spec:
  params:
    - name: producer
      type: string
    - name: consumer
      type: string
    - name: service-account
      type: string
      default: pipeline
  tasks:
    - name: producer
      taskRef:
        name: shipwright-buildrun
      params:
        - name: build
          value: $(params.producer)
        - name: service-account
          value: $(params.service-account)
    - name: consumer
      runAfter: [producer]
      taskRef:
        name: shipwright-buildrun
      params:
        - name: build
          value: $(params.consumer)
        - name: service-account
          value: $(params.service-account)
```

Save it as `rebuild-chain.yaml` and apply it:

```bash
kubectl apply -n NAMESPACE -f rebuild-chain.yaml
```

Run the chain, naming the two Builds and the account they run as:

```bash
kubectl create -n NAMESPACE -f - <<EOF
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: rebuild-chain-
spec:
  pipelineRef:
    name: rebuild-chain
  taskRunTemplate:
    serviceAccountName: buildrun-pipeline
  params:
    - name: producer
      value: PRODUCER-BUILD
    - name: consumer
      value: CONSUMER-BUILD
    - name: service-account
      value: SERVICEACCOUNT
EOF
```

The consumer's BuildRun is created only after the producer's reaches `Succeeded=True`; a
failed producer fails the PipelineRun and the consumer never starts. A chain longer than two
is more tasks with `runAfter`. The same Task is what a Pipelines-as-Code PipelineRun uses to
start a build from a push.

### The image comes from outside the cluster

A base image on `registry.access.redhat.com` or `quay.io` changes without telling the
cluster. Nothing here can watch it, so rebuild on a schedule instead. A CronJob creates a
BuildRun. Replace `NAMESPACE`, `BUILD` and `SERVICEACCOUNT`:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: buildrun-cron
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: buildrun-cron
rules:
  - apiGroups: ["shipwright.io"]
    resources: ["buildruns"]
    verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: buildrun-cron-creator
subjects:
  - kind: ServiceAccount
    name: buildrun-cron
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: buildrun-cron
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: BUILD-rebuild
spec:
  schedule: "0 3 * * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: buildrun-cron
          restartPolicy: Never
          containers:
            - name: create-buildrun
              image: image-registry.openshift-image-registry.svc:5000/openshift/cli:latest
              command: ["/bin/sh", "-c"]
              args:
                - |
                  kubectl create -f - <<EOF
                  apiVersion: shipwright.io/v1beta1
                  kind: BuildRun
                  metadata:
                    generateName: BUILD-
                    labels:
                      buildconfig-to-shipwright/trigger: cron
                  spec:
                    build:
                      name: BUILD
                    serviceAccount: SERVICEACCOUNT
                  EOF
```

Save it as `rebuild-nightly.yaml` and apply it:

```bash
kubectl apply -n NAMESPACE -f rebuild-nightly.yaml
```

If the registry can send a notification on push, as Quay can, point it at the Generic
listener above and drop the schedule.

## ConfigChange

On OpenShift a ConfigChange trigger ran the first build when the BuildConfig was created.
Creating a Build starts nothing, so run the first build yourself, once.

When the Build carries a `buildconfig-to-shipwright/buildrun-template` annotation, it holds a
BuildRun with the resources and ServiceAccount the plugin could not put on the Build. Apply
that one, as in the [lossy Docker example](examples/docker-lossy/README.md):

```bash
kubectl get build.shipwright.io BUILD -n NAMESPACE \
  -o jsonpath='{.metadata.annotations.buildconfig-to-shipwright/buildrun-template}' \
  | kubectl create -f -
```

Without the annotation, create the BuildRun yourself, naming the account from
[Read the annotation first](#read-the-annotation-first). A Build whose BuildConfig had a
pull secret but no resources has a generated account and no annotation, and a BuildRun that
leaves `serviceAccount` unset there drops the pull secret:

```bash
kubectl create -n NAMESPACE -f - <<EOF
apiVersion: shipwright.io/v1beta1
kind: BuildRun
metadata:
  generateName: BUILD-
spec:
  build:
    name: BUILD
  serviceAccount: SERVICEACCOUNT
EOF
```

## Not supported

The trigger rows of [known-limitations.md](known-limitations.md#not-supported) say what has
no equivalent and why, and the note under them says what upstream Shipwright Triggers plans
and what would have to change for the plugin to emit `spec.trigger` itself.
