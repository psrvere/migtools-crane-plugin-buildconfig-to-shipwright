package buildconfig

import (
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newTriggerTestConverter() (*Converter, *logrustest.Hook) {
	logger, hook := logrustest.NewNullLogger()
	return &Converter{Log: logger}, hook
}

func triggerTestBC(triggers ...buildv1.BuildTriggerPolicy) *buildv1.BuildConfig {
	bc := &buildv1.BuildConfig{}
	bc.Name = "my-app"
	bc.Namespace = "myns"
	bc.Spec.Triggers = triggers
	bc.Spec.Strategy = buildv1.BuildStrategy{
		Type: buildv1.DockerBuildStrategyType,
		DockerStrategy: &buildv1.DockerBuildStrategy{
			From: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/base:latest"},
		},
	}
	return bc
}

func warnMessages(hook *logrustest.Hook) []string {
	var msgs []string
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			msgs = append(msgs, entry.Message)
		}
	}
	return msgs
}

func assertContainsAll(t *testing.T, msg string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

func TestProcessTriggersWebhookTypes(t *testing.T) {
	cases := []struct {
		name          string
		triggerType   buildv1.BuildTriggerType
		canonicalName string
	}{
		{"github", buildv1.GitHubWebHookBuildTriggerType, "GitHub"},
		{"github deprecated", buildv1.GitHubWebHookBuildTriggerTypeDeprecated, "GitHub"},
		{"gitlab", buildv1.GitLabWebHookBuildTriggerType, "GitLab"},
		{"bitbucket", buildv1.BitbucketWebHookBuildTriggerType, "Bitbucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, hook := newTriggerTestConverter()
			bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: tc.triggerType})
			c.processTriggers(bc, &shipwrightv1beta1.Build{})

			warnings := warnMessages(hook)
			if len(warnings) != 2 {
				t.Fatalf("expected 2 warnings (per-trigger + summary), got %d: %v", len(warnings), warnings)
			}
			assertContainsAll(t, warnings[0],
				tc.canonicalName+" webhook trigger is dropped",
				"my-app",
				"no replacement URL",
				"Pipelines-as-Code",
				"Tekton Triggers",
			)
		})
	}
}

func TestProcessTriggersGenericAllowEnv(t *testing.T) {
	cases := []struct {
		name         string
		triggerType  buildv1.BuildTriggerType
		webhook      *buildv1.WebHookTrigger
		wantAllowEnv bool
	}{
		{"allowEnv true", buildv1.GenericWebHookBuildTriggerType, &buildv1.WebHookTrigger{AllowEnv: true}, true},
		{"allowEnv false", buildv1.GenericWebHookBuildTriggerType, &buildv1.WebHookTrigger{}, false},
		{"nil webhook", buildv1.GenericWebHookBuildTriggerType, nil, false},
		{"deprecated generic allowEnv true", buildv1.GenericWebHookBuildTriggerTypeDeprecated, &buildv1.WebHookTrigger{AllowEnv: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, hook := newTriggerTestConverter()
			bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: tc.triggerType, GenericWebHook: tc.webhook})
			c.processTriggers(bc, &shipwrightv1beta1.Build{})

			warnings := warnMessages(hook)
			if len(warnings) != 2 {
				t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
			}
			assertContainsAll(t, warnings[0], "Generic webhook trigger is dropped", "no replacement URL")
			hasAllowEnv := strings.Contains(warnings[0], "allowEnv")
			if hasAllowEnv != tc.wantAllowEnv {
				t.Errorf("allowEnv note present=%v, want %v; message: %q", hasAllowEnv, tc.wantAllowEnv, warnings[0])
			}
		})
	}
}

func TestProcessTriggersImageChange(t *testing.T) {
	cases := []struct {
		name        string
		triggerType buildv1.BuildTriggerType
		imageChange *buildv1.ImageChangeTrigger
		wantWatched string
	}{
		{
			"from set with namespace",
			buildv1.ImageChangeBuildTriggerType,
			&buildv1.ImageChangeTrigger{From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "builder:latest", Namespace: "other-ns"}},
			"ImageStreamTag other-ns/builder:latest",
		},
		{
			"from set without namespace defaults to bc namespace",
			buildv1.ImageChangeBuildTriggerType,
			&buildv1.ImageChangeTrigger{From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "builder:latest"}},
			"ImageStreamTag myns/builder:latest",
		},
		{
			"empty from falls back to strategy image",
			buildv1.ImageChangeBuildTriggerType,
			&buildv1.ImageChangeTrigger{},
			"the strategy image quay.io/example/base:latest",
		},
		{
			"nil imageChange falls back to strategy image",
			buildv1.ImageChangeBuildTriggerType,
			nil,
			"the strategy image quay.io/example/base:latest",
		},
		{
			"deprecated imageChange variant",
			buildv1.ImageChangeBuildTriggerTypeDeprecated,
			&buildv1.ImageChangeTrigger{From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "builder:latest", Namespace: "other-ns"}},
			"ImageStreamTag other-ns/builder:latest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, hook := newTriggerTestConverter()
			bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: tc.triggerType, ImageChange: tc.imageChange})
			c.processTriggers(bc, &shipwrightv1beta1.Build{})

			warnings := warnMessages(hook)
			if len(warnings) != 2 {
				t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
			}
			assertContainsAll(t, warnings[0],
				"ImageChange trigger is dropped",
				"builds will no longer start when "+tc.wantWatched+" changes",
				"no equivalent",
			)
		})
	}
}

