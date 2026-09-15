//go:build !documentation

package buildconfig

import "testing"

func TestMetadataName(t *testing.T) {
	metadata := (&BuildConfigTransformPlugin{}).Metadata()

	if metadata.Name != "BuildConfigToBuildsPlugin" {
		t.Errorf("metadata name = %q, want %q", metadata.Name, "BuildConfigToBuildsPlugin")
	}
}
