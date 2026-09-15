//go:build !documentation

package buildconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func volumesBuildConfigRequest(strategyType string, strategyKey string, volumes []interface{}) transform.PluginRequest {
	return transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "volumes-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
					},
				},
				"strategy": map[string]interface{}{
					"type": strategyType,
					strategyKey: map[string]interface{}{
						"volumes": volumes,
					},
				},
				"output": map[string]interface{}{
					"to": map[string]interface{}{
						"kind": "DockerImage",
						"name": "quay.io/example/myapp:latest",
					},
				},
			},
		}},
	}
}

func decodeBuild(t *testing.T, resp transform.PluginResponse) *shipwrightv1beta1.Build {
	t.Helper()
	if len(resp.NewResources) < 1 {
		t.Fatal("expected at least 1 new resource")
	}
	b := &shipwrightv1beta1.Build{}
	jsonBytes, _ := json.Marshal(resp.NewResources[0].Object)
	if err := json.Unmarshal(jsonBytes, b); err != nil {
		t.Fatalf("failed to decode Build: %v", err)
	}
	return b
}

// wantVolume is one expected entry of a converted Build's spec.volumes.
type wantVolume struct {
	name          string
	secretName    string
	configMapName string
}

// firstEntry returns the first log entry that satisfies pred, or nil.
func firstEntry(hook *logrustest.Hook, pred func(*logrus.Entry) bool) *logrus.Entry {
	for _, entry := range hook.AllEntries() {
		if pred(entry) {
			return entry
		}
	}
	return nil
}

// entryContaining returns the first log entry whose message contains every
// substring, or nil.
func entryContaining(hook *logrustest.Hook, substrs ...string) *logrus.Entry {
	return firstEntry(hook, func(entry *logrus.Entry) bool {
		for _, s := range substrs {
			if !strings.Contains(entry.Message, s) {
				return false
			}
		}
		return true
	})
}

