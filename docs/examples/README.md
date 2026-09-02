# Worked examples

Each folder is one BuildConfig taken through the plugin, with the exact input, the flags,
the output the plugin produced, and what you do afterwards.

| Example | What it shows |
|---|---|
| [docker-external-registry](docker-external-registry/) | A Docker build whose image now goes to an external registry instead of an ImageStream. Registry mapping, push secret, dropped trigger and run policy |
| [s2i-imagestream-builder](s2i-imagestream-builder/) | A Source-to-Image build whose builder image lived in an ImageStream. ImageStream mapping, output on the internal registry, three dropped triggers including a webhook |
| [docker-lossy](docker-lossy/) | A Docker build with a pull secret, a Secret volume, resources, a post-commit hook, and an inline Dockerfile. Three generated resources, the BuildRun template, and what each warning asks of you |

Every folder has the same shape:

- `buildconfig.yaml`, the input, as exported from the source cluster.
- `optional-flags.json`, the value passed to `crane transform --optional-flags`.
- `expected/`, one file per resource the plugin generated, byte for byte.
- `README.md`, the walkthrough.

The `expected/` files are not hand-written. `TestExamplesMatchCommittedOutput` in
`buildconfig/examples_test.go` runs the plugin over every folder on each CI run and fails
if the output no longer matches. When a change in the plugin moves an example, regenerate
it and re-read its README:

```bash
go test ./buildconfig -run TestExamplesMatchCommittedOutput -update
```

Every example was run on an OpenShift cluster with the Builds for Red Hat OpenShift operator:
transform and apply through crane, the Build registered by Shipwright, and a BuildRun that
pushed an image. The commands and results are in each example's pull request.

For what happens to every BuildConfig field, see the [support matrix](../support-matrix.md).
