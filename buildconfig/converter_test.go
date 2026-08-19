package buildconfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/konveyor/crane-lib/transform"
	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestRunSkipsNonBuildConfig(t *testing.T) {
	tests := []struct {
		name         string
		resource     map[string]interface{}
		wantWhiteOut bool
	}{
		{
			name: "Deployment is skipped",
			resource: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "myapp",
					"namespace": "default",
				},
			},
			wantWhiteOut: false,
		},
		{
			name: "BuildConfig with wrong API group is skipped",
			resource: map[string]interface{}{
				"apiVersion": "wrong.group/v1",
				"kind":       "BuildConfig",
				"metadata": map[string]interface{}{
					"name":      "myapp",
					"namespace": "default",
				},
			},
			wantWhiteOut: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
			request := transform.PluginRequest{
				Unstructured: unstructured.Unstructured{Object: tt.resource},
			}
			resp, err := plugin.Run(request)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.IsWhiteOut != tt.wantWhiteOut {
				t.Errorf("IsWhiteOut = %v, want %v", resp.IsWhiteOut, tt.wantWhiteOut)
			}
			if resp.Patches != nil {
				t.Errorf("Patches should be nil for non-BuildConfig, got %d patches", len(resp.Patches))
			}
		})
	}
}

func TestParseOptionalFields(t *testing.T) {
	tests := []struct {
		name   string
		extras map[string]string
		check  func(t *testing.T, opts PluginOptionalFields)
	}{
		{
			name:   "empty extras",
			extras: map[string]string{},
			check: func(t *testing.T, opts PluginOptionalFields) {
				if opts.RegistryMapping != nil {
					t.Error("RegistryMapping should be nil")
				}
				if opts.ImageStreamMapping != nil {
					t.Error("ImageStreamMapping should be nil")
				}
				if opts.SearchRegistries != nil {
					t.Error("SearchRegistries should be nil")
				}
			},
		},
		{
			name: "registry mapping parsed",
			extras: map[string]string{
				"registry-mapping": "old.io=new.io,old2.io=new2.io",
			},
			check: func(t *testing.T, opts PluginOptionalFields) {
				if len(opts.RegistryMapping) != 2 {
					t.Fatalf("expected 2 registry mappings, got %d", len(opts.RegistryMapping))
				}
				if opts.RegistryMapping["old.io"] != "new.io" {
					t.Errorf("expected old.io=new.io, got %s", opts.RegistryMapping["old.io"])
				}
			},
		},
		{
			name: "imagestream mapping parsed",
			extras: map[string]string{
				"imagestream-mapping": "myns/mystream:latest=quay.io/org/img:latest",
			},
			check: func(t *testing.T, opts PluginOptionalFields) {
				if len(opts.ImageStreamMapping) != 1 {
					t.Fatalf("expected 1 imagestream mapping, got %d", len(opts.ImageStreamMapping))
				}
				if opts.ImageStreamMapping["myns/mystream:latest"] != "quay.io/org/img:latest" {
					t.Errorf("unexpected mapping: %v", opts.ImageStreamMapping)
				}
			},
		},
		{
			name: "search registries parsed",
			extras: map[string]string{
				"search-registries": "docker.io,quay.io",
			},
			check: func(t *testing.T, opts PluginOptionalFields) {
				if len(opts.SearchRegistries) != 2 {
					t.Fatalf("expected 2 search registries, got %d", len(opts.SearchRegistries))
				}
			},
		},
		{
			name: "strategy mapping parsed",
			extras: map[string]string{
				"default-build-strategy": "docker=my-buildah,s2i=my-s2i",
			},
			check: func(t *testing.T, opts PluginOptionalFields) {
				if opts.StrategyMapping["docker"] != "my-buildah" {
					t.Errorf("expected docker=my-buildah, got %s", opts.StrategyMapping["docker"])
				}
				if opts.StrategyMapping["s2i"] != "my-s2i" {
					t.Errorf("expected s2i=my-s2i, got %s", opts.StrategyMapping["s2i"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := ParseOptionalFields(tt.extras)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, opts)
		})
	}
}

func TestResolveImageRef(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		refName     string
		namespace   string
		opts        PluginOptionalFields
		wantRef     string
		wantWarning bool
		wantErr     bool
	}{
		{
			name:    "DockerImage returns name directly",
			kind:    "DockerImage",
			refName: "golang:1.21-alpine",
			wantRef: "golang:1.21-alpine",
		},
		{
			name:      "ImageStreamTag resolved via mapping",
			kind:      "ImageStreamTag",
			refName:   "mystream:latest",
			namespace: "myns",
			opts: PluginOptionalFields{
				ImageStreamMapping: map[string]string{
					"myns/mystream:latest": "quay.io/org/img:latest",
				},
			},
			wantRef: "quay.io/org/img:latest",
		},
		{
			name:        "ImageStreamTag falls back to internal registry URL",
			kind:        "ImageStreamTag",
			refName:     "mystream:v1",
			namespace:   "myns",
			wantRef:     "image-registry.openshift-image-registry.svc:5000/myns/mystream:v1",
			wantWarning: true,
		},
		{
			name:        "ImageStreamImage falls back to internal registry URL",
			kind:        "ImageStreamImage",
			refName:     "mystream@sha256:abc123",
			namespace:   "myns",
			wantRef:     "image-registry.openshift-image-registry.svc:5000/myns/mystream@sha256:abc123",
			wantWarning: true,
		},
		{
			name:      "Registry mapping applied after resolution",
			kind:      "ImageStreamTag",
			refName:   "mystream:latest",
			namespace: "myns",
			opts: PluginOptionalFields{
				ImageStreamMapping: map[string]string{
					"myns/mystream:latest": "old-registry.io/org/img:latest",
				},
				RegistryMapping: map[string]string{
					"old-registry.io": "new-registry.io",
				},
			},
			wantRef: "new-registry.io/org/img:latest",
		},
		{
			name:      "Registry mapping applied on fallback URL",
			kind:      "ImageStreamTag",
			refName:   "mystream:v1",
			namespace: "myns",
			opts: PluginOptionalFields{
				RegistryMapping: map[string]string{
					"image-registry.openshift-image-registry.svc:5000": "quay.io",
				},
			},
			wantRef: "quay.io/myns/mystream:v1",
		},
		{
			name:    "unknown kind returns error",
			kind:    "UnknownKind",
			refName: "something",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, warning, err := resolveImageRef(tt.kind, tt.refName, tt.namespace, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
			if tt.wantWarning && warning == "" {
				t.Error("expected warning, got empty string")
			}
			if !tt.wantWarning && warning != "" {
				t.Errorf("unexpected warning: %s", warning)
			}
		})
	}
}

func TestConvertDockerStrategyBasic(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "myapp-build",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
						"ref": "main",
					},
				},
				"strategy": map[string]interface{}{
					"type": "Docker",
					"dockerStrategy": map[string]interface{}{
						"dockerfilePath": "Dockerfile.prod",
					},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.IsWhiteOut {
		t.Error("expected IsWhiteOut to be true")
	}

	if len(resp.NewResources) < 1 {
		t.Fatal("expected at least 1 new resource")
	}

	buildRes := resp.NewResources[0]
	if buildRes.GetKind() != "Build" {
		t.Errorf("expected kind Build, got %s", buildRes.GetKind())
	}
	if buildRes.GetAPIVersion() != "shipwright.io/v1beta1" {
		t.Errorf("expected apiVersion shipwright.io/v1beta1, got %s", buildRes.GetAPIVersion())
	}
	if buildRes.GetName() != "myapp-build" {
		t.Errorf("expected name myapp-build, got %s", buildRes.GetName())
	}

	annotations := buildRes.GetAnnotations()
	if annotations["crane.konveyor.io/converted-from"] != "build.openshift.io/v1/BuildConfig/myapp-build" {
		t.Errorf("missing or wrong converted-from annotation: %v", annotations)
	}

	// Verify strategy
	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(buildRes.Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Strategy.Name != "buildah" {
		t.Errorf("expected strategy name buildah, got %s", b.Spec.Strategy.Name)
	}

	// Verify dockerfile param
	foundDockerfile := false
	for _, pv := range b.Spec.ParamValues {
		if pv.Name == "dockerfile" && pv.SingleValue != nil && *pv.SingleValue.Value == "Dockerfile.prod" {
			foundDockerfile = true
		}
	}
	if !foundDockerfile {
		t.Error("expected dockerfile param with value Dockerfile.prod")
	}

	// Verify source
	if b.Spec.Source == nil || b.Spec.Source.Type != shipwrightv1beta1.GitType {
		t.Error("expected Git source type")
	}
	if b.Spec.Source.Git.URL != "https://github.com/example/myapp.git" {
		t.Errorf("expected git URL, got %s", b.Spec.Source.Git.URL)
	}

	// Verify output
	if b.Spec.Output.Image != "quay.io/example/myapp:latest" {
		t.Errorf("expected output image quay.io/example/myapp:latest, got %s", b.Spec.Output.Image)
	}
}

func TestConvertDockerStrategyAllFields(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}

	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "full-docker",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
						"ref": "main",
					},
					"contextDir": "src",
					"sourceSecret": map[string]interface{}{
						"name": "git-creds",
					},
				},
				"strategy": map[string]interface{}{
					"type": "Docker",
					"dockerStrategy": map[string]interface{}{
						"dockerfilePath": "Dockerfile.prod",
						"from": map[string]interface{}{
							"kind": "DockerImage",
							"name": "golang:1.21-alpine",
						},
						"noCache":   true,
						"forcePull": true,
						"buildArgs": []interface{}{
							map[string]interface{}{"name": "GO_VERSION", "value": "1.21"},
							map[string]interface{}{"name": "GOOS", "value": "linux"},
						},
						"imageOptimizationPolicy": "SkipLayers",
						"env": []interface{}{
							map[string]interface{}{"name": "GOFLAGS", "value": "-mod=vendor"},
						},
						"pullSecret": map[string]interface{}{
							"name": "my-pull-secret",
						},
					},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
					"pushSecret": map[string]interface{}{
						"name": "quay-push-secret",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.IsWhiteOut {
		t.Error("expected IsWhiteOut = true")
	}

	// Should have Build + ServiceAccount
	if len(resp.NewResources) != 2 {
		t.Fatalf("expected 2 new resources (Build + ServiceAccount), got %d", len(resp.NewResources))
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	// Check strategy
	if b.Spec.Strategy.Name != "buildah" {
		t.Errorf("expected strategy buildah, got %s", b.Spec.Strategy.Name)
	}

	// Check all params exist
	paramNames := map[string]bool{}
	for _, pv := range b.Spec.ParamValues {
		paramNames[pv.Name] = true
	}
	for _, expected := range []string{"runtime-stage-from", "no-cache", "pull", "dockerfile", "build-args", "squash"} {
		if !paramNames[expected] {
			t.Errorf("missing param %s", expected)
		}
	}

	// Check env
	if len(b.Spec.Env) != 1 || b.Spec.Env[0].Name != "GOFLAGS" {
		t.Errorf("unexpected env: %v", b.Spec.Env)
	}

	// Check source
	if b.Spec.Source.Git.CloneSecret == nil || *b.Spec.Source.Git.CloneSecret != "git-creds" {
		t.Error("expected cloneSecret git-creds")
	}
	if b.Spec.Source.ContextDir == nil || *b.Spec.Source.ContextDir != "src" {
		t.Error("expected contextDir src")
	}

	// Check output
	if b.Spec.Output.Image != "quay.io/example/myapp:latest" {
		t.Errorf("unexpected output image: %s", b.Spec.Output.Image)
	}
	if b.Spec.Output.PushSecret == nil || *b.Spec.Output.PushSecret != "quay-push-secret" {
		t.Error("expected pushSecret quay-push-secret")
	}

	// Check ServiceAccount
	sa := resp.NewResources[1]
	if sa.GetKind() != "ServiceAccount" {
		t.Errorf("expected kind ServiceAccount, got %s", sa.GetKind())
	}
	if sa.GetName() != "full-docker" {
		t.Errorf("expected SA name full-docker, got %s", sa.GetName())
	}
}

func TestConvertDockerStrategyWithStrategyOverride(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "myapp",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
					},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
		Extras: map[string]string{
			"default-build-strategy": "docker=my-custom-buildah",
		},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Strategy.Name != "my-custom-buildah" {
		t.Errorf("expected strategy my-custom-buildah, got %s", b.Spec.Strategy.Name)
	}
}

func TestConvertUnsupportedStrategy(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	tests := []struct {
		name         string
		strategyType string
	}{
		{"Custom strategy", "Custom"},
		{"JenkinsPipeline strategy", "JenkinsPipeline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := transform.PluginRequest{
				Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "build.openshift.io/v1",
					"kind":       "BuildConfig",
					"metadata": map[string]interface{}{
						"name":      "myapp",
						"namespace": "myns",
					},
					"spec": map[string]interface{}{
						"source": map[string]interface{}{},
						"strategy": map[string]interface{}{
							"type": tt.strategyType,
						},
						"output": map[string]interface{}{},
					},
				}},
			}

			resp, err := plugin.Run(request)
			if err != nil {
				t.Fatalf("expected no error for unsupported strategy, got: %v", err)
			}
			if resp.IsWhiteOut {
				t.Error("expected IsWhiteOut to be false for unsupported strategy")
			}
			if len(resp.NewResources) > 0 {
				t.Error("expected no new resources for unsupported strategy")
			}
		})
	}
}