func TestConvertStrategyVolumes(t *testing.T) {
	secretVol := map[string]interface{}{
		"name":   "secret-vol",
		"source": map[string]interface{}{"type": "Secret", "secret": map[string]interface{}{"secretName": "my-secret"}},
	}
	configVol := map[string]interface{}{
		"name":   "config-vol",
		"source": map[string]interface{}{"type": "ConfigMap", "configMap": map[string]interface{}{"name": "my-config"}},
	}
	csiVol := map[string]interface{}{
		"name":   "csi-vol",
		"source": map[string]interface{}{"type": "CSI", "csi": map[string]interface{}{"driver": "inline.storage.kubernetes.io"}},
	}

	tests := []struct {
		name         string
		strategyType string
		strategyKey  string
		volumes      []interface{}
		wantVolumes  []wantVolume
		// wantWarn lists groups of substrings; each group must co-occur in one
		// warn-level entry.
		wantWarn [][]string
		// wantAbsent lists substrings no entry may contain.
		wantAbsent   []string
		wantNoErrors bool
	}{
		{
			name:         "docker: secret, configMap, csi",
			strategyType: "Docker",
			strategyKey:  "dockerStrategy",
			volumes:      []interface{}{secretVol, configVol, csiVol},
			// Supported volumes are converted; the unsupported CSI volume is skipped.
			wantVolumes: []wantVolume{
				{name: "secret-vol", secretName: "my-secret"},
				{name: "config-vol", configMapName: "my-config"},
			},
			wantWarn: [][]string{
				{`Skipping volume "csi-vol"`, "unsupported volume source type"},
				{"Volumes were converted to Build spec volumes", "Buildah", "Registered=False", "UndefinedVolume", "docs/volume-migration.md"},
				{"add an overridable volume named 'secret-vol'", "UndefinedVolume", "(2) add a volumeMount", "(3) point the Build at the strategy copy"},
				{"add an overridable volume named 'config-vol'", "UndefinedVolume", "(2) add a volumeMount", "(3) point the Build at the strategy copy"},
			},
			// The old wording implying volumes are not converted, the pre-BUILD-2324
			// understatement and the stale RFE link must all be gone.
			wantAbsent: []string{"Volumes require the Buildah ClusterBuildStrategy", "only take effect", "BUILD-1747"},
		},
		{
			name:         "source: secret",
			strategyType: "Source",
			strategyKey:  "sourceStrategy",
			volumes:      []interface{}{secretVol},
			wantVolumes:  []wantVolume{{name: "secret-vol", secretName: "my-secret"}},
			wantWarn: [][]string{
				{"Volumes were converted to Build spec volumes", "Source-to-Image", "Registered=False", "UndefinedVolume"},
				{"add an overridable volume named 'secret-vol'"},
			},
		},
		{
			name:         "docker: mount paths echoed in the remediation",
			strategyType: "Docker",
			strategyKey:  "dockerStrategy",
			volumes: []interface{}{
				map[string]interface{}{
					"name":   "secret-vol",
					"source": map[string]interface{}{"type": "Secret", "secret": map[string]interface{}{"secretName": "my-secret"}},
					"mounts": []interface{}{
						map[string]interface{}{"destinationPath": "/etc/npm"},
						map[string]interface{}{"destinationPath": "/etc/pip"},
					},
				},
			},
			wantVolumes: []wantVolume{{name: "secret-vol", secretName: "my-secret"}},
			wantWarn: [][]string{
				{
					`Volume "secret-vol" was converted`,
					"original BuildConfig destination paths: /etc/npm, /etc/pip",
					"(1) add an overridable volume named 'secret-vol'",
					"overridable: true",
					"(2) add a volumeMount for 'secret-vol'",
					"(3) point the Build at the strategy copy via spec.strategy.name",
				},
			},
			wantNoErrors: true,
		},
		{
			name:         "docker: empty and duplicate names",
			strategyType: "Docker",
			strategyKey:  "dockerStrategy",
			volumes: []interface{}{
				map[string]interface{}{
					"name":   "",
					"source": map[string]interface{}{"type": "Secret", "secret": map[string]interface{}{"secretName": "unnamed-secret"}},
				},
				map[string]interface{}{
					"name":   "dup-vol",
					"source": map[string]interface{}{"type": "Secret", "secret": map[string]interface{}{"secretName": "first-secret"}},
				},
				map[string]interface{}{
					"name":   "dup-vol",
					"source": map[string]interface{}{"type": "ConfigMap", "configMap": map[string]interface{}{"name": "second-config"}},
				},
			},
			// The empty-name volume and the duplicate are skipped; the first dup-vol wins.
			wantVolumes: []wantVolume{{name: "dup-vol", secretName: "first-secret"}},
			wantWarn: [][]string{
				{"Skipping volume with empty name"},
				{`Skipping duplicate volume "dup-vol"`},
			},
		},
		{
			name:         "docker: every volume skipped",
			strategyType: "Docker",
			strategyKey:  "dockerStrategy",
			volumes:      []interface{}{csiVol},
			// When nothing was converted, neither the summary warning nor a per-volume
			// remediation may be emitted; both would falsely claim success.
			wantAbsent: []string{"Volumes were converted to Build spec volumes", "add an overridable volume named"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			plugin := &BuildConfigTransformPlugin{Log: logger}

			resp, err := plugin.Run(volumesBuildConfigRequest(tt.strategyType, tt.strategyKey, tt.volumes))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			b := decodeBuild(t, resp)
			if len(b.Spec.Volumes) != len(tt.wantVolumes) {
				t.Fatalf("expected %d Build spec volumes, got %d: %+v", len(tt.wantVolumes), len(b.Spec.Volumes), b.Spec.Volumes)
			}
			for i, want := range tt.wantVolumes {
				got := b.Spec.Volumes[i]
				if got.Name != want.name {
					t.Errorf("volume %d: expected name %q, got %q", i, want.name, got.Name)
				}
				if want.secretName != "" && (got.Secret == nil || got.Secret.SecretName != want.secretName) {
					t.Errorf("volume %d: expected secret %q, got %+v", i, want.secretName, got.VolumeSource)
				}
				if want.configMapName != "" && (got.ConfigMap == nil || got.ConfigMap.Name != want.configMapName) {
					t.Errorf("volume %d: expected configMap %q, got %+v", i, want.configMapName, got.VolumeSource)
				}
				// A volume carries exactly one source; the other must stay unset.
				if want.secretName == "" && got.Secret != nil {
					t.Errorf("volume %d: unexpected secret source %+v", i, got.Secret)
				}
				if want.configMapName == "" && got.ConfigMap != nil {
					t.Errorf("volume %d: unexpected configMap source %+v", i, got.ConfigMap)
				}
			}

			for _, group := range tt.wantWarn {
				entry := entryContaining(hook, group...)
				if entry == nil {
					t.Errorf("expected a log entry containing %q", group)
					continue
				}
				if entry.Level != logrus.WarnLevel {
					t.Errorf("entry containing %q should be warn-level, got %s", group, entry.Level)
				}
			}
			for _, absent := range tt.wantAbsent {
				if entry := entryContaining(hook, absent); entry != nil {
					t.Errorf("no entry may contain %q, got %q", absent, entry.Message)
				}
			}
			if tt.wantNoErrors {
				isError := func(entry *logrus.Entry) bool { return entry.Level == logrus.ErrorLevel }
				if entry := firstEntry(hook, isError); entry != nil {
					t.Errorf("no error-level logs expected, got %q", entry.Message)
				}
			}
		})
	}
}

func TestConvertBuildVolumeSource(t *testing.T) {
	tests := []struct {
		name    string
		source  buildv1.BuildVolumeSource
		wantErr string
		check   func(t *testing.T, vs corev1.VolumeSource)
	}{
		{
			name: "secret source",
			source: buildv1.BuildVolumeSource{
				Type:   buildv1.BuildVolumeSourceTypeSecret,
				Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
			},
			check: func(t *testing.T, vs corev1.VolumeSource) {
				if vs.Secret == nil || vs.Secret.SecretName != "my-secret" {
					t.Errorf("unexpected secret source: %+v", vs)
				}
			},
		},
		{
			name: "configMap source",
			source: buildv1.BuildVolumeSource{
				Type: buildv1.BuildVolumeSourceTypeConfigMap,
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
				},
			},
			check: func(t *testing.T, vs corev1.VolumeSource) {
				if vs.ConfigMap == nil || vs.ConfigMap.Name != "my-config" {
					t.Errorf("unexpected configMap source: %+v", vs)
				}
			},
		},
		{
			name:    "nil secret",
			source:  buildv1.BuildVolumeSource{Type: buildv1.BuildVolumeSourceTypeSecret},
			wantErr: "secret volume source is nil",
		},
		{
			name:    "nil configMap",
			source:  buildv1.BuildVolumeSource{Type: buildv1.BuildVolumeSourceTypeConfigMap},
			wantErr: "configMap volume source is nil",
		},
		{
			name:    "unsupported type",
			source:  buildv1.BuildVolumeSource{Type: buildv1.BuildVolumeSourceTypeCSI},
			wantErr: "unsupported volume source type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs, err := convertBuildVolumeSource(tt.source)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, vs)
		})
	}
}
