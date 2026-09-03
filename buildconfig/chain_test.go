package buildconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	shipwrightv1beta1 "github.com/shipwright-io/build/pkg/apis/build/v1beta1"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	corev1 "k8s.io/api/core/v1"
)

// The fixture is OpenShift's documented chained build (BUILD-2326): artifact-build
// compiles a WAR with S2I and pushes artifact-image:latest; image-build copies the
// WAR out of that image into a runtime image. Both live in namespace "chain".
//
// Every test converts each BuildConfig through its own Converter. crane runs the
// plugin once per resource, so a shared Converter would let a test pass on state
// the shipped plugin never has.

const (
	chainNS          = "chain"
	chainArtifactRef = "artifact-image:latest"
	chainRuntimeRef  = "jee-runtime:latest"
	chainRegistry    = internalRegistryURL + "/" + chainNS + "/"
)

func chainProducer() *buildv1.BuildConfig {
	bc := &buildv1.BuildConfig{}
	bc.Name = "artifact-build"
	bc.Namespace = chainNS
	bc.Spec.Source = buildv1.BuildSource{
		Type: buildv1.BuildSourceGit,
		Git:  &buildv1.GitBuildSource{URI: "https://github.com/openshift/openshift-jee-sample.git"},
	}
	bc.Spec.Strategy = buildv1.BuildStrategy{
		Type: buildv1.SourceBuildStrategyType,
		SourceStrategy: &buildv1.SourceBuildStrategy{
			From: corev1.ObjectReference{Kind: "ImageStreamTag", Name: "wildfly:10.1", Namespace: "openshift"},
		},
	}
	bc.Spec.Output.To = &corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainArtifactRef}
	return bc
}

func chainConsumer(mods ...func(*buildv1.BuildConfig)) *buildv1.BuildConfig {
	dockerfile := "FROM jee-runtime:latest\nCOPY ROOT.war /deployments/ROOT.war"
	bc := &buildv1.BuildConfig{}
	bc.Name = "image-build"
	bc.Namespace = chainNS
	bc.Spec.Source = buildv1.BuildSource{
		Type:       buildv1.BuildSourceImage,
		Dockerfile: &dockerfile,
		Images: []buildv1.ImageSource{{
			From:  corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainArtifactRef},
			Paths: []buildv1.ImageSourcePath{{SourcePath: "/wildfly/standalone/deployments/ROOT.war", DestinationDir: "."}},
		}},
	}
	bc.Spec.Strategy = buildv1.BuildStrategy{
		Type: buildv1.DockerBuildStrategyType,
		DockerStrategy: &buildv1.DockerBuildStrategy{
			From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainRuntimeRef},
		},
	}
	bc.Spec.Output.To = &corev1.ObjectReference{Kind: "ImageStreamTag", Name: "image-build:latest"}
	bc.Spec.Triggers = []buildv1.BuildTriggerPolicy{{
		Type:        buildv1.ImageChangeBuildTriggerType,
		ImageChange: &buildv1.ImageChangeTrigger{From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainArtifactRef}},
	}}
	for _, m := range mods {
		m(bc)
	}
	return bc
}

func withoutTrigger(bc *buildv1.BuildConfig) { bc.Spec.Triggers = nil }
func withoutPaths(bc *buildv1.BuildConfig)   { bc.Spec.Source.Images[0].Paths = nil }

// cleanOutput removes every other reason for a warning, so the outcome reflects
// the chain notice alone: no inline Dockerfile, a registry output with a push
// secret, and the one runPolicy Shipwright already matches.
func cleanOutput(bc *buildv1.BuildConfig) {
	bc.Spec.Source.Dockerfile = nil
	bc.Spec.Output.To = &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/acme/image-build:latest"}
	bc.Spec.Output.PushSecret = &corev1.LocalObjectReference{Name: "quay-push"}
	bc.Spec.RunPolicy = buildv1.BuildRunPolicyParallel
}