func TestConvertRegistryParams(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "myapp",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git":  map[string]interface{}{"uri": "https://example.com/repo.git"},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
		Extras: map[string]string{
			"search-registries":   "docker.io,quay.io",
			"insecure-registries": "my-registry.local:5000",
			"block-registries":    "blocked.io",
		},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	paramsByName := map[string]shipwrightv1beta1.ParamValue{}
	for _, pv := range b.Spec.ParamValues {
		paramsByName[pv.Name] = pv
	}

	// Search registries
	searchParam, ok := paramsByName["registries-search"]
	if !ok {
		t.Fatal("missing registries-search param")
	}
	if len(searchParam.Values) != 2 {
		t.Errorf("expected 2 search registries, got %d", len(searchParam.Values))
	}

	// Insecure registries — verify the bug fix: must use insecure list, not block list
	insecureParam, ok := paramsByName["registries-insecure"]
	if !ok {
		t.Fatal("missing registries-insecure param")
	}
	if len(insecureParam.Values) != 1 || *insecureParam.Values[0].Value != "my-registry.local:5000" {
		t.Errorf("insecure registries should contain my-registry.local:5000, got %v", insecureParam.Values)
	}

	// Block registries
	blockParam, ok := paramsByName["registries-block"]
	if !ok {
		t.Fatal("missing registries-block param")
	}
	if len(blockParam.Values) != 1 || *blockParam.Values[0].Value != "blocked.io" {
		t.Errorf("block registries should contain blocked.io, got %v", blockParam.Values)
	}
}

