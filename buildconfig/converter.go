package buildconfig

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

const (
	defaultDockerStrategy = "buildah"
	defaultS2IStrategy    = "source-to-image"

	NoCacheParamName          = "no-cache"
	SquashParamName           = "squash"
	ForcePullParamName        = "pull"
	RuntimeStageFromParamName = "runtime-stage-from"
	BuildArgsParamName        = "build-args"

	Timeout = 10 * time.Minute

	ConvertedFromAnnotation      = "crane.konveyor.io/converted-from"
	ConversionWarningsAnnotation = "crane.konveyor.io/conversion-warnings"

	// maxConversionWarningsBytes caps the ConversionWarningsAnnotation value.
	// Kubernetes rejects an object whose annotations total more than 256 KiB
	// (apimachinery TotalAnnotationSizeLimitB), and warning text embeds
	// user-controlled build arg names, so an unbounded value could make the
	// converted Build unappliable. 32 KiB holds well over a hundred warnings
	// and leaves the rest of the budget to BuildRunTemplateAnnotation and
	// OriginalTriggersAnnotation. Warnings are always logged in full.
	maxConversionWarningsBytes = 32 << 10
	BuildRunTemplateAnnotation = "buildconfig-to-shipwright/buildrun-template"
	OriginalTriggersAnnotation = "buildconfig-to-shipwright/original-triggers"
	// ConversionOutcomeAnnotation records the conversion disposition on the
	// generated Build so the outcome is observable in the output, not only in
	// the logs (BUILD-2318). Set to OutcomeConverted or OutcomeConvertedWithWarnings.
	ConversionOutcomeAnnotation = "buildconfig-to-shipwright/conversion-outcome"
	// ConversionReasonAnnotation records why a BuildConfig was skipped or failed,
	// on the passed-through BuildConfig itself. Without it a BuildConfig the
	// plugin declined is indistinguishable in the output from one it never saw
	// (BUILD-2319).
	ConversionReasonAnnotation = "buildconfig-to-shipwright/conversion-reason"

	ConfigMapsRFE = "https://issues.redhat.com/browse/BUILD-1745"
	SecretsRFE    = "https://issues.redhat.com/browse/BUILD-1744"
	// VolumeMigrationDoc is the runbook for making converted Build volumes
	// pass Shipwright validation (repo-relative; upstream URL not assumed).
	VolumeMigrationDoc  = "docs/volume-migration.md in the crane-plugin-buildconfig-to-shipwright repository"
	CustomScriptsRFE    = "https://issues.redhat.com/browse/BUILD-1641"
	IncrementalBuildRFE = "https://issues.redhat.com/browse/BUILD-1607"
	ForcePullFlagS2iRFE = "https://issues.redhat.com/browse/BUILD-1606"
)

// The per-BuildConfig conversion outcome model (OutcomeState, Outcome, the
// outcome* constructors) and the warnf warning recorder live in outcome.go.

type Converter struct {
	Log  logrus.FieldLogger
	Opts PluginOptionalFields

	// curNS and curName name the BuildConfig currently being converted. warnf
	// reads them to attribute every warning, so a run covering hundreds of
	// BuildConfigs produces a log an operator can actually trace (BUILD-2319).
	curNS, curName string

	// warnings collects every conversion warning recorded via warnf so a
	// conversion can be classified as converted-with-warnings and the messages
	// surfaced on the Outcome. It accumulates across the Converter's lifetime;
	// Convert slices out the per-BuildConfig warnings by index.
	warnings []string

	// assignedNames tracks generated names (keyed by kind/namespace/name) so
	// that distinct originals resolving to the same sanitized name within a
	// single converter lifetime are detected and disambiguated.
	assignedNames map[string]string
}

// uniqueName sanitizes a generated resource name into a valid DNS-1123 label
// and guards against two distinct original names resolving to the same final
// name for the same kind and namespace.
func (c *Converter) uniqueName(kind, namespace, original string) string {
	name, changed := sanitizeDNS1123Label(original)
	if changed {
		c.warnf("Generated %s name %q is not a valid DNS-1123 label of at most %d characters — using %q instead", kind, original, maxGeneratedNameLength, name)
	}

	if c.assignedNames == nil {
		c.assignedNames = map[string]string{}
	}
	key := kind + "/" + namespace + "/" + name
	if owner, ok := c.assignedNames[key]; ok && owner != original {
		name = withHashSuffix(name, original)
		c.warnf("Generated %s name for %q collides with the name already generated for %q — using %q instead", kind, original, owner, name)
		key = kind + "/" + namespace + "/" + name
		if owner, ok := c.assignedNames[key]; ok && owner != original {
			// A genuine error (resources may overwrite each other), so log it
			// loudly — but still record it as a conversion warning so the
			// outcome reflects it.
			msg := fmt.Sprintf("Hash-suffixed %s name %q for %q still collides with the name already generated for %q — resources may overwrite each other", kind, name, original, owner)
			c.Log.Error(c.recordWarning(msg))
		}
	}
	c.assignedNames[key] = original
	return name
}

