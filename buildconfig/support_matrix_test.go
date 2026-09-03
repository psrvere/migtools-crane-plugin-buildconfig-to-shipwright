package buildconfig

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const supportMatrixPath = "../docs/support-matrix.md"

// TestSupportMatrixCoversEveryWarning keeps docs/support-matrix.md and the
// warnings in this package in step, in both directions:
//
//   - every warning template the code can emit appears in the doc, so a new
//     or reworded warning without a row fails the build;
//   - every warning quoted in the doc's warning reference matches a template
//     that still exists in the code, so a row for a deleted warning fails;
//   - the warning reference numbers its rows W1 to Wn with no gap and no
//     repeat, so a deleted or duplicated row fails;
//   - every OutcomeState value and every *Annotation constant appears in the
//     doc.
//
// A template is the format string handed to warnf or recordWarning, the
// Sprintf handed to outcomeFailed or outcomeSkipped, a string assigned to a
// variable named msg, warning or reason, the format string of every fmt.Errorf
// in the package (their text reaches outcomeFailed through err.Error(), or is
// embedded in a warning), and the Sprintf inside resolveImageRef and
// omittedWarningsNotice. Format verbs are replaced by an ellipsis before
// comparing, which is how the doc quotes them. The test proves completeness,
// not correctness: a row can quote the right warning and describe the wrong
// behaviour.
func TestSupportMatrixCoversEveryWarning(t *testing.T) {
	doc := readSupportMatrix(t)
	src := parseNonTestFiles(t)

	templates := collectWarningTemplates(src)
	if len(templates) < 40 {
		t.Fatalf("collected only %d warning templates; the collector is probably broken", len(templates))
	}

	// Code to doc.
	for _, tmpl := range templates {
		if !strings.Contains(doc, tmpl) {
			t.Errorf("warning has no row in %s:\n  %s", supportMatrixPath, tmpl)
		}
	}

	// Doc to code.
	quoted := quotedWarnings(t, doc)
	for id, text := range quoted {
		if !matchesAnyTemplate(text, templates) {
			t.Errorf("%s quotes %s, which no longer matches any warning in the code:\n  %s", supportMatrixPath, id, text)
		}
	}

	// Outcome states and annotation keys.
	for name, value := range src.stringConsts {
		isOutcome := strings.HasPrefix(name, "Outcome")
		isAnnotation := strings.HasSuffix(name, "Annotation")
		if (isOutcome || isAnnotation) && !strings.Contains(doc, value) {
			t.Errorf("%s does not mention %s (%s = %q)", supportMatrixPath, value, name, value)
		}
	}
}

// parsedPackage is the non-test source of this package plus its string
// constants, resolved so a template referenced by name can be looked up.
type parsedPackage struct {
	files        []*ast.File
	stringConsts map[string]string
}

func parseNonTestFiles(t *testing.T) *parsedPackage {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &parsedPackage{stringConsts: map[string]string{}}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			p.files = append(p.files, f)
		}
	}
	// Two passes so a const defined after its use still resolves.
	for _, f := range p.files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := p.stringOf(vs.Values[i]); ok {
							p.stringConsts[name.Name] = s
						}
					}
				}
			}
		}
	}
	return p
}

// stringOf evaluates a string literal, a concatenation of string literals,
// or a reference to a string constant.
func (p *parsedPackage) stringOf(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok1 := p.stringOf(v.X)
		r, ok2 := p.stringOf(v.Y)
		return l + r, ok1 && ok2
	case *ast.Ident:
		s, ok := p.stringConsts[v.Name]
		return s, ok
	case *ast.ParenExpr:
		return p.stringOf(v.X)
	}
	return "", false
}

var formatVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func normalizeTemplate(s string) string {
	return formatVerb.ReplaceAllString(s, "…")
}

// isFmtCall reports whether call is fmt.<name>(...).
func isFmtCall(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt" && sel.Sel.Name == name
}