func TestConvertSourceStrategyBasic(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "s2i-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
						"ref": "main",
					},
				},
				"strategy": map[string]interface{}{
					"type": "Source",
					"sourceStrategy": map[string]interface{}{
						"from": map[string]interface{}{
							"kind": "DockerImage",
							"name": "registry.redhat.io/ubi8/python-39:latest",
						},
						"env": []interface{}{
							map[string]interface{}{"name": "APP_MODULE", "value": "myapp:app"},
						},
					},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.IsWhiteOut {
		t.Error("expected IsWhiteOut = true")
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Strategy.Name != "source-to-image" {
		t.Errorf("expected strategy source-to-image, got %s", b.Spec.Strategy.Name)
	}

	// Check builder-image param
	foundBuilder := false
	for _, pv := range b.Spec.ParamValues {
		if pv.Name == "builder-image" && pv.SingleValue != nil && *pv.SingleValue.Value == "registry.redhat.io/ubi8/python-39:latest" {
			foundBuilder = true
		}
	}
	if !foundBuilder {
		t.Error("expected builder-image param")
	}

	// Check env
	if len(b.Spec.Env) != 1 || b.Spec.Env[0].Name != "APP_MODULE" {
		t.Errorf("unexpected env: %v", b.Spec.Env)
	}
}

func TestConvertSourceStrategyWithS2IOverride(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "s2i-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git":  map[string]interface{}{"uri": "https://example.com/repo.git"},
				},
				"strategy": map[string]interface{}{
					"type": "Source",
					"sourceStrategy": map[string]interface{}{
						"from": map[string]interface{}{
							"kind": "DockerImage",
							"name": "python:3.9",
						},
					},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
		Extras: map[string]string{
			"default-build-strategy": "s2i=my-custom-s2i",
		},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Strategy.Name != "my-custom-s2i" {
		t.Errorf("expected strategy my-custom-s2i, got %s", b.Spec.Strategy.Name)
	}
}

func TestConvertBinarySource(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "binary-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type":   "Binary",
					"binary": map[string]interface{}{"asFile": "app.jar"},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Source.Type != shipwrightv1beta1.LocalType {
		t.Errorf("expected Local source type, got %s", b.Spec.Source.Type)
	}
	if b.Spec.Source.Local == nil || b.Spec.Source.Local.Name != "local-copy" {
		t.Error("expected Local source with name local-copy")
	}
}

func TestConvertBinaryArchiveSourceRejected(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "binary-archive-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type":   "Binary",
					"binary": map[string]interface{}{},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Source != nil && b.Spec.Source.Local != nil {
		t.Error("expected no Local source for unsupported binary archive (asFile empty)")
	}
}

func TestConvertImageSource(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "image-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Image",
					"images": []interface{}{
						map[string]interface{}{
							"from": map[string]interface{}{
								"kind": "DockerImage",
								"name": "registry.example.com/source:latest",
							},
							"pullSecret": map[string]interface{}{
								"name": "pull-secret",
							},
						},
					},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	if b.Spec.Source.Type != shipwrightv1beta1.OCIArtifactType {
		t.Errorf("expected OCIArtifact source type, got %s", b.Spec.Source.Type)
	}
	if b.Spec.Source.OCIArtifact.Image != "registry.example.com/source:latest" {
		t.Errorf("unexpected OCI image: %s", b.Spec.Source.OCIArtifact.Image)
	}
	if b.Spec.Source.OCIArtifact.PullSecret == nil || *b.Spec.Source.OCIArtifact.PullSecret != "pull-secret" {
		t.Error("expected OCIArtifact pullSecret")
	}
}

func TestConvertOutputImageStreamTag(t *testing.T) {
	tests := []struct {
		name      string
		outputTo  map[string]interface{}
		extras    map[string]string
		wantImage string
	}{
		{
			name: "ImageStreamTag with mapping",
			outputTo: map[string]interface{}{
				"kind": "ImageStreamTag",
				"name": "myapp:latest",
			},
			extras: map[string]string{
				"imagestream-mapping": "myns/myapp:latest=quay.io/org/myapp:latest",
			},
			wantImage: "quay.io/org/myapp:latest",
		},
		{
			name: "ImageStreamTag fallback without tag defaults to latest",
			outputTo: map[string]interface{}{
				"kind": "ImageStreamTag",
				"name": "myapp",
			},
			wantImage: "image-registry.openshift-image-registry.svc:5000/myns/myapp:latest",
		},
		{
			name: "ImageStreamTag fallback with tag",
			outputTo: map[string]interface{}{
				"kind": "ImageStreamTag",
				"name": "myapp:v2",
			},
			wantImage: "image-registry.openshift-image-registry.svc:5000/myns/myapp:v2",
		},
		{
			name: "ImageStreamTag with registry mapping on fallback",
			outputTo: map[string]interface{}{
				"kind": "ImageStreamTag",
				"name": "myapp:latest",
			},
			extras: map[string]string{
				"registry-mapping": "image-registry.openshift-image-registry.svc:5000=quay.io",
			},
			wantImage: "quay.io/myns/myapp:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
			request := transform.PluginRequest{
				Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "build.openshift.io/v1",
					"kind":       "BuildConfig",
					"metadata": map[string]interface{}{
						"name":      "myapp",
						"namespace": "myns",
					},
					"spec": map[string]interface{}{
						"source": map[string]interface{}{
							"type": "Git",
							"git":  map[string]interface{}{"uri": "https://example.com/repo.git"},
						},
						"strategy": map[string]interface{}{
							"type":           "Docker",
							"dockerStrategy": map[string]interface{}{},
						},
						"output": map[string]interface{}{
							"to": tt.outputTo,
						},
					},
				}},
				Extras: tt.extras,
			}

			resp, err := plugin.Run(request)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			b := &shipwrightv1beta1.Build{}
			jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
			json.Unmarshal(jsonBytes, b)

			if b.Spec.Output.Image != tt.wantImage {
				t.Errorf("output image = %q, want %q", b.Spec.Output.Image, tt.wantImage)
			}
		})
	}
}

