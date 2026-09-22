//go:build !documentation

package buildconfig

import (
	"strings"
	"testing"
)

const (
	dockerEnvWarnMarker = "sets dockerStrategy.env"
	sourceEnvWarnMarker = "sets sourceStrategy.env"
)

// strategyEnvSpec is a Parallel-runPolicy BuildConfig with a DockerImage output
// and a push secret, so the only strategy-dependent warnings are the env ones.
func strategyEnvSpec(strategy, source string) string {
	return `{
		"runPolicy": "Parallel",
		"source": ` + source + `,
		"strategy": ` + strategy + `,
		"output": {"to": {"kind": "DockerImage", "name": "quay.io/example/app:latest"}, "pushSecret": {"name": "push"}}
	}`
}

const gitSource = `{"type": "Git", "git": {"uri": "https://github.com/example/app.git"}}`

// BUILD-2476: spec.env keeps every entry, and one warning per strategy names
// each entry without its value.
func TestConvertStrategyEnvWarns(t *testing.T) {
	env := `[
		{"name": "ARTIFACT_URL", "value": "https://example.com/a.jar"},
		{"name": "TOKEN", "valueFrom": {"secretKeyRef": {"name": "creds", "key": "token"}}}
	]`
	tests := []struct {
		name     string
		strategy string
		marker   string
		other    string
	}{
		{"docker", `{"type": "Docker", "dockerStrategy": {"env": ` + env + `}}`, dockerEnvWarnMarker, sourceEnvWarnMarker},
		{"source", `{"type": "Source", "sourceStrategy": {"from": {"kind": "DockerImage", "name": "registry.example.com/builder:1"}, "env": ` + env + `}}`, sourceEnvWarnMarker, dockerEnvWarnMarker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, outcome, warns := convertOutputSpec(t, strategyEnvSpec(tt.strategy, gitSource), PluginOptionalFields{})

			if n := countContaining(warns, tt.marker); n != 1 {
				t.Fatalf("%q warnings = %d, want 1 (%v)", tt.marker, n, warns)
			}
			if n := countContaining(warns, tt.other); n != 0 {
				t.Errorf("%q warnings = %d, want 0 (%v)", tt.other, n, warns)
			}
			for _, w := range warns {
				if !strings.Contains(w, tt.marker) {
					continue
				}
				if !strings.Contains(w, "ARTIFACT_URL, TOKEN") {
					t.Errorf("warning does not name both entries: %s", w)
				}
				if strings.Contains(w, "example.com/a.jar") || strings.Contains(w, "creds") {
					t.Errorf("warning leaks an entry's value or source: %s", w)
				}
			}
			if outcome.State != OutcomeConvertedWithWarnings {
				t.Errorf("outcome = %s, want %s", outcome.State, OutcomeConvertedWithWarnings)
			}
			if len(b.Spec.Env) != 2 || b.Spec.Env[0].Name != "ARTIFACT_URL" || b.Spec.Env[1].Name != "TOKEN" {
				t.Errorf("spec.env = %+v, want both entries in order", b.Spec.Env)
			}
		})
	}
}

// Neither warning fires without strategy env, including when git proxy
// settings put HTTP_PROXY and friends into spec.env.
func TestConvertStrategyEnvNoWarning(t *testing.T) {
	proxySource := `{"type": "Git", "git": {"uri": "https://github.com/example/app.git", "httpProxy": "http://proxy:3128"}}`
	tests := []struct {
		name     string
		strategy string
		source   string
	}{
		{"docker without env", `{"type": "Docker", "dockerStrategy": {}}`, gitSource},
		{"docker with empty env", `{"type": "Docker", "dockerStrategy": {"env": []}}`, gitSource},
		{"source without env", `{"type": "Source", "sourceStrategy": {"from": {"kind": "DockerImage", "name": "registry.example.com/builder:1"}}}`, gitSource},
		{"docker with git proxy only", `{"type": "Docker", "dockerStrategy": {}}`, proxySource},
		{"source with git proxy only", `{"type": "Source", "sourceStrategy": {"from": {"kind": "DockerImage", "name": "registry.example.com/builder:1"}}}`, proxySource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, warns := convertOutputSpec(t, strategyEnvSpec(tt.strategy, tt.source), PluginOptionalFields{})
			for _, marker := range []string{dockerEnvWarnMarker, sourceEnvWarnMarker} {
				if n := countContaining(warns, marker); n != 0 {
					t.Errorf("%q warnings = %d, want 0 (%v)", marker, n, warns)
				}
			}
		})
	}
}

const forbiddenEnvWarnMarker = "a name Shipwright forbids for security reasons"