func (c *Converter) Convert(bc *buildv1.BuildConfig) ([]unstructured.Unstructured, Outcome) {
	c.curNS, c.curName = bc.Namespace, bc.Name
	// Clear the attribution once this conversion is done so a warning raised
	// later on a reused Converter is not misattributed to this BuildConfig.
	defer func() { c.curNS, c.curName = "", "" }()
	startWarnings := len(c.warnings)
	b := &shipwrightv1beta1.Build{}
	b.Name = c.uniqueName("Build", bc.Namespace, bc.Name)
	b.Kind = "Build"
	b.APIVersion = "shipwright.io/v1beta1"
	b.Namespace = bc.Namespace
	b.Spec.ParamValues = []shipwrightv1beta1.ParamValue{}
	b.Annotations = c.copyAnnotations(bc)
	b.Annotations[ConvertedFromAnnotation] = fmt.Sprintf("build.openshift.io/v1/BuildConfig/%s", bc.Name)
	b.Labels = c.copyLabels(bc)

	var newResources []unstructured.Unstructured

	switch bc.Spec.Strategy.Type {
	case buildv1.DockerBuildStrategyType:
		c.Log.Infof("Docker strategy detected for BuildConfig %s", bc.Name)
		if err := c.processDockerStrategy(bc, b); err != nil {
			return nil, outcomeFailed(err.Error())
		}
	case buildv1.SourceBuildStrategyType:
		c.Log.Infof("Source strategy detected for BuildConfig %s", bc.Name)
		if err := c.processSourceStrategy(bc, b); err != nil {
			return nil, outcomeFailed(err.Error())
		}
	case buildv1.CustomBuildStrategyType:
		reason := "Custom build strategy is not supported for conversion"
		c.warnf("%s — passing BuildConfig %s through unchanged", reason, bc.Name)
		return nil, outcomeSkipped(reason)
	case buildv1.JenkinsPipelineBuildStrategyType:
		reason := "JenkinsPipeline build strategy is not supported for conversion"
		c.warnf("%s — passing BuildConfig %s through unchanged. Consider migrating to Tekton Pipelines directly.", reason, bc.Name)
		return nil, outcomeSkipped(reason)
	default:
		return nil, outcomeFailed(fmt.Sprintf("unknown build strategy type %q for BuildConfig %s", bc.Spec.Strategy.Type, bc.Name))
	}

	// Shipwright Builds require spec.output.image; a BuildConfig without an
	// output image cannot be converted into a valid Build.
	if bc.Spec.Output.To == nil || bc.Spec.Output.To.Name == "" {
		reason := "BuildConfig has no output image (spec.output.to is missing or empty); a Shipwright Build requires spec.output.image"
		c.warnf("%s — passing BuildConfig %s through unchanged", reason, bc.Name)
		return nil, outcomeSkipped(reason)
	}

	// PullSecret → ServiceAccount
	generatedSA := ""
	pullSecret := c.getPullSecret(bc)
	if pullSecret != nil {
		if bc.Spec.ServiceAccount != "" {
			// crane migrates the named ServiceAccount as its own resource and
			// this plugin never sees it, so emitting a same-named account would
			// overwrite it (crane keeps the last duplicate; imagePullSecrets is
			// an atomic list on apply). Leave the account alone and tell the
			// operator how to attach the pull secret on the target.
			ns, sa, secret := bc.Namespace, bc.Spec.ServiceAccount, pullSecret.Name
			c.warnf("BuildConfig %s/%s names ServiceAccount %q and pull secret %q. crane migrates that ServiceAccount as-is and this conversion does not modify it, so attach the pull secret on the target cluster before running the BuildRun: oc -n %s secrets link %s %s --for=pull,mount",
				ns, bc.Name, sa, secret, ns, sa, secret)
		} else {
			sa := c.generateServiceAccount(bc, pullSecret)
			generatedSA = sa.Name
			saUnstructured, err := toUnstructured(sa)
			if err != nil {
				return nil, outcomeFailed(fmt.Sprintf("error converting ServiceAccount to unstructured: %v", err))
			}
			newResources = append(newResources, saUnstructured)
		}
	}

	// A ServiceAccount named by the BuildConfig exists on the source cluster and
	// carries associations this conversion cannot see, let alone migrate: secrets,
	// imagePullSecrets, RoleBindings/ClusterRoleBindings, and SCC associations.
	// Crane converts BuildConfigs, not RBAC objects, so those stay behind. Without
	// this warning the failure surfaces much later and far from its cause — either
	// the BuildRun runs as the namespace default ServiceAccount and fails on an
	// image pull or push, or it fails outright with ServiceAccountNotFound.
	if bc.Spec.ServiceAccount != "" {
		c.warnf("The original ServiceAccount %q on BuildConfig %s/%s may carry additional secrets, imagePullSecrets, and RBAC bindings. Verify these associations are available in the target cluster for the Shipwright BuildRun.",
			bc.Spec.ServiceAccount, bc.Namespace, bc.Name)
	}

	// Inline Dockerfile → ConfigMap, before processSource so a BuildConfig skipped
	// above never emits an orphan ConfigMap. Appended after the ServiceAccount.
	if cm := c.processInlineDockerfile(bc, b); cm != nil {
		cmUnstructured, err := toUnstructured(cm)
		if err != nil {
			return nil, outcomeFailed(fmt.Sprintf("error converting inline-Dockerfile ConfigMap to unstructured: %v", err))
		}
		newResources = append(newResources, cmUnstructured)
	}

	if err := c.processSource(bc, b); err != nil {
		return nil, outcomeFailed(err.Error())
	}
	c.processOutput(bc, b)
	c.processCompletionDeadline(bc, b)
	c.processNodeSelector(bc, b)
	c.processRunPolicy(bc)
	c.processPostCommit(bc)
	c.processBuildsHistoryLimits(bc, b)
	c.addRegistries(b)
	c.processTriggers(bc, b)

	if err := c.processResources(bc, b, generatedSA); err != nil {
		return nil, outcomeFailed(err.Error())
	}

	// Classify the outcome now that every field has been processed, and record
	// it on the Build so the disposition is observable in the output (BUILD-2318).
	outcome := outcomeConverted()
	if len(c.warnings) > startWarnings {
		outcome.State = OutcomeConvertedWithWarnings
		// Build the annotation before snapshotting: boundedWarnings records a
		// warning of its own when it truncates, and snapshotting first would
		// leave that notice out of the Outcome the caller sees.
		b.Annotations[ConversionWarningsAnnotation] = c.boundedWarnings(c.warnings[startWarnings:])
		// Copy so the Outcome does not alias the Converter's growing slice.
		outcome.Warnings = append([]string(nil), c.warnings[startWarnings:]...)
	}
	b.Annotations[ConversionOutcomeAnnotation] = string(outcome.State)

	buildUnstructured, err := toUnstructured(b)
	if err != nil {
		return nil, outcomeFailed(fmt.Sprintf("error converting Build to unstructured: %v", err))
	}

	result := []unstructured.Unstructured{buildUnstructured}
	result = append(result, newResources...)
	return result, outcome
}

// copyLabels copies user-defined metadata.labels from the BuildConfig to the
// generated Shipwright Build. OpenShift-internal labels (openshift.io/build*
// and the deprecated "buildconfig" label) reference BuildConfig concepts that
// do not exist after migration, so they are filtered out. Returns nil when no
// labels survive filtering.
func (c *Converter) copyLabels(bc *buildv1.BuildConfig) map[string]string {
	return c.filterMetadata("label", bc.Name, bc.Labels,
		[]string{"openshift.io/build"},
		[]string{buildv1.BuildConfigLabelDeprecated})
}

// copyAnnotations copies user-defined metadata.annotations from the
// BuildConfig to the generated Shipwright Build. Platform-managed annotations
// (anything under openshift.io/ or kubectl.kubernetes.io/, e.g.
// openshift.io/generated-by or kubectl.kubernetes.io/last-applied-configuration)
// describe pre-migration state and are filtered out. Always returns a non-nil
// map so the caller can add converter-owned annotations to it.
func (c *Converter) copyAnnotations(bc *buildv1.BuildConfig) map[string]string {
	annotations := c.filterMetadata("annotation", bc.Name, bc.Annotations,
		[]string{"openshift.io/", "kubectl.kubernetes.io/"},
		nil)
	if annotations == nil {
		annotations = map[string]string{}
	}
	return annotations
}

