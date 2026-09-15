//go:build !documentation

package buildconfig

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Every case here is a BuildConfig whose only loss is the field named. Before
// BUILD-2319 each converted to a Build stamped "converted", with the warning
// text discarded, so an audit of the migrated output reported a clean success
// on a Build that had lost something.
func TestConvertSilentDropsAreRecorded(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		wantWarning string
	}{
		{
			name: "dropped triggers",
			spec: `{
				"runPolicy": "Parallel",
				"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
				"strategy": {"type": "Docker", "dockerStrategy": {}},
				"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}},
				"triggers": [{"type": "GitHub", "github": {"secretReference": {"name": "gh"}}}]
			}`,
			wantWarning: "GitHub webhook trigger is dropped",
		},
		{
			// This one logged via c.Log.Warn, with no trailing f, so it hid from
			// a grep for c.Log.Warnf when the other trigger drops were found.
			name: "dropped generic webhook trigger",
			spec: `{
				"runPolicy": "Parallel",
				"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
				"strategy": {"type": "Docker", "dockerStrategy": {}},
				"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}},
				"triggers": [{"type": "Generic", "generic": {"allowEnv": true}}]
			}`,
			wantWarning: "allowEnv",
		},
		{
			name: "dropped postCommit hook",
			spec: `{
				"runPolicy": "Parallel",
				"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
				"strategy": {"type": "Docker", "dockerStrategy": {}},
				"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}},
				"postCommit": {"script": "make test"}
			}`,
			wantWarning: "PostCommit hook",
		},
		{
			name: "dropped invalid nodeSelector",
			spec: `{
				"runPolicy": "Parallel",
				"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
				"strategy": {"type": "Docker", "dockerStrategy": {}},
				"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}},
				"nodeSelector": {"BAD KEY!": "x"}
			}`,
			wantWarning: "is invalid",
		},
		{
			name: "dropped runPolicy, with no build args to carry the annotation",
			spec: `{
				"runPolicy": "Serial",
				"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
				"strategy": {"type": "Docker", "dockerStrategy": {}},
				"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
			}`,
			wantWarning: `uses runPolicy "Serial"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := parseBuildConfigJSON(t, `{"metadata": {"name": "app", "namespace": "ns"}, "spec": `+tt.spec+`}`)
			resources, outcome := freshConverter().Convert(bc)

			if outcome.State != OutcomeConvertedWithWarnings {
				t.Fatalf("state = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
			}
			if len(resources) == 0 {
				t.Fatal("expected the converted Build")
			}
			annotations := resources[0].GetAnnotations()
			if got := annotations[ConversionOutcomeAnnotation]; got != string(OutcomeConvertedWithWarnings) {
				t.Errorf("outcome annotation = %q, want %q", got, OutcomeConvertedWithWarnings)
			}
			got := annotations[ConversionWarningsAnnotation]
			if !strings.Contains(got, tt.wantWarning) {
				t.Errorf("warnings annotation does not mention %q; got %q", tt.wantWarning, got)
			}
		})
	}
}

// Attribution is applied in warnf, so it reaches every warning without each
// format string naming the BuildConfig. A run over hundreds of BuildConfigs
// interleaves its warnings on one stream; without the prefix an operator cannot
// tell which BuildConfig a line belongs to.
func TestWarnfAttributesEveryWarning(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	converter := &Converter{Log: logger}

	bc := parseBuildConfigJSON(t, `{"metadata": {"name": "app", "namespace": "ns"}, "spec": {
		"runPolicy": "Serial",
		"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
		"strategy": {"type": "Docker", "dockerStrategy": {}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}},
		"postCommit": {"script": "make test"},
		"triggers": [{"type": "ConfigChange"}]
	}}`)
	_, outcome := converter.Convert(bc)

	if len(outcome.Warnings) < 3 {
		t.Fatalf("expected several warnings to compare, got %d", len(outcome.Warnings))
	}
	for _, w := range outcome.Warnings {
		if !strings.HasPrefix(w, "[ns/app] ") {
			t.Errorf("recorded warning is not attributed: %q", w)
		}
	}
	for _, entry := range hook.AllEntries() {
		if entry.Level != logrus.WarnLevel {
			continue
		}
		if !strings.HasPrefix(entry.Message, "[ns/app] ") {
			t.Errorf("logged warning is not attributed: %q", entry.Message)
		}
	}
}

// warnf must stay usable before Convert has named a BuildConfig. Prefixing
// unconditionally would emit a meaningless "[/]" there.
func TestWarnfWithoutABuildConfigIsUnprefixed(t *testing.T) {
	converter := freshConverter()
	converter.warnf("something happened outside a conversion")

	if len(converter.warnings) != 1 {
		t.Fatalf("expected 1 recorded warning, got %d", len(converter.warnings))
	}
	if strings.HasPrefix(converter.warnings[0], "[") {
		t.Errorf("warning raised outside Convert should not be attributed; got %q", converter.warnings[0])
	}
}

// A BuildConfig the plugin declines still reaches the migrated output. Without a
// disposition on it, it is indistinguishable there from one the plugin never
// received, and the reason survives only in the run log.
func TestPassedThroughBuildConfigCarriesItsDisposition(t *testing.T) {
	tests := []struct {
		name               string
		strategy           string
		existingAnnotation map[string]interface{}
		wantOutcome        OutcomeState
		wantReason         string
	}{
		{
			name:        "skipped, no annotations on the source",
			strategy:    `{"type": "Custom", "customStrategy": {"from": {"kind": "DockerImage", "name": "quay.io/x/y:z"}}}`,
			wantOutcome: OutcomeSkipped,
			wantReason:  "Custom build strategy is not supported",
		},
		{
			name:               "skipped, source already has annotations",
			strategy:           `{"type": "Custom", "customStrategy": {"from": {"kind": "DockerImage", "name": "quay.io/x/y:z"}}}`,
			existingAnnotation: map[string]interface{}{"team": "builds"},
			wantOutcome:        OutcomeSkipped,
			wantReason:         "Custom build strategy is not supported",
		},
		{
			name:        "failed, unknown strategy type",
			strategy:    `{"type": "Bogus"}`,
			wantOutcome: OutcomeFailed,
			wantReason:  "unknown build strategy type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := map[string]interface{}{"name": "app", "namespace": "ns"}
			if tt.existingAnnotation != nil {
				metadata["annotations"] = tt.existingAnnotation
			}
			var strategy map[string]interface{}
			if err := json.Unmarshal([]byte(tt.strategy), &strategy); err != nil {
				t.Fatalf("bad strategy fixture: %v", err)
			}
			original := unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "build.openshift.io/v1",
				"kind":       "BuildConfig",
				"metadata":   metadata,
				"spec": map[string]interface{}{
					"runPolicy": "Parallel",
					"source":    map[string]interface{}{"type": "Git", "git": map[string]interface{}{"uri": "https://github.com/example/app.git"}},
					"strategy":  strategy,
					"output":    map[string]interface{}{"to": map[string]interface{}{"kind": "DockerImage", "name": "quay.io/example/app:latest"}},
				},
			}}

			plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
			resp, err := plugin.Run(transform.PluginRequest{Unstructured: original})
			// A per-BuildConfig failure must never become a plugin error: crane
			// aborts the whole migration on one (BUILD-2318).
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.IsWhiteOut {
				t.Error("a BuildConfig that was not converted must not be whited out")
			}
			if len(resp.NewResources) != 0 {
				t.Errorf("expected no new resources, got %d", len(resp.NewResources))
			}
			if len(resp.Patches) == 0 {
				t.Fatal("expected a patch recording the disposition")
			}

			originalJSON, err := original.MarshalJSON()
			if err != nil {
				t.Fatalf("marshaling the original: %v", err)
			}
			patchedJSON, err := resp.Patches.Apply(originalJSON)
			if err != nil {
				t.Fatalf("applying the disposition patch: %v", err)
			}
			patched := map[string]interface{}{}
			if err := json.Unmarshal(patchedJSON, &patched); err != nil {
				t.Fatalf("unmarshaling the patched BuildConfig: %v", err)
			}
			annotations, _ := patched["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})

			if got := annotations[ConversionOutcomeAnnotation]; got != string(tt.wantOutcome) {
				t.Errorf("outcome annotation = %v, want %q", got, tt.wantOutcome)
			}
			reason, _ := annotations[ConversionReasonAnnotation].(string)
			if !strings.Contains(reason, tt.wantReason) {
				t.Errorf("reason annotation = %q, want it to mention %q", reason, tt.wantReason)
			}
			for key, want := range tt.existingAnnotation {
				if got := annotations[key]; got != want {
					t.Errorf("existing annotation %q = %v, want %v; the patch must not replace annotations", key, got, want)
				}
			}
		})
	}
}

// The annotation carries every warning now, not just the build-args ones, so the
// existing byte cap has to hold for the whole set rather than one block of it.
func TestWarningsAnnotationStaysBounded(t *testing.T) {
	converter := freshConverter()
	bc := parseBuildConfigJSON(t, `{"metadata": {"name": "app", "namespace": "ns"}, "spec": {
		"runPolicy": "Serial",
		"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
		"strategy": {"type": "Docker", "dockerStrategy": {"buildArgs": [`+strings.Repeat(`{"name": "BAD=`+strings.Repeat("x", 400)+`", "value": "v"},`, 120)+`{"name": "OK", "value": "v"}]}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}}`)

	resources, outcome := converter.Convert(bc)
	if outcome.State != OutcomeConvertedWithWarnings {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
	}
	annotation := resources[0].GetAnnotations()[ConversionWarningsAnnotation]
	if len(annotation) > maxConversionWarningsBytes {
		t.Errorf("annotation is %d bytes, over the %d cap", len(annotation), maxConversionWarningsBytes)
	}
	if !strings.Contains(annotation, "conversion warning(s) omitted") {
		t.Errorf("expected the truncation notice; got the last 200 bytes: %q", annotation[max(0, len(annotation)-200):])
	}
}

func TestTruncateReasonKeepsValidUTF8(t *testing.T) {
	reason := strings.Repeat("é", maxConversionReasonBytes)
	got := truncateReason(reason)
	if len(got) > maxConversionReasonBytes+len("…") {
		t.Errorf("truncated reason is %d bytes, over the cap", len(got))
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncated reason is not valid UTF-8: %q", got)
	}
}

func TestErrorLevelDropsAreAttributedToo(t *testing.T) {
	converter := freshConverter()
	bc := parseBuildConfigJSON(t, `{"metadata": {"name": "app", "namespace": "ns"}, "spec": {
		"runPolicy": "Parallel",
		"source": {"type": "Dockerfile", "dockerfile": "FROM scratch\\nRUN true"},
		"strategy": {"type": "Docker", "dockerStrategy": {}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}}`)

	resources, outcome := converter.Convert(bc)
	if outcome.State != OutcomeConvertedWithWarnings {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
	}
	for _, w := range outcome.Warnings {
		if !strings.HasPrefix(w, "[ns/app] ") {
			t.Errorf("warning logged at ERROR level is not attributed: %q", w)
		}
	}
	annotation := resources[0].GetAnnotations()[ConversionWarningsAnnotation]
	if !strings.Contains(annotation, "Inline Dockerfile") {
		t.Errorf("expected the inline Dockerfile drop in the annotation; got %q", annotation)
	}
}

// boundedWarnings records a warning of its own when it truncates. Snapshotting
// Outcome.Warnings before that call left the notice out of the Outcome, so a
// caller auditing Outcome.Warnings under-reported what the conversion dropped.
func TestTruncationNoticeReachesTheOutcome(t *testing.T) {
	converter := freshConverter()
	bc := parseBuildConfigJSON(t, `{"metadata": {"name": "app", "namespace": "ns"}, "spec": {
		"runPolicy": "Parallel",
		"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
		"strategy": {"type": "Docker", "dockerStrategy": {"buildArgs": [`+strings.Repeat(`{"name": "BAD=`+strings.Repeat("x", 400)+`", "value": "v"},`, 120)+`{"name": "OK", "value": "v"}]}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}}`)

	_, outcome := converter.Convert(bc)
	if len(outcome.Warnings) != len(converter.warnings) {
		t.Errorf("Outcome.Warnings has %d entries, the recorder has %d; the Outcome must carry every warning this conversion produced",
			len(outcome.Warnings), len(converter.warnings))
	}
	for _, w := range outcome.Warnings {
		if strings.Contains(w, "Conversion warnings exceeded") {
			return
		}
	}
	t.Error("the truncation notice is missing from Outcome.Warnings")
}

func TestEscapeJSONPointer(t *testing.T) {
	tests := []struct{ in, want string }{
		{"buildconfig-to-shipwright/conversion-outcome", "buildconfig-to-shipwright~1conversion-outcome"},
		{"a~b", "a~0b"},
		// "~" must be escaped before "/", or "~1" from the slash is re-escaped to "~01".
		{"a~/b", "a~0~1b"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		if got := escapeJSONPointer(tt.in); got != tt.want {
			t.Errorf("escapeJSONPointer(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
