package buildconfig

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// sanitizedTrigger is the schema of one OriginalTriggersAnnotation entry:
// only non-secret, non-runtime trigger fields survive sanitization. The
// original trigger type is preserved verbatim (including deprecated lowercase
// variants) — this is a preservation record, not a normalization.
type sanitizedTrigger struct {
	Type            buildv1.BuildTriggerType `json:"type"`
	SecretReference *sanitizedSecretRef      `json:"secretReference,omitempty"`
	AllowEnv        bool                     `json:"allowEnv,omitempty"`
	ImageChange     *sanitizedImageChange    `json:"imageChange,omitempty"`
}

type sanitizedSecretRef struct {
	Name string `json:"name"`
}

type sanitizedImageChange struct {
	From   *sanitizedFrom `json:"from,omitempty"`
	Paused bool           `json:"paused,omitempty"`
}

type sanitizedFrom struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// triggerWebhook returns the webhook config for whichever webhook field
// matches the trigger's (canonicalized) type, or nil for non-webhook types.
func triggerWebhook(trigger buildv1.BuildTriggerPolicy) *buildv1.WebHookTrigger {
	switch canonicalTriggerType(trigger.Type) {
	case buildv1.GitHubWebHookBuildTriggerType:
		return trigger.GitHubWebHook
	case buildv1.GenericWebHookBuildTriggerType:
		return trigger.GenericWebHook
	case buildv1.GitLabWebHookBuildTriggerType:
		return trigger.GitLabWebHook
	case buildv1.BitbucketWebHookBuildTriggerType:
		return trigger.BitbucketWebHook
	}
	return nil
}

// sanitizeTrigger keeps type, secretReference name, allowEnv, imageChange
// from, and paused; it drops deprecated inline secret values and
// lastTriggeredImageID so secrets and runtime state never reach the
// annotation.
func sanitizeTrigger(trigger buildv1.BuildTriggerPolicy) sanitizedTrigger {
	s := sanitizedTrigger{Type: trigger.Type}
	if wh := triggerWebhook(trigger); wh != nil {
		if wh.SecretReference != nil && wh.SecretReference.Name != "" {
			s.SecretReference = &sanitizedSecretRef{Name: wh.SecretReference.Name}
		}
		s.AllowEnv = wh.AllowEnv
	}
	if ict := trigger.ImageChange; ict != nil {
		// An empty imageChange block is kept, not omitted: it is the form
		// that means "watch the build strategy's From image", which is
		// distinct from an ImageChange trigger carrying no parameters at all.
		ic := &sanitizedImageChange{Paused: ict.Paused}
		if ict.From != nil {
			ic.From = &sanitizedFrom{Kind: ict.From.Kind, Namespace: ict.From.Namespace, Name: ict.From.Name}
		}
		s.ImageChange = ic
	}
	return s
}

// preserveOriginalTriggers stores the sanitized spec.triggers on the
// converted Build under OriginalTriggersAnnotation (BUILD-2392). No
// annotation is written when the BuildConfig has no triggers.
//
// Nothing reads the annotation today; it exists so that once triggers become
// available (upstream trigger sources BUILD-2074, operator install
// BUILD-1706), a tool or a person can rebuild them without the original
// BuildConfig. Deprecated inline webhook secret values and
// lastTriggeredImageID (runtime state) are never included.
func (c *Converter) preserveOriginalTriggers(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if b == nil || len(bc.Spec.Triggers) == 0 {
		return
	}
	sanitized := make([]sanitizedTrigger, 0, len(bc.Spec.Triggers))
	for _, trigger := range bc.Spec.Triggers {
		sanitized = append(sanitized, sanitizeTrigger(trigger))
	}
	data, err := json.Marshal(sanitized)
	if err != nil {
		c.warnf("BuildConfig %s: could not preserve original triggers in annotation %s: %v", bc.Name, OriginalTriggersAnnotation, err)
		return
	}
	if b.Annotations == nil {
		b.Annotations = map[string]string{}
	}
	b.Annotations[OriginalTriggersAnnotation] = string(data)
}

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

	c.preserveOriginalTriggers(bc, b)

	seen := map[string]bool{}
	for _, trigger := range bc.Spec.Triggers {
		t := canonicalTriggerType(trigger.Type)
		seen[string(t)] = true

		switch t {
		case buildv1.GitHubWebHookBuildTriggerType,
			buildv1.GitLabWebHookBuildTriggerType,
			buildv1.BitbucketWebHookBuildTriggerType:
			c.warnf(webhookTriggerWarning, bc.Name, t)
		case buildv1.GenericWebHookBuildTriggerType:
			msg := fmt.Sprintf(webhookTriggerWarning, bc.Name, t)
			if trigger.GenericWebHook != nil && trigger.GenericWebHook.AllowEnv {
				msg += " Note: webhook-injected environment variables (allowEnv) have no equivalent in Shipwright."
			}
			c.warnf("%s", msg)
		case buildv1.ImageChangeBuildTriggerType:
			c.warnf("BuildConfig %s: ImageChange trigger is dropped — builds will no longer start when %s changes. Shipwright has no equivalent of image change triggers today.",
				bc.Name, imageChangeWatchedRef(bc, trigger.ImageChange))
		case buildv1.ConfigChangeBuildTriggerType:
			// Checked at runtime rather than assumed, so there is no hard
			// dependency on the BuildRun template (BUILD-2261): when the
			// annotation is absent the warning falls back to manual wording.
			if b != nil && b.Annotations[BuildRunTemplateAnnotation] != "" {
				c.warnf("BuildConfig %s: ConfigChange trigger is dropped — the automatic first build will not happen. The generated Build carries a BuildRun template (annotation %s); apply it once after review to start the first build.",
					bc.Name, BuildRunTemplateAnnotation)
			} else {
				c.warnf("BuildConfig %s: ConfigChange trigger is dropped — the automatic first build will not happen; create a BuildRun manually once to start the first build.",
					bc.Name)
			}
		default:
			c.warnf("BuildConfig %s: unsupported trigger type %q is dropped during migration.", bc.Name, trigger.Type)
		}
	}

	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)
	c.warnf("Found %d trigger(s) (%s) on BuildConfig %s — none work in Shipwright today; builds must be started manually or by your own automation.",
		len(bc.Spec.Triggers), strings.Join(types, ", "), bc.Name)
}

// webhookTriggerWarning is shared by all webhook trigger types; it takes the
// BuildConfig name and the canonical trigger type.
const webhookTriggerWarning = "BuildConfig %s: %s webhook trigger is dropped — the old OpenShift webhook URL will stop working after migration, and Shipwright provides no replacement URL. Remove or repoint the webhook in your Git provider, then set up Pipelines-as-Code or Tekton Triggers to create BuildRuns on push events."

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
