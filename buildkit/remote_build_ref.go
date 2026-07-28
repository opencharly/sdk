package buildkit

// remote_build_ref.go — the pure remote-ref pivot DETECTION for `charly box build` (the BUILD-cone
// "every capability is a plugin" cutover, P8b): decide whether a build must pivot to a remote @ref
// source. This is the detection half only — a shallow charly.yml peek, never the K1 loader — so it
// lives plugin-side in sdk; the K1-coupled RESOLUTION of the returned ref (EnsureRepoDownloaded →
// clone/cache) stays a thin host seam (charly's "remote-image-resolve" HostBuild). Relocated from
// charly core's former BuildCmd.checkRemoteRefsAndPivot / detectRemoteIncludePassthrough.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// DetectRemoteBuildRef decides whether `charly box build <boxes>` must pivot to a remote @ref source
// and returns that ref. An explicit remote ref among the requested boxes wins first; otherwise a
// thin workspace whose SOLE import is one flat remote ref auto-pivots a locally-undeclared image to
// that upstream source (so `cd ~/projects/ecovoyage && charly box build versa` rebuilds from
// upstream without any flags). Byte-equivalent to the former core BuildCmd.checkRemoteRefsAndPivot.
// Callers pass boxes already run through NormalizeBoxArgs.
func DetectRemoteBuildRef(dir string, boxes []string) (string, bool) {
	for _, img := range boxes {
		if spec.IsRemoteImageRef(kit.StripURLScheme(img)) {
			return img, true
		}
	}
	return detectRemoteIncludePassthrough(dir, boxes)
}

// detectRemoteIncludePassthrough inspects dir's charly.yml for a single
// `@github.com/owner/repo/...charly.yml:ref` include. If found AND the requested image isn't
// declared locally in the workspace (i.e. the image lives upstream), returns the synthesized
// remote-image-ref `@github.com/owner/repo/<image>:ref` plus true. Otherwise returns ("", false).
//
// Conservative: only fires when (a) there's exactly one include, (b) it's a remote
// @github.com/...charly.yml ref, (c) the user asked for a single image, and (d) the workspace
// charly.yml has no local `box:` entry of that name.
func detectRemoteIncludePassthrough(dir string, boxes []string) (string, bool) {
	if len(boxes) != 1 {
		return "", false
	}
	boxName := boxes[0]
	unifiedPath := filepath.Join(dir, kit.UnifiedFileName)
	data, err := os.ReadFile(unifiedPath)
	if err != nil {
		return "", false
	}
	var peek struct {
		// Read the `import:` list generically (items are either bare strings —
		// flat imports — or single-key `alias: ref` maps — namespaced imports).
		Import []any                      `yaml:"import" json:"import"`
		Box    map[string]json.RawMessage `yaml:"box" json:"box"`
	}
	if err := yaml.Unmarshal(data, &peek); err != nil {
		return "", false
	}
	// The passthrough fires only for a thin project whose SOLE import is one
	// flat remote ref (a single-string import naming another repo). A project
	// with namespaced imports or multiple imports uses the normal build path.
	var stringImports []string
	for _, it := range peek.Import {
		if s, ok := it.(string); ok {
			stringImports = append(stringImports, s)
		}
	}
	if len(peek.Import) != 1 || len(stringImports) != 1 {
		return "", false
	}
	// If the image is declared locally, keep the normal local path.
	if _, hasLocal := peek.Box[boxName]; hasLocal {
		return "", false
	}
	inc := stringImports[0]
	if !strings.HasPrefix(inc, "@") {
		return "", false
	}
	// Parse `@github.com/owner/repo/...:ref` and substitute the image name.
	bare := strings.TrimPrefix(inc, "@")
	versionIdx := strings.LastIndex(bare, ":")
	var version string
	pathPart := bare
	if versionIdx > 0 {
		pathPart = bare[:versionIdx]
		version = bare[versionIdx+1:]
	}
	// pathPart is e.g. github.com/opencharly/charly/charly.yml.
	// Strip the trailing filename to get the repo root.
	slashIdx := strings.LastIndex(pathPart, "/")
	if slashIdx < 0 {
		return "", false
	}
	repoRoot := pathPart[:slashIdx]
	// Synthesize @github.com/owner/repo/<image>[:ref].
	ref := "@" + repoRoot + "/" + boxName
	if version != "" {
		ref += ":" + version
	}
	return ref, true
}
