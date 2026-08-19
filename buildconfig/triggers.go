package buildconfig

import (
	"fmt"
	"sort"
	"strings"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// BuildRunTemplateAnnotation marks a generated Build that carries an inert
// BuildRun template (BUILD-2261). The ConfigChange trigger warning checks it
// at runtime so there is no hard dependency: when the annotation is absent
// the warning falls back to manual-BuildRun wording.
const BuildRunTemplateAnnotation = "buildconfig-to-shipwright/buildrun-template"

// canonicalTriggerType normalizes the deprecated lowercase trigger type
// variants ("github", "generic", "imageChange") to their canonical names.
func canonicalTriggerType(t buildv1.BuildTriggerType) buildv1.BuildTriggerType {
	switch t {
	case buildv1.GitHubWebHookBuildTriggerTypeDeprecated:
		return buildv1.GitHubWebHookBuildTriggerType
	case buildv1.GenericWebHookBuildTriggerTypeDeprecated:
		return buildv1.GenericWebHookBuildTriggerType
	case buildv1.ImageChangeBuildTriggerTypeDeprecated:
		return buildv1.ImageChangeBuildTriggerType
	default:
		return t
	}
}

// processTriggers warns, per trigger, that spec.triggers automation is
// dropped during migration (BUILD-2257). Neither the Builds for Red Hat
// OpenShift operator nor upstream Shipwright provides working webhook or
// image-change triggering today, so the honest extent of support is a clear,
// typed warning with a manual path forward. Called only for BuildConfigs
// that are actually converted — passthrough paths (Custom/JenkinsPipeline
// strategies, missing output) keep the original BuildConfig, so nothing is
// dropped there.
func (c *Converter) processTriggers(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if len(bc.Spec.Triggers) == 0 {
		return
	}

	seen := map[string]bool{}
	for _, trigger := range bc.Spec.Triggers {
		t := canonicalTriggerType(trigger.Type)
		seen[string(t)] = true

		switch t {
		case buildv1.GitHubWebHookBuildTriggerType,
			buildv1.GitLabWebHookBuildTriggerType,
			buildv1.BitbucketWebHookBuildTriggerType:
			c.Log.Warnf(webhookTriggerWarning, bc.Name, t)
		case buildv1.GenericWebHookBuildTriggerType:
			msg := fmt.Sprintf(webhookTriggerWarning, bc.Name, t)
			if trigger.GenericWebHook != nil && trigger.GenericWebHook.AllowEnv {
				msg += " Note: webhook-injected environment variables (allowEnv) have no equivalent in Shipwright."
			}
			c.Log.Warn(msg)
		case buildv1.ImageChangeBuildTriggerType:
			c.Log.Warnf("BuildConfig %s: ImageChange trigger is dropped — builds will no longer start when %s changes. Shipwright has no equivalent of image change triggers today.",
				bc.Name, imageChangeWatchedRef(bc, trigger.ImageChange))
		case buildv1.ConfigChangeBuildTriggerType:
			if b != nil && b.Annotations[BuildRunTemplateAnnotation] != "" {
				c.Log.Warnf("BuildConfig %s: ConfigChange trigger is dropped — the automatic first build will not happen. The generated Build carries a BuildRun template (annotation %s); apply it once after review to start the first build.",
					bc.Name, BuildRunTemplateAnnotation)
			} else {
				c.Log.Warnf("BuildConfig %s: ConfigChange trigger is dropped — the automatic first build will not happen; create a BuildRun manually once to start the first build.",
					bc.Name)
			}
		default:
			c.Log.Warnf("BuildConfig %s: unsupported trigger type %q is dropped during migration.", bc.Name, trigger.Type)
		}
	}

	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)
	c.Log.Warnf("Found %d trigger(s) (%s) on BuildConfig %s — none work in Shipwright today; builds must be started manually or by your own automation.",
		len(bc.Spec.Triggers), strings.Join(types, ", "), bc.Name)
}

// webhookTriggerWarning is shared by all webhook trigger types; it takes the
// BuildConfig name and the canonical trigger type.
const webhookTriggerWarning = "BuildConfig %s: %s webhook trigger is dropped — the old OpenShift webhook URL will stop working after migration, and Shipwright provides no replacement URL. Set up Pipelines-as-Code or Tekton Triggers to create BuildRuns on push events."

// imageChangeWatchedRef names what an ImageChange trigger was watching: the
// referenced ImageStreamTag, or the build strategy's From image when the
// trigger has an empty From (only one such trigger is allowed per
// BuildConfig, and it watches the strategy image).
func imageChangeWatchedRef(bc *buildv1.BuildConfig, ict *buildv1.ImageChangeTrigger) string {
	if ict != nil && ict.From != nil && ict.From.Name != "" {
		ns := ict.From.Namespace
		if ns == "" {
			ns = bc.Namespace
		}
		return fmt.Sprintf("%s %s/%s", ict.From.Kind, ns, ict.From.Name)
	}

	var from *corev1.ObjectReference
	switch {
	case bc.Spec.Strategy.DockerStrategy != nil:
		from = bc.Spec.Strategy.DockerStrategy.From
	case bc.Spec.Strategy.SourceStrategy != nil:
		from = &bc.Spec.Strategy.SourceStrategy.From
	}
	if from != nil && from.Name != "" {
		return fmt.Sprintf("the strategy image %s", from.Name)
	}
	return "the strategy image"
}
