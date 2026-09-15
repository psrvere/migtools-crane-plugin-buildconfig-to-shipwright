//go:build documentation

package buildconfig

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var updateExamples = flag.Bool("update", false, "rewrite docs/examples/*/expected from the plugin's current output")

const examplesDir = "../docs/examples"

// TestExamplesMatchCommittedOutput runs the plugin over every worked example
// under docs/examples and compares what it produces with the committed files
// in that example's expected/ directory. Each example is a directory holding
// buildconfig.yaml (the input) and optional-flags.json (the crane
// --optional-flags value). The plugin's output is one file per generated
// resource, named <Kind>_<name>.yaml.
//
// A behaviour change fails this test until the example is regenerated with
//
//	go test ./buildconfig -run TestExamplesMatchCommittedOutput -update
//
// and the example's README is re-read against the new output.
func TestExamplesMatchCommittedOutput(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join(examplesDir, "*", "buildconfig.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatalf("no examples found under %s", examplesDir)
	}
	for _, input := range inputs {
		dir := filepath.Dir(input)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			got := runExample(t, dir)
			expectedDir := filepath.Join(dir, "expected")
			if *updateExamples {
				writeExpected(t, expectedDir, got)
				return
			}
			compareExpected(t, expectedDir, got)
		})
	}
}

// runExample converts the example's BuildConfig through the plugin's Run, the
// same entry point crane calls, and returns the generated resources keyed by
// file name.
func runExample(t *testing.T, dir string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "buildconfig.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]interface{}
	if err := yaml.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("parse buildconfig.yaml: %v", err)
	}
	flags := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join(dir, "optional-flags.json")); err == nil {
		if err := json.Unmarshal(raw, &flags); err != nil {
			t.Fatalf("parse optional-flags.json: %v", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	logger := logrus.New()
	logger.SetOutput(os.Stderr)
	plugin := &BuildConfigTransformPlugin{Log: logger}
	resp, err := plugin.Run(transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: obj},
		Extras:       flags,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsWhiteOut {
		t.Fatalf("example was not converted (IsWhiteOut=false); worked examples must convert")
	}

	got := map[string]string{}
	for _, r := range resp.NewResources {
		out, err := yaml.Marshal(r.Object)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("%s_%s.yaml", r.GetKind(), r.GetName())
		got[name] = string(out)
	}
	return got
}

func writeExpected(t *testing.T, dir string, got map[string]string) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range got {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func compareExpected(t *testing.T, dir string, got map[string]string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		want[filepath.Base(f)] = string(raw)
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("%s/%s is committed but the plugin no longer generates it", dir, name)
		}
	}
	for name, content := range got {
		if w, ok := want[name]; !ok {
			t.Errorf("the plugin generates %s but %s has no such file; run with -update", name, dir)
		} else if w != content {
			t.Errorf("%s/%s differs from the plugin's output; run with -update and re-read the example README\n--- committed\n%s\n--- generated\n%s", dir, name, indent(w), indent(content))
		}
	}
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ")
}
