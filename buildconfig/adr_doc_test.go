//go:build documentation

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

var (
	// adrTitle is anchored to the start of the file, so a record whose first
	// line is not the title fails even if a conforming heading appears later.
	adrTitle  = regexp.MustCompile(`\A# ADR-(\d{4}): \S`)
	adrStatus = regexp.MustCompile(`(?m)^Status: (accepted|proposed|superseded)`)
	// adrLink matches the target of a markdown link to a .md file, with or
	// without a fragment.
	adrLink   = regexp.MustCompile(`\]\(([^)#\s]+\.md)(?:#[^)]*)?\)`)
	adrRecord = regexp.MustCompile(`\A\d{4}-.*\.md\z`)
)

// TestADRsAreWellFormed fails when a decision record under docs/adr is
// missing a part every record must have, when the index and the records
// disagree about which records exist, or when a relative link in any of them
// points at a file that is not there. It checks shape only; nothing checks
// that a new decision gets a record.
func TestADRsAreWellFormed(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(adrDir, "[0-9][0-9][0-9][0-9]-*.md"))
	if err != nil {
		t.Fatalf("glob %s: %v", adrDir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no decision records under %s", adrDir)
	}
	indexPath := filepath.Join(adrDir, "README.md")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading %s: %v", indexPath, err)
	}
	index := string(indexBytes)

	records := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		records[name] = true
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		doc := string(body)
		m := adrTitle.FindStringSubmatch(doc)
		if m == nil {
			t.Errorf("%s: the file must open with '# ADR-NNNN: <title>'", name)
		} else if !strings.HasPrefix(name, m[1]+"-") {
			t.Errorf("%s: heading says ADR-%s but the file name says otherwise", name, m[1])
		}
		if !adrStatus.MatchString(doc) {
			t.Errorf("%s: missing a 'Status: accepted|proposed|superseded' line", name)
		}
		for _, section := range []string{"## Context", "## Decision", "## Rules", "## Consequences"} {
			if !strings.Contains(doc, "\n"+section+"\n") {
				t.Errorf("%s: missing the %q section", name, section)
			}
		}
		if !strings.Contains(index, "("+name+")") {
			t.Errorf("%s is not linked from %s", name, indexPath)
		}
		checkRelativeLinks(t, name, doc)
	}

	// Index to records: every record the index links must exist, so a
	// removed or renumbered record cannot leave a stale row behind.
	for _, m := range adrLink.FindAllStringSubmatch(index, -1) {
		if adrRecord.MatchString(m[1]) && !records[m[1]] {
			t.Errorf("%s links %s, which does not exist", indexPath, m[1])
		}
	}
	checkRelativeLinks(t, "README.md", index)
}

