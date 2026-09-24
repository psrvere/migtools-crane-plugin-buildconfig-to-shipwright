//go:build !documentation

package buildconfig

import (
	"strings"
	"testing"

	"github.com/konveyor/crane-lib/transform"
	buildv1 "github.com/openshift/api/build/v1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func trustedCABuildConfigRequest(strategyType, strategyKey string, mountTrustedCA bool, volumes []interface{}) transform.PluginRequest {
	strategy := map[string]interface{}{}
	if volumes != nil {
		strategy["volumes"] = volumes
	}
	return transform.PluginRequest{
		Unstructured: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "build.openshift.io/v1",
			"kind":       "BuildConfig",
			"metadata": map[string]interface{}{
				"name":      "trusted-ca-app",
				"namespace": "myns",
			},
			"spec": map[string]interface{}{
				"mountTrustedCA": mountTrustedCA,
				"source": map[string]interface{}{
					"type": "Git",
					"git": map[string]interface{}{
						"uri": "https://github.com/example/myapp.git",
					},
				},
				"strategy": map[string]interface{}{
					"type":      strategyType,
					strategyKey: strategy,
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

// The request builder names its BuildConfig trusted-ca-app; the converter
// derives the ConfigMap name from the BuildConfig's name, the same input
// every other generated name comes from, and sanitizes it.
const testTrustedCAConfigMapName = "trusted-ca-app" + TrustedCABundleConfigMapSuffix

func findConfigMap(resp transform.PluginResponse, name string) *unstructured.Unstructured {
	for i := range resp.NewResources {
		r := resp.NewResources[i]
		if r.GetKind() == "ConfigMap" && r.GetName() == name {
			return &r
		}
	}
	return nil
}

func TestConvertMountTrustedCA(t *testing.T) {
	for _, tt := range []struct {
		name        string
		strategy    string
		strategyKey string
	}{
		{"docker strategy", "Docker", "dockerStrategy"},
		{"source strategy", "Source", "sourceStrategy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logger, hook := logrustest.NewNullLogger()
			plugin := &BuildConfigTransformPlugin{Log: logger}
			resp, err := plugin.Run(trustedCABuildConfigRequest(tt.strategy, tt.strategyKey, true, nil))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			b := decodeBuild(t, resp)
			if len(b.Spec.Volumes) != 1 {
				t.Fatalf("expected 1 Build spec volume, got %d: %+v", len(b.Spec.Volumes), b.Spec.Volumes)
			}
			vol := b.Spec.Volumes[0]
			if vol.Name != TrustedCAVolumeName {
				t.Errorf("expected volume name %q, got %q", TrustedCAVolumeName, vol.Name)
			}
			if vol.ConfigMap == nil || vol.ConfigMap.Name != testTrustedCAConfigMapName {
				t.Errorf("expected configMap volume source %q, got %+v", testTrustedCAConfigMapName, vol.VolumeSource)
			}
			if vol.ConfigMap != nil && (len(vol.ConfigMap.Items) != 1 || vol.ConfigMap.Items[0].Key != TrustedCABundleKey || vol.ConfigMap.Items[0].Path != TrustedCABundleKey) {
				t.Errorf("expected volume projection restricted to %s, got %+v", TrustedCABundleKey, vol.ConfigMap.Items)
			}
			// Optional must stay unset. With optional: true the kubelet
			// mounts an empty directory and the build runs without the
			// trust it asked for, which is the one outcome this mapping
			// exists to prevent.
			if vol.ConfigMap != nil && vol.ConfigMap.Optional != nil {
				t.Errorf("expected ConfigMap volume source Optional to stay unset so a missing bundle fails the mount, got %v", *vol.ConfigMap.Optional)
			}

			cm := findConfigMap(resp, testTrustedCAConfigMapName)
			if cm == nil {
				t.Fatalf("expected ConfigMap %q in new resources, got %+v", testTrustedCAConfigMapName, resp.NewResources)
			}
			if cm.GetNamespace() != "myns" {
				t.Errorf("expected ConfigMap namespace myns, got %q", cm.GetNamespace())
			}
			if v := cm.GetLabels()[InjectTrustedCABundleLabel]; v != "true" {
				t.Errorf("expected label %s=true on ConfigMap, got labels %+v", InjectTrustedCABundleLabel, cm.GetLabels())
			}
			if v := cm.GetAnnotations()[ConvertedFromAnnotation]; v == "" {
				t.Errorf("expected %s annotation on ConfigMap for traceability, got %+v", ConvertedFromAnnotation, cm.GetAnnotations())
			}

			// Shipped strategies define the trusted-ca volume: no warning expected.
			for _, entry := range hook.AllEntries() {
				if strings.Contains(entry.Message, "not a shipped strategy") {
					t.Errorf("unexpected non-shipped-strategy warning: %q", entry.Message)
				}
			}
		})
	}
}

func TestConvertMountTrustedCADisabled(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	resp, err := plugin.Run(trustedCABuildConfigRequest("Docker", "dockerStrategy", false, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := decodeBuild(t, resp)
	if len(b.Spec.Volumes) != 0 {
		t.Errorf("expected no Build spec volumes, got %+v", b.Spec.Volumes)
	}
	if cm := findConfigMap(resp, testTrustedCAConfigMapName); cm != nil {
		t.Errorf("expected no trusted CA ConfigMap, got %+v", cm)
	}
}

func TestConvertMountTrustedCAVolumeNameCollision(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	request := trustedCABuildConfigRequest("Docker", "dockerStrategy", true, []interface{}{
		map[string]interface{}{
			"name":   TrustedCAVolumeName,
			"source": map[string]interface{}{"type": "ConfigMap", "configMap": map[string]interface{}{"name": "my-own-ca"}},
		},
	})

	resp, err := plugin.Run(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The explicit strategy volume wins; the mapping is skipped entirely.
	b := decodeBuild(t, resp)
	if len(b.Spec.Volumes) != 1 {
		t.Fatalf("expected 1 Build spec volume, got %d: %+v", len(b.Spec.Volumes), b.Spec.Volumes)
	}
	if b.Spec.Volumes[0].ConfigMap == nil || b.Spec.Volumes[0].ConfigMap.Name != "my-own-ca" {
		t.Errorf("expected explicit volume backed by my-own-ca to be kept, got %+v", b.Spec.Volumes[0])
	}
	if cm := findConfigMap(resp, testTrustedCAConfigMapName); cm != nil {
		t.Errorf("expected no trusted CA ConfigMap when mapping is skipped, got %+v", cm)
	}

	var sawSkip bool
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "skipping the trusted CA mapping") {
			sawSkip = true
			if entry.Level != logrus.WarnLevel {
				t.Errorf("collision message should be warn-level, got %s", entry.Level)
			}
		}
	}
	if !sawSkip {
		t.Error("expected warn-and-skip message for trusted-ca volume name collision")
	}

	// processStrategyVolumes converts the explicit "trusted-ca" volume before
	// the mapping ever sees it and defers, so it is the one that warns about
	// declaring the volume in the target strategy. The generic per-volume and
	// summary remediation ("add an overridable volume ..." / "does not
	// declare them") is wrong for this name on strategy-catalog cb2432c+,
	// which already ships it — the catalog-version check must run instead,
	// and it must not be followed by the summary warning, since this is the
	// only volume on the BuildConfig.
	var sawCatalogCheck, sawGenericPerVolume, sawGenericSummary bool
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "already declare an overridable volume by this name") && strings.Contains(entry.Message, "cb2432c") {
			sawCatalogCheck = true
		}
		if strings.Contains(entry.Message, "add an overridable volume named") {
			sawGenericPerVolume = true
		}
		if strings.Contains(entry.Message, "does not declare them") {
			sawGenericSummary = true
		}
	}
	if !sawCatalogCheck {
		t.Error("expected the catalog-version check warning for the explicit trusted-ca volume")
	}
	if sawGenericPerVolume {
		t.Error("did not expect the generic per-volume remediation warning (W25) for a volume named trusted-ca")
	}
	if sawGenericSummary {
		t.Error("did not expect the generic strategy-does-not-declare-them summary warning (W26) when the only converted volume is trusted-ca")
	}
}

// A strategy volume literally named "trusted-ca" goes through
// processStrategyVolumes whether or not mountTrustedCA is set — the
// collision check in processMountTrustedCA only ever sees this volume after
// processStrategyVolumes has already converted it. This proves the
// catalog-version check warning fires, and the misleading generic
// remediation does not, even with mountTrustedCA absent entirely, so the
// fix does not depend on the mapping having run first.
func TestConvertStrategyVolumeNamedTrustedCAWithoutMountTrustedCA(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	resp, err := plugin.Run(trustedCABuildConfigRequest("Docker", "dockerStrategy", false, []interface{}{
		map[string]interface{}{
			"name":   TrustedCAVolumeName,
			"source": map[string]interface{}{"type": "Secret", "secret": map[string]interface{}{"secretName": "my-own-ca-secret"}},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := decodeBuild(t, resp)
	if len(b.Spec.Volumes) != 1 || b.Spec.Volumes[0].Name != TrustedCAVolumeName {
		t.Fatalf("expected 1 Build spec volume named %s, got %+v", TrustedCAVolumeName, b.Spec.Volumes)
	}
	if b.Spec.Volumes[0].Secret == nil || b.Spec.Volumes[0].Secret.SecretName != "my-own-ca-secret" {
		t.Errorf("expected the explicit secret volume to be kept, got %+v", b.Spec.Volumes[0])
	}
	if cm := findConfigMap(resp, testTrustedCAConfigMapName); cm != nil {
		t.Errorf("expected no generated trusted CA ConfigMap when mountTrustedCA is unset, got %+v", cm)
	}

	var sawCatalogCheck, sawGenericPerVolume, sawGenericSummary bool
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "already declare an overridable volume by this name") && strings.Contains(entry.Message, "cb2432c") {
			sawCatalogCheck = true
		}
		if strings.Contains(entry.Message, "add an overridable volume named") {
			sawGenericPerVolume = true
		}
		if strings.Contains(entry.Message, "does not declare them") {
			sawGenericSummary = true
		}
	}
	if !sawCatalogCheck {
		t.Error("expected the catalog-version check warning for a strategy volume named trusted-ca, even without mountTrustedCA")
	}
	if sawGenericPerVolume {
		t.Error("did not expect the generic per-volume remediation warning (W25) for a volume named trusted-ca")
	}
	if sawGenericSummary {
		t.Error("did not expect the generic strategy-does-not-declare-them summary warning (W26) when the only converted volume is trusted-ca")
	}
}

func TestConvertMountTrustedCACustomStrategyWarning(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	mountTrustedCA := true
	bc := &buildv1.BuildConfig{}
	bc.Name = "trusted-ca-app"
	bc.Namespace = "myns"
	bc.Spec.MountTrustedCA = &mountTrustedCA
	bc.Spec.Strategy = buildv1.BuildStrategy{
		Type:           buildv1.DockerBuildStrategyType,
		DockerStrategy: &buildv1.DockerBuildStrategy{},
	}
	bc.Spec.Output.To = &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/myapp:latest"}

	c := &Converter{
		Log:  logger,
		Opts: PluginOptionalFields{StrategyMapping: map[string]string{"docker": "my-custom-strategy"}},
	}
	result, outcome := c.Convert(bc)
	if outcome.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcome.Reason)
	}
	if len(result) < 2 {
		t.Fatalf("expected Build and ConfigMap, got %+v", result)
	}

	// The volume is still appended — the user may have added trusted-ca to
	// their custom strategy — and the emitted resources must prove it.
	vols, _, err := unstructured.NestedSlice(result[0].Object, "spec", "volumes")
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 Build spec volume on custom-strategy Build, got %v (err %v)", vols, err)
	}
	if name, _, _ := unstructured.NestedString(vols[0].(map[string]interface{}), "name"); name != TrustedCAVolumeName {
		t.Errorf("expected volume %q on Build, got %q", TrustedCAVolumeName, name)
	}
	var sawConfigMap bool
	for _, r := range result[1:] {
		if r.GetKind() == "ConfigMap" && r.GetName() == testTrustedCAConfigMapName {
			sawConfigMap = true
		}
	}
	if !sawConfigMap {
		t.Fatalf("expected ConfigMap %q among converted resources, got %+v", testTrustedCAConfigMapName, result)
	}

	// The warning must state the real outcome per the BUILD-2342 fail-visible
	// contract: Shipwright rejects the Build, it does not sit inert.
	var sawWarning bool
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "not a shipped strategy") && strings.Contains(entry.Message, "my-custom-strategy") {
			sawWarning = true
			if entry.Level != logrus.WarnLevel {
				t.Errorf("non-shipped-strategy message should be warn-level, got %s", entry.Level)
			}
			if !strings.Contains(entry.Message, "UndefinedVolume") || !strings.Contains(entry.Message, "Registered=False") {
				t.Errorf("warning should state the UndefinedVolume rejection outcome, got %q", entry.Message)
			}
		}
	}
	if !sawWarning {
		t.Error("expected non-shipped-strategy warning for custom strategy mapping")
	}
}

