package kit

import "strings"

// container_name.go — the deterministic container-naming mechanism (kind-blind
// string formatting) moved to kit in P4. A deploy key `<base>/<instance>` maps to
// `charly-<key-with-slash-replaced-by-dash>`; see /charly-core:deploy.

// ContainerName returns the deterministic container name for an image or a
// `<base>/<instance>` deploy key (the `/` is canonicalized to `-`).
func ContainerName(boxName string) string {
	return "charly-" + strings.ReplaceAll(boxName, "/", "-")
}

// ContainerNameInstance returns the container name with an optional instance suffix.
func ContainerNameInstance(boxName, instance string) string {
	if instance == "" {
		return ContainerName(boxName)
	}
	return "charly-" + strings.ReplaceAll(boxName, "/", "-") + "-" + instance
}
