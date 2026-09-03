package buildconfig

import (
	"fmt"

	buildv1 "github.com/openshift/api/build/v1"
)

// chainRunOrderSentence is appended to a message that already names an image
// another BuildConfig in the export may build (BUILD-2326). It takes the
// BuildConfig's namespace. The plugin sees one BuildConfig per process and
// Shipwright does not order BuildRuns, so the most it can say is what to do if
// the image turns out to be chained.
const chainRunOrderSentence = " If another BuildConfig in namespace %s builds that image, run its BuildRun to completion before starting this Build; Shipwright does not order BuildRuns."

// chainCandidate reports whether an image input may be the output of another
// BuildConfig in the same export: an ImageStreamTag in the BuildConfig's own
// namespace. crane exports one namespace, so a producer elsewhere is outside
// the converted set. An imported stream matches too, and the plugin cannot
// tell the two apart, so every notice built on this is worded "if".
func chainCandidate(kind, namespace, bcNamespace string) bool {
	return kind == "ImageStreamTag" && namespace == bcNamespace
}

// chainInput is one image input of a BuildConfig, with its kind and namespace
// defaulted the way the conversion sites default them.
type chainInput struct {
	kind, name, namespace string
}

// chainInputs lists the strategy image and, when it has no paths, the single
// source.images entry. source.images with paths is carried by the paths
// warning instead, and more than one entry never converts.
func chainInputs(bc *buildv1.BuildConfig) []chainInput {
	var inputs []chainInput
	// The strategy image, defaulted the way the trigger warning sees it.
	if from, _ := imageChangeWatchedObject(bc, nil); from != nil {
		inputs = append(inputs, chainInput{kind: string(from.Kind), name: from.Name, namespace: from.Namespace})
	}
	if images := bc.Spec.Source.Images; len(images) == 1 && len(images[0].Paths) == 0 && images[0].From.Name != "" {
		namespace := images[0].From.Namespace
		if namespace == "" {
			namespace = bc.Namespace
		}
		inputs = append(inputs, chainInput{kind: string(images[0].From.Kind), name: images[0].From.Name, namespace: namespace})
	}
	return inputs
}

// processChainCandidates prints one info line per same-namespace ImageStreamTag
// input that no warning already names (BUILD-2326). An input an ImageChange
// trigger watches is carried by that trigger's warning, and source.images with
// paths by the paths warning, so both are left out here. This is Info rather
// than warnf on purpose: nothing was lost, so the conversion stays clean and
// the note reaches the terminal but not the Build.
func (c *Converter) processChainCandidates(bc *buildv1.BuildConfig) {
	// seen starts with the images whose trigger warning already ends with the
	// run-order sentence, exactly the candidates processTriggers saw, and then
	// collects each image this pass notices.
	seen := map[string]bool{}
	for _, trigger := range bc.Spec.Triggers {
		if canonicalTriggerType(trigger.Type) != buildv1.ImageChangeBuildTriggerType {
			continue
		}
		if from, _ := imageChangeWatchedObject(bc, trigger.ImageChange); from != nil && chainCandidate(string(from.Kind), from.Namespace, bc.Namespace) {
			seen[from.Namespace+"/"+from.Name] = true
		}
	}

	for _, in := range chainInputs(bc) {
		if !chainCandidate(in.kind, in.namespace, bc.Namespace) {
			continue
		}
		key := in.namespace + "/" + in.name
		if seen[key] {
			continue
		}
		seen[key] = true
		// The conversion site already resolved this reference and reported any
		// mapping warning, so only the resolved name is wanted here.
		imageRef, _, err := resolveImageRef(in.kind, in.name, in.namespace, c.Opts)
		if err != nil {
			continue
		}
		// Assigned to msg so the support-matrix test holds the doc to this
		// wording, the way it does for every warning.
		msg := fmt.Sprintf("BuildConfig %s pulls %s from its own namespace.", bc.Name, imageRef)
		msg += fmt.Sprintf(chainRunOrderSentence, bc.Namespace)
		c.Log.Info(msg)
	}
}
