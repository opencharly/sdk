// discover.go — the K1 port of charly/unified.go's discover walk (ApplyDiscover / applyDiscoveredManifest):
// scans each `discover:` spec for directories carrying its manifest (via the shared kit.FindEntityDirs
// primitive) and PARSES every document each discovered manifest contains into a spec.DiscoveredManifest.
// Faithful port, MINUS the host-only candy-image lazy-ref registration + normalizeNodeInto fold
// (materialize — the registry-coupled kind-decode the host keeps, boundary law) — this file only WALKS +
// PARSES, exactly like the rest of loaderkit.
package loaderkit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// runDiscover walks every flat scan spec on specs and parses every discovered manifest's documents
// into a spec.DiscoveredManifest — one per discovered directory. Faithful port of
// UnifiedFile.ApplyDiscover, minus the host-only entity registration (materialize, kept host-side).
func (w *walker) runDiscover(rootDir string, specs []kit.ScanSpec) ([]spec.DiscoveredManifest, error) {
	var out []spec.DiscoveredManifest
	for _, s := range specs {
		manifest := s.Manifest
		if manifest == "" {
			manifest = kit.UnifiedFileName
		}
		scanPath := s.Path
		if !filepath.IsAbs(scanPath) {
			scanPath = filepath.Join(rootDir, scanPath)
		}
		dirs, err := kit.FindEntityDirs(scanPath, manifest, s.Recursive)
		if err != nil {
			return nil, fmt.Errorf("discover %q: %w", s.Path, err)
		}
		for _, d := range dirs {
			dm, err := w.parseDiscoveredManifest(d, manifest, rootDir)
			if err != nil {
				return nil, err
			}
			out = append(out, dm)
		}
	}
	return out, nil
}

// parseDiscoveredManifest loads one discovered manifest and PARSES every document it contains
// (classify → skip empty → gate → parse) into a spec.DiscoveredManifest. Faithful port of
// UnifiedFile.applyDiscoveredManifest, MINUS the host-only candy-image lazy-ref registration +
// normalizeNodeInto fold (materialize, kept host-side) — this only accumulates the parsed
// spec.ParsedProject per document; the host registers/folds each node by shape afterward.
func (w *walker) parseDiscoveredManifest(dir, manifest, rootDir string) (spec.DiscoveredManifest, error) {
	target := filepath.Join(dir, manifest)
	data, err := os.ReadFile(target)
	if err != nil {
		return spec.DiscoveredManifest{}, fmt.Errorf("reading %s: %w", target, err)
	}
	dm := spec.DiscoveredManifest{Dir: dir, Manifest: manifest, RootDir: rootDir}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return spec.DiscoveredManifest{}, fmt.Errorf("%s: %w", target, err)
		}
		shape, cerr := kit.ClassifyDoc(&node)
		if cerr != nil {
			return spec.DiscoveredManifest{}, fmt.Errorf("%s: %w", target, cerr)
		}
		if shape == kit.DocShapeEmpty {
			continue // empty / directive-only document — nothing to register
		}
		// VALIDATE-BEFORE-EXECUTE: the whole node-form manifest against the host's #NodeDoc gate
		// (strict + closed) — the SAME grammar gate parseDocs applies to the root charly.yml, so
		// GateDoc is the sole load-time gate for EVERY loaded document, discovered manifests
		// included.
		raw, merr := yaml.Marshal(&node)
		if merr != nil {
			return spec.DiscoveredManifest{}, fmt.Errorf("%s: re-marshal node-form doc: %w", target, merr)
		}
		if verr := w.seams.GateDoc(target, raw); verr != nil {
			return spec.DiscoveredManifest{}, verr
		}
		_, pp, perr := w.seams.Parser.ParseDoc(&node, w.seams.Threaded())
		if perr != nil {
			// A malformed node-form manifest is a HARD error, never silently dropped (a swallowed
			// parse error would discover "0 candies").
			return spec.DiscoveredManifest{}, fmt.Errorf("%s: %w", target, perr)
		}
		dm.Docs = append(dm.Docs, pp)
	}
	return dm, nil
}