func TestConvertOutputImageLabels(t *testing.T) {
	tests := []struct {
		name        string
		imageLabels []interface{}
		wantLabels  map[string]string
	}{
		{
			name: "imageLabels mapped to output labels",
			imageLabels: []interface{}{
				map[string]interface{}{"name": "vendor", "value": "Acme"},
				map[string]interface{}{"name": "io.openshift.tags", "value": "web,frontend"},
			},
			wantLabels: map[string]string{
				"vendor":            "Acme",
				"io.openshift.tags": "web,frontend",
			},
		},
		{
			name: "label with empty value is preserved",
			imageLabels: []interface{}{
				map[string]interface{}{"name": "empty-label"},
			},
			wantLabels: map[string]string{"empty-label": ""},
		},
		{
			name: "duplicate label names last wins",
			imageLabels: []interface{}{
				map[string]interface{}{"name": "vendor", "value": "First"},
				map[string]interface{}{"name": "vendor", "value": "Second"},
			},
			wantLabels: map[string]string{"vendor": "Second"},
		},
		{
			name: "label with empty name is skipped",
			imageLabels: []interface{}{
				map[string]interface{}{"name": "", "value": "ignored"},
			},
			wantLabels: nil,
		},
		{
			name:        "no imageLabels leaves output labels unset",
			imageLabels: nil,
			wantLabels:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
			output := map[string]interface{}{
				"to": map[string]interface{}{
					"kind": "DockerImage",
					"name": "quay.io/org/myapp:latest",
				},
			}
			if tt.imageLabels != nil {
				output["imageLabels"] = tt.imageLabels
			}
			request := transform.PluginRequest{
				Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "build.openshift.io/v1",
					"kind":       "BuildConfig",
					"metadata": map[string]interface{}{
						"name":      "myapp",
						"namespace": "myns",
					},
					"spec": map[string]interface{}{
						"source": map[string]interface{}{
							"type": "Git",
							"git":  map[string]interface{}{"uri": "https://example.com/repo.git"},
						},
						"strategy": map[string]interface{}{
							"type":           "Docker",
							"dockerStrategy": map[string]interface{}{},
						},
						"output": output,
					},
				}},
			}

			resp, err := plugin.Run(request)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			b := &shipwrightv1beta1.Build{}
			jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
			json.Unmarshal(jsonBytes, b)

			if !reflect.DeepEqual(b.Spec.Output.Labels, tt.wantLabels) {
				t.Errorf("output labels = %#v, want %#v", b.Spec.Output.Labels, tt.wantLabels)
			}
		})
	}
}

