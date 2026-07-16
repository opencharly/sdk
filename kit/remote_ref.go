package kit

import "strings"

// remote_ref.go — the pure remote-ref parsing helpers (K4: relocated from charly/remote_image.go
// and charly/refs.go — genuinely pure string parsing with no project-loader dependency). Consumed
// directly by candy/plugin-deploy-pod and by charly core's remaining callers (the check harness,
// android_deploy_cmd.go), which now import kit directly (K3 ZERO-ALIASES — no alias file).

// StripURLScheme removes http:// or https:// from a remote ref if present.
func StripURLScheme(ref string) string {
	ref = strings.TrimPrefix(ref, "https://")
	ref = strings.TrimPrefix(ref, "http://")
	return ref
}

// ParsedRef represents a parsed remote reference with version.
// Works for both candy refs and image refs.
// Format: @host/org/repo/sub/path:version
type ParsedRef struct {
	Raw      string // original string, e.g. "@github.com/org/repo/candy/name:v1.0.0"
	RepoPath string // e.g. "github.com/org/repo"
	SubPath  string // e.g. "candy/name" (path within repo)
	Name     string // e.g. "name" (last segment)
	Version  string // e.g. "v1.0.0"
}

// IsRemoteImageRef returns true if a ref looks like a remote image reference (starts with @)
func IsRemoteImageRef(ref string) bool {
	return strings.HasPrefix(ref, "@")
}

// ParseRemoteRef parses a remote reference into repo path, sub-path, name, and version.
// e.g. "@github.com/org/repo/candy/name:v1.0.0" -> ParsedRef{RepoPath: "github.com/org/repo", SubPath: "candy/name", Name: "name", Version: "v1.0.0"}
func ParseRemoteRef(ref string) *ParsedRef {
	raw := ref

	// Strip @ prefix
	ref = strings.TrimPrefix(ref, "@")

	// Split version
	version := ""
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		version = ref[idx+1:]
		ref = ref[:idx]
	}

	// Split into repo path (first 3 segments) and sub-path (rest)
	repoPath, subPath, name := splitRepoAndSubPath(ref)

	return &ParsedRef{
		Raw:      raw,
		RepoPath: repoPath,
		SubPath:  subPath,
		Name:     name,
		Version:  version,
	}
}

// splitRepoAndSubPath splits a ref into repo path (host/org/repo), sub-path, and name.
// e.g. "github.com/org/repo/candy/name" -> ("github.com/org/repo", "candy/name", "name")
// For short refs like "pixi", returns ("", "", "pixi").
func splitRepoAndSubPath(ref string) (repoPath, subPath, name string) {
	parts := strings.SplitN(ref, "/", 4) // [host, org, repo, sub/path]
	if len(parts) < 4 {
		// Not enough segments for a remote ref — treat as local name
		name = parts[len(parts)-1]
		if len(parts) <= 1 {
			return "", "", name
		}
		return strings.Join(parts, "/"), "", name
	}
	repoPath = strings.Join(parts[:3], "/")
	subPath = parts[3]
	if idx := strings.LastIndex(subPath, "/"); idx != -1 {
		name = subPath[idx+1:]
	} else {
		name = subPath
	}
	return repoPath, subPath, name
}

// ResolveBoxName extracts the short box name from a ref that may be
// a local box name or a remote ref (github.com/org/repo/box[@version]).
func ResolveBoxName(box string) string {
	ref := StripURLScheme(box)
	if IsRemoteImageRef(ref) {
		return ParseRemoteRef(ref).Name
	}
	return box
}
