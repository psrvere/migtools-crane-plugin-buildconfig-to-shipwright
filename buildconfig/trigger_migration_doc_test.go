//go:build documentation

package buildconfig

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
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
	for _, f := range fences {
		startLine := 1 + strings.Count(page[:f[2]], "\n")
		checkYAMLDocuments(t, page[f[2]:f[3]], startLine, triggerRunbookPath)
	}
}

// errReporter is the part of *testing.T that checkYAMLDocuments needs. The
// regression test below passes a recorder instead of a *testing.T, so a
// document it deliberately breaks fails only that recorder, not the whole
// build.
type errReporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// checkYAMLDocuments walks a fence body as a real YAML document stream,
// using the same reader kubectl-style tooling uses to split one, and reports
// every document that is not something kubectl apply could read. Splitting
// on the literal "\n---\n" missed separators like "--- # next document" or
// one with trailing whitespace, which left the next document glued onto the
// previous one and past this check unseen.
//
// The reader does not hand back a byte offset per document, so the reported
// line number is approximate: it adds up the newlines inside every document
// already read plus one line for every separator already crossed. That is
// exact when each document is separated by a single "---" line, which is
// the only style trigger-migration.md uses today; a run of several
// separators in a row, or a leading separator before the first document,
// can shift a later report by a line or two.
func checkYAMLDocuments(t errReporter, body string, startLine int, label string) {
	t.Helper()
	reader := k8syaml.NewYAMLReader(bufio.NewReader(strings.NewReader(body)))
	consumedLines := 0
	docIndex := 0
	for {
		doc, err := reader.Read()
		if err != nil && err != io.EOF {
			t.Errorf("%s: %v", label, err)
			return
		}
		atEOF := err == io.EOF
		if len(doc) == 0 && atEOF {
			break
		}
		line := startLine + consumedLines + docIndex
		var obj map[string]any
		if uerr := yaml.Unmarshal(doc, &obj); uerr != nil {
			t.Errorf("%s: document at line %d: %v", label, line, uerr)
		} else if obj == nil {
			// An empty or comment-only document unmarshals to nil without an
			// error, which is what a stray trailing --- leaves behind.
			t.Errorf("%s: document at line %d is empty", label, line)
		} else if obj["kind"] == nil || obj["apiVersion"] == nil {
			t.Errorf("%s: document at line %d has no kind or apiVersion", label, line)
		}
		consumedLines += strings.Count(string(doc), "\n")
		docIndex++
		if atEOF {
			break
		}
	}
}

// recordingReporter is an errReporter that keeps every message instead of
// failing a test, so a check that deliberately hits a bad document can be
// asserted on without a *testing.T of its own.
type recordingReporter struct {
	errors []string
}

func (r *recordingReporter) Helper() {}

func (r *recordingReporter) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// TestTriggerRunbookYAMLParsesCatchesCommentedSeparator is a regression case
// for the literal "\n---\n" split checkYAMLDocuments replaced. A separator
// with a trailing comment, and a second document missing kind, both need to
// still be caught rather than silently merged into the first document.
func TestTriggerRunbookYAMLParsesCatchesCommentedSeparator(t *testing.T) {
	body := "kind: ServiceAccount\napiVersion: v1\n" +
		"--- # next document\n" +
		"apiVersion: v1\nmetadata:\n  name: bad\n"
	rec := &recordingReporter{}
	checkYAMLDocuments(rec, body, 1, "regression body")
	if len(rec.errors) == 0 {
		t.Fatalf("expected the second document (missing kind) to be reported, but the check recorded nothing")
	}
}
