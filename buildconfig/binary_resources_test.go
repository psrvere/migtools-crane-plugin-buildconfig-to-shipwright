//go:build !documentation

package buildconfig

import (
	"strings"
	"testing"
)

const (
	localResourcesWarnMarker = "but the Build has a Local source"
	templateWarnMarker       = "Resource requirements are not supported on Shipwright Build"
	customStepsWarnMarker    = "is a custom mapping with unknown step names"
)

// resourcesSpec is a Parallel-runPolicy Docker BuildConfig with a DockerImage
// output and a push secret, so the only resource-dependent warnings are the
// ones under test.
func resourcesSpec(source string) string {
	return `{
		"runPolicy": "Parallel",
		"source": ` + source + `,
		"strategy": {"type": "Docker", "dockerStrategy": {}},
		"resources": {"requests": {"cpu": "500m"}, "limits": {"memory": "2Gi", "cpu": "2"}},
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}, "pushSecret": {"name": "push"}}
	}`
}

// BUILD-2477: a binary source with resources gets one warning saying the build
// runs with the strategy's defaults, instead of W48 pointing at a template that
// cannot start a Local-source Build. The template is still written.
func TestConvertBinarySourceWithResources(t *testing.T) {
	tests := []struct {
		name   string
		source string
		opts   PluginOptionalFields
	}{
		{"binary", `{"type": "Binary", "binary": {}}`, PluginOptionalFields{}},
		{"binary with asFile", `{"type": "Binary", "binary": {"asFile": "app.jar"}}`, PluginOptionalFields{}},
		{"binary with custom strategy", `{"type": "Binary", "binary": {}}`, PluginOptionalFields{StrategyMapping: map[string]string{"docker": "buildah-with-volumes"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _, warns := convertOutputSpec(t, resourcesSpec(tt.source), tt.opts)

			if n := countContaining(warns, localResourcesWarnMarker); n != 1 {
				t.Fatalf("Local-source resources warnings = %d, want 1 (%v)", n, warns)
			}
			for _, marker := range []string{templateWarnMarker, customStepsWarnMarker} {
				if n := countContaining(warns, marker); n != 0 {
					t.Errorf("%q warnings = %d, want 0 (%v)", marker, n, warns)
				}
			}
			for _, w := range warns {
				if strings.Contains(w, localResourcesWarnMarker) &&
					!strings.Contains(w, "requests cpu=500m, limits cpu=2 memory=2Gi") {
					t.Errorf("warning does not list the requested values: %s", w)
				}
			}
			if b.Annotations[BuildRunTemplateAnnotation] == "" {
				t.Errorf("BuildRun template annotation missing; it keeps the requested values")
			}
		})
	}
}

// A git source with resources keeps W48 and gets no Local-source warning.
func TestConvertGitSourceWithResourcesKeepsTemplateWarning(t *testing.T) {
	_, _, warns := convertOutputSpec(t, resourcesSpec(`{"type": "Git", "git": {"uri": "https://github.com/example/app.git"}}`), PluginOptionalFields{})

	if n := countContaining(warns, templateWarnMarker); n != 1 {
		t.Errorf("template warnings = %d, want 1 (%v)", n, warns)
	}
	if n := countContaining(warns, localResourcesWarnMarker); n != 0 {
		t.Errorf("Local-source resources warnings = %d, want 0 (%v)", n, warns)
	}
}