func TestConvertGitProxyConfig(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	httpProxy := "http://proxy.example.com:8080"
	httpsProxy := "https://proxy.example.com:8443"
	noProxy := "localhost,127.0.0.1"
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "proxy-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri":        "https://github.com/example/myapp.git",
						"httpProxy":  httpProxy,
						"httpsProxy": httpsProxy,
						"noProxy":    noProxy,
					},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	json.Unmarshal(jsonBytes, b)

	envByName := map[string]string{}
	for _, env := range b.Spec.Env {
		envByName[env.Name] = env.Value
	}

	if envByName["HTTP_PROXY"] != httpProxy {
		t.Errorf("HTTP_PROXY = %q, want %q", envByName["HTTP_PROXY"], httpProxy)
	}
	if envByName["HTTPS_PROXY"] != httpsProxy {
		t.Errorf("HTTPS_PROXY = %q, want %q", envByName["HTTPS_PROXY"], httpsProxy)
	}
	if envByName["NO_PROXY"] != noProxy {
		t.Errorf("NO_PROXY = %q, want %q", envByName["NO_PROXY"], noProxy)
	}
}

func TestConvertSourceSecretsWarnings(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "secrets-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
					},
					"secrets": []interface{}{
						map[string]interface{}{
							"secret":         map[string]interface{}{"name": "npm-token"},
							"destinationDir": "root",
						},
						map[string]interface{}{
							"secret": map[string]interface{}{"name": "another-secret"},
						},
					},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	_, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var secretWarnings []string
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "mounts secret") {
			secretWarnings = append(secretWarnings, entry.Message)
		}
	}

	if len(secretWarnings) != 2 {
		t.Fatalf("expected 2 per-secret warnings, got %d: %v", len(secretWarnings), secretWarnings)
	}

	wants := []struct {
		name string
		dest string
	}{
		{name: "npm-token", dest: "'root'"},
		{name: "another-secret", dest: "'.'"},
	}
	for i, want := range wants {
		msg := secretWarnings[i]
		if !strings.Contains(msg, "BuildConfig 'secrets-app' mounts secret '"+want.name+"' to "+want.dest) {
			t.Errorf("warning %d = %q, want secret %q with dest %s", i, msg, want.name, want.dest)
		}
		if !strings.Contains(msg, "(1) add an overridable volume named '"+want.name+"'") ||
			!strings.Contains(msg, "(2) add a BuildVolume override") ||
			!strings.Contains(msg, "(3) update your Dockerfile to use 'RUN cp'") {
			t.Errorf("warning %d missing 3-step migration guidance: %q", i, msg)
		}
	}

	// The old generic warning must be gone
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "Secrets are not yet supported") {
			t.Errorf("old generic secrets warning still emitted: %q", entry.Message)
		}
	}
}

func TestConvertSourceConfigMapsWarnings(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "configmaps-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
					},
					"configMaps": []interface{}{
						map[string]interface{}{
							"configMap":      map[string]interface{}{"name": "build-settings"},
							"destinationDir": "etc/maven",
						},
						map[string]interface{}{
							"configMap": map[string]interface{}{"name": "extra-config"},
						},
					},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}

	_, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var cmWarnings []string
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "mounts ConfigMap") {
			cmWarnings = append(cmWarnings, entry.Message)
		}
	}

	if len(cmWarnings) != 2 {
		t.Fatalf("expected 2 per-ConfigMap warnings, got %d: %v", len(cmWarnings), cmWarnings)
	}

	wants := []struct {
		name string
		dest string
	}{
		{name: "build-settings", dest: "'etc/maven'"},
		{name: "extra-config", dest: "'.'"},
	}
	for i, want := range wants {
		msg := cmWarnings[i]
		if !strings.Contains(msg, "BuildConfig 'configmaps-app' mounts ConfigMap '"+want.name+"' to "+want.dest) {
			t.Errorf("warning %d = %q, want ConfigMap %q with dest %s", i, msg, want.name, want.dest)
		}
		if !strings.Contains(msg, "(1) add an overridable volume named '"+want.name+"'") ||
			!strings.Contains(msg, "(2) add a BuildVolume override") ||
			!strings.Contains(msg, "(3) update your Dockerfile to use 'RUN cp'") {
			t.Errorf("warning %d missing 3-step migration guidance: %q", i, msg)
		}
	}

	// The old generic warning must be gone
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "ConfigMaps are not yet supported") {
			t.Errorf("old generic ConfigMaps warning still emitted: %q", entry.Message)
		}
	}
}

