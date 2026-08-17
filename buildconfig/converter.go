package buildconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	defaultDockerStrategy = "buildah"
	defaultS2IStrategy    = "source-to-image"

	NoCacheParamName          = "no-cache"
	SquashParamName           = "squash"
	ForcePullParamName        = "pull"
	RuntimeStageFromParamName = "runtime-stage-from"

	Timeout = 10 * time.Minute

	ConvertedFromAnnotation = "crane.konveyor.io/converted-from"

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
}

func (c *Converter) Convert(bc *buildv1.BuildConfig) ([]unstructured.Unstructured, error) {
	b := &shipwrightv1beta1.Build{}
	b.Name = bc.Name
	b.Kind = "Build"
	b.APIVersion = "shipwright.io/v1beta1"
	b.Namespace = bc.Namespace
	b.Spec.ParamValues = []shipwrightv1beta1.ParamValue{}
	b.Annotations = map[string]string{
		ConvertedFromAnnotation: fmt.Sprintf("build.openshift.io/v1/BuildConfig/%s", bc.Name),
	}

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

	// PullSecret → ServiceAccount
	pullSecret := c.getPullSecret(bc)
	if pullSecret != nil {
		sa := c.generateServiceAccount(bc, pullSecret)
		saUnstructured, err := toUnstructured(sa)
		if err != nil {
			return nil, fmt.Errorf("error converting ServiceAccount to unstructured: %w", err)
		}
		newResources = append(newResources, saUnstructured)
	}

	c.processSource(bc, b)
	c.processOutput(bc, b)
	c.addRegistries(b)

	buildUnstructured, err := toUnstructured(b)
	if err != nil {
		return nil, fmt.Errorf("error converting Build to unstructured: %w", err)
	}

	result := []unstructured.Unstructured{buildUnstructured}
	result = append(result, newResources...)
	return result, nil
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

	// Volumes — warning only
	if len(ds.Volumes) > 0 {
		c.Log.Warnf("Volumes are not yet supported in the Buildah Strategy in Shipwright. RFE: %s", DockerStrategyVolumesRFE)
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
	if len(ss.Volumes) > 0 {
		c.Log.Warnf("Volumes are not yet supported in the Source-to-Image Strategy in Shipwright. RFE: %s", DockerStrategyVolumesRFE)
	}

	return nil
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
	saName := bc.Spec.ServiceAccount
	if saName == "" {
		saName = bc.Name
	}

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
		if bc.Spec.Source.Binary.AsFile != "" {
			c.Log.Errorf("Archive source is not supported in Shipwright. BuildConfig: %s", bc.Name)
			return
		}
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
			if len(bc.Spec.Output.ImageLabels) > 0 {
				c.Log.Error("BuildConfig sets output imageLabels but has no pushSecret. " +
					"Shipwright applies labels via an injected image-processing step that " +
					"does NOT use the ServiceAccount's registry credentials and will fail " +
					"with '401 Unauthorized' against the internal registry. Create a " +
					"docker-registry secret (e.g. from the builder ServiceAccount token: " +
					"'oc create token builder') and set spec.output.pushSecret on the " +
					"converted Build.")
			} else {
				c.Log.Warn("No explicit pushSecret found for ImageStreamTag output. Ensure the BuildRun uses a ServiceAccount with internal registry push access.")
			}
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
	return u, nil
}