// filterMetadata copies a metadata map (labels or annotations), dropping any
// key that matches one of dropPrefixes or dropKeys. Each drop is logged at
// INFO with the metadata kind so migrations remain auditable. Returns nil when
// the input is empty or nothing survives filtering, so empty maps are omitted
// from the serialized Build.
func (c *Converter) filterMetadata(kind, bcName string, in map[string]string, dropPrefixes, dropKeys []string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		if metadataKeyMatches(k, dropPrefixes, dropKeys) {
			c.Log.Infof("Dropping OpenShift-internal %s %q from BuildConfig %s — it references pre-migration build machinery", kind, k, bcName)
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func metadataKeyMatches(key string, prefixes, keys []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	for _, k := range keys {
		if key == k {
			return true
		}
	}
	return false
}

// validBuildArgName reports whether a build arg name is safe to embed in a
// docker --build-arg NAME=VALUE pair or a Shipwright ObjectKeyRef Format
// template. Names containing '=', '$', '{', '}', whitespace, or control
// characters would corrupt the emitted format — e.g. a name containing
// ${SECRET_VALUE} would be substituted a second time at BuildRun resolution,
// relocating secret material into the arg-name position — and can never match
// a Dockerfile ARG, so such args are skipped with a warning.
func validBuildArgName(name string) bool {
	if name == "" {
		return false
	}
	if strings.ContainsAny(name, "=${}") {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// boundedWarnings joins warnings for ConversionWarningsAnnotation, keeping the
// value under maxConversionWarningsBytes. Only whole warnings are kept; when
// any are dropped a final line records how many, so a truncated annotation is
// never mistaken for the complete set. The full list is always in the logs.
func (c *Converter) boundedWarnings(warnings []string) string {
	joined := strings.Join(warnings, "\n")
	if len(joined) <= maxConversionWarningsBytes {
		return joined
	}

	// Reserve room for the omitted-count line up front. Its length grows with
	// the count, so budget for the worst case: every warning omitted.
	reserved := len(omittedWarningsNotice(len(warnings))) + 1 // +1 for its newline

	var b strings.Builder
	kept := 0
	for _, w := range warnings {
		needed := len(w)
		if kept > 0 {
			needed++ // separator newline
		}
		if b.Len()+needed+reserved > maxConversionWarningsBytes {
			break
		}
		if kept > 0 {
			b.WriteString("\n")
		}
		b.WriteString(w)
		kept++
	}

	if kept > 0 {
		b.WriteString("\n")
	}
	b.WriteString(omittedWarningsNotice(len(warnings) - kept))
	c.warnf("Conversion warnings exceeded %d bytes — %d of %d warnings were omitted from annotation %s; the full list is in the warnings logged above.", maxConversionWarningsBytes, len(warnings)-kept, len(warnings), ConversionWarningsAnnotation)
	return b.String()
}

// omittedWarningsNotice is the trailing line of a truncated warnings annotation.
func omittedWarningsNotice(omitted int) string {
	return fmt.Sprintf("... %d more conversion warning(s) omitted to stay within the Kubernetes annotation size limit — see the crane plugin logs for the full list.", omitted)
}

func (c *Converter) processDockerStrategy(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) error {
	strategyName := defaultDockerStrategy
	if override, ok := c.Opts.StrategyMapping["docker"]; ok && override != "" {
		strategyName = override
	}
	clusterKind := shipwrightv1beta1.ClusterBuildStrategyKind
	b.Spec.Strategy = shipwrightv1beta1.Strategy{
		Kind: &clusterKind,
		Name: strategyName,
	}

	ds := bc.Spec.Strategy.DockerStrategy
	if ds == nil {
		return nil
	}

	// From
	if ds.From != nil && ds.From.Name != "" {
		namespace := ds.From.Namespace
		if namespace == "" {
			namespace = bc.Namespace
		}
		imageRef, warning, err := resolveImageRef(string(ds.From.Kind), ds.From.Name, namespace, c.Opts)
		if err != nil {
			return fmt.Errorf("error resolving Docker strategy From field: %w", err)
		}
		if warning != "" {
			c.warnf("%s", warning)
		}
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:        RuntimeStageFromParamName,
			SingleValue: &shipwrightv1beta1.SingleValue{Value: &imageRef},
		})
	}

	// NoCache
	if ds.NoCache {
		noCacheValue := "true"
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:        NoCacheParamName,
			SingleValue: &shipwrightv1beta1.SingleValue{Value: &noCacheValue},
		})
	}

	// Env
	if ds.Env != nil {
		b.Spec.Env = append(b.Spec.Env, ds.Env...)
	}

	// ForcePull
	if ds.ForcePull {
		pullValue := "always"
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:        ForcePullParamName,
			SingleValue: &shipwrightv1beta1.SingleValue{Value: &pullValue},
		})
	}

	// DockerfilePath
	if ds.DockerfilePath != "" {
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:        "dockerfile",
			SingleValue: &shipwrightv1beta1.SingleValue{Value: &ds.DockerfilePath},
		})
	}

	// BuildArgs — literal values pass through as NAME=VALUE; ConfigMap/Secret
	// backed args map to Shipwright's native ObjectKeyRef resolution (resolved
	// at BuildRun time); fieldRef/resourceFieldRef have no Shipwright
	// equivalent and are skipped with a warning.
	if len(ds.BuildArgs) > 0 {
		values := []shipwrightv1beta1.SingleValue{}
		literal, mapped, skipped := 0, 0, 0
		for _, arg := range ds.BuildArgs {
			if !validBuildArgName(arg.Name) {
				c.warnf("Build arg with invalid name %q was skipped — names must be non-empty and must not contain '=', '$', '{', '}', whitespace, or control characters (BuildConfig %s).", arg.Name, bc.Name)
				skipped++
				continue
			}
			if arg.ValueFrom != nil && arg.Value != "" {
				c.warnf("Build arg %q sets both value and valueFrom; using valueFrom and ignoring the literal value (BuildConfig %s).", arg.Name, bc.Name)
			}
			switch {
			case arg.ValueFrom == nil:
				envNameValue := arg.Name + "=" + arg.Value
				values = append(values, shipwrightv1beta1.SingleValue{Value: &envNameValue})
				literal++
			case arg.ValueFrom.ConfigMapKeyRef != nil:
				ref := arg.ValueFrom.ConfigMapKeyRef
				if ref.Name == "" || ref.Key == "" {
					c.warnf("Build arg %q references a ConfigMap with an empty name or key and was skipped (BuildConfig %s).", arg.Name, bc.Name)
					skipped++
					continue
				}
				if ref.Optional != nil && *ref.Optional {
					c.warnf("Build arg %q references ConfigMap %q key %q with optional: true — Shipwright has no 'optional' equivalent; a missing key will fail the BuildRun (BuildConfig %s).", arg.Name, ref.Name, ref.Key, bc.Name)
				}
				format := arg.Name + "=${CONFIGMAP_VALUE}"
				values = append(values, shipwrightv1beta1.SingleValue{
					ConfigMapValue: &shipwrightv1beta1.ObjectKeyRef{Name: ref.Name, Key: ref.Key, Format: &format},
				})
				mapped++
			case arg.ValueFrom.SecretKeyRef != nil:
				ref := arg.ValueFrom.SecretKeyRef
				if ref.Name == "" || ref.Key == "" {
					c.warnf("Build arg %q references a Secret with an empty name or key and was skipped (BuildConfig %s).", arg.Name, bc.Name)
					skipped++
					continue
				}
				if ref.Optional != nil && *ref.Optional {
					c.warnf("Build arg %q references Secret %q key %q with optional: true — Shipwright has no 'optional' equivalent; a missing key will fail the BuildRun (BuildConfig %s).", arg.Name, ref.Name, ref.Key, bc.Name)
				}
				format := arg.Name + "=${SECRET_VALUE}"
				values = append(values, shipwrightv1beta1.SingleValue{
					SecretValue: &shipwrightv1beta1.ObjectKeyRef{Name: ref.Name, Key: ref.Key, Format: &format},
				})
				mapped++
			case arg.ValueFrom.FieldRef != nil || arg.ValueFrom.ResourceFieldRef != nil:
				c.warnf("Build arg %q uses fieldRef/resourceFieldRef which has no Shipwright equivalent. This build arg was skipped — set it manually in the generated Build (BuildConfig %s).", arg.Name, bc.Name)
				skipped++
			default:
				c.warnf("Build arg %q has an empty or unsupported valueFrom source. This build arg was skipped — set it manually in the generated Build (BuildConfig %s).", arg.Name, bc.Name)
				skipped++
			}
		}
		if len(values) > 0 {
			b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
				Name:   BuildArgsParamName,
				Values: values,
			})
		}
		c.Log.Infof("Processed %d build args: %d literal, %d mapped to ConfigMap/Secret refs, %d skipped for BuildConfig %s", len(ds.BuildArgs), literal, mapped, skipped, bc.Name)
	}

	// ImageOptimizationPolicy (squash)
	if ds.ImageOptimizationPolicy != nil {
		switch *ds.ImageOptimizationPolicy {
		case buildv1.ImageOptimizationSkipLayers, buildv1.ImageOptimizationSkipLayersAndWarn:
			squashValue := "true"
			b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
				Name:        SquashParamName,
				SingleValue: &shipwrightv1beta1.SingleValue{Value: &squashValue},
			})
		case buildv1.ImageOptimizationNone:
			// no param needed
		}
	}

	// Volumes — converted to Build spec volumes under their original names.
	// Shipwright rejects the Build (Registered=False, reason UndefinedVolume)
	// until the strategy declares matching overridable volumes; per-volume
	// remediation is emitted by processStrategyVolumes.
	if len(ds.Volumes) > 0 {
		if converted := c.processStrategyVolumes(bc, ds.Volumes, b); converted > 0 {
			c.warnStrategyVolumesRejected("Buildah")
		}
	}

	return nil
}

