//go:build !documentation

package buildconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// freshConverter returns a Converter with a discarding logger.
func freshConverter() *Converter {
	logger, _ := logrustest.NewNullLogger()
	return &Converter{Log: logger, Opts: PluginOptionalFields{}}
}

// buildConfigRequestFromSpec wraps a BuildConfig spec (a JSON object string) into
// a plugin request. It differs from converter_test.go's buildConfigRequest, which
// starts from a fixed Docker skeleton and varies it through functional options;
// here the whole spec is supplied verbatim so an outcome test can pin the exact
// shape it needs.
func buildConfigRequestFromSpec(spec string) transform.PluginRequest {
	specMap := map[string]interface{}{}
	if err := json.Unmarshal([]byte(spec), &specMap); err != nil {
		panic("buildConfigRequestFromSpec: invalid spec JSON: " + err.Error())
	}
	return transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata":   map[string]interface{}{"name": "app", "namespace": "ns"},
			"spec":       specMap,
		}},
	}
}

// convertedDockerBC is a minimal BuildConfig that converts with no warnings:
// runPolicy Parallel (Serial would warn), a git source, and a DockerImage output
// with an explicit pushSecret (BUILD-2316 warns when a DockerImage output names
// no push secret, so a zero-warning fixture must carry one).
const convertedDockerBC = `{
	"runPolicy": "Parallel",
	"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
	"strategy": {"type": "Docker", "dockerStrategy": {}},
	"output": {
		"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"},
		"pushSecret": {"name": "quay-push-secret"}
	}
}`

func TestConvertOutcomeConverted(t *testing.T) {
	bc := parseBuildConfigJSON(t, `{"spec": `+convertedDockerBC+`}`)
	resources, outcome := freshConverter().Convert(bc)
	if outcome.State != OutcomeConverted {
		t.Fatalf("state = %q (reason %q), want %q", outcome.State, outcome.Reason, OutcomeConverted)
	}
	if len(resources) == 0 {
		t.Fatal("expected at least the converted Build")
	}
	if got := resources[0].GetAnnotations()[ConversionOutcomeAnnotation]; got != string(OutcomeConverted) {
		t.Errorf("outcome annotation = %q, want %q", got, OutcomeConverted)
	}
	if len(outcome.Warnings) != 0 {
		t.Errorf("a clean conversion must carry no warnings, got %v", outcome.Warnings)
	}
}

func TestConvertOutcomeConvertedWithWarnings(t *testing.T) {
	// Serial runPolicy is dropped during conversion and emits a warning.
	bc := parseBuildConfigJSON(t, `{"spec": {
		"runPolicy": "Serial",
		"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
		"strategy": {"type": "Docker", "dockerStrategy": {}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}}`)
	resources, outcome := freshConverter().Convert(bc)
	if outcome.State != OutcomeConvertedWithWarnings {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
	}
	if got := resources[0].GetAnnotations()[ConversionOutcomeAnnotation]; got != string(OutcomeConvertedWithWarnings) {
		t.Errorf("outcome annotation = %q, want %q", got, OutcomeConvertedWithWarnings)
	}
	if len(outcome.Warnings) == 0 {
		t.Fatal("converted-with-warnings outcome must carry the warning messages")
	}
	found := false
	for _, w := range outcome.Warnings {
		if strings.Contains(w, "runPolicy") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a runPolicy warning in outcome.Warnings, got %v", outcome.Warnings)
	}
}

func TestConvertOutcomeSkippedCustomStrategy(t *testing.T) {
	bc := parseBuildConfigJSON(t, `{"spec": {
		"strategy": {"type": "Custom", "customStrategy": {}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}}`)
	resources, outcome := freshConverter().Convert(bc)
	if outcome.State != OutcomeSkipped {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeSkipped)
	}
	if outcome.Reason == "" {
		t.Error("skipped outcome must carry a reason")
	}
	if len(resources) != 0 {
		t.Errorf("skipped conversion must emit no resources, got %d", len(resources))
	}
}

