package buildconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestProcessInlineDockerfile covers BUILD-2275 and BUILD-2340. spec.source.dockerfile
// holds raw Dockerfile contents, which Shipwright has no field for. Under the Docker
// strategy the content is now preserved in a ConfigMap (BUILD-2340) with a loud ERROR;
// under Source it is inapplicable and warns; empty and nil are silent.
func TestProcessInlineDockerfile(t *testing.T) {
	inline := "FROM golang:1.21\nCOPY . /app"
	empty := ""

	tests := []struct {
		name            string
		strategyType    buildv1.BuildStrategyType
		dockerfile      *string
		wantConfigMap   bool
		wantSourceWarn  bool
		wantDockerError bool
	}{
		{
			name:            "Docker strategy with an inline Dockerfile preserves it in a ConfigMap",
			strategyType:    buildv1.DockerBuildStrategyType,
			dockerfile:      &inline,
			wantConfigMap:   true,
			wantDockerError: true,
		},
		{
			name:         "Docker strategy without an inline Dockerfile emits no ConfigMap",
			strategyType: buildv1.DockerBuildStrategyType,
		},
		{
			name:         "Docker strategy with an explicitly empty Dockerfile emits no ConfigMap",
			strategyType: buildv1.DockerBuildStrategyType,
			dockerfile:   &empty,
		},
		{
			name:           "Source strategy with an inline Dockerfile warns it was not migrated",
			strategyType:   buildv1.SourceBuildStrategyType,
			dockerfile:     &inline,
			wantSourceWarn: true,
		},
		{
			name:         "Source strategy without an inline Dockerfile stays silent",
			strategyType: buildv1.SourceBuildStrategyType,
		},
		{
			// omitempty on a *string suppresses only a nil pointer, so `dockerfile: ""`
			// arrives as a non-nil pointer to "". There is no content to lose.
			name:         "Source strategy with an explicitly empty Dockerfile stays silent",
			strategyType: buildv1.SourceBuildStrategyType,
			dockerfile:   &empty,
		},
		{
			// Convert() returns before processInlineDockerfile for Custom and
			// JenkinsPipeline, so those pass through unchanged. Locking the switch's
			// silence in here means relaxing that early return cannot quietly
			// reintroduce a silent drop.
			name:         "Custom strategy falls through the switch without a diagnostic",
			strategyType: buildv1.CustomBuildStrategyType,
			dockerfile:   &inline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			c := &Converter{Log: logger}
			bc := &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "dockerfile-app", Namespace: "myns"},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{
						Source: buildv1.BuildSource{
							Dockerfile: tt.dockerfile,
							Git:        &buildv1.GitBuildSource{URI: "https://example.com/repo.git"},
						},
						Strategy: buildv1.BuildStrategy{Type: tt.strategyType},
					},
				},
			}
			b := &shipwrightv1beta1.Build{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

			cm := c.processInlineDockerfile(bc, b)

			if (cm != nil) != tt.wantConfigMap {
				t.Fatalf("ConfigMap returned = %v, want %v", cm != nil, tt.wantConfigMap)
			}

			var sourceWarn, dockerErr *logrus.Entry
			var messages []string
			for _, entry := range hook.AllEntries() {
				messages = append(messages, entry.Message)
				if !strings.Contains(entry.Message, "nline Dockerfile") {
					continue
				}
				switch entry.Level {
				case logrus.WarnLevel:
					sourceWarn = entry
				case logrus.ErrorLevel:
					dockerErr = entry
				}
			}

			if (dockerErr != nil) != tt.wantDockerError {
				t.Errorf("Docker-strategy error emitted = %v, want %v (logged: %q)", dockerErr != nil, tt.wantDockerError, messages)
			}
			if (sourceWarn != nil) != tt.wantSourceWarn {
				t.Fatalf("Source-strategy warning emitted = %v, want %v (logged: %q)", sourceWarn != nil, tt.wantSourceWarn, messages)
			}

			if tt.wantConfigMap {
				if cm.APIVersion != "v1" || cm.Kind != "ConfigMap" {
					t.Errorf("ConfigMap TypeMeta = %s/%s, want v1/ConfigMap", cm.APIVersion, cm.Kind)
				}
				if cm.Name != "dockerfile-app-dockerfile" {
					t.Errorf("ConfigMap name = %q, want %q", cm.Name, "dockerfile-app-dockerfile")
				}
				if cm.Namespace != "myns" {
					t.Errorf("ConfigMap namespace = %q, want %q", cm.Namespace, "myns")
				}
				if got := cm.Data[inlineDockerfileKey]; got != inline {
					t.Errorf("ConfigMap data[%q] = %q, want %q", inlineDockerfileKey, got, inline)
				}
				if got := cm.Annotations[ConvertedFromAnnotation]; got != "build.openshift.io/v1/BuildConfig/dockerfile-app" {
					t.Errorf("ConfigMap %s = %q, want the BuildConfig pointer", ConvertedFromAnnotation, got)
				}
				if got := b.Annotations[InlineDockerfileConfigMapAnnotation]; got != cm.Name {
					t.Errorf("Build %s = %q, want %q", InlineDockerfileConfigMapAnnotation, got, cm.Name)
				}
				// The ERROR must attribute the BuildConfig, name the ConfigMap, and
				// point at the RFE so the user knows why the Build will not run yet.
				if !strings.Contains(dockerErr.Message, "myns/dockerfile-app") {
					t.Errorf("Docker error = %q, want it to name the BuildConfig as myns/dockerfile-app", dockerErr.Message)
				}
				if !strings.Contains(dockerErr.Message, cm.Name) {
					t.Errorf("Docker error = %q, want it to name the ConfigMap %q", dockerErr.Message, cm.Name)
				}
				if !strings.Contains(dockerErr.Message, "BUILD-1495") {
					t.Errorf("Docker error = %q, want it to reference BUILD-1495", dockerErr.Message)
				}
			}

			if tt.wantSourceWarn {
				if !strings.Contains(sourceWarn.Message, "myns/dockerfile-app") {
					t.Errorf("message = %q, want it to name the BuildConfig as myns/dockerfile-app", sourceWarn.Message)
				}
				if !strings.Contains(sourceWarn.Message, "Source-to-Image") {
					t.Errorf("message = %q, want it to name the strategy that ignores the Dockerfile", sourceWarn.Message)
				}
				if !strings.Contains(sourceWarn.Message, "reconfigure the BuildConfig strategy type") {
					t.Errorf("message = %q, want it to tell the user how to fix the likely misconfiguration", sourceWarn.Message)
				}
			}
		})
	}
}