func collectWarningTemplates(p *parsedPackage) []string {
	seen := map[string]bool{}
	var out []string
	add := func(e ast.Expr) {
		s, ok := p.stringOf(e)
		if !ok {
			return
		}
		n := normalizeTemplate(s)
		if n == "…" || !strings.ContainsAny(n, "abcdefghijklmnopqrstuvwxyz") || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	addSprintfArg := func(e ast.Expr) {
		if call, ok := e.(*ast.CallExpr); ok && isFmtCall(call, "Sprintf") && len(call.Args) > 0 {
			add(call.Args[0])
		}
	}

	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				switch fn := v.Fun.(type) {
				case *ast.SelectorExpr:
					if isFmtCall(v, "Errorf") && len(v.Args) > 0 {
						add(v.Args[0])
					}
					if (fn.Sel.Name == "warnf" || fn.Sel.Name == "recordWarning") && len(v.Args) > 0 {
						if s, ok := p.stringOf(v.Args[0]); ok && s == "%s" && len(v.Args) > 1 {
							add(v.Args[1])
						} else {
							add(v.Args[0])
						}
					}
				case *ast.Ident:
					if (fn.Name == "outcomeFailed" || fn.Name == "outcomeSkipped") && len(v.Args) > 0 {
						addSprintfArg(v.Args[0])
					}
				}
			case *ast.AssignStmt:
				if len(v.Lhs) != 1 || len(v.Rhs) != 1 {
					return true
				}
				lhs, ok := v.Lhs[0].(*ast.Ident)
				if !ok || (lhs.Name != "msg" && lhs.Name != "warning" && lhs.Name != "reason") {
					return true
				}
				// A Sprintf contributes its format string; a literal or a
				// += suffix contributes itself.
				if call, ok := v.Rhs[0].(*ast.CallExpr); ok && isFmtCall(call, "Sprintf") {
					addSprintfArg(call)
				} else {
					add(v.Rhs[0])
				}
			case *ast.FuncDecl:
				if v.Name.Name == "resolveImageRef" || v.Name.Name == "omittedWarningsNotice" {
					ast.Inspect(v.Body, func(n ast.Node) bool {
						if call, ok := n.(*ast.CallExpr); ok && isFmtCall(call, "Sprintf") {
							add(call.Args[0])
						}
						return true
					})
				}
			}
			return true
		})
	}
	return out
}

// quotedWarnings returns the first backtick-quoted string of every row in
// the doc's "Warning reference" table, keyed by its W-number. A row with no
// quote is an error unless it is a known prose row, so a row that loses its
// backticks cannot drop out of the check unnoticed. Every number from W1 to
// the highest row must appear exactly once, so a deleted row, even a prose
// one, or a duplicated number fails here instead of vanishing from the map.
func quotedWarnings(t *testing.T, doc string) map[string]string {
	t.Helper()
	_, section, found := strings.Cut(doc, "## Warning reference")
	if !found {
		t.Fatalf("%s has no '## Warning reference' section", supportMatrixPath)
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	row := regexp.MustCompile("(?m)^\\| W(\\d+) \\| ([^\n]*)$")
	quote := regexp.MustCompile("`([^`]+)`")
	prose := map[string]bool{"W33": true}
	out := map[string]string{}
	rows := map[int]int{}
	highest := 0
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("%s row W%s: %v", supportMatrixPath, m[1], err)
		}
		id := "W" + m[1]
		rows[n]++
		if n > highest {
			highest = n
		}
		q := quote.FindStringSubmatch(m[2])
		switch {
		case q != nil:
			out[id] = q[1]
		case !prose[id]:
			t.Errorf("%s row %s quotes no warning and is not a known prose row", supportMatrixPath, id)
		}
	}
	if highest == 0 {
		t.Fatalf("%s has no rows in its warning reference table", supportMatrixPath)
	}
	for n := 1; n <= highest; n++ {
		if rows[n] != 1 {
			t.Errorf("%s has %d rows for W%d in the warning reference; want exactly one", supportMatrixPath, rows[n], n)
		}
	}
	return out
}

// matchesAnyTemplate reports whether a quoted warning is exactly one of the
// templates, allowing a template's ellipses to stand for any text. That lets
// a row quote a rendered form such as "Custom build strategy … — passing
// BuildConfig … through unchanged" against the template
// "… — passing BuildConfig … through unchanged".
func matchesAnyTemplate(text string, templates []string) bool {
	for _, tmpl := range templates {
		if text == tmpl {
			return true
		}
		parts := strings.Split(tmpl, "…")
		for i, part := range parts {
			parts[i] = regexp.QuoteMeta(part)
		}
		if regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(text) {
			return true
		}
	}
	return false
}

func readSupportMatrix(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(supportMatrixPath)
	if err != nil {
		t.Fatalf("read %s: %v", supportMatrixPath, err)
	}
	return string(b)
}