func TestProcessCompletionDeadline(t *testing.T) {
	deadline := int64(1800)
	maxDeadline := int64(maxTimeoutSeconds)
	overflowDeadline := int64(maxTimeoutSeconds) + 1
	zeroDeadline := int64(0)
	negativeDeadline := int64(-30)

	tests := []struct {
		name            string
		buildConfig     *buildv1.BuildConfig
		expectedTimeout *metav1.Duration
	}{
		{
			name: "completionDeadlineSeconds set maps to Build timeout",
			buildConfig: &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bc",
					Namespace: "default",
				},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{
						CompletionDeadlineSeconds: &deadline,
					},
				},
			},
			expectedTimeout: &metav1.Duration{Duration: 1800 * time.Second},
		},
		{
			name: "completionDeadlineSeconds unset leaves timeout nil",
			buildConfig: &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bc",
					Namespace: "default",
				},
				Spec: buildv1.BuildConfigSpec{},
			},
			expectedTimeout: nil,
		},
		{
			name: "completionDeadlineSeconds at maximum representable value maps to Build timeout",
			buildConfig: &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bc",
					Namespace: "default",
				},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{
						CompletionDeadlineSeconds: &maxDeadline,
					},
				},
			},
			expectedTimeout: &metav1.Duration{Duration: time.Duration(maxTimeoutSeconds) * time.Second},
		},
		{
			name: "completionDeadlineSeconds above maximum is skipped to avoid overflow",
			buildConfig: &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bc",
					Namespace: "default",
				},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{
						CompletionDeadlineSeconds: &overflowDeadline,
					},
				},
			},
			expectedTimeout: nil,
		},
		{
			name: "completionDeadlineSeconds of zero is skipped",
			buildConfig: &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bc",
					Namespace: "default",
				},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{
						CompletionDeadlineSeconds: &zeroDeadline,
					},
				},
			},
			expectedTimeout: nil,
		},
		{
			name: "negative completionDeadlineSeconds is skipped",
			buildConfig: &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-bc",
					Namespace: "default",
				},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{
						CompletionDeadlineSeconds: &negativeDeadline,
					},
				},
			},
			expectedTimeout: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := logrustest.NewNullLogger()
			c := &Converter{Log: logger}
			b := &shipwrightv1beta1.Build{}

			c.processCompletionDeadline(tt.buildConfig, b)

			if tt.expectedTimeout == nil {
				if b.Spec.Timeout != nil {
					t.Errorf("Timeout = %v, want nil", b.Spec.Timeout)
				}
				return
			}
			if b.Spec.Timeout == nil {
				t.Fatalf("Timeout = nil, want %v", tt.expectedTimeout.Duration)
			}
			if b.Spec.Timeout.Duration != tt.expectedTimeout.Duration {
				t.Errorf("Timeout = %v, want %v", b.Spec.Timeout.Duration, tt.expectedTimeout.Duration)
			}
		})
	}
}

func TestConvertNoOutputImage(t *testing.T) {
	tests := []struct {
		name   string
		output map[string]interface{}
	}{
		{"output missing entirely", nil},
		{"empty output", map[string]interface{}{}},
		{"output.to with empty name", map[string]interface{}{
			"to": map[string]interface{}{"kind": "DockerImage", "name": ""},
		}},
		{"pushSecret but no output.to", map[string]interface{}{
			"pushSecret": map[string]interface{}{"name": "push-creds"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			plugin := &BuildConfigTransformPlugin{Log: logger}

			spec := map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git":  map[string]interface{}{"uri": "https://example.com/repo.git"},
				},
				"strategy": map[string]interface{}{
					"type":           "Docker",
					"dockerStrategy": map[string]interface{}{},
				},
			}
			if tt.output != nil {
				spec["output"] = tt.output
			}

			request := transform.PluginRequest{
				Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "build.openshift.io/v1",
					"kind":       "BuildConfig",
					"metadata": map[string]interface{}{
						"name":      "no-output-app",
						"namespace": "myns",
					},
					"spec": spec,
				}},
			}

			resp, err := plugin.Run(request)
			if err != nil {
				t.Fatalf("expected no error for BuildConfig without output image, got: %v", err)
			}
			if resp.IsWhiteOut {
				t.Error("expected IsWhiteOut to be false — BuildConfig should pass through unchanged")
			}
			if len(resp.NewResources) > 0 {
				t.Errorf("expected no new resources, got %d", len(resp.NewResources))
			}

			found := false
			for _, entry := range hook.AllEntries() {
				if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "no output image") &&
					strings.Contains(entry.Message, "no-output-app") {
					found = true
				}
			}
			if !found {
				t.Error("expected a warning explaining the BuildConfig has no output image")
			}
		})
	}
}

// unmarshalBuildRunTemplate decodes the BuildRun template annotation
// (BUILD-2261) into the real Shipwright type so the assertions round-trip
// through the same API the target cluster will use.
func unmarshalBuildRunTemplate(t *testing.T, value string) shipwrightv1beta1.BuildRun {
	t.Helper()
	tmpl := shipwrightv1beta1.BuildRun{}
	if err := yaml.Unmarshal([]byte(value), &tmpl); err != nil {
		t.Fatalf("annotation value is not a valid BuildRun: %v\n%s", err, value)
	}
	return tmpl
}

func runBuildRunTemplateConversion(t *testing.T, spec map[string]interface{}) map[string]string {
	t.Helper()
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "myapp",
				"namespace": "myns",
			},
			"spec": spec,
		}},
	}
	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.NewResources) < 1 {
		t.Fatal("expected at least 1 new resource")
	}
	return resp.NewResources[0].GetAnnotations()
}

func TestConvertResourcesDockerStrategy(t *testing.T) {
	annotations := runBuildRunTemplateConversion(t, map[string]interface{}{
		"source": map[string]interface{}{
			"type": "Git",
			"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
		},
		"strategy": map[string]interface{}{
			"type":           "Docker",
			"dockerStrategy": map[string]interface{}{},
		},
		"output": map[string]interface{}{
			"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/myapp:latest"},
		},
		"resources": map[string]interface{}{
			"requests": map[string]interface{}{"cpu": "500m", "memory": "1Gi"},
			"limits":   map[string]interface{}{"cpu": "2", "memory": "4Gi"},
		},
	})

	value, ok := annotations[BuildRunTemplateAnnotation]
	if !ok {
		t.Fatalf("expected annotation %s, got: %v", BuildRunTemplateAnnotation, annotations)
	}

	tmpl := unmarshalBuildRunTemplate(t, value)

	if tmpl.APIVersion != "shipwright.io/v1beta1" {
		t.Errorf("expected apiVersion shipwright.io/v1beta1, got %s", tmpl.APIVersion)
	}
	if tmpl.Kind != "BuildRun" {
		t.Errorf("expected kind BuildRun, got %s", tmpl.Kind)
	}
	if tmpl.Name != "myapp-buildrun" {
		t.Errorf("expected metadata.name myapp-buildrun, got %s", tmpl.Name)
	}
	if tmpl.Namespace != "myns" {
		t.Errorf("expected metadata.namespace myns, got %s", tmpl.Namespace)
	}
	if tmpl.Spec.Build.Name == nil || *tmpl.Spec.Build.Name != "myapp" {
		t.Errorf("expected spec.build.name myapp, got %v", tmpl.Spec.Build.Name)
	}
	if tmpl.Spec.ServiceAccount != nil {
		t.Errorf("expected no serviceAccount, got %s", *tmpl.Spec.ServiceAccount)
	}
	if len(tmpl.Spec.StepResources) != 1 {
		t.Fatalf("expected 1 stepResources entry, got %d", len(tmpl.Spec.StepResources))
	}
	step := tmpl.Spec.StepResources[0]
	if step.Name != "build-and-push" {
		t.Errorf("expected step name build-and-push, got %s", step.Name)
	}
	if step.Resources.Requests.Cpu().String() != "500m" || step.Resources.Requests.Memory().String() != "1Gi" {
		t.Errorf("unexpected requests: %v", step.Resources.Requests)
	}
	if step.Resources.Limits.Cpu().String() != "2" || step.Resources.Limits.Memory().String() != "4Gi" {
		t.Errorf("unexpected limits: %v", step.Resources.Limits)
	}
}

