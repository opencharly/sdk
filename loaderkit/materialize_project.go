// materialize_project.go — the K1 task #48 port of charly's per-document/per-namespace MATERIALIZE
// + root-wins MERGE ORCHESTRATION (charly/materialize.go's former materializeLoadedProject) out of
// charly core. This is the kind-blind walk+merge MECHANISM: which document, which discovered node,
// root-wins merge, namespace fold + byID pointer-identity — none of it touches the provider
// registry. The three genuinely host-/registry-/bootstrap-coupled leaf legs (the per-document
// registry kind-decode, the bootstrap-candy-routed discovered-manifest fold, the binary-embedded
// default vocabulary) are reached through MaterializeProjectSeams, the same injected-seam pattern
// LoadSeams / spec.WalkSeams / spec.MaterializeSeams already established (#46/#47) — so this
// orchestration runs plugin-side (in loaderkit) exactly as LoadUnified does, calling back for only
// the coupled leaves. charly's hostMaterializeProjectSeams is the sole host constructor.
package loaderkit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
	"gopkg.in/yaml.v3"
)

// MaterializeProjectSeams bundles the host-coupled leaf legs MaterializeLoadedProject's kind-blind
// orchestration calls out to. Each is registry-/host-/bootstrap-coupled and CANNOT run kind-blind
// inside loaderkit; the host wrapper (charly's hostMaterializeProjectSeams) is the sole constructor
// and always populates every field. A nil field panics on use.
type MaterializeProjectSeams struct {
	// MaterializeProject folds ONE document's parsed entity nodes into uf via the registered
	// spec.Materializer (registry kind-decode + the per-node not-found policy). uf already carries
	// the document's decoded reserved directives; this adds the Box/Candy/Bundle/PluginKinds
	// entities, accumulating across the document's node list.
	MaterializeProject func(pp *spec.ParsedProject, uf *UnifiedFile) error
	// FoldDiscoveredManifests folds every discovered manifest's parsed nodes into uf — a LAYER
	// candy registers a lazy `From:` reference (the bootstrap-critical candyIsImage box⊻layer
	// routing stays host-side), every other kind materializes via the registered spec.Materializer.
	// Shared host-side with charly's ApplyDiscover (R3), so it stays one host leaf.
	FoldDiscoveredManifests func(dms []spec.DiscoveredManifest, uf *UnifiedFile) error
	// ApplyEmbeddedDefaults merges the binary-embedded build vocabulary + sidecar templates UNDER
	// uf's own entries (project-wins). Host-resident: the embedded bytes and the host node-form
	// parser are charly's.
	ApplyEmbeddedDefaults func(uf *UnifiedFile) error
}

// MaterializeLoadedProject replays the per-document/per-namespace MATERIALIZE + root-wins MERGE over
// a walk envelope, reconstructing the typed *UnifiedFile identically to charly's former inline
// loadUnifiedInto:
//  1. each document (root file + flat imports, in walk order) — decode its reserved directives into
//     a fresh sub UnifiedFile, materialize its parsed nodes (registry kind-decode, via the seam),
//     then root-wins merge the sub into merged (first-seen wins → root wins);
//  2. the discovered manifests — register a lazy layer-candy `From:` reference OR materialize the
//     node, explicit-entry-wins (via the seam, the SAME per-node handler ApplyDiscover uses, R3);
//  3. the binary-embedded default vocabulary (project-wins, via the seam);
//  4. the mounted namespace subtrees — recurse into merged.Namespaces[alias].
func MaterializeLoadedProject(lp *spec.LoadedProject, merged *UnifiedFile, byID map[int64]*UnifiedFile, seams MaterializeProjectSeams) error {
	// Register THIS project's *UnifiedFile under its walk-assigned id BEFORE recursing into its
	// namespaces, so a namespaced cycle-back / diamond REFERENCE mount nested in this subtree
	// resolves to this SAME pointer — the pointer identity the former loadNamespaceCached preserved
	// (the intentional main↔cachyos mutual import). byID persists across the WHOLE materialize.
	if lp.ID != 0 {
		byID[lp.ID] = merged
	}
	// RootDir: this project's OWN base directory, from its root document's SrcDir — the SAME dir
	// every OTHER doc's MergeUnified(merged, &sub, d.SrcDir) call below already threads
	// per-document; the root document (lp.Docs[0], always present) names the project's own dir.
	if len(lp.Docs) > 0 {
		merged.RootDir = lp.Docs[0].SrcDir
	}
	// 1. Documents (root + flat imports) — root-wins merge, in walk order.
	for i := range lp.Docs {
		d := &lp.Docs[i]
		var sub UnifiedFile
		if len(d.Directives) > 0 {
			// Decode the RAW reserved-directive mapping (YAML) into a sub UnifiedFile — the EXACT
			// decode the former mergeUnifiedDocs did, honoring the custom YAML unmarshalers on
			// import/discover.
			if err := yaml.Unmarshal(d.Directives, &sub); err != nil {
				return fmt.Errorf("%s: decoding directives: %w", d.SrcLabel, err)
			}
		}
		// Materialize the document's parsed entity nodes into sub (registry kind-decode, host seam).
		if err := seams.MaterializeProject(&d.Project, &sub); err != nil {
			return fmt.Errorf("%s: %w", d.SrcLabel, err)
		}
		// Imports are already resolved + flattened into lp.Docs by the walk — drop the sub's Import
		// so the merge never re-processes them (the former mergeUnifiedDocs cleared sub.Import too).
		sub.Import = nil
		NormalizeV4Aliases(&sub)
		MergeUnified(merged, &sub, d.SrcDir)
	}
	// 2. Discovered manifests (explicit-entry-wins), applied after the documents.
	if err := seams.FoldDiscoveredManifests(lp.Discovered, merged); err != nil {
		return err
	}
	// 3. Binary-embedded default vocabulary (project-wins).
	if err := seams.ApplyEmbeddedDefaults(merged); err != nil {
		return err
	}
	// 4. Mounted namespaces — each an isolated child UnifiedFile. A REFERENCE mount (cycle-break /
	// diamond) resolves to the SAME *UnifiedFile already registered under its target id (pointer
	// identity preserved); a DEFINITION mount materializes its inline child fresh.
	for i := range lp.Namespaces {
		nm := lp.Namespaces[i]
		if nm == nil {
			continue
		}
		if merged.Namespaces == nil {
			merged.Namespaces = map[string]*UnifiedFile{}
		}
		if nm.Ref {
			shared := byID[nm.RefID]
			if shared == nil {
				return fmt.Errorf("namespace %q: dangling reference to project id %d", nm.Alias, nm.RefID)
			}
			merged.Namespaces[nm.Alias] = shared
			continue
		}
		sub := &UnifiedFile{}
		if err := MaterializeLoadedProject(&nm.Project, sub, byID, seams); err != nil {
			return err
		}
		merged.Namespaces[nm.Alias] = sub
	}
	return nil
}
