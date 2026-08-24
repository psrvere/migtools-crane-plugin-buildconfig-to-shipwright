package buildconfig

import (
	"fmt"
	"sort"
	"strings"
)

const internalRegistryURL = "image-registry.openshift-image-registry.svc:5000"

func resolveImageRef(kind, name, namespace string, opts PluginOptionalFields) (string, string, error) {
	switch kind {
	case "DockerImage":
		// A name with a "/" carries a registry or path and is a real pull spec;
		// pass it through registry mapping unchanged. A bare name (no "/" anywhere,
		// registry and namespace both empty, per imagepolicy.go@42e5e40:343) may have
		// resolved on OpenShift to an ImageStream with lookupPolicy.local, which
		// nothing on the target can do.
		if strings.Contains(name, "/") {
			return applyRegistryMapping(name, opts.RegistryMapping), "", nil
		}
		if mapped, ok := opts.ImageStreamMapping[namespace+"/"+name]; ok {
			return applyRegistryMapping(mapped, opts.RegistryMapping), "", nil
		}
		if ref := applyRegistryMapping(name, opts.RegistryMapping); ref != name {
			return ref, "", nil
		}
		return name, fmt.Sprintf("DockerImage reference %q in namespace %q has no registry or path. On OpenShift a name like this may have resolved to an ImageStream with lookupPolicy.local, which Shipwright cannot do. If it is an ImageStream, pass --imagestream-mapping %s/%s=<registry/image:tag>; if it is a public image, qualify the name with its registry, or list that registry in --search-registries so buildah can resolve it at build time.", name, namespace, namespace, name), nil

	case "ImageStreamTag", "ImageStreamImage":
		key := namespace + "/" + name
		if mapped, ok := opts.ImageStreamMapping[key]; ok {
			return applyRegistryMapping(mapped, opts.RegistryMapping), "", nil
		}

		streamRef := name
		fallback := internalRegistryURL + "/" + namespace + "/" + streamRef
		originalFallback := fallback
		fallback = applyRegistryMapping(fallback, opts.RegistryMapping)

		// Only warn if the fallback wasn't transformed by registry mapping
		var warning string
		if fallback == originalFallback {
			warning = fmt.Sprintf("ImageStream reference %q in namespace %q could not be resolved — no --imagestream-mapping provided. Using fallback: %s. Provide --imagestream-mapping to set the correct image reference.", name, namespace, fallback)
		}
		return fallback, warning, nil

	default:
		return "", "", fmt.Errorf("unknown image reference kind %q for %q", kind, name)
	}
}

func applyRegistryMapping(imageRef string, registryMapping map[string]string) string {
	// Iterate in a deterministic order: longest prefix first (most specific
	// mapping wins), ties broken lexically. Plain map iteration order is
	// random in Go, which made the winner nondeterministic when multiple
	// keys matched (BUILD-2339).
	keys := make([]string, 0, len(registryMapping))
	for k := range registryMapping {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	for _, oldRegistry := range keys {
		if oldRegistry == "" {
			// A malformed mapping entry (e.g. "=newvalue") yields an empty
			// key, which HasPrefix would match against every image ref.
			// Ignore it rather than silently remapping everything.
			continue
		}
		if strings.HasPrefix(imageRef, oldRegistry) {
			newRegistry := registryMapping[oldRegistry]
			if newRegistry == "" {
				// A malformed mapping entry (e.g. "quay.io=" or a bare
				// "quay.io" token) yields an empty value; substituting it
				// would produce an invalid ref like "/team/app:v1". Skip
				// the entry and keep looking for a usable mapping.
				continue
			}
			// Note: if imageRef is a bare registry with no path (already not
			// a valid image ref), this intentionally passes it through as the
			// bare replacement registry.
			return newRegistry + imageRef[len(oldRegistry):]
		}
	}
	return imageRef
}