func (c *Converter) processSourceStrategy(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) error {
	strategyName := defaultS2IStrategy
	if override, ok := c.Opts.StrategyMapping["s2i"]; ok && override != "" {
		strategyName = override
	}
	clusterKind := shipwrightv1beta1.ClusterBuildStrategyKind
	b.Spec.Strategy = shipwrightv1beta1.Strategy{
		Kind: &clusterKind,
		Name: strategyName,
	}

	ss := bc.Spec.Strategy.SourceStrategy
	if ss == nil {
		return nil
	}

	// From → builder-image param
	if ss.From.Name != "" {
		namespace := ss.From.Namespace
		if namespace == "" {
			namespace = bc.Namespace
		}
		kind := string(ss.From.Kind)
		if kind == "" {
			kind = "ImageStreamTag"
		}
		imageRef, warning, err := resolveImageRef(kind, ss.From.Name, namespace, c.Opts)
		if err != nil {
			return fmt.Errorf("error resolving Source strategy From field: %w", err)
		}
		if warning != "" {
			c.warnf("%s", warning)
		}
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:        "builder-image",
			SingleValue: &shipwrightv1beta1.SingleValue{Value: &imageRef},
		})
	}

	// Env
	if ss.Env != nil {
		b.Spec.Env = append(b.Spec.Env, ss.Env...)
	}

	// Warnings for unsupported features
	if ss.Scripts != "" {
		c.warnf("Custom scripts are not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: %s", CustomScriptsRFE)
	}
	if ss.Incremental != nil && *ss.Incremental {
		c.warnf("Incremental build is not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: %s", IncrementalBuildRFE)
	}
	if ss.ForcePull {
		c.warnf("ForcePull flag is not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: %s", ForcePullFlagS2iRFE)
	}
	// Volumes — converted to Build spec volumes under their original names.
	// Shipwright rejects the Build (Registered=False, reason UndefinedVolume)
	// until the strategy declares matching overridable volumes; per-volume
	// remediation is emitted by processStrategyVolumes.
	if len(ss.Volumes) > 0 {
		if converted := c.processStrategyVolumes(bc, ss.Volumes, b); converted > 0 {
			c.warnStrategyVolumesRejected("Source-to-Image")
		}
	}

	return nil
}

// processStrategyVolumes converts BuildConfig strategy volumes into Shipwright
// Build spec volumes and returns the number of volumes appended. Secret and
// ConfigMap sources are supported; volumes with an empty name, a duplicate
// name, or an unsupported source type are skipped with a warning so the rest
// of the conversion can proceed. Each converted volume gets a remediation
// warning: Shipwright matches Build volumes to strategy volumes by exact
// name, takes mount paths only from strategy step volumeMounts, and rejects
// Builds whose volume names the strategy does not declare (UndefinedVolume).
func (c *Converter) processStrategyVolumes(bc *buildv1.BuildConfig, volumes []buildv1.BuildVolume, b *shipwrightv1beta1.Build) int {
	converted := 0
	seen := make(map[string]bool, len(volumes))
	for _, bcVolume := range volumes {
		c.Log.Infof("Processing volume %q for BuildConfig %s", bcVolume.Name, bc.Name)

		if bcVolume.Name == "" {
			c.warnf("Skipping volume with empty name for BuildConfig %s: the Shipwright Build API requires volumes to be named", bc.Name)
			continue
		}
		if seen[bcVolume.Name] {
			c.warnf("Skipping duplicate volume %q for BuildConfig %s: a volume with this name was already converted", bcVolume.Name, bc.Name)
			continue
		}

		volumeSource, err := convertBuildVolumeSource(bcVolume.Source)
		if err != nil {
			c.warnf("Skipping volume %q for BuildConfig %s: %v", bcVolume.Name, bc.Name, err)
			continue
		}

		seen[bcVolume.Name] = true
		b.Spec.Volumes = append(b.Spec.Volumes, shipwrightv1beta1.BuildVolume{
			Name:         bcVolume.Name,
			VolumeSource: volumeSource,
		})
		converted++

		destinations := "no destination paths were declared in the BuildConfig; use the path your build expects"
		paths := make([]string, 0, len(bcVolume.Mounts))
		for _, m := range bcVolume.Mounts {
			// Defensive: file-sourced BuildConfigs may carry empty destination
			// paths that API-server validation would normally reject.
			if m.DestinationPath != "" {
				paths = append(paths, m.DestinationPath)
			}
		}
		if len(paths) > 0 {
			destinations = "original BuildConfig destination paths: " + strings.Join(paths, ", ")
		}
		c.warnf("Volume %q was converted, but the Build will fail validation (reason: UndefinedVolume) until you: (1) add an overridable volume named '%s' to your ClusterBuildStrategy copy — volumes: [{name: %s, overridable: true, emptyDir: {}}] (placeholder source; the converted Build's override supplies the real Secret/ConfigMap), (2) add a volumeMount for '%s' on the strategy build step (%s), (3) point the Build at the strategy copy via spec.strategy.name. See %s.", bcVolume.Name, bcVolume.Name, bcVolume.Name, bcVolume.Name, destinations, VolumeMigrationDoc)
	}
	return converted
}

// warnStrategyVolumesRejected emits the per-BuildConfig summary warning for
// converted strategy volumes: Shipwright validates Build spec volumes by name
// against the strategy, so conversion alone leaves the Build failing
// validation until the strategy declares the volumes.
func (c *Converter) warnStrategyVolumesRejected(strategyLabel string) {
	c.warnf("Volumes were converted to Build spec volumes, but the shipped %s ClusterBuildStrategy does not declare them: Shipwright will reject the Build (Registered=False, reason: UndefinedVolume) until a matching volume with 'overridable: true' is added to a copy of the strategy. See %s.", strategyLabel, VolumeMigrationDoc)
}

