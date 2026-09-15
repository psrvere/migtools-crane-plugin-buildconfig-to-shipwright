//go:build !documentation

package buildconfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestProcessNodeSelector(t *testing.T) {
	// 64 characters — one over the label-value limit enforced by
	// validation.IsValidLabelValue, the same helper Shipwright's own
	// pkg/validate/nodeselector.go uses.
	tooLongValue := strings.Repeat("a", 64)

	tests := []struct {
		name         string
		nodeSelector buildv1.OptionalNodeSelector
		want         map[string]string
		wantLevel    logrus.Level
		wantPhrase   string
	}{
		{
			name:         "single entry maps to Build spec.nodeSelector",
			nodeSelector: buildv1.OptionalNodeSelector{"disktype": "ssd"},
			want:         map[string]string{"disktype": "ssd"},
			wantLevel:    logrus.InfoLevel,
			wantPhrase:   "Mapping nodeSelector",
		},
		{
			name: "multiple entries all map across",
			nodeSelector: buildv1.OptionalNodeSelector{
				"disktype":             "ssd",
				"topology.io/region":   "us-east-1",
				"node-role.io/builder": "",
			},
			want: map[string]string{
				"disktype":             "ssd",
				"topology.io/region":   "us-east-1",
				"node-role.io/builder": "",
			},
			wantLevel:  logrus.InfoLevel,
			wantPhrase: "Mapping nodeSelector",
		},
		{
			name:         "nil nodeSelector leaves the field unset",
			nodeSelector: nil,
			want:         nil,
		},
		{
			// OpenShift distinguishes nil from an explicit empty map (nil lets
			// cluster-wide build defaults apply, {} opts out). Shipwright has no
			// cluster-wide default, so both leave the field unset.
			name:         "empty nodeSelector leaves the field unset",
			nodeSelector: buildv1.OptionalNodeSelector{},
			want:         nil,
		},
		{
			name:         "invalid key drops the whole selector",
			nodeSelector: buildv1.OptionalNodeSelector{"bad key": "ssd"},
			want:         nil,
			wantLevel:    logrus.WarnLevel,
			wantPhrase:   "is not a valid label key",
		},
		{
			name:         "invalid value drops the whole selector",
			nodeSelector: buildv1.OptionalNodeSelector{"disktype": tooLongValue},
			want:         nil,
			wantLevel:    logrus.WarnLevel,
			wantPhrase:   "is not a valid label value",
		},
		{
			// All-or-nothing: a partial selector would silently schedule the
			// build somewhere the BuildConfig never asked for.
			name: "one invalid entry drops the valid ones too",
			nodeSelector: buildv1.OptionalNodeSelector{
				"disktype": "ssd",
				"bad key":  "value",
			},
			want:       nil,
			wantLevel:  logrus.WarnLevel,
			wantPhrase: "is not a valid label key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			c := &Converter{Log: logger}
			bc := &buildv1.BuildConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "selector-app", Namespace: "myns"},
				Spec: buildv1.BuildConfigSpec{
					CommonSpec: buildv1.CommonSpec{NodeSelector: tt.nodeSelector},
				},
			}
			b := &shipwrightv1beta1.Build{}

			c.processNodeSelector(bc, b)

			if !reflect.DeepEqual(b.Spec.NodeSelector, tt.want) {
				t.Errorf("nodeSelector = %#v, want %#v", b.Spec.NodeSelector, tt.want)
			}

			entries := hook.AllEntries()
			if tt.wantPhrase == "" {
				if len(entries) != 0 {
					t.Errorf("expected no log entries, got %d: %v", len(entries), entries[0].Message)
				}
				return
			}
			if len(entries) != 1 {
				t.Fatalf("expected exactly 1 log entry, got %d", len(entries))
			}
			entry := entries[0]
			if entry.Level != tt.wantLevel {
				t.Errorf("level = %v, want %v (message: %s)", entry.Level, tt.wantLevel, entry.Message)
			}
			if !strings.Contains(entry.Message, tt.wantPhrase) {
				t.Errorf("message = %q, want it to contain %q", entry.Message, tt.wantPhrase)
			}
			if !strings.Contains(entry.Message, "myns/selector-app") {
				t.Errorf("message = %q, want it to name the BuildConfig namespace/name", entry.Message)
			}
		})
	}
}

