package buildconfig

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const adrDir = "../docs/adr"

// TestADRsAreWellFormed fails when a decision record under docs/adr is
// missing a part every record must have, or is not listed in the index. It
// checks shape only; nothing checks that a new decision gets a record.
func TestADRsAreWellFormed(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(adrDir, "[0-9][0-9][0-9][0-9]-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no decision records under %s", adrDir)
	}
	index, err := os.ReadFile(filepath.Join(adrDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	title := regexp.MustCompile(`(?m)^# ADR-(\d{4}): \S`)
	for _, f := range files {
		name := filepath.Base(f)
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc := string(body)
		m := title.FindStringSubmatch(doc)
		if m == nil {
			t.Errorf("%s: first heading must be '# ADR-NNNN: <title>'", name)
		} else if !strings.HasPrefix(name, m[1]+"-") {
			t.Errorf("%s: heading says ADR-%s but the file name says otherwise", name, m[1])
		}
		if !regexp.MustCompile(`(?m)^Status: (accepted|proposed|superseded)`).MatchString(doc) {
			t.Errorf("%s: missing a 'Status: accepted|proposed|superseded' line", name)
		}
		for _, section := range []string{"## Context", "## Decision", "## Rules", "## Consequences"} {
			if !strings.Contains(doc, "\n"+section+"\n") {
				t.Errorf("%s: missing the %q section", name, section)
			}
		}
		if !strings.Contains(string(index), "("+name+")") {
			t.Errorf("%s is not linked from %s/README.md", name, adrDir)
		}
	}
}

// TestNoDirectWarnLoggingInConverter pins ADR-0003: a dropped or degraded
// field is recorded through warnf (or recordWarning), never logged straight
// to the Converter's logger, because a direct log call would not count toward
// the outcome or reach the warnings annotation.
func TestNoDirectWarnLoggingInConverter(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				// warnf itself is the sanctioned caller of c.Log.Warn.
				if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "warnf" {
					return false
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				method, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !strings.HasPrefix(method.Sel.Name, "Warn") {
					return true
				}
				logField, ok := method.X.(*ast.SelectorExpr)
				if !ok || logField.Sel.Name != "Log" {
					return true
				}
				if recv, ok := logField.X.(*ast.Ident); ok && recv.Name == "c" {
					t.Errorf("%s: c.Log.%s bypasses the outcome model; use c.warnf (ADR-0003)", fset.Position(call.Pos()), method.Sel.Name)
				}
				return true
			})
		}
	}
}