// convertBuildVolumeSource converts an OpenShift BuildVolumeSource into the
// Kubernetes VolumeSource used by Shipwright.
func convertBuildVolumeSource(bcSource buildv1.BuildVolumeSource) (corev1.VolumeSource, error) {
	volumeSource := corev1.VolumeSource{}

	switch bcSource.Type {
	case buildv1.BuildVolumeSourceTypeSecret:
		if bcSource.Secret == nil {
			return volumeSource, fmt.Errorf("secret volume source is nil")
		}
		volumeSource.Secret = bcSource.Secret
	case buildv1.BuildVolumeSourceTypeConfigMap:
		if bcSource.ConfigMap == nil {
			return volumeSource, fmt.Errorf("configMap volume source is nil")
		}
		volumeSource.ConfigMap = bcSource.ConfigMap
	default:
		return volumeSource, fmt.Errorf("unsupported volume source type %q; supported types are Secret and ConfigMap", bcSource.Type)
	}

	return volumeSource, nil
}

func (c *Converter) getPullSecret(bc *buildv1.BuildConfig) *corev1.LocalObjectReference {
	if bc.Spec.Strategy.DockerStrategy != nil && bc.Spec.Strategy.DockerStrategy.PullSecret != nil {
		return bc.Spec.Strategy.DockerStrategy.PullSecret
	}
	if bc.Spec.Strategy.SourceStrategy != nil && bc.Spec.Strategy.SourceStrategy.PullSecret != nil {
		return bc.Spec.Strategy.SourceStrategy.PullSecret
	}
	return nil
}

// generateServiceAccount builds a ServiceAccount that carries the BuildConfig's
// pull secret. It is only called when the BuildConfig names no ServiceAccount of
// its own, so the generated name always derives from the BuildConfig name: a
// named ServiceAccount is migrated by crane as its own resource and this plugin
// must not emit a same-named object that would overwrite it.
func (c *Converter) generateServiceAccount(bc *buildv1.BuildConfig, pullSecret *corev1.LocalObjectReference) *corev1.ServiceAccount {
	if pullSecret == nil {
		return nil
	}
	saName := c.uniqueName("ServiceAccount", bc.Namespace, bc.Name)

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: bc.Namespace,
		},
		ImagePullSecrets: []corev1.LocalObjectReference{
			{Name: pullSecret.Name},
		},
		Secrets: []corev1.ObjectReference{
			{Name: pullSecret.Name},
		},
	}
}

// processSource maps the BuildConfig source onto the Build. It returns an error
// when the source cannot be represented in Shipwright at all (multiple source
// types, an extracted binary archive, multiple image sources, or an
// unresolvable image) — those leave the Build with no usable source, so the
// caller fails the whole conversion rather than shipping an incomplete Build
// (BUILD-2318). Degradations that still yield a usable source (ignored image
// As/Paths, an absent source, a non-git sourceSecret) are warnings, not errors.
// The inline Dockerfile is handled earlier by processInlineDockerfile.
func (c *Converter) processSource(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) error {
	git := bc.Spec.Source.Git
	binary := bc.Spec.Source.Binary
	images := bc.Spec.Source.Images

	// sourceSecret only authenticates git clones (ssh-privatekey / basic-auth). On a
	// binary, image, or source-less BuildConfig it was inert on OpenShift too, so there
	// is nothing to map — warn once, attributed to the BuildConfig, and drop it. This
	// fires before the multiple-source error below so the drop is recorded either way.
	if git == nil && bc.Spec.Source.SourceSecret != nil && bc.Spec.Source.SourceSecret.Name != "" {
		c.warnf("BuildConfig %s/%s sets sourceSecret %q but has no git source; sourceSecret only authenticates git clones and was not migrated.", bc.Namespace, bc.Name, bc.Spec.Source.SourceSecret.Name)
	}

	sourceCount := 0
	if git != nil {
		sourceCount++
	}
	if binary != nil {
		sourceCount++
	}
	if len(images) > 0 {
		sourceCount++
	}

	if sourceCount > 1 {
		return fmt.Errorf("multiple source types are not supported in a single build in Shipwright (BuildConfig %s)", bc.Name)
	}

	if sourceCount == 0 {
		c.warnf("No source type specified for BuildConfig: %s", bc.Name)
		return nil
	}

	if git != nil {
		var cloneSecret *string
		if bc.Spec.Source.SourceSecret != nil {
			cloneSecret = &bc.Spec.Source.SourceSecret.Name
		}

		git := &shipwrightv1beta1.Git{
			URL:         bc.Spec.Source.Git.URI,
			CloneSecret: cloneSecret,
		}
		if bc.Spec.Source.Git.Ref != "" {
			git.Revision = &bc.Spec.Source.Git.Ref
		}

		source := &shipwrightv1beta1.Source{
			Git:  git,
			Type: shipwrightv1beta1.GitType,
		}
		b.Spec.Source = source
		c.processGitProxyConfig(bc, b)
	} else if binary != nil {
		source := &shipwrightv1beta1.Source{
			Type: shipwrightv1beta1.LocalType,
			Local: &shipwrightv1beta1.Local{
				Name:    "local-copy",
				Timeout: &metav1.Duration{Duration: Timeout},
			},
		}
		if bc.Spec.Source.Binary.AsFile == "" {
			return fmt.Errorf("binary archive source (extracted archive) is not supported in Shipwright, only single-file binary sources (asFile) (BuildConfig %s)", bc.Name)
		}
		c.Log.Infof("Processing binary source as single file (asFile: %s). BuildConfig: %s", bc.Spec.Source.Binary.AsFile, bc.Name)
		b.Spec.Source = source
	} else if len(images) > 0 {
		if len(images) > 1 {
			return fmt.Errorf("multiple image sources are not supported in Shipwright (BuildConfig %s)", bc.Name)
		}
		image := images[0]
		if image.As != nil {
			c.warnf("Image source 'As' field is not supported in Shipwright. BuildConfig: %s", bc.Name)
		}
		if image.Paths != nil {
			c.warnf("Image source 'Paths' field is not supported in Shipwright. BuildConfig: %s", bc.Name)
		}

		namespace := image.From.Namespace
		if namespace == "" {
			namespace = bc.Namespace
		}
		imageRef, warning, err := resolveImageRef(string(image.From.Kind), image.From.Name, namespace, c.Opts)
		if err != nil {
			return fmt.Errorf("failed to resolve image source (BuildConfig %s): %w", bc.Name, err)
		}
		if warning != "" {
			c.warnf("%s", warning)
		}

		source := &shipwrightv1beta1.Source{
			Type: shipwrightv1beta1.OCIArtifactType,
			OCIArtifact: &shipwrightv1beta1.OCIArtifact{
				Image: imageRef,
			},
		}
		if image.PullSecret != nil {
			source.OCIArtifact.PullSecret = &image.PullSecret.Name
		}
		b.Spec.Source = source
	}

	if b.Spec.Source != nil && bc.Spec.Source.ContextDir != "" {
		b.Spec.Source.ContextDir = &bc.Spec.Source.ContextDir
	}

	if b.Spec.Source != nil && len(bc.Spec.Source.ConfigMaps) > 0 {
		for _, cm := range bc.Spec.Source.ConfigMaps {
			destDir := cm.DestinationDir
			if destDir == "" {
				destDir = "."
			}
			c.warnf("BuildConfig '%s' mounts ConfigMap '%s' to '%s' during build. Shipwright uses BuildVolume to mount ConfigMaps, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '%s' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the ConfigMap, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for ConfigMap files.", bc.Name, cm.ConfigMap.Name, destDir, cm.ConfigMap.Name)
		}
	}
	if b.Spec.Source != nil && len(bc.Spec.Source.Secrets) > 0 {
		for _, secret := range bc.Spec.Source.Secrets {
			destDir := secret.DestinationDir
			if destDir == "" {
				destDir = "."
			}
			c.warnf("BuildConfig '%s' mounts secret '%s' to '%s' during build. Shipwright uses BuildVolume to mount secrets, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '%s' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the secret, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for secret files.", bc.Name, secret.Secret.Name, destDir, secret.Secret.Name)
		}
	}

	return nil
}

