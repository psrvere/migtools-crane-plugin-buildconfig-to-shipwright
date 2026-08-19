package buildconfig

import (
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// postCommitRequest builds a minimal convertible BuildConfig carrying the given
// spec.postCommit. A nil postCommit omits the field entirely.
func postCommitRequest(postCommit map[string]interface{}) transform.PluginRequest {
	spec := map[string]interface{}{
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
	}
	if postCommit != nil {
		spec["postCommit"] = postCommit
	}
	return transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "hooked-app",
				"namespace": "myns",
			},
			"spec": spec,
		}},
	}
}

// runAndCollectPostCommitWarnings runs the plugin and returns every WARN entry
// about postCommit — both the per-form warning and the invalid-combination one.
func runAndCollectPostCommitWarnings(t *testing.T, request transform.PluginRequest) []string {
	t.Helper()
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}

	if _, err := plugin.Run(request); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var warnings []string
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && strings.Contains(strings.ToLower(entry.Message), "postcommit") {
			warnings = append(warnings, entry.Message)
		}
	}
	return warnings
}

// TestPostCommitWarnsPerForm covers the five valid postCommit forms documented
// on BuildPostCommitSpec, plus the zero-value cases that must stay silent.
func TestPostCommitWarnsPerForm(t *testing.T) {
	tests := []struct {
		name         string
		postCommit   map[string]interface{}
		wantWarning  bool
		wantContains string
	}{
		{
			name:         "form 1: script only",
			postCommit:   map[string]interface{}{"script": "rake test --verbose"},
			wantWarning:  true,
			wantContains: "PostCommit hook (script)",
		},
		{
			name: "form 2: command only",
			postCommit: map[string]interface{}{
				"command": []interface{}{"rake", "test"},
			},
			wantWarning:  true,
			wantContains: "PostCommit hook (command: rake test)",
		},
		{
			name: "form 3: args only",
			postCommit: map[string]interface{}{
				"args": []interface{}{"rake", "test", "--verbose"},
			},
			wantWarning:  true,
			wantContains: "PostCommit hook (args: rake test --verbose)",
		},
		{
			name: "form 4: script with args",
			postCommit: map[string]interface{}{
				"script": "rake test $1",
				"args":   []interface{}{"--verbose"},
			},
			wantWarning:  true,
			wantContains: "PostCommit hook (script with args: --verbose)",
		},
		{
			name: "form 5: command with args",
			postCommit: map[string]interface{}{
				"command": []interface{}{"rake", "test"},
				"args":    []interface{}{"--verbose"},
			},
			wantWarning:  true,
			wantContains: "PostCommit hook (command: rake test, args: --verbose)",
		},
		{
			name:        "postCommit absent",
			postCommit:  nil,
			wantWarning: false,
		},
		{
			name:        "postCommit present but empty",
			postCommit:  map[string]interface{}{},
			wantWarning: false,
		},
		{
			name: "postCommit with zero-value fields",
			postCommit: map[string]interface{}{
				"script":  "",
				"command": []interface{}{},
				"args":    []interface{}{},
			},
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := runAndCollectPostCommitWarnings(t, postCommitRequest(tt.postCommit))

			if !tt.wantWarning {
				if len(warnings) != 0 {
					t.Fatalf("expected no postCommit warning, got %d: %v", len(warnings), warnings)
				}
				return
			}

			if len(warnings) != 1 {
				t.Fatalf("expected exactly 1 postCommit warning, got %d: %v", len(warnings), warnings)
			}
			if !strings.Contains(warnings[0], tt.wantContains) {
				t.Errorf("warning missing form descriptor %q\ngot: %s", tt.wantContains, warnings[0])
			}
		})
	}
}

