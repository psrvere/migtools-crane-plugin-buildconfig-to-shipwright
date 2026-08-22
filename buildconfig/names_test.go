package buildconfig

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var dns1123LabelRegexp = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func newNameTestBC(name, namespace, serviceAccount, pullSecret string) *buildv1.BuildConfig {
	bc := &buildv1.BuildConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				ServiceAccount: serviceAccount,
				Source: buildv1.BuildSource{
					Git: &buildv1.GitBuildSource{URI: "https://github.com/example/myapp.git"},
				},
				Strategy: buildv1.BuildStrategy{
					Type:           buildv1.DockerBuildStrategyType,
					DockerStrategy: &buildv1.DockerBuildStrategy{},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/myapp:latest"},
				},
			},
		},
	}
	if pullSecret != "" {
		bc.Spec.Strategy.DockerStrategy.PullSecret = &corev1.LocalObjectReference{Name: pullSecret}
	}
	return bc
}

func convertWithFreshConverter(t *testing.T, bc *buildv1.BuildConfig) []unstructured.Unstructured {
	t.Helper()
	logger, _ := logrustest.NewNullLogger()
	converter := &Converter{Log: logger, Opts: PluginOptionalFields{}}
	resources, outcome := converter.Convert(bc)
	if outcome.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcome.Reason)
	}
	return resources
}

func serviceAccountFromResources(t *testing.T, resources []unstructured.Unstructured) *corev1.ServiceAccount {
	t.Helper()
	for _, r := range resources {
		if r.GetKind() != "ServiceAccount" {
			continue
		}
		sa := &corev1.ServiceAccount{}
		jsonBytes, _ := json.Marshal(r.Object)
		if err := json.Unmarshal(jsonBytes, sa); err != nil {
			t.Fatalf("error decoding ServiceAccount: %v", err)
		}
		return sa
	}
	t.Fatal("no ServiceAccount found in converted resources")
	return nil
}

func TestGeneratedNamesAreDNS1123Compliant(t *testing.T) {
	longName := strings.Repeat("a", 60) + "-with-a-very-long-suffix-over-63-chars"

	tests := []struct {
		name       string
		bcName     string
		wantPrefix string
		wantSame   bool
	}{
		{
			name:     "valid short name is unchanged",
			bcName:   "my-app",
			wantSame: true,
		},
		{
			name:       "over-63-char name is truncated with hash suffix",
			bcName:     longName,
			wantPrefix: strings.Repeat("a", 54),
		},
		{
			name:       "dots are sanitized with hash suffix",
			bcName:     "my.app.v2",
			wantPrefix: "my-app-v2-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := newNameTestBC(tt.bcName, "myns", "", "my-pull-secret")
			resources := convertWithFreshConverter(t, bc)

			buildName := resources[0].GetName()
			saName := serviceAccountFromResources(t, resources).Name

			for _, generated := range []string{buildName, saName} {
				if len(generated) > 63 {
					t.Errorf("generated name %q is longer than 63 characters (%d)", generated, len(generated))
				}
				if !dns1123LabelRegexp.MatchString(generated) {
					t.Errorf("generated name %q is not a valid DNS-1123 label", generated)
				}
			}

			if tt.wantSame && buildName != tt.bcName {
				t.Errorf("Build name = %q, want unchanged %q", buildName, tt.bcName)
			}
			if tt.wantPrefix != "" {
				if !strings.HasPrefix(buildName, tt.wantPrefix) {
					t.Errorf("Build name = %q, want prefix %q", buildName, tt.wantPrefix)
				}
				if buildName == tt.bcName {
					t.Errorf("Build name %q should have been rewritten", buildName)
				}
			}

			// Deterministic across independent converter invocations.
			rerun := convertWithFreshConverter(t, newNameTestBC(tt.bcName, "myns", "", "my-pull-secret"))
			if rerun[0].GetName() != buildName {
				t.Errorf("name generation is not deterministic: %q vs %q", rerun[0].GetName(), buildName)
			}
		})
	}
}

func TestCollidingTruncatedNamesGetDistinctNames(t *testing.T) {
	// Both names share the same first 63 characters and only differ afterward.
	common := strings.Repeat("x", 63)
	nameA := common + "-alpha"
	nameB := common + "-beta"

	logger, _ := logrustest.NewNullLogger()
	converter := &Converter{Log: logger, Opts: PluginOptionalFields{}}

	resourcesA, outcomeA := converter.Convert(newNameTestBC(nameA, "myns", "", ""))
	if outcomeA.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcomeA.Reason)
	}
	resourcesB, outcomeB := converter.Convert(newNameTestBC(nameB, "myns", "", ""))
	if outcomeB.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcomeB.Reason)
	}

	buildNameA := resourcesA[0].GetName()
	buildNameB := resourcesB[0].GetName()

	if buildNameA == buildNameB {
		t.Fatalf("colliding truncated names must resolve to distinct names, both got %q", buildNameA)
	}
	for _, generated := range []string{buildNameA, buildNameB} {
		if len(generated) > 63 || !dns1123LabelRegexp.MatchString(generated) {
			t.Errorf("generated name %q is not a valid DNS-1123 label of at most 63 characters", generated)
		}
	}
}

func TestSharedServiceAccountMergesImagePullSecrets(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	converter := &Converter{Log: logger, Opts: PluginOptionalFields{}}

	convert := func(bc *buildv1.BuildConfig) []unstructured.Unstructured {
		t.Helper()
		resources, outcome := converter.Convert(bc)
		if outcome.State == OutcomeFailed {
			t.Fatalf("unexpected conversion failure: %s", outcome.Reason)
		}
		return resources
	}

	first := convert(newNameTestBC("app-one", "myns", "shared-builder", "secret-a"))
	firstSA := serviceAccountFromResources(t, first)
	if len(firstSA.ImagePullSecrets) != 1 || firstSA.ImagePullSecrets[0].Name != "secret-a" {
		t.Fatalf("first SA imagePullSecrets = %v, want [secret-a]", firstSA.ImagePullSecrets)
	}

	second := convert(newNameTestBC("app-two", "myns", "shared-builder", "secret-b"))
	secondSA := serviceAccountFromResources(t, second)

	if secondSA.Name != "shared-builder" {
		t.Errorf("shared SA name = %q, want %q", secondSA.Name, "shared-builder")
	}

	gotPull := []string{}
	for _, s := range secondSA.ImagePullSecrets {
		gotPull = append(gotPull, s.Name)
	}
	if len(gotPull) != 2 || gotPull[0] != "secret-a" || gotPull[1] != "secret-b" {
		t.Errorf("merged SA imagePullSecrets = %v, want [secret-a secret-b]", gotPull)
	}

	gotSecrets := []string{}
	for _, s := range secondSA.Secrets {
		gotSecrets = append(gotSecrets, s.Name)
	}
	if len(gotSecrets) != 2 || gotSecrets[0] != "secret-a" || gotSecrets[1] != "secret-b" {
		t.Errorf("merged SA secrets = %v, want [secret-a secret-b]", gotSecrets)
	}

	// Re-using an already-merged secret must not duplicate it.
	third := convert(newNameTestBC("app-three", "myns", "shared-builder", "secret-b"))
	thirdSA := serviceAccountFromResources(t, third)
	if len(thirdSA.ImagePullSecrets) != 2 {
		t.Errorf("duplicate pull secret was not deduplicated: %v", thirdSA.ImagePullSecrets)
	}
}
