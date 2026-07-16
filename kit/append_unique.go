package kit

// append_unique.go — the generic append-without-duplicates helper (K4 lane B: relocated from
// charly/security.go, a genuinely pure mechanism). Shared between charly core's
// devices.go/config_image.go/security.go (group 3, not moving yet) and candy/plugin-deploy-pod
// (pod_lifecycle_resolve.go's move) via the kit_aliases.go passthrough.

// AppendUnique appends items to dst, skipping any already present in dst.
func AppendUnique(dst []string, items ...string) []string {
	seen := make(map[string]bool, len(dst))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range items {
		if !seen[v] {
			dst = append(dst, v)
			seen[v] = true
		}
	}
	return dst
}
