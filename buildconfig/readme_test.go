package buildconfig

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

const readmePath = "../README.md"

// TestReadmeOptionalFlagsAreValidJSON fails when a --optional-flags example in
// the README is not a JSON object of strings, which is what crane parses. The
// README once showed a key=value form that crane rejects.
func TestReadmeOptionalFlagsAreValidJSON(t *testing.T) {
	readme := readFile(t, readmePath)
	re := regexp.MustCompile(`--optional-flags\s+(?:'([^']*)'|"([^"]*)")`)
	matches := re.FindAllStringSubmatch(readme, -1)
	if len(matches) == 0 {
		t.Fatalf("%s has no --optional-flags example", readmePath)
	}
	for _, m := range matches {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		var flags map[string]string
		if err := json.Unmarshal([]byte(raw), &flags); err != nil {
			t.Errorf("%s: --optional-flags value is not a JSON object of strings: %s\n  %v", readmePath, raw, err)
			continue
		}
		known := map[string]bool{}
		for _, f := range (&BuildConfigTransformPlugin{}).Metadata().OptionalFields {
			known[f.FlagName] = true
		}
		for k := range flags {
			if !known[k] {
				t.Errorf("%s: --optional-flags example uses %q, which is not a flag the plugin declares", readmePath, k)
			}
		}
	}
}

// TestReadmeVersionsMatchPins fails when a version the README quotes drifts
// from where it is pinned: the Go version in go.mod, the Shipwright version in
// go.mod and the Minikube setup script, and the crane commit in the CI
// workflow.
func TestReadmeVersionsMatchPins(t *testing.T) {
	readme := readFile(t, readmePath)
	gomod := readFile(t, "../go.mod")

	goVersion := firstMatch(t, gomod, `(?m)^go (\d+\.\d+)`)
	if !strings.Contains(readme, "Go "+goVersion) {
		t.Errorf("%s does not say \"Go %s\"; go.mod requires %s", readmePath, goVersion, goVersion)
	}

	shipwright := firstMatch(t, gomod, `github.com/shipwright-io/build (v[\d.]+)`)
	setup := readFile(t, "../hack/setup-minikube-shipwright.sh")
	if scripted := firstMatch(t, setup, `SHIPWRIGHT_VERSION:-(v[\d.]+)`); scripted != shipwright {
		t.Errorf("hack/setup-minikube-shipwright.sh installs Shipwright %s but go.mod builds against %s", scripted, shipwright)
	}
	if !strings.Contains(readme, "Shipwright "+shipwright) {
		t.Errorf("%s does not mention Shipwright %s, the version go.mod builds against", readmePath, shipwright)
	}

	workflow := readFile(t, "../.github/workflows/test-e2e-minikube-pr.yml")
	cranePin := firstMatch(t, workflow, `git checkout ([0-9a-f]{40})`)
	if !strings.Contains(readme, cranePin) {
		t.Errorf("%s does not name the crane commit CI pins, %s", readmePath, cranePin)
	}
}

func firstMatch(t *testing.T, s, pattern string) string {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("pattern %q not found", pattern)
	}
	return m[1]
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
