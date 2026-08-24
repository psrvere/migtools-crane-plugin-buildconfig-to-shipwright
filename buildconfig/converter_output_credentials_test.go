package buildconfig

import (
	"encoding/json"
	"strings"
	"testing"

	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
)

// convertOutputSpec runs a BuildConfig (given as its spec JSON) through a
// Converter with the supplied options and returns the emitted Build, the
// outcome, and the WARN-level messages logged. Push-credential and
// rollout-trigger behaviour (BUILD-2316) is entirely a function of the output
// block plus the registry/imagestream mappings, so tests vary only those.
func convertOutputSpec(t *testing.T, spec string, opts PluginOptionalFields) (*shipwrightv1beta1.Build, Outcome, []string) {
	t.Helper()
	logger, hook := logrustest.NewNullLogger()
	c := &Converter{Log: logger, Opts: opts}
	bc := parseBuildConfigJSON(t, `{
		"apiVersion": "build.openshift.io/v1",
		"kind": "BuildConfig",
		"metadata": {"name": "app", "namespace": "ns"},
		"spec": `+spec+`
	}`)
	resources, outcome := c.Convert(bc)
	if outcome.State == OutcomeFailed {
		t.Fatalf("conversion failed: %s", outcome.Reason)
	}
	if len(resources) == 0 {
		t.Fatal("expected at least the converted Build")
	}
	// Find the Build by kind rather than assuming array position: the converter
	// also emits a ServiceAccount, and the order is not a guaranteed contract.
	b := &shipwrightv1beta1.Build{}
	found := false
	for _, r := range resources {
		if r.GetKind() != "Build" {
			continue
		}
		raw, _ := json.Marshal(r.Object)
		if err := json.Unmarshal(raw, b); err != nil {
			t.Fatalf("unmarshal Build: %v", err)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("no Build resource in converter output")
	}
	var warns []string
	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel {
			warns = append(warns, e.Message)
		}
	}
	return b, outcome, warns
}

func countContaining(msgs []string, substr string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

const (
	pushCredWarnMarker = "No explicit pushSecret found"
	rolloutWarnMarker  = "redirected off the internal registry"
)

// dockerOutputSpec is a Parallel-runPolicy Docker BuildConfig whose output is a
// DockerImage; the output block is supplied by the caller.
func dockerOutputSpec(output string) string {
	return `{
		"runPolicy": "Parallel",
		"source": {"type": "Git", "git": {"uri": "https://github.com/example/app.git"}},
		"strategy": {"type": "Docker", "dockerStrategy": {}},
		"output": ` + output + `
	}`
}

// AC1: a DockerImage output that names a pushSecret carries it onto the Build
// and converts cleanly.
func TestConvertOutputDockerImageWithPushSecret(t *testing.T) {
	b, outcome, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"},
		"pushSecret": {"name": "quay-push-secret"}
	}`), PluginOptionalFields{})

	if b.Spec.Output.PushSecret == nil || *b.Spec.Output.PushSecret != "quay-push-secret" {
		t.Errorf("output.pushSecret = %v, want %q", b.Spec.Output.PushSecret, "quay-push-secret")
	}
	if outcome.State != OutcomeConverted {
		t.Errorf("state = %q, want %q (warnings: %v)", outcome.State, OutcomeConverted, outcome.Warnings)
	}
	if n := countContaining(warns, pushCredWarnMarker); n != 0 {
		t.Errorf("push-credential warnings = %d, want 0", n)
	}
}

// AC2: a DockerImage output with no pushSecret is emitted without one and warns
// exactly once, naming the registry-secret remedy.
func TestConvertOutputDockerImageNoPushSecretWarns(t *testing.T) {
	b, outcome, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}
	}`), PluginOptionalFields{})

	if b.Spec.Output.PushSecret != nil {
		t.Errorf("output.pushSecret = %q, want unset", *b.Spec.Output.PushSecret)
	}
	if outcome.State != OutcomeConvertedWithWarnings {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
	}
	if n := countContaining(warns, pushCredWarnMarker); n != 1 {
		t.Errorf("push-credential warnings = %d, want 1 (%v)", n, warns)
	}
	if n := countContaining(outcome.Warnings, pushCredWarnMarker); n != 1 {
		t.Errorf("outcome.Warnings push-credential count = %d, want 1 (%v)", n, outcome.Warnings)
	}
	only := firstContaining(warns, pushCredWarnMarker)
	if !strings.Contains(only, "registry credential secret") {
		t.Errorf("DockerImage warning should name the registry-secret remedy, got: %q", only)
	}
}

