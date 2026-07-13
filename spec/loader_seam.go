package spec

import "gopkg.in/yaml.v3"

// loader_seam.go — the hand-written CONTRACT types for the unified-config loader seam (K1). These
// are interface + data contracts (no mechanism): the parse machinery lives in sdk/loaderkit, the
// materialize in charly core. They live in spec (the shared contract home, alongside the
// CUE-generated #ParsedProject / #LoadedProject wire types) so BOTH the loader plugin
// (candy/plugin-loader → loaderkit) and the host may reference them without either importing the
// other — keeping charly core's only loaderkit import the single Walk driver file.

// Threaded is the host-computed, registry-derived DATA the per-document parse consults instead of
// querying the provider registry (boundary law clause D): which words are recognized kinds /
// deploy substrates, which external kinds may nest sub-entity members, and each plugin verb's
// scalar-sugar primary field. The host snapshots it before the parse; the parse never touches the
// registry.
type Threaded struct {
	Kinds            map[string]bool   // recognizedKind
	DeploySubstrates map[string]bool   // recognizedDeploySubstrate
	StructuralKinds  map[string]bool   // externalKindMayNestMembers
	Primaries        map[string]string // pluginPrimaryFor: verb word → scalar-sugar primary field
}

// DocParser is the swappable per-document PARSE seam: the loader plugin candy implements it
// (candy/plugin-loader, delegating to loaderkit.ParseDoc), and the host resolves the registered
// loader provider to it and calls it for every config document — so an alternative loader plugin
// serves a different config front-end by implementing this. Typed (no wire envelope) since it runs
// on every document load. `directives` is the reserved-directive mapping (import/discover/version/
// repo/defaults/provides); `pp` is the decomposed entity nodes.
type DocParser interface {
	ParseDoc(doc *yaml.Node, t Threaded) (directives map[string]*yaml.Node, pp ParsedProject, err error)
}
