package buildconfig

import (
	"fmt"
	"strings"

	buildv1 "github.com/openshift/api/build/v1"
)

// postCommitWarningTemplate explains both what was dropped and what the user
// loses by moving the check downstream. In OpenShift the hook runs in a
// temporary container on the built image immediately after the last layer is
// committed and before the push, and a non-zero exit fails the build — so a
// failing hook keeps the image out of the registry. Nothing in Shipwright runs
// between commit and push, and a Tekton Pipeline step after the BuildRun runs
// once the image is already published. Stating that plainly matters more than
// naming a replacement.
const postCommitWarningTemplate = "PostCommit hook (%s) has no Shipwright equivalent and was dropped from " +
	"BuildConfig '%s'. In OpenShift this ran inside the built image before the push, and a failure failed " +
	"the build. To replicate, add a test step after the BuildRun in a Tekton Pipeline — note this runs after " +
	"the image is pushed, so it can no longer block a bad image from reaching the registry."

// processPostCommit warns when a BuildConfig carries a postCommit hook, which
// the conversion drops. It reads bc only: no Shipwright Build field can hold
// this, so there is nothing to write.
//
// Callers must invoke this only on the path that actually produces a Build.
// Convert returns early — passing the BuildConfig through unchanged — for the
// Custom and JenkinsPipeline strategies and for a missing spec.output.to. On
// those paths the hook is not dropped, and warning would be false.
func (c *Converter) processPostCommit(bc *buildv1.BuildConfig) {
	descriptor, ok := postCommitFormDescriptor(bc.Spec.PostCommit)
	if !ok {
		return
	}

	c.Log.Warnf(postCommitWarningTemplate, descriptor, bc.Name)

	// The BuildConfig API forbids script and command together. Accept the
	// input rather than fail the migration, but say so.
	if bc.Spec.PostCommit.Script != "" && len(bc.Spec.PostCommit.Command) > 0 {
		c.Log.Warnf("BuildConfig '%s' sets both script and command in spec.postCommit, which the BuildConfig "+
			"API does not allow. The script form was assumed for the warning above.", bc.Name)
	}
}

// postCommitFormDescriptor names which of the documented postCommit forms is in
// use, for interpolation into the warning. It reports false when the hook is
// empty and no warning is due.
//
// BuildPostCommitSpec documents five valid forms, not three mutually exclusive
// fields: script, command, args, script+args, and command+args. Only
// script+command together is invalid. script takes precedence because the API
// documents it as shorthand for command ["/bin/sh", "-ic"] plus args.
//
// The script body is deliberately never interpolated: it is free-form and
// frequently multi-line, which would break a single-line log entry.
func postCommitFormDescriptor(pc buildv1.BuildPostCommitSpec) (string, bool) {
	// Presence is tested with len(), not the joined string: args ([]string{""})
	// joins to "" yet still runs the image entrypoint with one empty argument,
	// so the hook fires and the warning is due.
	hasArgs := len(pc.Args) > 0
	args := strings.Join(pc.Args, " ")

	switch {
	case pc.Script != "":
		if hasArgs {
			return fmt.Sprintf("script with args: %s", args), true
		}
		return "script", true

	case len(pc.Command) > 0:
		command := strings.Join(pc.Command, " ")
		if hasArgs {
			return fmt.Sprintf("command: %s, args: %s", command, args), true
		}
		return fmt.Sprintf("command: %s", command), true

	case hasArgs:
		return fmt.Sprintf("args: %s", args), true

	default:
		return "", false
	}
}
