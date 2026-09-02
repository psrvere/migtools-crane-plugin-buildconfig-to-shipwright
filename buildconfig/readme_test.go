package buildconfig

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

const readmePath = "../README.md"

// optionalFlagsExample matches a --optional-flags value in the README. Only the
// single-quoted form is accepted: the value is a JSON object, so it is full of
// double quotes, and a double-quoted shell form would have to escape every one
// of them. Requiring one form keeps the examples copy-pasteable and keeps this
// pattern honest. [^\S\n] rather than \s so a match cannot run past the end of
// the line and pair with a quote in the next code block.
var optionalFlagsExample = regexp.MustCompile(`--optional-flags[^\S\n]+'([^']*)'`)

// TestReadmeOptionalFlagsAreValidJSON fails when a --optional-flags example in
// the README is not a JSON object of strings, which is what crane parses. The
// README once showed a key=value form that crane rejects.
func TestReadmeOptionalFlagsAreValidJSON(t *testing.T) {
	readme := readFile(t, readmePath)

	// Every occurrence has to be one this test can read. An example the pattern
	// skips is an example nothing checks, so count the mentions and require a
	// match for each.
	mentions := strings.Count(readme, "--optional-flags ")
	matches := optionalFlagsExample.FindAllStringSubmatch(readme, -1)
	if len(matches) == 0 {
		t.Fatalf("%s has no single-quoted --optional-flags example", readmePath)
	}
	if len(matches) != mentions {
		t.Errorf("%s has %d --optional-flags mentions but %d are single-quoted; write every example as --optional-flags '{...}'",
			readmePath, mentions, len(matches))
	}

	known := map[string]bool{}
	for _, f := range (&BuildConfigTransformPlugin{}).Metadata().OptionalFields {
		known[f.FlagName] = true
	}
	documented := map[string]bool{}
	for _, m := range matches {
		raw := m[1]
		var flags map[string]string
		if err := json.Unmarshal([]byte(raw), &flags); err != nil {
			t.Errorf("%s: --optional-flags value is not a JSON object of strings: %s\n  %v", readmePath, raw, err)
			continue
		}
		if flags == nil {
			t.Errorf("%s: --optional-flags value %q parses to no object", readmePath, raw)
			continue
		}
		for k := range flags {
			if !known[k] {
				t.Errorf("%s: --optional-flags example uses %q, which is not a flag the plugin declares", readmePath, k)
			}
			documented[k] = true
		}
	}

	// The other direction: a flag the plugin declares but the README never
	// names is a flag nobody can find.
	for flag := range known {
		if !strings.Contains(readme, "`"+flag+"`") {
			t.Errorf("%s does not document the %q flag the plugin declares", readmePath, flag)
		}
	}
}

// TestReadmeVersionsMatchPins fails when a version the README quotes drifts
// from where it is pinned: the Go version in go.mod, the Shipwright version in
// go.mod and the Minikube setup script, and the crane commit in the CI
// workflow. Every check reports independently, so one drift does not hide
// another.
func TestReadmeVersionsMatchPins(t *testing.T) {
	readme := readFile(t, readmePath)
	gomod := readFile(t, "../go.mod")

	t.Run("go", func(t *testing.T) {
		full, ok := firstMatch(t, gomod, `(?m)^go (\S+)`)
		if !ok {
			return
		}
		// go.mod pins a patch version; the README quotes major.minor. Match on a
		// word boundary so "Go 1.26" does not satisfy a go.mod asking for 1.260.
		minor := regexp.MustCompile(`^\d+\.\d+`).FindString(full)
		if !regexp.MustCompile(`Go ` + regexp.QuoteMeta(minor) + `\b`).MatchString(readme) {
			t.Errorf("%s does not say \"Go %s\"; go.mod requires %s", readmePath, minor, full)
		}
	})

	shipwright, ok := firstMatch(t, gomod, `github.com/shipwright-io/build (v[\d.]+)`)
	t.Run("shipwright-setup-script", func(t *testing.T) {
		if !ok {
			return
		}
		setup := readFile(t, "../hack/setup-minikube-shipwright.sh")
		scripted, found := firstMatch(t, setup, `SHIPWRIGHT_VERSION:-(v[\d.]+)`)
		if found && scripted != shipwright {
			t.Errorf("hack/setup-minikube-shipwright.sh installs Shipwright %s but go.mod builds against %s", scripted, shipwright)
		}
	})
	t.Run("shipwright-readme", func(t *testing.T) {
		if ok && !strings.Contains(readme, "Shipwright "+shipwright) {
			t.Errorf("%s does not mention Shipwright %s, the version go.mod builds against", readmePath, shipwright)
		}
	})

	t.Run("crane-pin", func(t *testing.T) {
		workflow := readFile(t, "../.github/workflows/test-e2e-minikube-pr.yml")
		// Exactly one `git checkout <sha>` is expected, the crane pin. A second
		// one would make an unanchored first-match silently guard the wrong repo.
		pins := regexp.MustCompile(`git checkout ([0-9a-f]{40})`).FindAllStringSubmatch(workflow, -1)
		if len(pins) != 1 {
			t.Fatalf("expected exactly one `git checkout <sha>` in the CI workflow, found %d; anchor this check to the crane clone", len(pins))
		}
		if !strings.Contains(readme, pins[0][1]) {
			t.Errorf("%s does not name the crane commit CI pins, %s", readmePath, pins[0][1])
		}
	})
}

// firstMatch reports the first capture of pattern in s. It returns false rather
// than aborting, so a pattern that stops matching does not hide the checks that
// come after it.
func firstMatch(t *testing.T, s, pattern string) (string, bool) {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(s)
	if m == nil {
		t.Errorf("pattern %q not found", pattern)
		return "", false
	}
	return m[1], true
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