func TestProcessTriggersImageChangeSourceStrategyFallback(t *testing.T) {
	c, hook := newTriggerTestConverter()
	bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: buildv1.ImageChangeBuildTriggerType})
	bc.Spec.Strategy = buildv1.BuildStrategy{
		Type: buildv1.SourceBuildStrategyType,
		SourceStrategy: &buildv1.SourceBuildStrategy{
			From: corev1.ObjectReference{Kind: "ImageStreamTag", Name: "nodejs:18"},
		},
	}
	c.processTriggers(bc, &shipwrightv1beta1.Build{})

	warnings := warnMessages(hook)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
	assertContainsAll(t, warnings[0], "the strategy image nodejs:18")
}

func TestProcessTriggersConfigChange(t *testing.T) {
	t.Run("without BuildRun template annotation", func(t *testing.T) {
		c, hook := newTriggerTestConverter()
		bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: buildv1.ConfigChangeBuildTriggerType})
		c.processTriggers(bc, &shipwrightv1beta1.Build{})

		warnings := warnMessages(hook)
		if len(warnings) != 2 {
			t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
		}
		assertContainsAll(t, warnings[0],
			"ConfigChange trigger is dropped",
			"automatic first build will not happen",
			"create a BuildRun manually once",
		)
	})

	t.Run("with BuildRun template annotation", func(t *testing.T) {
		c, hook := newTriggerTestConverter()
		bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: buildv1.ConfigChangeBuildTriggerType})
		b := &shipwrightv1beta1.Build{}
		b.Annotations = map[string]string{BuildRunTemplateAnnotation: "buildrun.yaml.tmpl"}
		c.processTriggers(bc, b)

		warnings := warnMessages(hook)
		if len(warnings) != 2 {
			t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
		}
		assertContainsAll(t, warnings[0],
			"ConfigChange trigger is dropped",
			"automatic first build will not happen",
			"apply it once after review",
			BuildRunTemplateAnnotation,
		)
	})
}

func TestProcessTriggersUnknownType(t *testing.T) {
	c, hook := newTriggerTestConverter()
	bc := triggerTestBC(buildv1.BuildTriggerPolicy{Type: buildv1.BuildTriggerType("Bogus")})
	c.processTriggers(bc, &shipwrightv1beta1.Build{})

	warnings := warnMessages(hook)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
	assertContainsAll(t, warnings[0], "unsupported trigger type", "Bogus")
}

func TestProcessTriggersMultipleAndSummary(t *testing.T) {
	c, hook := newTriggerTestConverter()
	bc := triggerTestBC(
		buildv1.BuildTriggerPolicy{Type: buildv1.GitHubWebHookBuildTriggerType},
		buildv1.BuildTriggerPolicy{Type: buildv1.GitHubWebHookBuildTriggerTypeDeprecated},
		buildv1.BuildTriggerPolicy{Type: buildv1.ImageChangeBuildTriggerType},
		buildv1.BuildTriggerPolicy{Type: buildv1.ConfigChangeBuildTriggerType},
	)
	c.processTriggers(bc, &shipwrightv1beta1.Build{})

	warnings := warnMessages(hook)
	// 4 per-trigger warnings + 1 summary
	if len(warnings) != 5 {
		t.Fatalf("expected 5 warnings, got %d: %v", len(warnings), warnings)
	}
	summary := warnings[len(warnings)-1]
	assertContainsAll(t, summary,
		"Found 4 trigger(s)",
		"(ConfigChange, GitHub, ImageChange)", // unique canonical types, sorted
		"my-app",
		"none work in Shipwright today",
	)
}

func TestProcessTriggersNone(t *testing.T) {
	c, hook := newTriggerTestConverter()
	bc := triggerTestBC()
	c.processTriggers(bc, &shipwrightv1beta1.Build{})

	if warnings := warnMessages(hook); len(warnings) != 0 {
		t.Fatalf("expected no warnings for BuildConfig without triggers, got %v", warnings)
	}
}

// TestConvertEmitsTriggerWarnings proves the Convert() wiring end to end via
// the plugin entrypoint, including deprecated type decoding from JSON.
func TestConvertEmitsTriggerWarnings(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	request := transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "triggered-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"triggers": []interface{}{
					map[string]interface{}{
						"type":   "github", // deprecated variant on purpose
						"github": map[string]interface{}{"secretReference": map[string]interface{}{"name": "gh-secret"}},
					},
					map[string]interface{}{"type": "ConfigChange"},
				},
				"source": map[string]interface{}{
					"type": "Git",
					"git":  map[string]interface{}{"uri": "https://github.com/example/myapp.git"},
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

	var triggerWarnings []string
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(entry.Message, "trigger") {
			triggerWarnings = append(triggerWarnings, entry.Message)
		}
	}
	if len(triggerWarnings) != 3 {
		t.Fatalf("expected 3 trigger warnings (GitHub + ConfigChange + summary), got %d: %v", len(triggerWarnings), triggerWarnings)
	}
	assertContainsAll(t, triggerWarnings[0], "GitHub webhook trigger is dropped", "triggered-app")
	assertContainsAll(t, triggerWarnings[1], "ConfigChange trigger is dropped")
	assertContainsAll(t, triggerWarnings[2], "Found 2 trigger(s)", "(ConfigChange, GitHub)", "none work in Shipwright today")
}