func (c *Converter) processGitProxyConfig(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if bc.Spec.Source.Git == nil {
		return
	}
	proxyConfig := bc.Spec.Source.Git.ProxyConfig
	// OpenShift's build controller injects both the uppercase and lowercase forms
	// (defaults.go@5235418:98-108); tools inside RUN steps that read only lowercase
	// (curl ignores uppercase HTTP_PROXY by design) would otherwise see no proxy.
	// Emit the lowercase twin right after each uppercase entry, same value, no dedupe.
	if proxyConfig.HTTPProxy != nil && *proxyConfig.HTTPProxy != "" {
		b.Spec.Env = append(b.Spec.Env,
			corev1.EnvVar{Name: "HTTP_PROXY", Value: *proxyConfig.HTTPProxy},
			corev1.EnvVar{Name: "http_proxy", Value: *proxyConfig.HTTPProxy})
	}
	if proxyConfig.HTTPSProxy != nil && *proxyConfig.HTTPSProxy != "" {
		b.Spec.Env = append(b.Spec.Env,
			corev1.EnvVar{Name: "HTTPS_PROXY", Value: *proxyConfig.HTTPSProxy},
			corev1.EnvVar{Name: "https_proxy", Value: *proxyConfig.HTTPSProxy})
	}
	if proxyConfig.NoProxy != nil && *proxyConfig.NoProxy != "" {
		b.Spec.Env = append(b.Spec.Env,
			corev1.EnvVar{Name: "NO_PROXY", Value: *proxyConfig.NoProxy},
			corev1.EnvVar{Name: "no_proxy", Value: *proxyConfig.NoProxy})
	}
}

func (c *Converter) processOutput(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if bc.Spec.Output.To == nil {
		return
	}
	isImageStreamTag := bc.Spec.Output.To.Kind == "ImageStreamTag"
	if isImageStreamTag {
		namespace := bc.Spec.Output.To.Namespace
		if namespace == "" {
			namespace = bc.Namespace
		}
		name := bc.Spec.Output.To.Name
		if !strings.Contains(name, ":") {
			name = name + ":latest"
		}

		key := namespace + "/" + name
		if mapped, ok := c.Opts.ImageStreamMapping[key]; ok {
			b.Spec.Output.Image = applyRegistryMapping(mapped, c.Opts.RegistryMapping)
		} else {
			b.Spec.Output.Image = applyRegistryMapping(
				internalRegistryURL+"/"+namespace+"/"+name,
				c.Opts.RegistryMapping,
			)
			c.warnf("Output ImageStreamTag %q resolved to fallback URL: %s", name, b.Spec.Output.Image)
		}
		// A push that no longer lands on the internal registry never updates the
		// source ImageStream, so any Deployment or DeploymentConfig watching it
		// to roll out silently stops firing (BUILD-2316, D-4). The check is the
		// registry prefix, not the exact path: a redirect that keeps the prefix
		// but changes the imagestream path is an accepted blind spot (D-4), since
		// the common redirect — to an external registry — always drops the prefix.
		if !strings.HasPrefix(b.Spec.Output.Image, internalRegistryURL+"/") {
			c.warnf("Output image for ImageStreamTag %q was redirected off the internal registry to %q; the ImageStream will no longer be updated, so any Deployment or DeploymentConfig watching it to roll out will stop firing.", namespace+"/"+name, b.Spec.Output.Image)
		}
	} else {
		b.Spec.Output.Image = bc.Spec.Output.To.Name
	}

	// The push credential is carried across only when the BuildConfig names one;
	// the plugin cannot read the builder ServiceAccount to derive it (BUILD-2316,
	// D-5). When none is carried, warn on both output kinds rather than only on
	// ImageStreamTag (D-3), with the remedy that fits each: an internal-registry
	// push needs a ServiceAccount with registry access, an external-registry push
	// needs a real registry credential secret.
	if bc.Spec.Output.PushSecret != nil && bc.Spec.Output.PushSecret.Name != "" {
		b.Spec.Output.PushSecret = &bc.Spec.Output.PushSecret.Name
	} else if isImageStreamTag {
		c.warnf("%s", "No explicit pushSecret found for ImageStreamTag output. Ensure the BuildRun uses a ServiceAccount with internal registry push access.")
	} else {
		c.warnf("%s", "No explicit pushSecret found for DockerImage output. Set spec.output.pushSecret to a registry credential secret, or ensure the BuildRun ServiceAccount carries credentials for the target registry; otherwise the push will fail.")
	}

	c.processOutputImageLabels(bc, b)
}

// processOutputImageLabels maps BuildConfig spec.output.imageLabels to the
// Shipwright Build spec.output.labels map, which build strategies apply to
// the pushed image.
func (c *Converter) processOutputImageLabels(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if len(bc.Spec.Output.ImageLabels) == 0 {
		return
	}
	labels := make(map[string]string, len(bc.Spec.Output.ImageLabels))
	for _, il := range bc.Spec.Output.ImageLabels {
		if il.Name == "" {
			c.warnf("%s", "Skipping output imageLabel with empty name")
			continue
		}
		if existing, ok := labels[il.Name]; ok && existing != il.Value {
			c.warnf("Duplicate output imageLabel %q: overriding value %q with %q", il.Name, existing, il.Value)
		}
		labels[il.Name] = il.Value
	}
	if len(labels) > 0 {
		b.Spec.Output.Labels = labels
	}
}

// maxTimeoutSeconds is the largest completionDeadlineSeconds value that can be
// represented as a time.Duration (int64 nanoseconds) without overflowing.
const maxTimeoutSeconds = math.MaxInt64 / int64(time.Second)

// processCompletionDeadline maps BuildConfig completionDeadlineSeconds to the
// Shipwright Build timeout so that migrated builds keep the same execution deadline
func (c *Converter) processCompletionDeadline(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if bc.Spec.CompletionDeadlineSeconds == nil {
		return
	}

	seconds := *bc.Spec.CompletionDeadlineSeconds
	if seconds <= 0 {
		c.warnf("completionDeadlineSeconds %d on BuildConfig %s is not positive; leaving Build timeout unset",
			seconds, bc.Name)
		return
	}
	if seconds > maxTimeoutSeconds {
		c.warnf("completionDeadlineSeconds %d on BuildConfig %s exceeds the maximum representable timeout of %d seconds; leaving Build timeout unset",
			seconds, bc.Name, maxTimeoutSeconds)
		return
	}

	timeout := metav1.Duration{
		Duration: time.Duration(seconds) * time.Second,
	}
	b.Spec.Timeout = &timeout
	c.Log.Infof("Mapping completionDeadlineSeconds %ds to Build timeout %s for BuildConfig %s",
		seconds, timeout.Duration, bc.Name)
}