// dockerfileRequest builds a plugin request for a BuildConfig with the given name,
// strategy, inline Dockerfile, and output image.
func dockerfileRequest(name, strategyType, dockerfile, outputName string) transform.PluginRequest {
	strategy := map[string]interface{}{"type": strategyType}
	switch strategyType {
	case "Docker":
		strategy["dockerStrategy"] = map[string]interface{}{}
	case "Source":
		strategy["sourceStrategy"] = map[string]interface{}{
			"from": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/builder:latest"},
		}
	}
	spec := map[string]interface{}{
		"source": map[string]interface{}{
			"type":       "Git",
			"git":        map[string]interface{}{"uri": "https://example.com/repo.git"},
			"dockerfile": dockerfile,
		},
		"strategy": strategy,
	}
	if outputName != "" {
		spec["output"] = map[string]interface{}{
			"to": map[string]interface{}{"kind": "DockerImage", "name": outputName},
		}
	}
	return transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata":   map[string]interface{}{"name": name, "namespace": "myns"},
			"spec":       spec,
		}},
	}
}

// TestConvertInlineDockerfileWiring proves the ConfigMap reaches NewResources during a real
// conversion, that the Build points at it, and that a BuildConfig skipped for a missing
// output image emits no orphan ConfigMap.
func TestConvertInlineDockerfileWiring(t *testing.T) {
	inline := "FROM golang:1.21\nCOPY . /app"

	t.Run("Docker strategy emits [Build, ConfigMap] and wires the pointer", func(t *testing.T) {
		plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
		resp, err := plugin.Run(dockerfileRequest("dockerfile-app", "Docker", inline, "quay.io/example/app:latest"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.NewResources) != 2 {
			t.Fatalf("NewResources = %d resources, want 2 ([Build, ConfigMap])", len(resp.NewResources))
		}

		build := resp.NewResources[0]
		cmU := resp.NewResources[1]
		if build.GetKind() != "Build" {
			t.Errorf("first resource kind = %q, want Build", build.GetKind())
		}
		if cmU.GetKind() != "ConfigMap" || cmU.GetAPIVersion() != "v1" {
			t.Errorf("second resource = %s/%s, want v1/ConfigMap", cmU.GetAPIVersion(), cmU.GetKind())
		}

		cm := &corev1.ConfigMap{}
		jsonBytes, _ := json.Marshal(cmU.Object)
		json.Unmarshal(jsonBytes, cm)
		if cm.Name != "dockerfile-app-dockerfile" {
			t.Errorf("ConfigMap name = %q, want dockerfile-app-dockerfile", cm.Name)
		}
		if cm.Data[inlineDockerfileKey] != inline {
			t.Errorf("ConfigMap data[%q] = %q, want the inline content", inlineDockerfileKey, cm.Data[inlineDockerfileKey])
		}

		anns := build.GetAnnotations()
		if anns[InlineDockerfileConfigMapAnnotation] != cm.Name {
			t.Errorf("Build %s = %q, want %q", InlineDockerfileConfigMapAnnotation, anns[InlineDockerfileConfigMapAnnotation], cm.Name)
		}
		if anns[ConversionOutcomeAnnotation] != string(OutcomeConvertedWithWarnings) {
			t.Errorf("Build %s = %q, want %q", ConversionOutcomeAnnotation, anns[ConversionOutcomeAnnotation], OutcomeConvertedWithWarnings)
		}
	})

	t.Run("Source strategy emits no ConfigMap", func(t *testing.T) {
		plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
		resp, err := plugin.Run(dockerfileRequest("dockerfile-app", "Source", inline, "quay.io/example/app:latest"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, r := range resp.NewResources {
			if r.GetKind() == "ConfigMap" {
				t.Fatalf("Source strategy emitted a ConfigMap, want none")
			}
		}
	})

	t.Run("skipped BuildConfig emits no orphan ConfigMap", func(t *testing.T) {
		plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
		// No output image → the BuildConfig is skipped and passed through unchanged.
		resp, err := plugin.Run(dockerfileRequest("dockerfile-app", "Docker", inline, ""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.NewResources) != 0 {
			t.Fatalf("skipped BuildConfig produced %d resources, want 0", len(resp.NewResources))
		}
		if resp.IsWhiteOut {
			t.Error("skipped BuildConfig was whited out, want passthrough")
		}
	})
}

// TestConvertInlineDockerfileConfigMapNaming proves two BuildConfigs whose names sanitize to
// the same ConfigMap base get distinct names through uniqueName, and that the same input
// converts to an identical ConfigMap on two independent runs (BUILD-2339 idempotency).
func TestConvertInlineDockerfileConfigMapNaming(t *testing.T) {
	inline := "FROM golang:1.21\nCOPY . /app"

	newBC := func(name string) *buildv1.BuildConfig {
		return &buildv1.BuildConfig{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "myns"},
			Spec: buildv1.BuildConfigSpec{
				CommonSpec: buildv1.CommonSpec{
					Source:   buildv1.BuildSource{Dockerfile: &inline, Git: &buildv1.GitBuildSource{URI: "https://example.com/repo.git"}},
					Strategy: buildv1.BuildStrategy{Type: buildv1.DockerBuildStrategyType, DockerStrategy: &buildv1.DockerBuildStrategy{}},
					Output:   buildv1.BuildOutput{To: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/app:latest"}},
				},
			},
		}
	}

	// my.app and my_app both sanitize to my-app; a single Converter must keep their
	// ConfigMaps distinct.
	c := &Converter{Log: logrus.New()}
	res1, out1 := c.Convert(newBC("my.app"))
	res2, out2 := c.Convert(newBC("my_app"))
	if out1.State == OutcomeFailed || out2.State == OutcomeFailed {
		t.Fatalf("conversion failed: %q / %q", out1.Reason, out2.Reason)
	}

	name1 := configMapName(t, res1)
	name2 := configMapName(t, res2)
	if name1 == "" || name2 == "" {
		t.Fatalf("expected a ConfigMap in each conversion, got %q and %q", name1, name2)
	}
	if name1 == name2 {
		t.Errorf("ConfigMap names collide: both %q", name1)
	}

	// Idempotency: the same BuildConfig through a fresh Converter yields the same ConfigMap.
	c2 := &Converter{Log: logrus.New()}
	res3, _ := c2.Convert(newBC("my.app"))
	if got := configMapName(t, res3); got != name1 {
		t.Errorf("ConfigMap name not idempotent: %q then %q", name1, got)
	}
}

// configMapName returns the name of the first ConfigMap in a conversion result, or "".
func configMapName(t *testing.T, res []unstructured.Unstructured) string {
	t.Helper()
	for _, r := range res {
		if r.GetKind() == "ConfigMap" {
			return r.GetName()
		}
	}
	return ""
}

// TestConvertSourceSecretNonGit proves sourceSecret is warned about and dropped on binary,
// image, and source-less BuildConfigs, and stays silent — and converts to cloneSecret — on
// a git source.
func TestConvertSourceSecretNonGit(t *testing.T) {
	const marker = "sourceSecret only authenticates git clones"

	tests := []struct {
		name     string
		source   map[string]interface{}
		wantWarn bool
	}{
		{
			name: "binary source with sourceSecret warns",
			source: map[string]interface{}{
				"type":         "Binary",
				"binary":       map[string]interface{}{"asFile": "app.jar"},
				"sourceSecret": map[string]interface{}{"name": "s"},
			},
			wantWarn: true,
		},
		{
			name: "image source with sourceSecret warns",
			source: map[string]interface{}{
				"type": "Image",
				"images": []interface{}{
					map[string]interface{}{"from": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/o/img:1"}},
				},
				"sourceSecret": map[string]interface{}{"name": "s"},
			},
			wantWarn: true,
		},
		{
			name: "no source with sourceSecret warns",
			source: map[string]interface{}{
				"type":         "None",
				"sourceSecret": map[string]interface{}{"name": "s"},
			},
			wantWarn: true,
		},
		{
			name: "git source with sourceSecret does not warn",
			source: map[string]interface{}{
				"type":         "Git",
				"git":          map[string]interface{}{"uri": "https://example.com/repo.git"},
				"sourceSecret": map[string]interface{}{"name": "s"},
			},
			wantWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			plugin := &BuildConfigTransformPlugin{Log: logger}
			resp, err := plugin.Run(transform.PluginRequest{
				Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "build.openshift.io/v1",
					"kind":       "BuildConfig",
					"metadata":   map[string]interface{}{"name": "myapp", "namespace": "myns"},
					"spec": map[string]interface{}{
						"source": tt.source,
						"strategy": map[string]interface{}{
							"type":           "Docker",
							"dockerStrategy": map[string]interface{}{},
						},
						"output": map[string]interface{}{
							"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/app:latest"},
						},
					},
				}},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var warns []string
			for _, entry := range hook.AllEntries() {
				if strings.Contains(entry.Message, marker) {
					warns = append(warns, entry.Message)
				}
			}

			if tt.wantWarn {
				if len(warns) != 1 {
					t.Fatalf("expected exactly 1 sourceSecret warning, got %d: %v", len(warns), warns)
				}
				if !strings.Contains(warns[0], `sourceSecret "s"`) || !strings.Contains(warns[0], "myns/myapp") {
					t.Errorf("warning = %q, want it to name sourceSecret \"s\" and myns/myapp", warns[0])
				}
				return
			}

			if len(warns) != 0 {
				t.Fatalf("git source produced a sourceSecret warning, want none: %v", warns)
			}
			// The git source must still carry the cloneSecret.
			build := &shipwrightv1beta1.Build{}
			jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
			json.Unmarshal(jsonBytes, build)
			if build.Spec.Source == nil || build.Spec.Source.Git == nil || build.Spec.Source.Git.CloneSecret == nil || *build.Spec.Source.Git.CloneSecret != "s" {
				t.Errorf("git cloneSecret = %v, want \"s\"", build.Spec.Source)
			}
		})
	}
}
