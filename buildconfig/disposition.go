package buildconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// maxConversionReasonBytes caps the reason recorded on a passed-through
// BuildConfig. Reasons are short by construction, but a failure reason can wrap
// an arbitrary error string, and an oversized annotation would make the
// BuildConfig itself unappliable. The full reason is always logged.
const maxConversionReasonBytes = 4 << 10

// dispositionPatch builds the JSON patch that stamps a skipped or failed
// outcome onto the BuildConfig the plugin is passing through unchanged.
//
// Without it a BuildConfig the plugin declined to convert is indistinguishable
// in the migrated output from one the plugin never received: the reason lives
// only in the run log, which is the thing an audit is trying not to read
// (BUILD-2319).
//
// The patch shape depends on the source object. RFC 6902 "add" against
// /metadata/annotations/<key> requires the annotations map to exist already, so
// a BuildConfig without one gets a single op adding the whole map instead. That
// also avoids replacing annotations the BuildConfig already carries.
func dispositionPatch(u unstructured.Unstructured, outcome Outcome) (jsonpatch.Patch, error) {
	annotations := map[string]string{
		ConversionOutcomeAnnotation: string(outcome.State),
	}
	if reason := truncateReason(outcome.Reason); reason != "" {
		annotations[ConversionReasonAnnotation] = reason
	}

	var ops []map[string]interface{}
	if len(u.GetAnnotations()) == 0 {
		// No annotations map on the object: create it whole. "add" on a missing
		// object member creates it, and metadata itself always exists.
		ops = append(ops, map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations",
			"value": annotations,
		})
	} else {
		// Deterministic order so the emitted patch is stable across runs. The
		// outcome is always set; the reason only when non-empty.
		ops = append(ops, map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations/" + escapeJSONPointer(ConversionOutcomeAnnotation),
			"value": annotations[ConversionOutcomeAnnotation],
		})
		if reason, ok := annotations[ConversionReasonAnnotation]; ok {
			ops = append(ops, map[string]interface{}{
				"op":    "add",
				"path":  "/metadata/annotations/" + escapeJSONPointer(ConversionReasonAnnotation),
				"value": reason,
			})
		}
	}

	raw, err := json.Marshal(ops)
	if err != nil {
		return nil, fmt.Errorf("marshaling disposition patch: %w", err)
	}
	patch, err := jsonpatch.DecodePatch(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding disposition patch: %w", err)
	}
	return patch, nil
}

// escapeJSONPointer escapes a JSON Pointer reference token per RFC 6901: "~"
// becomes "~0" and "/" becomes "~1". Annotation keys are domain-prefixed and
// always contain a "/", so skipping this would address the wrong path.
func escapeJSONPointer(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}

// truncateReason bounds a reason to maxConversionReasonBytes, trimming back to
// a rune boundary so the annotation value stays valid UTF-8. The ellipsis is
// counted against the cap, so the returned string never exceeds it.
func truncateReason(reason string) string {
	if len(reason) <= maxConversionReasonBytes {
		return reason
	}
	truncated := reason[:maxConversionReasonBytes-len("…")]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated + "…"
}