// processNodeSelector maps BuildConfig spec.nodeSelector to Shipwright Build
// spec.nodeSelector (BUILD-2264). Shipwright merges the Build's node selector
// with any BuildRun override and applies the result to the build pod template,
// so the placement the BuildConfig asked for survives migration without the
// user having to touch anything.
//
// nil and an empty map are handled alike. OpenShift distinguishes them — nil
// lets the cluster-wide buildDefaults.nodeSelector apply, an explicit empty map
// opts out of it — but Shipwright has no cluster-wide default, so both leave
// the field unset.
//
// An invalid key or value drops the whole selector rather than the offending
// entry: Shipwright's Build reconciler would mark the Build
// NodeSelectorNotValid and never register it, and a partially applied selector
// would silently schedule the build somewhere the BuildConfig never asked for.
func (c *Converter) processNodeSelector(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if len(bc.Spec.NodeSelector) == 0 {
		return
	}

	if err := validateNodeSelector(bc.Spec.NodeSelector); err != nil {
		c.warnf("nodeSelector on BuildConfig %s/%s is invalid: %v; dropping the whole nodeSelector — migrated builds will not be pinned to any node",
			bc.Namespace, bc.Name, err)
		return
	}

	b.Spec.NodeSelector = maps.Clone(bc.Spec.NodeSelector)
	c.Log.Infof("Mapping nodeSelector %v to Build spec.nodeSelector for BuildConfig %s/%s",
		bc.Spec.NodeSelector, bc.Namespace, bc.Name)
}

// validateNodeSelector reports the first entry Shipwright would reject, using
// the same apimachinery helpers as its own pkg/validate/nodeselector.go so the
// two cannot drift apart. Entries are checked in sorted key order: Go
// randomizes map iteration, and a warning that fingers a different key on each
// run is useless when triaging a bulk migration.
func validateNodeSelector(selector map[string]string) error {
	for _, key := range slices.Sorted(maps.Keys(selector)) {
		if errs := validation.IsQualifiedName(key); len(errs) > 0 {
			return fmt.Errorf("key %q is not a valid label key (%s)", key, strings.Join(errs, "; "))
		}
		if errs := validation.IsValidLabelValue(selector[key]); len(errs) > 0 {
			return fmt.Errorf("value %q for key %q is not a valid label value (%s)", selector[key], key, strings.Join(errs, "; "))
		}
	}
	return nil
}

// processRunPolicy reports the build scheduling behaviour that is lost during
// conversion. Shipwright has no equivalent of runPolicy: BuildRuns are independent
// objects that run concurrently, with no queue, no ordering and no cancellation of
// superseded runs. Parallel is the one policy Shipwright already matches.
func (c *Converter) processRunPolicy(bc *buildv1.BuildConfig) {
	// An absent runPolicy still meant Serial scheduling in OpenShift, so the
	// behaviour lost on conversion is the same as an explicit Serial.
	policy := bc.Spec.RunPolicy
	if policy == "" {
		policy = buildv1.BuildRunPolicySerial
	}

	switch policy {
	case buildv1.BuildRunPolicyParallel:
		c.Log.Infof("BuildConfig %s uses runPolicy %q; Shipwright BuildRuns already run independently and concurrently, so build scheduling is unchanged",
			bc.Name, policy)
	case buildv1.BuildRunPolicySerial:
		c.warnf("BuildConfig %s uses runPolicy %q, which is dropped: OpenShift queued its builds and ran them one at a time, but Shipwright BuildRuns run concurrently. Serialize the runs in your CI/CD pipeline if build ordering matters, for example when several BuildRuns push the same image tag",
			bc.Name, policy)
	case buildv1.BuildRunPolicySerialLatestOnly:
		c.warnf("BuildConfig %s uses runPolicy %q, which is dropped: OpenShift queued its builds and cancelled superseded ones so that only the latest ran, but Shipwright BuildRuns run concurrently and are never auto-cancelled. Serialize the runs and cancel superseded ones in your CI/CD pipeline if you depend on this",
			bc.Name, policy)
	default:
		c.warnf("BuildConfig %s uses unrecognized runPolicy %q, which is dropped: Shipwright has no build scheduling policy and BuildRuns run concurrently",
			bc.Name, policy)
	}
}

// minRetentionLimit and maxRetentionLimit mirror the Shipwright CRD validation
// on BuildRetention.SucceededLimit and BuildRetention.FailedLimit
// (+kubebuilder:validation:Minimum=1, +kubebuilder:validation:Maximum=10000 in
// shipwright-io/build v0.19.0).
const (
	minRetentionLimit = 1
	maxRetentionLimit = 10000
)

// resolveRetentionLimit validates one BuildConfig build-history limit against the
// Shipwright CRD bounds, returning the value to store or nil when the field is
// unset or unusable. Out-of-range values, including 0, which OpenShift allows
// ("retain none") but the Shipwright CRD rejects, are warned and dropped so the
// retention block stays unset and migrated BuildRuns are never auto-pruned
// unexpectedly.
func (c *Converter) resolveRetentionLimit(bc *buildv1.BuildConfig, limit *int32, bcField, swField string) *uint {
	if limit == nil {
		return nil
	}

	v := *limit
	if v < minRetentionLimit || v > maxRetentionLimit {
		// warnf, not c.Log.Warnf: a dropped retention limit is a field-drop
		// degradation, so it must feed the conversion outcome model (BUILD-2318)
		// and classify the Build as converted-with-warnings.
		c.warnf("%s %d on BuildConfig %s/%s is outside the Shipwright %s range [%d,%d]; leaving retention unset — migrated BuildRuns will not be auto-pruned",
			bcField, v, bc.Namespace, bc.Name, swField, minRetentionLimit, maxRetentionLimit)
		return nil
	}

	c.Log.Infof("Mapping %s %d to Build %s for BuildConfig %s/%s (OpenShift pruned old Build objects; Shipwright will prune BuildRuns)",
		bcField, v, swField, bc.Namespace, bc.Name)
	resolved := uint(v)
	return &resolved
}

// processBuildsHistoryLimits maps the BuildConfig build-history limits onto the
// Shipwright Build retention block (BUILD-2259, BUILD-2260). The block is
// allocated once, and only when at least one limit survives validation.
func (c *Converter) processBuildsHistoryLimits(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	succeeded := c.resolveRetentionLimit(bc, bc.Spec.SuccessfulBuildsHistoryLimit, "successfulBuildsHistoryLimit", "retention.succeededLimit")
	failed := c.resolveRetentionLimit(bc, bc.Spec.FailedBuildsHistoryLimit, "failedBuildsHistoryLimit", "retention.failedLimit")
	if succeeded == nil && failed == nil {
		return
	}

	if b.Spec.Retention == nil {
		b.Spec.Retention = &shipwrightv1beta1.BuildRetention{}
	}
	if succeeded != nil {
		b.Spec.Retention.SucceededLimit = succeeded
	}
	if failed != nil {
		b.Spec.Retention.FailedLimit = failed
	}
}