// checkRelativeLinks fails for every relative markdown link in doc whose
// target is missing from disk, so a dead cross-reference cannot merge green.
func checkRelativeLinks(t *testing.T, name, doc string) {
	t.Helper()
	for _, m := range adrLink.FindAllStringSubmatch(doc, -1) {
		target := m[1]
		if strings.Contains(target, "://") {
			continue
		}
		if _, err := os.Stat(filepath.Join(adrDir, target)); err != nil {
			t.Errorf("%s links %s, which does not exist: %v", name, target, err)
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
		t.Fatalf("parsing package: %v", err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, pos := range directWarnCalls(file) {
				t.Errorf("%s: a Warn call on the Converter's logger bypasses the outcome model; use c.warnf (ADR-0003)", fset.Position(pos))
			}
		}
	}
}

// TestDirectWarnCallsCatchEveryShape pins the guard's own coverage. Each line
// marked "// drop" is a shape a bypass could take and must be caught; the
// unmarked lines are the sanctioned paths and must not be.
func TestDirectWarnCallsCatchEveryShape(t *testing.T) {
	const src = `package buildconfig

import "github.com/sirupsen/logrus"

type Converter struct{ Log logrus.FieldLogger }

func (c *Converter) warnf(format string, args ...interface{}) { c.Log.Warn(format) }
func (c *Converter) recordWarning(msg string) string          { return msg }
func (c *Converter) plain()                                   { c.Log.Warnf("dropped %s", "x") } // drop
func (c *Converter) noF()                                     { c.Log.Warn("dropped") } // drop
func (conv *Converter) renamed()                              { conv.Log.Warnf("dropped") } // drop
func (c *Converter) chained()                                 { c.Log.WithField("bc", "x").Warnf("dropped") } // drop
func (c *Converter) chainedTwice()                            { c.Log.WithField("bc", "x").WithError(nil).Warning("dropped") } // drop
func (c *Converter) inClosure()                               { func() { c.Log.Warnln("dropped") }() } // drop
func helper(cv *Converter)                                    { cv.Log.Warnf("dropped") } // drop
func (c *Converter) sanctioned()                              { c.warnf("dropped %s", "x") }
func (c *Converter) errorLevel()                              { c.Log.Error(c.recordWarning("x")) }
func (c *Converter) info()                                    { c.Log.Infof("fine") }
func unrelated(l logrus.FieldLogger)                          { l.Warn("not the converter") }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	want := map[int]bool{}
	for i, line := range strings.Split(src, "\n") {
		if strings.HasSuffix(line, "// drop") {
			want[i+1] = true
		}
	}
	got := map[int]bool{}
	for _, pos := range directWarnCalls(file) {
		got[fset.Position(pos).Line] = true
	}
	for line := range want {
		if !got[line] {
			t.Errorf("fixture line %d is a bypass the guard did not catch", line)
		}
	}
	for line := range got {
		if !want[line] {
			t.Errorf("fixture line %d is sanctioned but the guard flagged it", line)
		}
	}
}

// directWarnCalls returns the position of every Warn* call on a Converter's
// Log field, outside warnf itself. The Converter is found by type, not by
// receiver name: every function whose receiver or parameter is *Converter
// contributes that identifier, so conv.Log.Warnf and a helper taking a
// *Converter are caught alongside c.Log.Warnf. A chained call such as
// c.Log.WithField(...).Warnf is caught by walking the selector chain back to
// the Log field. What it cannot see is a local alias (log := c.Log), which
// nothing in the package does.
func directWarnCalls(file *ast.File) []token.Pos {
	var out []token.Pos
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name == "warnf" {
			continue
		}
		converters := converterIdents(fn)
		if len(converters) == 0 {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(sel.Sel.Name, "Warn") {
				return true
			}
			if converters[logOwner(sel.X)] {
				out = append(out, call.Pos())
			}
			return true
		})
	}
	return out
}

// converterIdents returns the names fn binds to a *Converter or Converter in
// its receiver and parameter lists.
func converterIdents(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	var fields []*ast.Field
	if fn.Recv != nil {
		fields = append(fields, fn.Recv.List...)
	}
	if fn.Type.Params != nil {
		fields = append(fields, fn.Type.Params.List...)
	}
	for _, f := range fields {
		typ := f.Type
		if star, ok := typ.(*ast.StarExpr); ok {
			typ = star.X
		}
		if id, ok := typ.(*ast.Ident); ok && id.Name == "Converter" {
			for _, name := range f.Names {
				names[name.Name] = true
			}
		}
	}
	return names
}

// logOwner walks a selector chain such as c.Log, c.Log.WithField(...) or
// c.Log.WithFields(...).WithError(...) back to the identifier that owns the
// Log field. It returns "" when the chain does not pass through a Log field.
func logOwner(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.CallExpr:
			e = v.Fun
		case *ast.SelectorExpr:
			if v.Sel.Name == "Log" {
				if id, ok := v.X.(*ast.Ident); ok {
					return id.Name
				}
				return ""
			}
			e = v.X
		default:
			return ""
		}
	}
}