func chainMapping() map[string]string {
	return map[string]string{
		chainNS + "/" + chainArtifactRef: "quay.io/acme/artifact:latest",
		chainNS + "/" + chainRuntimeRef:  "quay.io/acme/jee-runtime:latest",
	}
}

// convertAlone converts one BuildConfig through a fresh Converter and returns
// the Build (nil when the conversion failed), the outcome, and the log hook.
func convertAlone(t *testing.T, bc *buildv1.BuildConfig, mapping map[string]string) (*shipwrightv1beta1.Build, Outcome, *logrustest.Hook) {
	t.Helper()
	logger, hook := logrustest.NewNullLogger()
	c := &Converter{Log: logger, Opts: PluginOptionalFields{ImageStreamMapping: mapping}}
	resources, outcome := c.Convert(bc)
	if outcome.State == OutcomeFailed {
		return nil, outcome, hook
	}
	for _, u := range resources {
		if u.GetKind() != "Build" {
			continue
		}
		b := &shipwrightv1beta1.Build{}
		raw, err := json.Marshal(u.Object)
		if err != nil {
			t.Fatalf("marshal Build: %v", err)
		}
		if err := json.Unmarshal(raw, b); err != nil {
			t.Fatalf("unmarshal Build: %v", err)
		}
		return b, outcome, hook
	}
	t.Fatalf("no Build among %d resources", len(resources))
	return nil, outcome, hook
}

