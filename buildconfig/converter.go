package buildconfig

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const (
	defaultDockerStrategy = "buildah"
	defaultS2IStrategy    = "source-to-image"

	NoCacheParamName          = "no-cache"
	SquashParamName           = "squash"
	ForcePullParamName        = "pull"
	RuntimeStageFromParamName = "runtime-stage-from"

	Timeout = 10 * time.Minute

	ConvertedFromAnnotation    = "crane.konveyor.io/converted-from"
	BuildRunTemplateAnnotation = "buildconfig-to-shipwright/buildrun-template"
	OriginalTriggersAnnotation = "buildconfig-to-shipwright/original-triggers"

	ConfigMapsRFE            = "https://issues.redhat.com/browse/BUILD-1745"
	SecretsRFE               = "https://issues.redhat.com/browse/BUILD-1744"
	DockerStrategyVolumesRFE = "https://issues.redhat.com/browse/BUILD-1747"
	CustomScriptsRFE         = "https://issues.redhat.com/browse/BUILD-1641"
	IncrementalBuildRFE      = "https://issues.redhat.com/browse/BUILD-1607"
	ForcePullFlagS2iRFE      = "https://issues.redhat.com/browse/BUILD-1606"
)

type Converter struct {
	Log  logrus.FieldLogger
	Opts PluginOptionalFields

	// assignedNames tracks generated names (keyed by kind/namespace/name) so
	// that distinct originals resolving to the same sanitized name within a
	// single converter lifetime are detected and disambiguated.
	assignedNames map[string]string
	// serviceAccounts caches generated ServiceAccounts by namespace/name so
	// that BuildConfigs sharing a builder ServiceAccount merge their
	// imagePullSecrets instead of overwriting each other. This only spans one
	// Converter: crane invokes the plugin once per resource in a separate
	// process, so BuildConfigs converted by different invocations cannot be
	// merged here — generateServiceAccount warns when that case is possible.
	serviceAccounts map[string]*corev1.ServiceAccount
}

// uniqueName sanitizes a generated resource name into a valid DNS-1123 label
// and guards against two distinct original names resolving to the same final
// name for the same kind and namespace.
func (c *Converter) uniqueName(kind, namespace, original string) string {
	name, changed := sanitizeDNS1123Label(original)
	if changed {
		c.Log.Warnf("Generated %s name %q is not a valid DNS-1123 label of at most %d characters — using %q instead", kind, original, maxGeneratedNameLength, name)
	}

	if c.assignedNames == nil {
		c.assignedNames = map[string]string{}
	}
	key := kind + "/" + namespace + "/" + name
	if owner, ok := c.assignedNames[key]; ok && owner != original {
		name = withHashSuffix(name, original)
		c.Log.Warnf("Generated %s name for %q collides with the name already generated for %q — using %q instead", kind, original, owner, name)
		key = kind + "/" + namespace + "/" + name
		if owner, ok := c.assignedNames[key]; ok && owner != original {
			c.Log.Errorf("Hash-suffixed %s name %q for %q still collides with the name already generated for %q — resources may overwrite each other", kind, name, original, owner)
		}
	}
	c.assignedNames[key] = original
	return name
}

