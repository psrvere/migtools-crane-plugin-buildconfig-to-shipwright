package buildconfig

import (
	"fmt"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// InlineDockerfileConfigMapAnnotation is set on the generated Build and holds
	// the name of the ConfigMap that preserves an inline Dockerfile's content. The
	// Build cannot yet run from it — the buildah strategy has no Dockerfile-content
	// parameter (BUILD-1495) — but the content is not lost on migration.
	InlineDockerfileConfigMapAnnotation = "buildconfig-to-shipwright/inline-dockerfile-configmap"
	// inlineDockerfileKey is the ConfigMap data key holding the Dockerfile text.
	inlineDockerfileKey = "Dockerfile"
	// InlineDockerfileRFE tracks giving the buildah strategy a way to build from
	// preserved Dockerfile content.
	InlineDockerfileRFE = "https://issues.redhat.com/browse/BUILD-1495"
)

// processInlineDockerfile handles spec.source.dockerfile, which holds raw Dockerfile
// contents that Shipwright cannot represent: v1beta1 Source has no dockerfile field, and
// Source-to-Image builds the Dockerfile it generates itself.
//
// For a Docker strategy the content is preserved in a ConfigMap returned to the caller,
// the Build is pointed at it through InlineDockerfileConfigMapAnnotation, and the drop is
// recorded loudly (ERROR) because the Build still runs against whatever Dockerfile the
// source repository holds, not this content, until BUILD-1495 lands. For a Source strategy
// the field is inapplicable and usually signals the wrong strategy type, so it warns and
// returns nil. The empty and nil cases are silent — an explicit `dockerfile: ""` carries no
// content to lose (omitempty on a *string suppresses only a nil pointer), and reporting it
// would tell the user to reconfigure a strategy over nothing.
//
// Custom and JenkinsPipeline strategies never reach here: Convert() returns before this for
// those, passing the BuildConfig through unchanged, so nothing is dropped.
//
// Returns a non-nil ConfigMap only for the Docker strategy with non-empty inline content;
// nil in every other case.
func (c *Converter) processInlineDockerfile(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) *corev1.ConfigMap {
	dockerfile := bc.Spec.Source.Dockerfile
	if dockerfile == nil || *dockerfile == "" {
		return nil
	}

	switch bc.Spec.Strategy.Type {
	case buildv1.DockerBuildStrategyType:
		cmName := c.uniqueName("ConfigMap", bc.Name+"-dockerfile")
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: bc.Namespace,
				Annotations: map[string]string{
					ConvertedFromAnnotation: fmt.Sprintf("build.openshift.io/v1/BuildConfig/%s", bc.Name),
				},
			},
			Data: map[string]string{
				inlineDockerfileKey: *dockerfile,
			},
		}
		b.Annotations[InlineDockerfileConfigMapAnnotation] = cmName

		// Logged at ERROR rather than through warnf (which logs at WARN) to stay as
		// loud as the BUILD-2275 drop: the Build does not build from this content
		// until BUILD-1495 gives the strategy a way to consume it. recordWarning
		// keeps the [ns/name] attribution and the converted-with-warnings
		// classification that warnf would have given it (BUILD-2319).
		msg := fmt.Sprintf("Inline Dockerfile on BuildConfig %s/%s cannot be consumed by the buildah strategy; its content was preserved in ConfigMap %s/%s (key %s). Commit it to the source repository as the Dockerfile, or see %s, before running the Build.",
			bc.Namespace, bc.Name, bc.Namespace, cmName, inlineDockerfileKey, InlineDockerfileRFE)
		c.Log.Errorf("%s", c.recordWarning(msg))
		return cm
	case buildv1.SourceBuildStrategyType:
		c.warnf("BuildConfig %s/%s has an inline Dockerfile set on a Source strategy. "+
			"Inline Dockerfiles are not used by Source-to-Image and were not migrated. "+
			"If this was intended for a Docker strategy build, reconfigure the BuildConfig strategy type.", bc.Namespace, bc.Name)
	}
	return nil
}