func TestConvertResourcesSourceStrategyWithServiceAccount(t *testing.T) {
	annotations := runBuildRunTemplateConversion(t, map[string]interface{}{
		"source": map[string]interface{}{
			"type": "Git",
			"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
		},
		"strategy": map[string]interface{}{
			"type": "Source",
			"sourceStrategy": map[string]interface{}{
				"from": map[string]interface{}{
					"kind": "DockerImage",
					"name": "registry.example.com/builder:latest",
				},
				"pullSecret": map[string]interface{}{"name": "my-pull-secret"},
			},
		},
		"output": map[string]interface{}{
			"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/myapp:latest"},
		},
		"resources": map[string]interface{}{
			"limits": map[string]interface{}{"memory": "2Gi"},
		},
	})

	value, ok := annotations[BuildRunTemplateAnnotation]
	if !ok {
		t.Fatalf("expected annotation %s, got: %v", BuildRunTemplateAnnotation, annotations)
	}

	tmpl := unmarshalBuildRunTemplate(t, value)

	// Generated ServiceAccount (pull-secret flow) must be referenced.
	if tmpl.Spec.ServiceAccount == nil || *tmpl.Spec.ServiceAccount != "myapp" {
		t.Errorf("expected serviceAccount myapp, got %v", tmpl.Spec.ServiceAccount)
	}

	if len(tmpl.Spec.StepResources) != 2 {
		t.Fatalf("expected 2 stepResources entries, got %d", len(tmpl.Spec.StepResources))
	}
	wantSteps := []string{"s2i-generate", "buildah"}
	for i, want := range wantSteps {
		step := tmpl.Spec.StepResources[i]
		if step.Name != want {
			t.Errorf("expected step %d name %s, got %s", i, want, step.Name)
		}
		if step.Resources.Limits.Memory().String() != "2Gi" {
			t.Errorf("step %s: unexpected limits: %v", want, step.Resources.Limits)
		}
		if len(step.Resources.Requests) != 0 {
			t.Errorf("step %s: expected no requests, got %v", want, step.Resources.Requests)
		}
	}
}

func TestConvertResourcesExplicitServiceAccountPreserved(t *testing.T) {
	// Regression (BUILD-2261 CodeRabbit): a BuildConfig with an explicitly
	// configured spec.serviceAccount but NO pull secret must still carry
	// that ServiceAccount into the BuildRun template.
	annotations := runBuildRunTemplateConversion(t, map[string]interface{}{
		"serviceAccount": "custom-builder-sa",
		"source": map[string]interface{}{
			"type": "Git",
			"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
		},
		"strategy": map[string]interface{}{
			"type":           "Docker",
			"dockerStrategy": map[string]interface{}{},
		},
		"output": map[string]interface{}{
			"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/myapp:latest"},
		},
		"resources": map[string]interface{}{
			"limits": map[string]interface{}{"memory": "2Gi"},
		},
	})

	value, ok := annotations[BuildRunTemplateAnnotation]
	if !ok {
		t.Fatalf("expected annotation %s, got: %v", BuildRunTemplateAnnotation, annotations)
	}

	tmpl := unmarshalBuildRunTemplate(t, value)

	if tmpl.Spec.ServiceAccount == nil || *tmpl.Spec.ServiceAccount != "custom-builder-sa" {
		t.Errorf("expected serviceAccount custom-builder-sa, got %v", tmpl.Spec.ServiceAccount)
	}
}

func TestConvertResourcesRequestsOnly(t *testing.T) {
	annotations := runBuildRunTemplateConversion(t, map[string]interface{}{
		"source": map[string]interface{}{
			"type": "Git",
			"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
		},
		"strategy": map[string]interface{}{
			"type":           "Docker",
			"dockerStrategy": map[string]interface{}{},
		},
		"output": map[string]interface{}{
			"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/myapp:latest"},
		},
		"resources": map[string]interface{}{
			"requests": map[string]interface{}{"cpu": "250m"},
		},
	})

	value, ok := annotations[BuildRunTemplateAnnotation]
	if !ok {
		t.Fatalf("expected annotation %s for requests-only resources", BuildRunTemplateAnnotation)
	}
	tmpl := unmarshalBuildRunTemplate(t, value)
	if tmpl.Spec.StepResources[0].Resources.Requests.Cpu().String() != "250m" {
		t.Errorf("unexpected requests: %v", tmpl.Spec.StepResources[0].Resources.Requests)
	}
	if len(tmpl.Spec.StepResources[0].Resources.Limits) != 0 {
		t.Errorf("expected no limits, got %v", tmpl.Spec.StepResources[0].Resources.Limits)
	}
}