// The producer's output and the consumer's input go through the same mapping
// and the same fallback, so the chain stays linked by name on both sides. This
// pins the property the original scope item 1 asked for.
func TestChainSymbolicReferencePreserved(t *testing.T) {
	cases := []struct {
		name    string
		mapping map[string]string
		want    string
	}{
		{"no mapping keeps the internal registry tag", nil, chainRegistry + chainArtifactRef},
		{"mapping applies to both sides", chainMapping(), "quay.io/acme/artifact:latest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			producer, _, _ := convertAlone(t, chainProducer(), tc.mapping)
			consumer, _, _ := convertAlone(t, chainConsumer(), tc.mapping)
			if producer.Spec.Output.Image != tc.want {
				t.Errorf("producer output = %q, want %q", producer.Spec.Output.Image, tc.want)
			}
			if consumer.Spec.Source == nil || consumer.Spec.Source.OCIArtifact == nil {
				t.Fatalf("consumer has no OCI artifact source: %+v", consumer.Spec.Source)
			}
			if got := consumer.Spec.Source.OCIArtifact.Image; got != tc.want {
				t.Errorf("consumer input = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChainImageChangeWarningCarriesRunOrder(t *testing.T) {
	b, outcome, hook := convertAlone(t, chainConsumer(), nil)
	warnings := warnMessages(hook)
	found := firstContaining(warnings, "ImageChange trigger is dropped")
	if found == "" {
		t.Fatalf("no ImageChange warning among %v", warnings)
	}
	existing := "ImageChange trigger is dropped — builds will no longer start when ImageStreamTag chain/artifact-image:latest changes. Shipwright has no equivalent of image change triggers today."
	if !strings.Contains(found, existing) {
		t.Errorf("warning lost its existing wording: %q", found)
	}
	if suffix := fmt.Sprintf(chainRunOrderSentence, chainNS); !strings.HasSuffix(found, suffix) {
		t.Errorf("warning does not end with the run-order sentence: %q", found)
	}
	if outcome.State != OutcomeConvertedWithWarnings {
		t.Errorf("outcome = %q, want %q", outcome.State, OutcomeConvertedWithWarnings)
	}
	if got := b.Annotations[ConversionOutcomeAnnotation]; got != string(OutcomeConvertedWithWarnings) {
		t.Errorf("annotation %s = %q", ConversionOutcomeAnnotation, got)
	}
}

// Without the trigger, only the paths exclusion in chainInputs can keep the
// info line away from an image the paths warning already carries.
func TestChainPathsWarningNamesImageAndRewrite(t *testing.T) {
	_, _, hook := convertAlone(t, chainConsumer(withoutTrigger), nil)
	warnings := warnMessages(hook)
	ref := chainRegistry + chainArtifactRef
	found := firstContaining(warnings, "source.images copied")
	if found == "" {
		t.Fatalf("no paths warning among %v", warnings)
	}
	assertContainsAll(t, found, ref, "1 path(s)", "COPY --from="+ref, "remove source.images", "spec.source.git", fmt.Sprintf(chainRunOrderSentence, chainNS))
	if n := countContaining(warnings, "Image source 'Paths' field is not supported"); n != 0 {
		t.Errorf("old paths wording still emitted: %v", warnings)
	}
	if n := len(logMessages(hook, logrus.InfoLevel, "pulls "+ref)); n != 0 {
		t.Errorf("artifact image is carried by the paths warning but also got %d info line(s)", n)
	}
}

// An input no warning names gets an info line and leaves the conversion clean.
func TestChainInfoForInputNoWarningNames(t *testing.T) {
	bc := chainConsumer(withoutTrigger, withoutPaths, cleanOutput)
	b, outcome, hook := convertAlone(t, bc, chainMapping())
	infos := logMessages(hook, logrus.InfoLevel, "")
	if n := countContaining(infos, "pulls quay.io/acme/artifact:latest from its own namespace"); n != 1 {
		t.Errorf("want exactly one info line for the artifact image, got %d: %v", n, infos)
	}
	if n := countContaining(infos, "pulls quay.io/acme/jee-runtime:latest from its own namespace"); n != 1 {
		t.Errorf("want exactly one info line for the strategy image, got %d: %v", n, infos)
	}
	if n := len(logMessages(hook, logrus.WarnLevel, "run its BuildRun")); n != 0 {
		t.Errorf("run-order text reached a warning: %v", warnMessages(hook))
	}
	if outcome.State != OutcomeConverted {
		t.Errorf("outcome = %q with warnings %v, want %q", outcome.State, outcome.Warnings, OutcomeConverted)
	}
	if got := b.Annotations[ConversionOutcomeAnnotation]; got != string(OutcomeConverted) {
		t.Errorf("annotation %s = %q", ConversionOutcomeAnnotation, got)
	}
}

// The trigger warning carries the image it watches; the info line names only
// the strategy image nothing else mentions.
func TestChainTriggerCarriesOnlyTheWatchedImage(t *testing.T) {
	_, _, hook := convertAlone(t, chainConsumer(withoutPaths), nil)
	infos := logMessages(hook, logrus.InfoLevel, "")
	if n := countContaining(infos, "pulls "+chainRegistry+chainRuntimeRef+" from its own namespace"); n != 1 {
		t.Errorf("want exactly one info line for the strategy image, got %d: %v", n, infos)
	}
	if n := countContaining(infos, "pulls "+chainRegistry+chainArtifactRef); n != 0 {
		t.Errorf("watched image got %d info line(s) on top of the trigger warning: %v", n, infos)
	}
}

func TestChainNoticeControls(t *testing.T) {
	dockerImage := func(bc *buildv1.BuildConfig) {
		bc.Spec.Strategy.DockerStrategy.From = &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/acme/jee-runtime:latest"}
		bc.Spec.Source.Images[0].From = corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/acme/artifact:latest"}
	}
	imageStreamImage := func(bc *buildv1.BuildConfig) {
		bc.Spec.Strategy.DockerStrategy.From = &corev1.ObjectReference{Kind: "ImageStreamImage", Name: "jee-runtime@sha256:abc"}
		bc.Spec.Source.Images[0].From = corev1.ObjectReference{Kind: "ImageStreamImage", Name: "artifact-image@sha256:def"}
	}
	otherNamespace := func(bc *buildv1.BuildConfig) {
		bc.Spec.Strategy.DockerStrategy.From.Namespace = "shared"
		bc.Spec.Source.Images[0].From.Namespace = "shared"
		bc.Spec.Triggers[0].ImageChange.From.Namespace = "shared"
	}
	cases := []struct {
		name string
		bc   *buildv1.BuildConfig
	}{
		{"builder image from the openshift namespace", chainProducer()},
		{"DockerImage inputs", chainConsumer(withoutTrigger, withoutPaths, dockerImage)},
		{"DockerImage inputs with paths keeps the sentence off the paths warning", chainConsumer(withoutTrigger, dockerImage)},
		{"ImageStreamImage inputs in the own namespace", chainConsumer(withoutTrigger, withoutPaths, imageStreamImage)},
		{"inputs and trigger in another namespace", chainConsumer(withoutPaths, otherNamespace)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, outcome, hook := convertAlone(t, tc.bc, nil)
			if outcome.State == OutcomeFailed {
				t.Fatalf("conversion failed: %s", outcome.Reason)
			}
			all := append(warnMessages(hook), logMessages(hook, logrus.InfoLevel, "")...)
			for _, text := range []string{"run its BuildRun", "from its own namespace"} {
				if n := countContaining(all, text); n != 0 {
					t.Errorf("%d message(s) carry %q: %v", n, text, all)
				}
			}
		})
	}
}

func TestChainMultipleSourceErrorNamesTheRewrite(t *testing.T) {
	git := &buildv1.GitBuildSource{URI: "https://github.com/example/app.git"}
	binary := &buildv1.BinaryBuildSource{AsFile: "app.war"}
	cases := []struct {
		name        string
		bc          *buildv1.BuildConfig
		wantRewrite bool
	}{
		{"git plus source.images", chainConsumer(withoutTrigger, func(bc *buildv1.BuildConfig) { bc.Spec.Source.Git = git }), true},
		{"binary plus source.images", chainConsumer(withoutTrigger, func(bc *buildv1.BuildConfig) { bc.Spec.Source.Binary = binary }), true},
		{"git plus binary is not a chain", func() *buildv1.BuildConfig { bc := chainProducer(); bc.Spec.Source.Binary = binary; return bc }(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, outcome, _ := convertAlone(t, tc.bc, nil)
			if outcome.State != OutcomeFailed {
				t.Fatalf("converted with outcome %q, want %q", outcome.State, OutcomeFailed)
			}
			if got := strings.Contains(outcome.Reason, "COPY --from="); got != tc.wantRewrite {
				t.Errorf("rewrite named = %v, want %v: %q", got, tc.wantRewrite, outcome.Reason)
			}
		})
	}
}

// A Source strategy may leave the kind off its from; the conversion treats
// that as ImageStreamTag, and so must the chain notices. The explicit trigger
// from is an ImageStreamTag by definition, with or without the kind written.
func TestChainImplicitKindIsImageStreamTag(t *testing.T) {
	s2i := func(mods ...func(*buildv1.BuildConfig)) *buildv1.BuildConfig {
		bc := chainProducer()
		bc.Namespace = chainNS
		bc.Spec.Strategy.SourceStrategy.From = corev1.ObjectReference{Name: "builder:latest"}
		for _, m := range mods {
			m(bc)
		}
		return bc
	}
	builderRef := chainRegistry + "builder:latest"

	t.Run("strategy from without kind gets the info line", func(t *testing.T) {
		_, outcome, hook := convertAlone(t, s2i(), nil)
		if outcome.State == OutcomeFailed {
			t.Fatalf("conversion failed: %s", outcome.Reason)
		}
		if n := len(logMessages(hook, logrus.InfoLevel, "pulls "+builderRef+" from its own namespace")); n != 1 {
			t.Errorf("want one info line for the builder, got %d", n)
		}
	})

	t.Run("empty trigger from watches the strategy image", func(t *testing.T) {
		bc := s2i(func(bc *buildv1.BuildConfig) {
			bc.Spec.Triggers = []buildv1.BuildTriggerPolicy{{Type: buildv1.ImageChangeBuildTriggerType, ImageChange: &buildv1.ImageChangeTrigger{}}}
		})
		_, _, hook := convertAlone(t, bc, nil)
		found := firstContaining(warnMessages(hook), "ImageChange trigger is dropped")
		if !strings.HasSuffix(found, fmt.Sprintf(chainRunOrderSentence, chainNS)) {
			t.Errorf("trigger warning lacks the run-order sentence: %q", found)
		}
		if n := len(logMessages(hook, logrus.InfoLevel, "pulls "+builderRef)); n != 0 {
			t.Errorf("watched builder also got %d info line(s)", n)
		}
	})

	t.Run("explicit trigger from without kind", func(t *testing.T) {
		bc := s2i(func(bc *buildv1.BuildConfig) {
			bc.Spec.Triggers = []buildv1.BuildTriggerPolicy{{Type: buildv1.ImageChangeBuildTriggerType, ImageChange: &buildv1.ImageChangeTrigger{From: &corev1.ObjectReference{Name: chainArtifactRef}}}}
		})
		_, _, hook := convertAlone(t, bc, nil)
		found := firstContaining(warnMessages(hook), "ImageChange trigger is dropped")
		if !strings.Contains(found, "when ImageStreamTag chain/artifact-image:latest changes") {
			t.Errorf("watched ref not formatted as an ImageStreamTag: %q", found)
		}
		if !strings.HasSuffix(found, fmt.Sprintf(chainRunOrderSentence, chainNS)) {
			t.Errorf("trigger warning lacks the run-order sentence: %q", found)
		}
	})
}

// An empty imageChange block on a Docker strategy watches the strategy image.
func TestChainImplicitWatchOnDockerStrategy(t *testing.T) {
	bc := chainConsumer(withoutPaths, func(bc *buildv1.BuildConfig) {
		bc.Spec.Triggers = []buildv1.BuildTriggerPolicy{{Type: buildv1.ImageChangeBuildTriggerType, ImageChange: &buildv1.ImageChangeTrigger{}}}
	})
	_, _, hook := convertAlone(t, bc, nil)
	found := firstContaining(warnMessages(hook), "ImageChange trigger is dropped")
	if !strings.Contains(found, "when the strategy image "+chainRuntimeRef+" changes") || !strings.HasSuffix(found, fmt.Sprintf(chainRunOrderSentence, chainNS)) {
		t.Errorf("unexpected trigger warning: %q", found)
	}
	infos := logMessages(hook, logrus.InfoLevel, "")
	if n := countContaining(infos, "pulls "+chainRegistry+chainRuntimeRef); n != 0 {
		t.Errorf("watched strategy image also got %d info line(s): %v", n, infos)
	}
	if n := countContaining(infos, "pulls "+chainRegistry+chainArtifactRef+" from its own namespace"); n != 1 {
		t.Errorf("want one info line for the unwatched artifact image, got %d: %v", n, infos)
	}
}

// Two triggers carry two images; nothing is left for an info line.
func TestChainTwoTriggersCarryBothImages(t *testing.T) {
	bc := chainConsumer(withoutPaths, func(bc *buildv1.BuildConfig) {
		bc.Spec.Triggers = append(bc.Spec.Triggers, buildv1.BuildTriggerPolicy{
			Type:        buildv1.ImageChangeBuildTriggerType,
			ImageChange: &buildv1.ImageChangeTrigger{From: &corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainRuntimeRef}},
		})
	})
	_, _, hook := convertAlone(t, bc, nil)
	warnings := warnMessages(hook)
	if n := countContaining(warnings, fmt.Sprintf(chainRunOrderSentence, chainNS)); n != 2 {
		t.Errorf("want the run-order sentence on both trigger warnings, got %d: %v", n, warnings)
	}
	if n := len(logMessages(hook, logrus.InfoLevel, "from its own namespace")); n != 0 {
		t.Errorf("carried images still got %d info line(s)", n)
	}
}

// The same image named by the strategy from and by source.images is one
// candidate, not two.
func TestChainDedupesSharedImage(t *testing.T) {
	bc := chainConsumer(withoutTrigger, withoutPaths, func(bc *buildv1.BuildConfig) {
		bc.Spec.Strategy.DockerStrategy.From = &corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainArtifactRef}
	})
	_, _, hook := convertAlone(t, bc, nil)
	if n := len(logMessages(hook, logrus.InfoLevel, "from its own namespace")); n != 1 {
		t.Errorf("want exactly one info line for the shared image, got %d", n)
	}
}

// The same image as the strategy from and as the source.images from, with
// paths: the paths warning names it, so the info line must not name it again.
// chainInputs drops the source.images entry for that reason but still returns
// the strategy image, which is what made this shape emit both.
func TestChainSharedImageWithPathsGetsOnlyTheWarning(t *testing.T) {
	bc := chainConsumer(withoutTrigger, func(bc *buildv1.BuildConfig) {
		bc.Spec.Strategy.DockerStrategy.From = &corev1.ObjectReference{Kind: "ImageStreamTag", Name: chainArtifactRef}
	})
	_, _, hook := convertAlone(t, bc, nil)
	ref := chainRegistry + chainArtifactRef
	if n := countContaining(warnMessages(hook), "source.images copied 1 path(s) from "+ref); n != 1 {
		t.Errorf("want one paths warning naming %s, got %d: %v", ref, n, warnMessages(hook))
	}
	if n := len(logMessages(hook, logrus.InfoLevel, "from its own namespace")); n != 0 {
		t.Errorf("the paths warning already names the image; got %d info line(s): %v", n, logMessages(hook, logrus.InfoLevel, ""))
	}
}

// Two tags mapped onto one image resolve to the same text. The info line is
// de-duplicated on the resolved reference, not only on the tag, so the
// operator does not read the identical sentence twice.
func TestChainDedupesOnTheResolvedImage(t *testing.T) {
	bc := chainConsumer(withoutTrigger, withoutPaths, cleanOutput)
	mapping := map[string]string{
		chainNS + "/" + chainArtifactRef: "quay.io/acme/same:latest",
		chainNS + "/" + chainRuntimeRef:  "quay.io/acme/same:latest",
	}
	_, outcome, hook := convertAlone(t, bc, mapping)
	infos := logMessages(hook, logrus.InfoLevel, "")
	if n := countContaining(infos, "pulls quay.io/acme/same:latest from its own namespace"); n != 1 {
		t.Errorf("want one info line for the shared target, got %d: %v", n, infos)
	}
	if outcome.State != OutcomeConverted {
		t.Errorf("outcome = %q with warnings %v, want %q", outcome.State, outcome.Warnings, OutcomeConverted)
	}
}

// Several source.images entries is the artifact chain with more than one
// producer. It fails, and the error owes the same rewrite the paths warning
// gives.
func TestChainMultipleImageSourcesNameTheRewrite(t *testing.T) {
	bc := chainConsumer(withoutTrigger, func(bc *buildv1.BuildConfig) {
		bc.Spec.Source.Images = append(bc.Spec.Source.Images, buildv1.ImageSource{
			From:  corev1.ObjectReference{Kind: "ImageStreamTag", Name: "second-artifact:latest"},
			Paths: []buildv1.ImageSourcePath{{SourcePath: "/out/app.jar", DestinationDir: "."}},
		})
	})
	_, outcome, _ := convertAlone(t, bc, nil)
	if outcome.State != OutcomeFailed {
		t.Fatalf("converted with outcome %q, want %q", outcome.State, OutcomeFailed)
	}
	assertContainsAll(t, outcome.Reason, "multiple image sources are not supported", "COPY --from=", "remove source.images")
}