func (c *Converter) Convert(bc *buildv1.BuildConfig) ([]unstructured.Unstructured, error) {
	b := &shipwrightv1beta1.Build{}
	b.Name = c.uniqueName("Build", bc.Namespace, bc.Name)
	b.Kind = "Build"
	b.APIVersion = "shipwright.io/v1beta1"
	b.Namespace = bc.Namespace
	b.Spec.ParamValues = []shipwrightv1beta1.ParamValue{}
	b.Annotations = map[string]string{
		ConvertedFromAnnotation: fmt.Sprintf("build.openshift.io/v1/BuildConfig/%s", bc.Name),
	}
	b.Labels = c.copyLabels(bc)

	var newResources []unstructured.Unstructured

	switch bc.Spec.Strategy.Type {
	case buildv1.DockerBuildStrategyType:
		c.Log.Infof("Docker strategy detected for BuildConfig %s", bc.Name)
		if err := c.processDockerStrategy(bc, b); err != nil {
			return nil, err
		}
	case buildv1.SourceBuildStrategyType:
		c.Log.Infof("Source strategy detected for BuildConfig %s", bc.Name)
		if err := c.processSourceStrategy(bc, b); err != nil {
			return nil, err
		}
	case buildv1.CustomBuildStrategyType:
		c.Log.Warnf("Custom build strategy is not supported for conversion — passing BuildConfig %s through unchanged", bc.Name)
		return nil, nil
	case buildv1.JenkinsPipelineBuildStrategyType:
		c.Log.Warnf("JenkinsPipeline build strategy is not supported for conversion — passing BuildConfig %s through unchanged. Consider migrating to Tekton Pipelines directly.", bc.Name)
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown build strategy type %q for BuildConfig %s", bc.Spec.Strategy.Type, bc.Name)
	}

	// Shipwright Builds require spec.output.image; a BuildConfig without an
	// output image cannot be converted into a valid Build.
	if bc.Spec.Output.To == nil || bc.Spec.Output.To.Name == "" {
		c.Log.Warnf("BuildConfig %s has no output image (spec.output.to is missing or empty) — a Shipwright Build requires spec.output.image, passing BuildConfig through unchanged", bc.Name)
		return nil, nil
	}

	// PullSecret → ServiceAccount
	generatedSA := ""
	pullSecret := c.getPullSecret(bc)
	if pullSecret != nil {
		sa := c.generateServiceAccount(bc, pullSecret)
		generatedSA = sa.Name
		saUnstructured, err := toUnstructured(sa)
		if err != nil {
			return nil, fmt.Errorf("error converting ServiceAccount to unstructured: %w", err)
		}
		newResources = append(newResources, saUnstructured)
	}

	c.processSource(bc, b)
	c.processOutput(bc, b)
	c.processCompletionDeadline(bc, b)
	c.addRegistries(b)
	c.processTriggers(bc, b)

	if err := c.processResources(bc, b, generatedSA); err != nil {
		return nil, err
	}

	buildUnstructured, err := toUnstructured(b)
	if err != nil {
		return nil, fmt.Errorf("error converting Build to unstructured: %w", err)
	}

	result := []unstructured.Unstructured{buildUnstructured}
	result = append(result, newResources...)
	return result, nil
}