// TestValidateNodeSelectorReportsLowestSortedInvalidEntry pins the ordering
// contract. Go randomizes map iteration, so validating in map order named a
// different culprit on each run — useless for triage during a bulk migration.
// Because validation is a pure function over the map, the guarantee can be
// asserted directly rather than inferred from repeated conversions.
func TestValidateNodeSelectorReportsLowestSortedInvalidEntry(t *testing.T) {
	tests := []struct {
		name     string
		selector map[string]string
		wantErr  string
	}{
		{
			name:     "no invalid entries",
			selector: map[string]string{"disktype": "ssd", "topology.io/region": "us-east-1"},
			wantErr:  "",
		},
		{
			name: "several invalid keys — the lowest-sorted one is reported",
			selector: map[string]string{
				"zz bad key": "value",
				"aa bad key": "value",
				"mm bad key": "value",
				"disktype":   "ssd",
			},
			wantErr: `key "aa bad key" is not a valid label key`,
		},
		{
			name:     "invalid value names its key",
			selector: map[string]string{"disktype": strings.Repeat("a", 64)},
			wantErr:  `for key "disktype" is not a valid label value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNodeSelector(tt.selector)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("err = nil, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestConvertNodeSelectorWiring proves processNodeSelector is reachable from
// Convert and that the value survives the typed -> unstructured round trip that
// produces the emitted YAML.
func TestConvertNodeSelectorWiring(t *testing.T) {
	tests := []struct {
		name         string
		nodeSelector map[string]interface{}
		want         map[string]string
	}{
		{
			name:         "nodeSelector reaches the emitted Build",
			nodeSelector: map[string]interface{}{"disktype": "ssd", "region": "us-east-1"},
			want:         map[string]string{"disktype": "ssd", "region": "us-east-1"},
		},
		{
			name:         "absent nodeSelector is omitted from the emitted Build",
			nodeSelector: nil,
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []bcOption{}
			if tt.nodeSelector != nil {
				opts = append(opts, withSpecField("nodeSelector", tt.nodeSelector))
			}

			plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
			resp, err := plugin.Run(buildConfigRequest("myapp", opts...))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Assert on the raw unstructured too: omitempty must actually keep
			// the key out of the emitted YAML, not emit an empty map.
			raw, found, err := unstructured.NestedStringMap(resp.NewResources[0].Object, "spec", "nodeSelector")
			if err != nil {
				t.Fatalf("spec.nodeSelector is not a string map: %v", err)
			}
			if tt.want == nil {
				if found {
					t.Errorf("spec.nodeSelector present in emitted Build = %#v, want absent", raw)
				}
				return
			}
			if !found {
				t.Fatalf("spec.nodeSelector absent from emitted Build, want %#v", tt.want)
			}
			if !reflect.DeepEqual(raw, tt.want) {
				t.Errorf("emitted spec.nodeSelector = %#v, want %#v", raw, tt.want)
			}
		})
	}
}

// TestConvertNodeSelectorWithResources pins the interaction with BUILD-2261:
// nodeSelector belongs on the Build, resources stay in the BuildRun template
// annotation, and neither disturbs the other.
func TestConvertNodeSelectorWithResources(t *testing.T) {
	plugin := &BuildConfigTransformPlugin{Log: logrus.New()}
	resp, err := plugin.Run(buildConfigRequest("myapp",
		withSpecField("nodeSelector", map[string]interface{}{"disktype": "ssd"}),
		withSpecField("resources", map[string]interface{}{
			"requests": map[string]interface{}{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]interface{}{"cpu": "500m", "memory": "1Gi"},
		}),
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Decode the whole emitted object into the real Shipwright type rather than
	// reading only the two fields under test. A narrow accessor would pass even
	// if conversion corrupted an unrelated part of the Build, and this is the
	// one test exercising nodeSelector and the BUILD-2261 template together.
	b := &shipwrightv1beta1.Build{}
	jsonBytes, err := json.Marshal(resp.NewResources[0].Object)
	if err != nil {
		t.Fatalf("marshaling emitted Build: %v", err)
	}
	if err := json.Unmarshal(jsonBytes, b); err != nil {
		t.Fatalf("unmarshaling emitted Build: %v", err)
	}

	wantSelector := map[string]string{"disktype": "ssd"}
	if !reflect.DeepEqual(b.Spec.NodeSelector, wantSelector) {
		t.Errorf("Build spec.nodeSelector = %#v, want %#v", b.Spec.NodeSelector, wantSelector)
	}

	value, ok := b.Annotations[BuildRunTemplateAnnotation]
	if !ok {
		t.Fatalf("expected annotation %s, got: %v", BuildRunTemplateAnnotation, b.Annotations)
	}
	tmpl := unmarshalBuildRunTemplate(t, value)
	if len(tmpl.Spec.StepResources) == 0 {
		t.Errorf("BuildRun template lost its stepResources: %s", value)
	}
	// The template must not duplicate the selector — the Build already carries
	// it, and Shipwright merges Build and BuildRun selectors anyway.
	if tmpl.Spec.NodeSelector != nil {
		t.Errorf("BuildRun template nodeSelector = %#v, want nil (it belongs on the Build)", tmpl.Spec.NodeSelector)
	}
}
