//go:build !documentation

package buildconfig

import (
	"testing"

	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
)

// BUILD-2339: multiple overlapping mapping keys must resolve deterministically,
// with the longest (most specific) prefix winning on every invocation.
func TestApplyRegistryMappingLongestPrefixWins(t *testing.T) {
	mapping := map[string]string{
		"quay.io":           "short.example.com",
		"quay.io/team":      "long.example.com",
		"quay.io/team/deep": "longest.example.com",
	}
	// Repeat to catch map-iteration nondeterminism regressions: before the
	// fix the winner depended on random map order.
	for i := 0; i < 200; i++ {
		got := applyRegistryMapping("quay.io/team/app:v1", mapping)
		want := "long.example.com/app:v1"
		if got != want {
			t.Fatalf("iteration %d: got %q, want %q", i, got, want)
		}
		got = applyRegistryMapping("quay.io/team/deep/app:v1", mapping)
		want = "longest.example.com/app:v1"
		if got != want {
			t.Fatalf("iteration %d: got %q, want %q", i, got, want)
		}
		got = applyRegistryMapping("quay.io/other/app:v1", mapping)
		want = "short.example.com/other/app:v1"
		if got != want {
			t.Fatalf("iteration %d: got %q, want %q", i, got, want)
		}
	}
}

func TestApplyRegistryMappingNoMatchUnchanged(t *testing.T) {
	mapping := map[string]string{"quay.io/team": "new.example.com"}
	got := applyRegistryMapping("docker.io/library/alpine:3", mapping)
	if got != "docker.io/library/alpine:3" {
		t.Fatalf("unmatched ref was rewritten: %q", got)
	}
}

// A malformed --registry-mapping entry like "=newvalue" produces an empty
// key; it must be ignored rather than acting as a global rewrite.
func TestApplyRegistryMappingIgnoresEmptyKey(t *testing.T) {
	mapping := map[string]string{"": "evil.example.com"}
	got := applyRegistryMapping("quay.io/team/app:v1", mapping)
	if got != "quay.io/team/app:v1" {
		t.Fatalf("empty mapping key rewrote ref: %q", got)
	}
}

// A malformed --registry-mapping entry like "quay.io=" (or a bare "quay.io"
// token, which crane-lib parses to an empty value) must not produce an
// invalid ref like "/team/app:v1". The entry is skipped; a longer or other
// valid mapping may still apply.
func TestApplyRegistryMappingIgnoresEmptyValue(t *testing.T) {
	mapping := map[string]string{"quay.io": ""}
	got := applyRegistryMapping("quay.io/team/app:v1", mapping)
	if got != "quay.io/team/app:v1" {
		t.Fatalf("empty mapping value rewrote ref: %q", got)
	}

	// An empty-value entry must not shadow a valid shorter-prefix mapping:
	// the longest prefix is skipped and the next candidate still applies.
	mapping = map[string]string{
		"quay.io/team": "",
		"quay.io":      "new.example.com",
	}
	got = applyRegistryMapping("quay.io/team/app:v1", mapping)
	if got != "new.example.com/team/app:v1" {
		t.Fatalf("valid fallback mapping not applied: %q", got)
	}
}

// BUILD-2339: emitted resources must not carry serialization noise.
//   - no metadata.creationTimestamp (omitted by omitzero on metav1.Time)
//   - no empty status object (removed by stripSerializationNoise)
func TestToUnstructuredOmitsSerializationNoise(t *testing.T) {
	b := &shipwrightv1beta1.Build{}
	b.Name = "noise-check"
	b.Namespace = "default"

	u, err := toUnstructured(b)
	if err != nil {
		t.Fatalf("toUnstructured: %v", err)
	}

	if _, present := u.Object["status"]; present {
		t.Errorf("status key present in output: %v", u.Object["status"])
	}
	meta, ok := u.Object["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing or wrong type: %v", u.Object["metadata"])
	}
	if _, present := meta["creationTimestamp"]; present {
		t.Errorf("creationTimestamp present in output: %v", meta["creationTimestamp"])
	}
}

func TestToUnstructuredRendersRetentionSucceededLimit(t *testing.T) {
	limit := uint(5)
	b := &shipwrightv1beta1.Build{}
	b.Name = "history-app"
	b.Namespace = "default"
	b.Spec.Retention = &shipwrightv1beta1.BuildRetention{SucceededLimit: &limit}

	u, err := toUnstructured(b)
	if err != nil {
		t.Fatalf("toUnstructured: %v", err)
	}

	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec missing or wrong type: %v", u.Object["spec"])
	}
	retention, ok := spec["retention"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec.retention missing or wrong type: %v", spec["retention"])
	}
	// toUnstructured round-trips through JSON, so numeric fields surface as
	// float64; accept int64 too in case the conversion path changes.
	switch got := retention["succeededLimit"].(type) {
	case float64:
		if got != 5 {
			t.Fatalf("spec.retention.succeededLimit = %v, want 5", got)
		}
	case int64:
		if got != 5 {
			t.Fatalf("spec.retention.succeededLimit = %v, want 5", got)
		}
	default:
		t.Fatalf("spec.retention.succeededLimit = %v (%T), want numeric 5",
			retention["succeededLimit"], retention["succeededLimit"])
	}
}