// copyLabels copies user-defined metadata.labels from the BuildConfig to the
// generated Shipwright Build. OpenShift-internal labels (openshift.io/build*
// and the deprecated "buildconfig" label) reference BuildConfig concepts that
// do not exist after migration, so they are filtered out. Returns nil when no
// labels survive filtering.
func (c *Converter) copyLabels(bc *buildv1.BuildConfig) map[string]string {
	if len(bc.Labels) == 0 {
		return nil
	}
	labels := map[string]string{}
	for k, v := range bc.Labels {
		if strings.HasPrefix(k, "openshift.io/build") || k == buildv1.BuildConfigLabelDeprecated {
			c.Log.Infof("Dropping OpenShift-internal label %q from BuildConfig %s — it references pre-migration build machinery", k, bc.Name)
			continue
		}
		labels[k] = v
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
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
			c.Log.Warn(warning)
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

	// BuildArgs
	if len(ds.BuildArgs) > 0 {
		values := []shipwrightv1beta1.SingleValue{}
		for _, arg := range ds.BuildArgs {
			envNameValue := arg.Name + "=" + arg.Value
			values = append(values, shipwrightv1beta1.SingleValue{Value: &envNameValue})
		}
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:   "build-args",
			Values: values,
		})
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

	// Volumes — convert to Build spec volumes; full support still requires the
	// ClusterBuildStrategy to define a matching overridable volume.
	if len(ds.Volumes) > 0 {
		if converted := c.processStrategyVolumes(bc, ds.Volumes, b); converted > 0 {
			c.Log.Warnf("Volumes were converted to Build spec volumes, but they only take effect if the Buildah ClusterBuildStrategy defines a matching overridable volume. RFE: %s", DockerStrategyVolumesRFE)
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
			c.Log.Warn(warning)
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
		c.Log.Warnf("Custom scripts are not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: %s", CustomScriptsRFE)
	}
	if ss.Incremental != nil && *ss.Incremental {
		c.Log.Warnf("Incremental build is not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: %s", IncrementalBuildRFE)
	}
	if ss.ForcePull {
		c.Log.Warnf("ForcePull flag is not yet supported in the Source-to-Image ClusterBuildStrategy in Shipwright. RFE: %s", ForcePullFlagS2iRFE)
	}
	// Volumes — convert to Build spec volumes; full support still requires the
	// ClusterBuildStrategy to define a matching overridable volume.
	if len(ss.Volumes) > 0 {
		if converted := c.processStrategyVolumes(bc, ss.Volumes, b); converted > 0 {
			c.Log.Warnf("Volumes were converted to Build spec volumes, but they only take effect if the Source-to-Image ClusterBuildStrategy defines a matching overridable volume. RFE: %s", DockerStrategyVolumesRFE)
		}
	}

	return nil
}

// processStrategyVolumes converts BuildConfig strategy volumes into Shipwright
// Build spec volumes and returns the number of volumes appended. Secret and
// ConfigMap sources are supported; volumes with an empty name, a duplicate
// name, or an unsupported source type are skipped with a warning so the rest
// of the conversion can proceed. Volume mount paths are not migrated: mount
// paths are defined in the BuildStrategy, not in the Build resource.
func (c *Converter) processStrategyVolumes(bc *buildv1.BuildConfig, volumes []buildv1.BuildVolume, b *shipwrightv1beta1.Build) int {
	converted := 0
	seen := make(map[string]bool, len(volumes))
	for _, bcVolume := range volumes {
		c.Log.Infof("Processing volume %q for BuildConfig %s", bcVolume.Name, bc.Name)

		if bcVolume.Name == "" {
			c.Log.Warnf("Skipping volume with empty name for BuildConfig %s: the Shipwright Build API requires volumes to be named", bc.Name)
			continue
		}
		if seen[bcVolume.Name] {
			c.Log.Warnf("Skipping duplicate volume %q for BuildConfig %s: a volume with this name was already converted", bcVolume.Name, bc.Name)
			continue
		}

		volumeSource, err := convertBuildVolumeSource(bcVolume.Source)
		if err != nil {
			c.Log.Warnf("Skipping volume %q for BuildConfig %s: %v", bcVolume.Name, bc.Name, err)
			continue
		}

		seen[bcVolume.Name] = true
		b.Spec.Volumes = append(b.Spec.Volumes, shipwrightv1beta1.BuildVolume{
			Name:         bcVolume.Name,
			VolumeSource: volumeSource,
		})
		converted++

		if len(bcVolume.Mounts) > 0 {
			paths := make([]string, 0, len(bcVolume.Mounts))
			for _, m := range bcVolume.Mounts {
				paths = append(paths, m.DestinationPath)
			}
			c.Log.Warnf("Volume mount paths for volume %q can not be migrated to the Shipwright Build; mount paths are defined in the BuildStrategy. Original destination paths: %s", bcVolume.Name, strings.Join(paths, ", "))
		}
	}
	return converted
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

func (c *Converter) generateServiceAccount(bc *buildv1.BuildConfig, pullSecret *corev1.LocalObjectReference) *corev1.ServiceAccount {
	if pullSecret == nil {
		return nil
	}
	saName := bc.Spec.ServiceAccount
	if saName == "" {
		saName = bc.Name
	}
	saName = c.uniqueName("ServiceAccount", bc.Namespace, saName)

	// A BuildConfig that names an existing ServiceAccount may share it with
	// sibling BuildConfigs. crane runs this plugin once per resource in its own
	// process, so ServiceAccounts generated for other BuildConfigs are not
	// visible here: each conversion emits the ServiceAccount carrying only its
	// own pull secret, and applying them in sequence keeps only the last one.
	if bc.Spec.ServiceAccount != "" {
		c.Log.Warnf("BuildConfig %s converts pull secret %q into ServiceAccount %q, which it may share with other BuildConfigs — pull secrets from BuildConfigs sharing this ServiceAccount are not merged, so review the generated imagePullSecrets before applying", bc.Name, pullSecret.Name, saName)
	}

	if c.serviceAccounts == nil {
		c.serviceAccounts = map[string]*corev1.ServiceAccount{}
	}
	key := bc.Namespace + "/" + saName
	if existing, ok := c.serviceAccounts[key]; ok {
		mergePullSecret(existing, pullSecret.Name)
		return existing
	}

	sa := &corev1.ServiceAccount{
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
	c.serviceAccounts[key] = sa
	return sa
}

// mergePullSecret adds secretName to the ServiceAccount's imagePullSecrets and
// secrets lists if not already present, preserving previously merged entries.
func mergePullSecret(sa *corev1.ServiceAccount, secretName string) {
	hasPullSecret := false
	for _, s := range sa.ImagePullSecrets {
		if s.Name == secretName {
			hasPullSecret = true
			break
		}
	}
	if !hasPullSecret {
		sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{Name: secretName})
	}

	hasSecret := false
	for _, s := range sa.Secrets {
		if s.Name == secretName {
			hasSecret = true
			break
		}
	}
	if !hasSecret {
		sa.Secrets = append(sa.Secrets, corev1.ObjectReference{Name: secretName})
	}
}

func (c *Converter) processSource(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	git := bc.Spec.Source.Git
	binary := bc.Spec.Source.Binary
	images := bc.Spec.Source.Images
	dockerfile := bc.Spec.Source.Dockerfile

	if dockerfile != nil && bc.Spec.Strategy.Type == buildv1.DockerBuildStrategyType {
		c.Log.Error("Inline Dockerfile is not supported in buildah strategy. Consider moving it to a separate file.")
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
		c.Log.Errorf("Multiple source types are not supported in a single build in Shipwright. BuildConfig: %s", bc.Name)
		return
	}

	if sourceCount == 0 {
		c.Log.Warnf("No source type specified for BuildConfig: %s", bc.Name)
		return
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
			c.Log.Errorf("Binary archive source (extracted archive) is not supported in Shipwright, only single-file binary sources (asFile). BuildConfig: %s", bc.Name)
			return
		}
		c.Log.Infof("Processing binary source as single file (asFile: %s). BuildConfig: %s", bc.Spec.Source.Binary.AsFile, bc.Name)
		b.Spec.Source = source
	} else if len(images) > 0 {
		if len(images) > 1 {
			c.Log.Errorf("Multiple image sources are not supported in Shipwright. BuildConfig: %s", bc.Name)
			return
		}
		image := images[0]
		if image.As != nil {
			c.Log.Errorf("Image source 'As' field is not supported in Shipwright. BuildConfig: %s", bc.Name)
		}
		if image.Paths != nil {
			c.Log.Errorf("Image source 'Paths' field is not supported in Shipwright. BuildConfig: %s", bc.Name)
		}

		namespace := image.From.Namespace
		if namespace == "" {
			namespace = bc.Namespace
		}
		imageRef, warning, err := resolveImageRef(string(image.From.Kind), image.From.Name, namespace, c.Opts)
		if err != nil {
			c.Log.Errorf("Failed to resolve image source: %v", err)
			return
		}
		if warning != "" {
			c.Log.Warn(warning)
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
			c.Log.Warnf("BuildConfig '%s' mounts ConfigMap '%s' to '%s' during build. Shipwright uses BuildVolume to mount ConfigMaps, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '%s' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the ConfigMap, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for ConfigMap files.", bc.Name, cm.ConfigMap.Name, destDir, cm.ConfigMap.Name)
		}
	}
	if b.Spec.Source != nil && len(bc.Spec.Source.Secrets) > 0 {
		for _, secret := range bc.Spec.Source.Secrets {
			destDir := secret.DestinationDir
			if destDir == "" {
				destDir = "."
			}
			c.Log.Warnf("BuildConfig '%s' mounts secret '%s' to '%s' during build. Shipwright uses BuildVolume to mount secrets, which requires the ClusterBuildStrategy to define an overridable volume. To migrate: (1) add an overridable volume named '%s' in the ClusterBuildStrategy, (2) add a BuildVolume override in the Build spec referencing the secret, (3) update your Dockerfile to use 'RUN cp' instead of 'ADD/COPY' for secret files.", bc.Name, secret.Secret.Name, destDir, secret.Secret.Name)
		}
	}
}

func (c *Converter) processGitProxyConfig(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if bc.Spec.Source.Git == nil {
		return
	}
	proxyConfig := bc.Spec.Source.Git.ProxyConfig
	if proxyConfig.HTTPProxy != nil && *proxyConfig.HTTPProxy != "" {
		b.Spec.Env = append(b.Spec.Env, corev1.EnvVar{Name: "HTTP_PROXY", Value: *proxyConfig.HTTPProxy})
	}
	if proxyConfig.HTTPSProxy != nil && *proxyConfig.HTTPSProxy != "" {
		b.Spec.Env = append(b.Spec.Env, corev1.EnvVar{Name: "HTTPS_PROXY", Value: *proxyConfig.HTTPSProxy})
	}
	if proxyConfig.NoProxy != nil && *proxyConfig.NoProxy != "" {
		b.Spec.Env = append(b.Spec.Env, corev1.EnvVar{Name: "NO_PROXY", Value: *proxyConfig.NoProxy})
	}
}

func (c *Converter) processOutput(bc *buildv1.BuildConfig, b *shipwrightv1beta1.Build) {
	if bc.Spec.Output.To == nil {
		return
	}
	if bc.Spec.Output.To.Kind == "ImageStreamTag" {
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
			c.Log.Warnf("Output ImageStreamTag %q resolved to fallback URL: %s", name, b.Spec.Output.Image)
		}
		if bc.Spec.Output.PushSecret == nil || bc.Spec.Output.PushSecret.Name == "" {
			c.Log.Warn("No explicit pushSecret found for ImageStreamTag output. Ensure the BuildRun uses a ServiceAccount with internal registry push access.")
		}
	} else {
		b.Spec.Output.Image = bc.Spec.Output.To.Name
	}

	if bc.Spec.Output.PushSecret != nil && bc.Spec.Output.PushSecret.Name != "" {
		b.Spec.Output.PushSecret = &bc.Spec.Output.PushSecret.Name
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
			c.Log.Warn("Skipping output imageLabel with empty name")
			continue
		}
		if existing, ok := labels[il.Name]; ok && existing != il.Value {
			c.Log.Warnf("Duplicate output imageLabel %q: overriding value %q with %q", il.Name, existing, il.Value)
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
		c.Log.Warnf("completionDeadlineSeconds %d on BuildConfig %s is not positive; leaving Build timeout unset",
			seconds, bc.Name)
		return
	}
	if seconds > maxTimeoutSeconds {
		c.Log.Warnf("completionDeadlineSeconds %d on BuildConfig %s exceeds the maximum representable timeout of %d seconds; leaving Build timeout unset",
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

func (c *Converter) addRegistries(b *shipwrightv1beta1.Build) {
	if len(c.Opts.SearchRegistries) > 0 {
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:   "registries-search",
			Values: toSingleValues(c.Opts.SearchRegistries),
		})
	}
	if len(c.Opts.InsecureRegistries) > 0 {
		b.Spec.ParamValues = append(b.Spec.ParamValues, shipwrightv1beta1.ParamValue{
			Name:   "registries-insecure",
			Values: toSingleValues(c.Opts.InsecureRegistries),
		})
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

	if len(stepNames) == 0 {
		c.Log.Warnf("Build strategy %q is a custom mapping with unknown step names — stepResources were omitted from the BuildRun template in annotation %s. Add stepResources entries matching the strategy's step names to carry over the BuildConfig resource requirements (requests: %v, limits: %v).", b.Spec.Strategy.Name, BuildRunTemplateAnnotation, res.Requests, res.Limits)
		return nil
	}

	c.Log.Infof("Generated BuildRun template with resource requirements (requests: %v, limits: %v) in annotation %s", res.Requests, res.Limits, BuildRunTemplateAnnotation)
	c.Log.Warnf("Resource requirements are not supported on Shipwright Build. Apply the BuildRun template from annotation %s (after review) or set stepResources on each BuildRun you create.", BuildRunTemplateAnnotation)

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
// tagged `omitempty,omitzero` in apimachinery v0.36, so encoding/json on
// Go >= 1.24 (this module pins go 1.26.0) omits the zero value entirely and
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
