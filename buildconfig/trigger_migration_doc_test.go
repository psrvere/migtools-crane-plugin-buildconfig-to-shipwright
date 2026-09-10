package buildconfig

import (
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const triggerRunbookPath = "../docs/trigger-migration.md"

// yamlFence matches one fenced yaml block. (?s) lets the body span lines, and
// the lazy body stops at the first closing fence.
var yamlFence = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// TestTriggerRunbookYAMLParses fails when a yaml block in the trigger runbook
// is not a sequence of documents that kubectl apply could read. The page is
// copy-paste recipes and nothing else exercises them in CI, so a stray tab or
// a misplaced dash would otherwise reach a reader's cluster first.
func TestTriggerRunbookYAMLParses(t *testing.T) {
	page := readFile(t, triggerRunbookPath)
	fences := yamlFence.FindAllStringSubmatchIndex(page, -1)
	if len(fences) == 0 {
		t.Fatalf("%s has no yaml block", triggerRunbookPath)
	}
	const sep = "\n---\n"
	for _, f := range fences {
		// offset walks the fence so each document reports its own line, not
		// the fence's: the recipes run to ten documents and 150 lines each.
		offset := f[2]
		for _, doc := range strings.Split(page[f[2]:f[3]], sep) {
			line := 1 + strings.Count(page[:offset], "\n")
			offset += len(doc) + len(sep)
			var obj map[string]any
			if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
				t.Errorf("%s: document at line %d: %v", triggerRunbookPath, line, err)
				continue
			}
			// An empty or comment-only document unmarshals to nil without an
			// error, which is what a stray trailing --- leaves behind.
			if obj == nil {
				t.Errorf("%s: document at line %d is empty", triggerRunbookPath, line)
				continue
			}
			if obj["kind"] == nil || obj["apiVersion"] == nil {
				t.Errorf("%s: document at line %d has no kind or apiVersion", triggerRunbookPath, line)
			}
		}
	}
}