// AC3: an ImageStreamTag output with no pushSecret warns exactly once, and that
// warning names the ServiceAccount remedy rather than the registry-secret one.
func TestConvertOutputImageStreamTagNoPushSecretWarns(t *testing.T) {
	_, _, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "ImageStreamTag", "name": "app:latest"}
	}`), PluginOptionalFields{})

	if n := countContaining(warns, pushCredWarnMarker); n != 1 {
		t.Fatalf("push-credential warnings = %d, want 1 (%v)", n, warns)
	}
	only := firstContaining(warns, pushCredWarnMarker)
	if !strings.Contains(only, "ServiceAccount") {
		t.Errorf("ImageStreamTag warning should name the ServiceAccount remedy, got: %q", only)
	}
	if strings.Contains(only, "registry credential secret") {
		t.Errorf("ImageStreamTag warning must not name the registry-secret remedy, got: %q", only)
	}
}

// AC4: an ImageStreamTag output that stays on the internal registry (no
// mappings) fires no rollout-trigger warning.
func TestConvertOutputImageStreamTagNoRedirectNoRolloutWarning(t *testing.T) {
	_, _, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "ImageStreamTag", "name": "app:latest"},
		"pushSecret": {"name": "internal-push"}
	}`), PluginOptionalFields{})

	if n := countContaining(warns, rolloutWarnMarker); n != 0 {
		t.Errorf("rollout-trigger warnings = %d, want 0 (%v)", n, warns)
	}
}

// AC5: a --registry-mapping that moves the output off the internal registry
// fires exactly one rollout-trigger warning naming the ImageStream.
func TestConvertOutputImageStreamTagRegistryMappingRolloutWarning(t *testing.T) {
	_, outcome, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "ImageStreamTag", "name": "app:latest"},
		"pushSecret": {"name": "quay-push"}
	}`), PluginOptionalFields{
		RegistryMapping: map[string]string{internalRegistryURL: "quay.io/acme"},
	})

	if n := countContaining(warns, rolloutWarnMarker); n != 1 {
		t.Fatalf("rollout-trigger warnings = %d, want 1 (%v)", n, warns)
	}
	only := firstContaining(warns, rolloutWarnMarker)
	if !strings.Contains(only, "ns/app") {
		t.Errorf("rollout warning should name the ImageStream ns/app, got: %q", only)
	}
	if countContaining(outcome.Warnings, rolloutWarnMarker) != 1 {
		t.Errorf("rollout warning missing from outcome.Warnings: %v", outcome.Warnings)
	}
}

// AC5: an --imagestream-mapping that points the output at an external registry
// likewise fires the rollout-trigger warning.
func TestConvertOutputImageStreamTagMappingRolloutWarning(t *testing.T) {
	_, _, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "ImageStreamTag", "name": "app:latest"},
		"pushSecret": {"name": "quay-push"}
	}`), PluginOptionalFields{
		ImageStreamMapping: map[string]string{"ns/app:latest": "quay.io/acme/app:latest"},
	})

	if n := countContaining(warns, rolloutWarnMarker); n != 1 {
		t.Fatalf("rollout-trigger warnings = %d, want 1 (%v)", n, warns)
	}
	if only := firstContaining(warns, rolloutWarnMarker); !strings.Contains(only, "ns/app") {
		t.Errorf("rollout warning should name the ImageStream ns/app, got: %q", only)
	}
}

// An ImageStreamTag that is both redirected off the internal registry and names
// no pushSecret must raise the rollout-trigger warning and the push-credential
// warning independently; neither suppresses the other.
func TestConvertOutputImageStreamTagRedirectedAndNoPushSecret(t *testing.T) {
	_, outcome, warns := convertOutputSpec(t, dockerOutputSpec(`{
		"to": {"kind": "ImageStreamTag", "name": "app:latest"}
	}`), PluginOptionalFields{
		RegistryMapping: map[string]string{internalRegistryURL: "quay.io/acme"},
	})

	if outcome.State != OutcomeConvertedWithWarnings {
		t.Fatalf("state = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
	}
	if n := countContaining(warns, rolloutWarnMarker); n != 1 {
		t.Errorf("rollout-trigger warnings = %d, want 1 (%v)", n, warns)
	}
	if n := countContaining(warns, pushCredWarnMarker); n != 1 {
		t.Errorf("push-credential warnings = %d, want 1 (%v)", n, warns)
	}
}

func firstContaining(msgs []string, substr string) string {
	for _, m := range msgs {
		if strings.Contains(m, substr) {
			return m
		}
	}
	return ""
}