func TestConvertResourcesEmptyNoAnnotation(t *testing.T) {
	specs := map[string]map[string]interface{}{
		"no resources field": {
			"source": map[string]interface{}{
				"type": "Git",
				"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
			},
			"strategy": map[string]interface{}{
				"type":           "Docker",
				"dockerStrategy": map[string]interface{}{},
			},
			"output": map[string]interface{}{
				"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/myapp:latest"},
			},
		},
		"empty resources": {
			"source": map[string]interface{}{
				"type": "Git",
				"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
			},
			"strategy": map[string]interface{}{
				"type":           "Docker",
				"dockerStrategy": map[string]interface{}{},
			},
			"output": map[string]interface{}{
				"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/myapp:latest"},
			},
			"resources": map[string]interface{}{},
		},
	}

	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			annotations := runBuildRunTemplateConversion(t, spec)
			if _, ok := annotations[BuildRunTemplateAnnotation]; ok {
				t.Errorf("expected no %s annotation, got: %v", BuildRunTemplateAnnotation, annotations)
			}
		})
	}
}

func parseBuildConfigJSON(t *testing.T, raw string) *buildv1.BuildConfig {
	t.Helper()
	bc := &buildv1.BuildConfig{}
	if err := json.Unmarshal([]byte(raw), bc); err != nil {
		t.Fatalf("failed to parse BuildConfig JSON: %v", err)
	}
	return bc
}

func TestConvertResourcesLogsWarning(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	converter := &Converter{Log: logger}

	bcJSON := `{
		"apiVersion": "build.openshift.io/v1",
		"kind": "BuildConfig",
		"metadata": {"name": "myapp", "namespace": "myns"},
		"spec": {
			"source": {"type": "Git", "git": {"uri": "https://github.com/example/myapp.git"}},
			"strategy": {"type": "Docker", "dockerStrategy": {}},
			"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/myapp:latest"}},
			"resources": {"limits": {"memory": "4Gi"}}
		}
	}`
	bc := parseBuildConfigJSON(t, bcJSON)

	if _, err := converter.Convert(bc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundWarn := false
	foundInfo := false
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "Resource requirements are not supported on Shipwright Build") {
			foundWarn = true
		}
		if entry.Level == logrus.InfoLevel && strings.Contains(entry.Message, "Generated BuildRun template with resource requirements") {
			foundInfo = true
		}
	}
	if !foundWarn {
		t.Error("expected WARN log about unsupported resource requirements")
	}
	if !foundInfo {
		t.Error("expected INFO log about generated BuildRun template")
	}
}

// TestConvertResourcesCustomStrategyOmitsStepResources covers the CodeRabbit
// finding on BUILD-2261: when the strategy is remapped to a custom
// ClusterBuildStrategy its step names are unknown, so the BuildRun template
// must still be emitted but without stepResources (default step names would
// be rejected at admission), and the user must be warned to fill them in.
func TestConvertResourcesCustomStrategyOmitsStepResources(t *testing.T) {
	tests := []struct {
		name         string
		mapping      map[string]string
		strategyJSON string
		wantStrategy string
	}{
		{
			name:         "Docker remapped",
			mapping:      map[string]string{"docker": "my-custom-buildah"},
			strategyJSON: `{"type": "Docker", "dockerStrategy": {}}`,
			wantStrategy: "my-custom-buildah",
		},
		{
			name:         "Source remapped",
			mapping:      map[string]string{"s2i": "my-custom-s2i"},
			strategyJSON: `{"type": "Source", "sourceStrategy": {"from": {"kind": "DockerImage", "name": "python:3.9"}}}`,
			wantStrategy: "my-custom-s2i",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			converter := &Converter{
				Log:  logger,
				Opts: PluginOptionalFields{StrategyMapping: tt.mapping},
			}

			bcJSON := `{
				"apiVersion": "build.openshift.io/v1",
				"kind": "BuildConfig",
				"metadata": {"name": "myapp", "namespace": "myns"},
				"spec": {
					"source": {"type": "Git", "git": {"uri": "https://github.com/example/myapp.git"}},
					"strategy": ` + tt.strategyJSON + `,
					"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/myapp:latest"}},
					"resources": {"requests": {"cpu": "250m"}, "limits": {"memory": "4Gi"}}
				}
			}`
			bc := parseBuildConfigJSON(t, bcJSON)

			result, err := converter.Convert(bc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			b := &shipwrightv1beta1.Build{}
			jsonBytes, _ := json.Marshal(result[0].Object)
			json.Unmarshal(jsonBytes, b)
			if b.Spec.Strategy.Name != tt.wantStrategy {
				t.Errorf("expected strategy %s, got %s", tt.wantStrategy, b.Spec.Strategy.Name)
			}

			value, ok := result[0].GetAnnotations()[BuildRunTemplateAnnotation]
			if !ok {
				t.Fatalf("expected annotation %s on converted Build", BuildRunTemplateAnnotation)
			}
			tmpl := unmarshalBuildRunTemplate(t, value)
			if len(tmpl.Spec.StepResources) != 0 {
				t.Errorf("expected stepResources omitted for custom strategy, got %v", tmpl.Spec.StepResources)
			}
			if tmpl.Spec.Build.Name == nil || *tmpl.Spec.Build.Name != b.Name {
				t.Errorf("expected template to reference build %q, got %v", b.Name, tmpl.Spec.Build.Name)
			}

			foundOmitWarn := false
			for _, entry := range hook.AllEntries() {
				if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "custom mapping with unknown step names") {
					foundOmitWarn = true
				}
				if entry.Level == logrus.InfoLevel && strings.Contains(entry.Message, "Generated BuildRun template with resource requirements") {
					t.Error("did not expect INFO log about generated stepResources for custom strategy")
				}
			}
			if !foundOmitWarn {
				t.Error("expected WARN log about omitted stepResources for custom strategy mapping")
			}
		})
	}
}