func TestConvertOutcomeSkippedNoOutput(t *testing.T) {
	bc := parseBuildConfigJSON(t, `{"spec": {
		"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
		"strategy": {"type": "Docker", "dockerStrategy": {}}
	}}`)
	_, outcome := freshConverter().Convert(bc)
	if outcome.State != OutcomeSkipped {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeSkipped)
	}
}

func TestConvertOutcomeFailedUnknownStrategy(t *testing.T) {
	bc := parseBuildConfigJSON(t, `{"spec": {
		"strategy": {"type": "Bogus"},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}}`)
	resources, outcome := freshConverter().Convert(bc)
	if outcome.State != OutcomeFailed {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeFailed)
	}
	if outcome.Reason == "" {
		t.Error("failed outcome must carry a reason")
	}
	if len(resources) != 0 {
		t.Errorf("failed conversion must emit no resources, got %d", len(resources))
	}
}

// TestConvertOutcomeFailedBrokenSource covers source shapes that Shipwright
// cannot represent: the converter bails out mid-source, leaving a Build with no
// usable source, so the conversion must be classified as failed (not
// converted-with-warnings) and the BuildConfig passed through unchanged rather
// than a structurally-incomplete Build shipped (BUILD-2318, review W2).
func TestConvertOutcomeFailedBrokenSource(t *testing.T) {
	cases := map[string]string{
		"multiple source types": `{"spec": {
			"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}, "binary": {"asFile": "app.jar"}},
			"strategy": {"type": "Docker", "dockerStrategy": {}},
			"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
		}}`,
		"binary archive without asFile": `{"spec": {
			"source": {"type": "Binary", "binary": {}},
			"strategy": {"type": "Docker", "dockerStrategy": {}},
			"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
		}}`,
		"multiple image sources": `{"spec": {
			"source": {"type": "Image", "images": [
				{"from": {"kind": "DockerImage", "name": "quay.io/example/a:latest"}},
				{"from": {"kind": "DockerImage", "name": "quay.io/example/b:latest"}}
			]},
			"strategy": {"type": "Docker", "dockerStrategy": {}},
			"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
		}}`,
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			bc := parseBuildConfigJSON(t, spec)
			resources, outcome := freshConverter().Convert(bc)
			if outcome.State != OutcomeFailed {
				t.Fatalf("state = %q, want %q", outcome.State, OutcomeFailed)
			}
			if outcome.Reason == "" {
				t.Error("failed outcome must carry a reason")
			}
			if len(resources) != 0 {
				t.Errorf("a broken-source conversion must emit no resources, got %d", len(resources))
			}
		})
	}
}

// TestRunDoesNotAbortOnConversionFailure is the core BUILD-2318 guarantee: a
// BuildConfig the plugin cannot convert must not return an error to crane
// (which would abort the whole migration). It is passed through unchanged.
func TestRunDoesNotAbortOnConversionFailure(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}

	resp, err := plugin.Run(buildConfigRequestFromSpec(`{
		"strategy": {"type": "Bogus"},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}}
	}`))
	if err != nil {
		t.Fatalf("Run must not return an error for a per-BuildConfig conversion failure, got: %v", err)
	}
	if resp.IsWhiteOut {
		t.Error("a failed conversion must pass the BuildConfig through unchanged (IsWhiteOut=false)")
	}
	if len(resp.NewResources) != 0 {
		t.Errorf("a failed conversion must emit no new resources, got %d", len(resp.NewResources))
	}
}

func TestRunConvertsValidBuildConfig(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}

	resp, err := plugin.Run(buildConfigRequestFromSpec(convertedDockerBC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsWhiteOut {
		t.Error("a successful conversion must white out the original BuildConfig")
	}
	if len(resp.NewResources) == 0 {
		t.Fatal("expected converted resources")
	}
}
