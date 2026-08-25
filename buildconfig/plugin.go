package buildconfig

import (
	"encoding/json"
	"fmt"

	"github.com/konveyor/crane-lib/transform"
	buildv1 "github.com/openshift/api/build/v1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const PluginVersion = "v0.1.0"

const (
	RegistryMappingFlag      = "registry-mapping"
	ImageStreamMappingFlag   = "imagestream-mapping"
	DefaultBuildStrategyFlag = "default-build-strategy"
	SearchRegistriesFlag     = "search-registries"
	InsecureRegistriesFlag   = "insecure-registries"
	BlockRegistriesFlag      = "block-registries"
)

type BuildConfigTransformPlugin struct {
	Log logrus.FieldLogger
}

func (p *BuildConfigTransformPlugin) Metadata() transform.PluginMetadata {
	return transform.PluginMetadata{
		Name:    "BuildConfigPlugin",
		Version: PluginVersion,
		OptionalFields: []transform.OptionalFields{
			{
				FlagName: RegistryMappingFlag,
				Help:     "Map of image registry paths to replace, format: old-registry1=new-registry1,old-registry2=new-registry2",
				Example:  "image-registry.openshift-image-registry.svc:5000=quay.io/myorg",
			},
			{
				FlagName: ImageStreamMappingFlag,
				Help:     "Map of ImageStreamTag references to concrete image URLs, format: namespace/name:tag=registry/image:tag",
				Example:  "myns/mystream:latest=quay.io/myorg/myimage:latest",
			},
			{
				FlagName: DefaultBuildStrategyFlag,
				Help:     "Override default ClusterBuildStrategy names, format: docker=my-buildah,s2i=my-s2i",
				Example:  "docker=my-buildah,s2i=my-s2i",
			},
			{
				FlagName: SearchRegistriesFlag,
				Help:     "Comma-separated list of search registries for Buildah",
				Example:  "docker.io,quay.io",
			},
			{
				FlagName: InsecureRegistriesFlag,
				Help:     "Comma-separated list of insecure registries for Buildah",
				Example:  "my-registry.local:5000",
			},
			{
				FlagName: BlockRegistriesFlag,
				Help:     "Comma-separated list of blocked registries for Buildah",
				Example:  "docker.io",
			},
		},
		RequestVersion:  []transform.Version{transform.V1},
		ResponseVersion: []transform.Version{transform.V1},
	}
}

func (p *BuildConfigTransformPlugin) Run(request transform.PluginRequest) (transform.PluginResponse, error) {
	u := request.Unstructured
	gvk := u.GetObjectKind().GroupVersionKind()

	if gvk.Kind != "BuildConfig" || gvk.Group != "build.openshift.io" {
		return transform.PluginResponse{
			Version: string(transform.V1),
		}, nil
	}

	opts, err := ParseOptionalFields(request.Extras)
	if err != nil {
		return transform.PluginResponse{}, fmt.Errorf("error parsing optional fields: %w", err)
	}

	bc := &buildv1.BuildConfig{}

	jsonBytes, err := u.MarshalJSON()
	if err != nil {
		return transform.PluginResponse{}, fmt.Errorf("error marshaling BuildConfig to JSON: %w", err)
	}

	err = json.Unmarshal(jsonBytes, bc)
	if err != nil {
		return transform.PluginResponse{}, fmt.Errorf("error decoding BuildConfig: %w", err)
	}

	converter := &Converter{
		Log:  p.log(),
		Opts: opts,
	}

	newResources, outcome := converter.Convert(bc)
	passThrough := transform.PluginResponse{Version: string(transform.V1)}

	switch outcome.State {
	case OutcomeFailed:
		// crane aborts the entire migration on any plugin error, so a single
		// BuildConfig that fails to convert must not return one. Leave it
		// unchanged and let the rest of the run continue (BUILD-2318).
		p.log().Errorf("BuildConfig %s/%s failed to convert (%s) — leaving it unchanged so the rest of the migration can continue", bc.Namespace, bc.Name, outcome.Reason)
		return p.passThroughWithDisposition(u, outcome, passThrough), nil
	case OutcomeSkipped:
		p.log().Warnf("BuildConfig %s/%s was not converted (%s) — passing through unchanged", bc.Namespace, bc.Name, outcome.Reason)
		return p.passThroughWithDisposition(u, outcome, passThrough), nil
	case OutcomeConverted, OutcomeConvertedWithWarnings:
		p.log().Infof("BuildConfig %s/%s conversion outcome: %s", bc.Namespace, bc.Name, outcome.State)
		return transform.PluginResponse{
			Version:      string(transform.V1),
			IsWhiteOut:   true,
			NewResources: newResources,
		}, nil
	default:
		// An unrecognized outcome state should never reach here. Fail safe:
		// pass the BuildConfig through unchanged rather than white it out and
		// ship whatever Convert returned for a state we do not understand.
		p.log().Errorf("BuildConfig %s/%s produced unknown conversion outcome %q — passing through unchanged", bc.Namespace, bc.Name, outcome.State)
		return passThrough, nil
	}
}

// passThroughWithDisposition returns the pass-through response with a patch that
// records why this BuildConfig was not converted, on the BuildConfig itself.
//
// A failure to build the patch is not worth failing the migration over: the
// BuildConfig still passes through unchanged, which is the behaviour BUILD-2318
// shipped, and the reason is already in the log. Warn and carry on.
func (p *BuildConfigTransformPlugin) passThroughWithDisposition(u unstructured.Unstructured, outcome Outcome, passThrough transform.PluginResponse) transform.PluginResponse {
	patch, err := dispositionPatch(u, outcome)
	if err != nil {
		p.log().Warnf("BuildConfig %s/%s: could not record the %s disposition on the passed-through BuildConfig: %v", u.GetNamespace(), u.GetName(), outcome.State, err)
		return passThrough
	}
	passThrough.Patches = patch
	return passThrough
}

func (p *BuildConfigTransformPlugin) log() logrus.FieldLogger {
	if p.Log != nil {
		return p.Log
	}
	return logrus.New()
}

type PluginOptionalFields struct {
	RegistryMapping    map[string]string
	ImageStreamMapping map[string]string
	StrategyMapping    map[string]string
	SearchRegistries   []string
	InsecureRegistries []string
	BlockRegistries    []string
}

func ParseOptionalFields(extras map[string]string) (PluginOptionalFields, error) {
	opts := PluginOptionalFields{}

	if v, ok := extras[RegistryMappingFlag]; ok && v != "" {
		opts.RegistryMapping = transform.ParseOptionalFieldMapVal(v)
	}
	if v, ok := extras[ImageStreamMappingFlag]; ok && v != "" {
		opts.ImageStreamMapping = transform.ParseOptionalFieldMapVal(v)
	}
	if v, ok := extras[DefaultBuildStrategyFlag]; ok && v != "" {
		opts.StrategyMapping = transform.ParseOptionalFieldMapVal(v)
	}
	if v, ok := extras[SearchRegistriesFlag]; ok && v != "" {
		opts.SearchRegistries = transform.ParseOptionalFieldSliceVal(v)
	}
	if v, ok := extras[InsecureRegistriesFlag]; ok && v != "" {
		opts.InsecureRegistries = transform.ParseOptionalFieldSliceVal(v)
	}
	if v, ok := extras[BlockRegistriesFlag]; ok && v != "" {
		opts.BlockRegistries = transform.ParseOptionalFieldSliceVal(v)
	}

	return opts, nil
}