func TestConvertMountTrustedCAAbsent(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	req := trustedCABuildConfigRequest("Docker", "dockerStrategy", false, nil)
	spec := req.Unstructured.Object["spec"].(map[string]interface{})
	delete(spec, "mountTrustedCA") // field absent → nil-pointer branch
	resp, err := plugin.Run(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := decodeBuild(t, resp)
	if len(b.Spec.Volumes) != 0 {
		t.Errorf("expected no Build spec volumes, got %+v", b.Spec.Volumes)
	}
	if cm := findConfigMap(resp, testTrustedCAConfigMapName); cm != nil {
		t.Errorf("expected no trusted CA ConfigMap, got %+v", cm)
	}
}

func TestConvertMountTrustedCAWithOtherVolumes(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	resp, err := plugin.Run(trustedCABuildConfigRequest("Docker", "dockerStrategy", true, []interface{}{
		map[string]interface{}{
			"name":   "other-vol",
			"source": map[string]interface{}{"type": "ConfigMap", "configMap": map[string]interface{}{"name": "other-cm"}},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := decodeBuild(t, resp)
	if len(b.Spec.Volumes) != 2 {
		t.Fatalf("expected 2 Build spec volumes (other-vol + trusted-ca), got %d: %+v", len(b.Spec.Volumes), b.Spec.Volumes)
	}
	names := map[string]bool{}
	for _, v := range b.Spec.Volumes {
		names[v.Name] = true
	}
	if !names["other-vol"] || !names[TrustedCAVolumeName] {
		t.Errorf("expected volumes other-vol and %s, got %+v", TrustedCAVolumeName, names)
	}
	if cm := findConfigMap(resp, testTrustedCAConfigMapName); cm == nil {
		t.Error("expected trusted CA ConfigMap alongside non-colliding volumes")
	}
}

func TestConvertMountTrustedCAUnsupportedSourceCollision(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	plugin := &BuildConfigTransformPlugin{Log: logger}
	// A user-declared trusted-ca volume with an unsupported source is skipped
	// by processStrategyVolumes — the mapping must still defer to it instead
	// of silently substituting the injected cluster bundle.
	resp, err := plugin.Run(trustedCABuildConfigRequest("Docker", "dockerStrategy", true, []interface{}{
		map[string]interface{}{
			"name":   TrustedCAVolumeName,
			"source": map[string]interface{}{"type": "CSI"},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b := decodeBuild(t, resp)
	if len(b.Spec.Volumes) != 0 {
		t.Fatalf("expected no Build spec volumes (user volume skipped, mapping deferred), got %+v", b.Spec.Volumes)
	}
	if cm := findConfigMap(resp, testTrustedCAConfigMapName); cm != nil {
		t.Errorf("expected no trusted CA ConfigMap when mapping is skipped, got %+v", cm)
	}
	var sawSkip bool
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "skipping the trusted CA mapping") {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Error("expected warn-and-skip message for user-declared trusted-ca volume with unsupported source")
	}
}

// A BuildConfig may carry both strategy blocks while spec.strategy.type names
// only one of them. The collision check has to read the block Convert
// dispatched on: here type Source, a trusted-ca volume on sourceStrategy that
// processStrategyVolumes drops as unsupported, and a populated dockerStrategy
// beside it. Reading the Docker list would find no trusted-ca, and the
// injected cluster bundle would take the name of the CA volume the user
// declared.
func TestConvertMountTrustedCAVolumesFollowStrategyType(t *testing.T) {
	logger, hook := logrustest.NewNullLogger()
	mount := true
	bc := &buildv1.BuildConfig{}
	bc.Name = "trusted-ca-app"
	bc.Namespace = "myns"
	bc.Spec.MountTrustedCA = &mount
	bc.Spec.Strategy = buildv1.BuildStrategy{
		Type: buildv1.SourceBuildStrategyType,
		SourceStrategy: &buildv1.SourceBuildStrategy{
			Volumes: []buildv1.BuildVolume{{
				Name:   TrustedCAVolumeName,
				Source: buildv1.BuildVolumeSource{Type: buildv1.BuildVolumeSourceTypeCSI},
			}},
		},
		DockerStrategy: &buildv1.DockerBuildStrategy{},
	}
	bc.Spec.Output.To = &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/myapp:latest"}

	c := &Converter{Log: logger}
	result, outcome := c.Convert(bc)
	if outcome.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcome.Reason)
	}
	if len(result) == 0 {
		t.Fatal("expected a converted Build")
	}

	vols, _, err := unstructured.NestedSlice(result[0].Object, "spec", "volumes")
	if err != nil {
		t.Fatalf("reading spec.volumes: %v", err)
	}
	if len(vols) != 0 {
		t.Errorf("expected no Build spec volume: the user's trusted-ca volume was dropped and the mapping must defer to it, got %+v", vols)
	}
	for _, r := range result[1:] {
		if r.GetKind() == "ConfigMap" && r.GetName() == testTrustedCAConfigMapName {
			t.Errorf("the injected bundle took the name of the user's own trusted-ca volume: %+v", r.Object)
		}
	}

	var sawSkip bool
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "skipping the trusted CA mapping") {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Error("expected the warn-and-skip message naming the BuildConfig's own trusted-ca volume")
	}
}

func TestConvertMountTrustedCAPerBuildConfigMaps(t *testing.T) {
	logger, _ := logrustest.NewNullLogger()
	c := &Converter{Log: logger}
	mount := true
	newBC := func(name string) *buildv1.BuildConfig {
		bc := &buildv1.BuildConfig{}
		bc.Name = name
		bc.Namespace = "myns"
		bc.Spec.MountTrustedCA = &mount
		bc.Spec.Strategy = buildv1.BuildStrategy{
			Type:           buildv1.DockerBuildStrategyType,
			DockerStrategy: &buildv1.DockerBuildStrategy{},
		}
		bc.Spec.Output.To = &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/example/myapp:latest"}
		return bc
	}
	hasCM := func(result []unstructured.Unstructured, name string) bool {
		for _, r := range result {
			if r.GetKind() == "ConfigMap" && r.GetName() == name {
				return true
			}
		}
		return false
	}

	first, outcome := c.Convert(newBC("app-one"))
	if outcome.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcome.Reason)
	}
	second, outcome := c.Convert(newBC("app-two"))
	if outcome.State == OutcomeFailed {
		t.Fatalf("unexpected conversion failure: %s", outcome.Reason)
	}

	// Each conversion owns its own ConfigMap, named after its Build.
	for i, tc := range []struct {
		result []unstructured.Unstructured
		want   string
	}{
		{first, "app-one" + TrustedCABundleConfigMapSuffix},
		{second, "app-two" + TrustedCABundleConfigMapSuffix},
	} {
		if !hasCM(tc.result, tc.want) {
			t.Errorf("conversion %d: expected per-Build ConfigMap %q, got %+v", i, tc.want, tc.result)
		}
		vols, _, err := unstructured.NestedSlice(tc.result[0].Object, "spec", "volumes")
		if err != nil || len(vols) != 1 {
			t.Fatalf("conversion %d: expected 1 Build spec volume, got %v (err %v)", i, vols, err)
		}
		cmRef, _, _ := unstructured.NestedString(vols[0].(map[string]interface{}), "configMap", "name")
		if cmRef != tc.want {
			t.Errorf("conversion %d: expected volume to reference its own ConfigMap %q, got %q", i, tc.want, cmRef)
		}
	}
}
