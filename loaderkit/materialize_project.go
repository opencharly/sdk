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

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// MaterializeProjectSeams is DEFINED in the dedicated spec module (spec/spec/materialize_project_seams.go,
// #55 2b C3) — the host supplies it spec-typed via LoaderExecutor.MaterializeProjectSeams(). This alias
// keeps loaderkit's own orchestration (MaterializeLoadedProject below) terse.
type MaterializeProjectSeams = spec.MaterializeProjectSeams

// MaterializeLoadedProject replays the per-document/per-namespace MATERIALIZE + root-wins MERGE over
// a walk envelope, reconstructing the typed *spec.UnifiedFile identically to charly's former inline
// loadUnifiedInto:
//  1. each document (root file + flat imports, in walk order) — decode its reserved directives into
//     a fresh sub spec.UnifiedFile, materialize its parsed nodes (registry kind-decode, via the seam),
//     then root-wins merge the sub into merged (first-seen wins → root wins);
//  2. the discovered manifests — register a lazy layer-candy `From:` reference OR materialize the
//     node, explicit-entry-wins (via the seam, the SAME per-node handler ApplyDiscover uses, R3);
//  3. the binary-embedded default vocabulary (project-wins, via the seam);
//  4. the mounted namespace subtrees — recurse into merged.Namespaces[alias].
func MaterializeLoadedProject(lp *spec.LoadedProject, merged *spec.UnifiedFile, byID map[int64]*spec.UnifiedFile, seams MaterializeProjectSeams) error {
	// Register THIS project's *spec.UnifiedFile under its walk-assigned id BEFORE recursing into its
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
		var sub spec.UnifiedFile
		if len(d.Directives) > 0 {
			// Decode the RAW reserved-directive mapping (YAML) into a sub spec.UnifiedFile — the EXACT
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
	// 4. Mounted namespaces — each an isolated child spec.UnifiedFile. A REFERENCE mount (cycle-break /
	// diamond) resolves to the SAME *spec.UnifiedFile already registered under its target id (pointer
	// identity preserved); a DEFINITION mount materializes its inline child fresh.
	for i := range lp.Namespaces {
		nm := lp.Namespaces[i]
		if nm == nil {
			continue
		}
		if merged.Namespaces == nil {
			merged.Namespaces = map[string]*spec.UnifiedFile{}
		}
		if nm.Ref {
			shared := byID[nm.RefID]
			if shared == nil {
				return fmt.Errorf("namespace %q: dangling reference to project id %d", nm.Alias, nm.RefID)
			}
			merged.Namespaces[nm.Alias] = shared
			continue
		}
		sub := &spec.UnifiedFile{}
		if err := MaterializeLoadedProject(&nm.Project, sub, byID, seams); err != nil {
			return err
		}
		merged.Namespaces[nm.Alias] = sub
	}
	return nil
}
