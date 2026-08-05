package loaderkit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/spec/spec"
)

// project_candies.go — the project's OWN candy set, scanned out of an already-loaded
// *spec.UnifiedFile (K-wave 2 cone R1, A2 unit 3). Relocated verbatim from
// charly/unified.go's projectCandiesScanned.
//
// It moved for the same reason ParseCandyManifest did, one level up: it is the body of the local
// candy scan, and while it lived in core, a plugin holding a perfectly good *spec.UnifiedFile still
// had to round-trip to the host (`buildengine-scan-local`, `buildengine-namespaced` — both DELETED,
// units 3 and 3b) to turn that uf into a ScannedCandy map. Its three dependencies were
// ScanCandyManifest, ScanInlineCandy, and the
// candy-manifest parse — the first two already live in this package, and the third landed here in
// unit 2, so nothing was left that a plugin could not do itself.

// ProjectCandiesScanned scans or synthesizes a candy per uf.Candy entry, returning the
// pre-completion, pre-finalize spec.ScannedCandy values. A `from:` entry is a DIRECTORY-based candy
// scanned off disk through parseDoc; every other entry is INLINE and synthesized from the already-
// decoded body.
//
// rootDir anchors a relative `from:` path and decides the outside-the-project Remote marking below.
// parseDoc is the per-document manifest parse (ParseCandyManifest bound to the caller's Threaded
// snapshot + build vocabulary).
func ProjectCandiesScanned(uf *spec.UnifiedFile, rootDir string, parseDoc func(path string) (*spec.CandyYAML, error)) (map[string]spec.ScannedCandy, error) {
	out := map[string]spec.ScannedCandy{}
	if uf == nil {
		return out, nil
	}
	for name, raw := range uf.Candy {
		il, ok := spec.DecodeInlineCandy(raw)
		if !ok {
			continue
		}
		if il.From != "" {
			// Directory-based candy — reuse the manifest scanner.
			p := il.From
			if !filepath.IsAbs(p) {
				p = filepath.Join(rootDir, p)
			}
			manifest := il.Manifest
			if manifest == "" {
				manifest = spec.UnifiedFileName
			}
			m, v, refs, err := ScanCandyManifest(p, name, manifest, parseDoc)
			if err != nil {
				return nil, fmt.Errorf("candy %q from %q: %w", name, il.From, err)
			}
			// Candies discovered via `include:` of a remote charly.yml live OUTSIDE the workspace's
			// project tree (typically in the github cache under ~/.cache/charly/repos/). Mark them
			// as Remote so the build's host-fs prep (candy/plugin-build's createRemoteCandyCopies)
			// stages them into .build/_candy/ and the emitted Containerfile COPY paths resolve
			// correctly. THIRD instance of the construct-then-mutate-Remote pattern (W9 mutation-site
			// inventory) — distinct from ScanRemoteCandy's explicit-fetch case and
			// QualifyRemoteSiblingDeps's sibling-dep qualification: this one fires on a plain
			// `from:`-directory candy whose resolved path happens to fall outside the project root.
			if absRoot, err := filepath.Abs(rootDir); err == nil {
				if absCandy, err := filepath.Abs(p); err == nil {
					if rel, err := filepath.Rel(absRoot, absCandy); err == nil && strings.HasPrefix(rel, "..") {
						v.Remote = true
					}
				}
			}
			out[name] = spec.ScannedCandy{Model: m, View: v, Refs: refs}
			continue
		}
		// Inline candy — synthesize. Always LOCAL (declared directly in this charly.yml), so no
		// remote-sibling qualification is needed — mirrors the W9 spike's local-candy case.
		m, v, refs := ScanInlineCandy(name, rootDir, &il.CandyYAML)
		out[name] = spec.ScannedCandy{Model: m, View: v, Refs: refs}
	}
	return out, nil
}
