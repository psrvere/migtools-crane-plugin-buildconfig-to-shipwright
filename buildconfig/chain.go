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
// refNamespace is the reference's own namespace, already defaulted;
// bcNamespace is the BuildConfig's.
func chainCandidate(kind, refNamespace, bcNamespace string) bool {
	return kind == "ImageStreamTag" && refNamespace == bcNamespace
}

// chainKey identifies an image input before resolution, which is how the
// notices de-duplicate against each other: two references that name the same
// ImageStreamTag are the same input whatever the mapping flags turn them into.
func chainKey(namespace, name string) string {
	return namespace + "/" + name
}

// chainInput is one image input of a BuildConfig. The namespace is defaulted
// the way the conversion sites default it. The kind is left as written, also
// the way the conversion sites leave it: processSource does not default an
// image-source kind either, and an empty one never reaches the notices, since
// resolveImageRef rejects it and fails the conversion first.
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

// chainWatchedByTrigger reports whether an ImageChange trigger's watched image
// is a chain candidate, and returns its key. This is the one place that
// decision is made: processTriggers reads the bool to decide whether the
// dropped-trigger warning ends with the run-order sentence, and
// processChainCandidates reads the key to skip an image that warning already
// names. Keeping both on one function is what holds ADR-0009's "one notice per
// image" to the code.
func chainWatchedByTrigger(bc *buildv1.BuildConfig, trigger buildv1.BuildTriggerPolicy) (string, bool) {
	if canonicalTriggerType(trigger.Type) != buildv1.ImageChangeBuildTriggerType {
		return "", false
	}
	from, _ := imageChangeWatchedObject(bc, trigger.ImageChange)
	if from == nil || !chainCandidate(string(from.Kind), from.Namespace, bc.Namespace) {
		return "", false
	}
	return chainKey(from.Namespace, from.Name), true
}

// chainWarnedBySourceImages reports whether the source.images paths warning
// already names a chain candidate, and returns its key. processSource emits
// that warning and appends the run-order sentence to it. chainInputs leaves
// the entry out for that reason, but the same ImageStreamTag can also be the
// strategy image, which chainInputs does return, so the key is what keeps the
// image to one notice.
func chainWarnedBySourceImages(bc *buildv1.BuildConfig) (string, bool) {
	images := bc.Spec.Source.Images
	if len(images) != 1 || len(images[0].Paths) == 0 || images[0].From.Name == "" {
		return "", false
	}
	namespace := images[0].From.Namespace
	if namespace == "" {
		namespace = bc.Namespace
	}
	if !chainCandidate(string(images[0].From.Kind), namespace, bc.Namespace) {
		return "", false
	}
	return chainKey(namespace, images[0].From.Name), true
}

// processChainCandidates prints one info line per same-namespace ImageStreamTag
// input that no warning already names (BUILD-2326). An input an ImageChange
// trigger watches is carried by that trigger's warning, and source.images with
// paths by the paths warning, so both are left out here. This is Info rather
// than warnf on purpose: nothing was lost, so the conversion stays clean and
// the note reaches the terminal but not the Build.
func (c *Converter) processChainCandidates(bc *buildv1.BuildConfig) {
	// seen starts with the images a warning already names, and then collects
	// each image this pass notices.
	seen := map[string]bool{}
	for _, trigger := range bc.Spec.Triggers {
		if key, chained := chainWatchedByTrigger(bc, trigger); chained {
			seen[key] = true
		}
	}
	if key, warned := chainWarnedBySourceImages(bc); warned {
		seen[key] = true
	}

	// printed guards the message rather than the input: two tags in one
	// namespace can be mapped to the same image, and the identical line twice
	// reads as a bug.
	printed := map[string]bool{}
	for _, in := range chainInputs(bc) {
		if !chainCandidate(in.kind, in.namespace, bc.Namespace) {
			continue
		}
		key := chainKey(in.namespace, in.name)
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
		if printed[imageRef] {
			continue
		}
		printed[imageRef] = true
		// Assigned to msg so the support-matrix test holds the doc to this
		// wording, the way it does for every warning.
		msg := fmt.Sprintf("BuildConfig %s pulls %s from its own namespace.", bc.Name, imageRef)
		msg += fmt.Sprintf(chainRunOrderSentence, bc.Namespace)
		c.Log.Info(msg)
	}
}