// TestPostCommitWarningContent pins the parts of the message that carry the
// migration meaning: the BuildConfig name, the pre-push gating semantics that
// are lost, and the remediation with its caveat.
func TestPostCommitWarningContent(t *testing.T) {
	warnings := runAndCollectPostCommitWarnings(t, postCommitRequest(
		map[string]interface{}{"script": "rake test"},
	))
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 postCommit warning, got %d: %v", len(warnings), warnings)
	}
	msg := warnings[0]

	for _, want := range []string{
		"has no Shipwright equivalent",
		"dropped from BuildConfig 'hooked-app'",
		"before the push",
		"failure failed the build",
		"Tekton Pipeline",
		"after the image is pushed",
		"can no longer block a bad image from reaching the registry",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("warning missing %q\ngot: %s", want, msg)
		}
	}
}

// TestPostCommitScriptNeverInterpolated guards against dumping a free-form,
// possibly multi-line script body into a single-line log entry.
func TestPostCommitScriptNeverInterpolated(t *testing.T) {
	warnings := runAndCollectPostCommitWarnings(t, postCommitRequest(
		map[string]interface{}{"script": "echo SECRET_SCRIPT_BODY\nrake test"},
	))
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 postCommit warning, got %d: %v", len(warnings), warnings)
	}
	if strings.Contains(warnings[0], "SECRET_SCRIPT_BODY") {
		t.Errorf("script body must not be interpolated into the warning\ngot: %s", warnings[0])
	}
	if strings.Contains(warnings[0], "\n") {
		t.Errorf("warning must stay on a single line\ngot: %q", warnings[0])
	}
}

// TestPostCommitScriptAndCommandInvalid covers the API-invalid combination.
// script wins (matching the documented form-1 equivalence) and an extra warning
// flags the invalid input.
func TestPostCommitScriptAndCommandInvalid(t *testing.T) {
	warnings := runAndCollectPostCommitWarnings(t, postCommitRequest(map[string]interface{}{
		"script":  "rake test",
		"command": []interface{}{"/bin/sh", "-c"},
	}))

	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings (form + invalid combination), got %d: %v", len(warnings), warnings)
	}

	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "PostCommit hook (script)") {
		t.Errorf("expected script to take precedence\ngot: %s", joined)
	}
	if !strings.Contains(joined, "both script and command") {
		t.Errorf("expected a warning flagging the invalid combination\ngot: %s", joined)
	}
}

// TestPostCommitSilentOnPassThroughPaths is the placement guard. Convert() has
// three early returns that pass the BuildConfig through unchanged; on those
// paths postCommit is NOT dropped, so warning about it would be false.
func TestPostCommitSilentOnPassThroughPaths(t *testing.T) {
	postCommit := map[string]interface{}{"script": "rake test"}

	tests := []struct {
		name   string
		mutate func(spec map[string]interface{})
	}{
		{
			name: "custom strategy passes through unchanged",
			mutate: func(spec map[string]interface{}) {
				spec["strategy"] = map[string]interface{}{
					"type": "Custom",
					"customStrategy": map[string]interface{}{
						"from": map[string]interface{}{
							"kind": "DockerImage",
							"name": "quay.io/example/builder:latest",
						},
					},
				}
			},
		},
		{
			name: "jenkinsPipeline strategy passes through unchanged",
			mutate: func(spec map[string]interface{}) {
				spec["strategy"] = map[string]interface{}{
					"type":                    "JenkinsPipeline",
					"jenkinsPipelineStrategy": map[string]interface{}{"jenkinsfile": "node {}"},
				}
			},
		},
		{
			name: "missing output.to passes through unchanged",
			mutate: func(spec map[string]interface{}) {
				spec["output"] = map[string]interface{}{}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := postCommitRequest(postCommit)
			spec := request.Unstructured.Object["spec"].(map[string]interface{})
			tt.mutate(spec)

			warnings := runAndCollectPostCommitWarnings(t, request)
			if len(warnings) != 0 {
				t.Fatalf("BuildConfig passes through unchanged, so postCommit is not dropped — "+
					"expected no warning, got %d: %v", len(warnings), warnings)
			}
		})
	}
}