// BUILD-2334: Shipwright v0.21.0 leaves a Build unregistered when spec.env
// carries a name on its blocklist. The entry stays in the Build and gets its
// own warning, which names the entry and never its value.
func TestConvertForbiddenStrategyEnvWarns(t *testing.T) {
	env := `[
		{"name": "NODE_OPTIONS", "value": "--max-old-space-size=4096"},
		{"name": "LD_LIBRARY_PATH", "value": "/opt/app/lib"},
		{"name": "APP_MODE", "value": "production"}
	]`
	tests := []struct {
		name     string
		strategy string
		field    string
	}{
		{"docker", `{"type": "Docker", "dockerStrategy": {"env": ` + env + `}}`, "dockerStrategy.env"},
		{"source", `{"type": "Source", "sourceStrategy": {"from": {"kind": "DockerImage", "name": "registry.example.com/builder:1"}, "env": ` + env + `}}`, "sourceStrategy.env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, outcome, warns := convertOutputSpec(t, strategyEnvSpec(tt.strategy, gitSource), PluginOptionalFields{})

			if n := countContaining(warns, forbiddenEnvWarnMarker); n != 2 {
				t.Fatalf("forbidden-name warnings = %d, want 2 (%v)", n, warns)
			}
			for _, name := range []string{"NODE_OPTIONS", "LD_LIBRARY_PATH"} {
				want := tt.field + " " + name + ", " + forbiddenEnvWarnMarker
				if countContaining(warns, want) != 1 {
					t.Errorf("no warning names %s under %s (%v)", name, tt.field, warns)
				}
			}
			for _, w := range warns {
				if !strings.Contains(w, forbiddenEnvWarnMarker) {
					continue
				}
				if strings.Contains(w, "max-old-space-size") || strings.Contains(w, "/opt/app/lib") {
					t.Errorf("warning leaks an entry's value: %s", w)
				}
				if strings.Contains(w, "APP_MODE") {
					t.Errorf("warning names an allowed entry: %s", w)
				}
			}

			// The entry is kept, so the operator can see what to act on.
			if len(b.Spec.Env) != 3 {
				t.Fatalf("spec.env = %+v, want all three entries", b.Spec.Env)
			}
			if b.Spec.Env[0].Name != "NODE_OPTIONS" || b.Spec.Env[1].Name != "LD_LIBRARY_PATH" || b.Spec.Env[2].Name != "APP_MODE" {
				t.Errorf("spec.env = %+v, want the three entries in order", b.Spec.Env)
			}
			if outcome.State != OutcomeConvertedWithWarnings {
				t.Errorf("outcome = %s, want %s", outcome.State, OutcomeConvertedWithWarnings)
			}
		})
	}
}

// Strategy env Shipwright allows draws no forbidden-name warning, and neither
// do the proxy variables the plugin writes into spec.env itself.
func TestConvertAllowedStrategyEnvNoForbiddenWarning(t *testing.T) {
	proxySource := `{"type": "Git", "git": {"uri": "https://example.com/app.git", "httpProxy": "http://proxy:3128", "httpsProxy": "http://proxy:3128", "noProxy": "example.com"}}`
	allowed := `[{"name": "ENVIRONMENT", "value": "prod"}, {"name": "LDAP_URL", "value": "ldap://example.com"}, {"name": "PERL_VERSION", "value": "5"}]`
	tests := []struct {
		name     string
		strategy string
		source   string
	}{
		{"docker with allowed env", `{"type": "Docker", "dockerStrategy": {"env": ` + allowed + `}}`, gitSource},
		{"source with allowed env", `{"type": "Source", "sourceStrategy": {"from": {"kind": "DockerImage", "name": "registry.example.com/builder:1"}, "env": ` + allowed + `}}`, gitSource},
		{"docker with git proxy only", `{"type": "Docker", "dockerStrategy": {}}`, proxySource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, warns := convertOutputSpec(t, strategyEnvSpec(tt.strategy, tt.source), PluginOptionalFields{})
			if n := countContaining(warns, forbiddenEnvWarnMarker); n != 0 {
				t.Errorf("forbidden-name warnings = %d, want 0 (%v)", n, warns)
			}
		})
	}
}

// The blocklist is matched the way Shipwright matches it: the two starred
// entries are prefixes, every other entry is exact.
func TestIsForbiddenEnvVar(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"NODE_OPTIONS", true},
		{"RUBYOPT", true},
		{"ENV", true},
		{"PERL5LIB", true},
		{"LD_PRELOAD", true},
		{"LD_ANYTHING_ELSE", true},
		{"BASH_FUNC_deploy%%", true},
		{"ENVIRONMENT", false},
		{"NODE_OPTIONS_EXTRA", false},
		{"MY_LD_PRELOAD", false},
		{"PERL_VERSION", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isForbiddenEnvVar(tt.name); got != tt.want {
			t.Errorf("isForbiddenEnvVar(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