func (c *Converter) addRegistries(b *shipwrightv1beta1.Build) {
	if len(c.Opts.SearchRegistries) > 0 {
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:   "registries-search",
			Values: toSingleValues(c.Opts.SearchRegistries),
		})
	}
	if len(c.Opts.InsecureRegistries) > 0 {
		// The two strategies push two different ways, so "this registry is
		// insecure" is expressed two different ways. A strategy-managed push
		// (buildah) reads the registries-insecure param off the Build. A
		// Shipwright-managed push (source-to-image) has no such param and would
		// be rejected with UndefinedParameter; it honors spec.output.insecure
		// instead. Route the single --insecure-registries intent to whichever
		// the converted strategy uses, keyed on the default strategy names the
		// plugin emits. A custom strategy override keeps the buildah-style param,
		// since we cannot know how it pushes.
		if b.Spec.Strategy.Name == defaultS2IStrategy {
			if slices.Contains(c.Opts.InsecureRegistries, imageRegistryHost(b.Spec.Output.Image)) {
				insecure := true
				b.Spec.Output.Insecure = &insecure
			}
		} else {
			b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
				Name:   "registries-insecure",
				Values: toSingleValues(c.Opts.InsecureRegistries),
			})
		}
	}
	if len(c.Opts.BlockRegistries) > 0 {
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:   "registries-block",
			Values: toSingleValues(c.Opts.BlockRegistries),
		})
	}
}

// processResources carries BuildConfig spec.resources forward as a BuildRun
// template annotation on the converted Build. The Shipwright Build CRD has no
// resources field — per-step overrides live on BuildRun.spec.stepResources —
// and emitting a real BuildRun into the migration stream would immediately
// trigger a build on the target cluster. The annotation is inert: the user
// reviews the template, copies it out, and applies it when they want to run
// a build. See BUILD-2261.
func (c *Converter) processResources(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build, generatedSA string) error {
	res := bc.Spec.Resources
	if len(res.Requests) == 0 && len(res.Limits) == 0 {
		return nil
	}

	// Step names must match the referenced ClusterBuildStrategy. Verified on
	// cluster (OpenShift Builds operator 1.8.1): buildah has a single
	// "build-and-push" step; source-to-image runs "s2i-generate" + "buildah".
	// When the strategy has been overridden to a custom ClusterBuildStrategy
	// via strategy mapping, its step names are unknown — emitting the default
	// names would make Shipwright reject the BuildRun at admission, so
	// stepResources are omitted and the user is told to fill them in.
	var stepNames []string
	switch bc.Spec.Strategy.Type {
	case buildv1.DockerBuildStrategyType:
		if b.Spec.Strategy.Name == defaultDockerStrategy {
			stepNames = []string{"build-and-push"}
		}
	case buildv1.SourceBuildStrategyType:
		if b.Spec.Strategy.Name == defaultS2IStrategy {
			stepNames = []string{"s2i-generate", "buildah"}
		}
	default:
		return nil
	}

	// The spec is built from Shipwright typed structs so field names cannot
	// drift from the API, then embedded in a minimal hand-built envelope so
	// the template YAML stays free of marshaling artifacts such as
	// "creationTimestamp: null" and "status: {}".
	spec := shipwrightv1beta1.BuildRunSpec{
		Build: shipwrightv1beta1.ReferencedBuild{Name: &b.Name},
	}
	if generatedSA != "" {
		sa := generatedSA
		spec.ServiceAccount = &sa
	} else if bc.Spec.ServiceAccount != "" {
		// Preserve an explicitly configured ServiceAccount even when no
		// pull secret forced us to generate one.
		sa := bc.Spec.ServiceAccount
		spec.ServiceAccount = &sa
	}
	for _, step := range stepNames {
		spec.StepResources = append(spec.StepResources, shipwrightv1beta1.StepResourceOverride{
			Name:      step,
			Resources: res,
		})
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("error marshaling BuildRun spec for BuildConfig %s: %w", bc.Name, err)
	}
	specMap := map[string]interface{}{}
	if err := json.Unmarshal(specJSON, &specMap); err != nil {
		return fmt.Errorf("error unmarshaling BuildRun spec for BuildConfig %s: %w", bc.Name, err)
	}

	template := map[string]interface{}{
		"apiVersion": "shipwright.io/v1beta1",
		"kind":       "BuildRun",
		"metadata": map[string]interface{}{
			"name":      b.Name + "-buildrun",
			"namespace": b.Namespace,
		},
		"spec": specMap,
	}

	templateYAML, err := yaml.Marshal(template)
	if err != nil {
		return fmt.Errorf("error marshaling BuildRun template for BuildConfig %s: %w", bc.Name, err)
	}

	b.Annotations[BuildRunTemplateAnnotation] = string(templateYAML)

	// Ahead of the stepNames early return below, so the mapping is reported for
	// every template written, not only those that also carry stepResources.
	if spec.ServiceAccount != nil {
		c.Log.Infof("Mapped serviceAccount %q to BuildRun template in annotation %s for BuildConfig %s/%s",
			*spec.ServiceAccount, BuildRunTemplateAnnotation, bc.Namespace, bc.Name)
	}

	if len(stepNames) == 0 {
		c.warnf("Build strategy %q is a custom mapping with unknown step names — stepResources were omitted from the BuildRun template in annotation %s. Add stepResources entries matching the strategy's step names to carry over the BuildConfig resource requirements (requests: %v, limits: %v).", b.Spec.Strategy.Name, BuildRunTemplateAnnotation, res.Requests, res.Limits)
		return nil
	}

	c.Log.Infof("Generated BuildRun template with resource requirements (requests: %v, limits: %v) in annotation %s", res.Requests, res.Limits, BuildRunTemplateAnnotation)
	c.warnf("Resource requirements are not supported on Shipwright Build. Apply the BuildRun template from annotation %s (after review) or set stepResources on each BuildRun you create.", BuildRunTemplateAnnotation)

	return nil
}

func toSingleValues(registries []string) []shipwrightv1beta1.SingleValue {
	values := make([]shipwrightv1beta1.SingleValue, len(registries))
	for i, r := range registries {
		r := r
		values[i] = shipwrightv1beta1.SingleValue{Value: &r}
	}
	return values
}

func toUnstructured(obj interface{}) (unstructured.Unstructured, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	u := unstructured.Unstructured{}
	err = json.Unmarshal(data, &u.Object)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	stripSerializationNoise(u.Object)
	return u, nil
}

// stripSerializationNoise removes fields that the typed->unstructured
// round-trip emits even though they carry no information, so that emitted
// resources are clean and byte-stable across runs (BUILD-2339):
//   - empty or null status objects (zero-value Status structs marshal as {})
//
// Note: metadata.creationTimestamp needs no handling here. metav1.Time is
// tagged `omitempty,omitzero` in apimachinery v0.34, so encoding/json on
// Go >= 1.24 (this module pins go 1.25.6) omits the zero value entirely and
// the `creationTimestamp: null` case can never reach this function.
func stripSerializationNoise(obj map[string]interface{}) {
	if status, present := obj["status"]; present {
		if status == nil {
			delete(obj, "status")
		} else if m, ok := status.(map[string]interface{}); ok && len(m) == 0 {
			delete(obj, "status")
		}
	}
}
