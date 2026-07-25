// materialize.go — the K1 unit-1 port of charly's per-node kind-decode DISPATCH POLICY
// (charly/node_normalize.go's former normalizeNodeInto) out of charly core. Materialize decides
// what to do with ONE parsed entity node: if the provider registry resolves its discriminator, the
// host-supplied DecodeEntity seam callback has already folded it (clause M stays host-side,
// unchanged — provider_registry.go / provider_kind_invoke.go are the TRUE mechanism, this package
// never touches the registry); otherwise Materialize applies the NOT-FOUND policy purely from the
// host-threaded spec.Threaded snapshot (clause D) plus two small state-check callbacks — a
// recognized-but-unconnected external deploy substrate routes to the bundle builder, a declared-
// but-unconnected kind is deferred (mid re-entrant connect pass) or warned-and-skipped, and a
// truly unrecognized discriminator is a hard load error. This is the exact fallback branch
// normalizeNodeInto's "not found" arm ran, faithfully ported (the former in-proc KindProvider fast
// path is NOT ported — it was dead code: spec.KindWords is permanently empty, so no real provider
// has ever satisfied ClassKind + KindProvider; the DecodeEntity seam callback always dispatches via
// the JSON-envelope path now, exactly as it already did in production).
package loaderkit

import (
	"fmt"
	"os"

	"github.com/opencharly/sdk/spec"
)

// Materialize folds ONE parsed entity node into acc via the registered Materializer plugin's
// not-found policy, calling back into the host-supplied seams for everything registry-coupled.
func Materialize(pn spec.ParsedNode, t spec.Threaded, seams spec.MaterializeSeams, acc *spec.MaterializedProject) error {
	found, err := seams.DecodeEntity(pn, acc)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	// An external DEPLOY substrate word (e.g. `exampledeploy`) at a deploy's edge is not a KIND —
	// it routes to the bundle builder, the same path the deploy-shape kinds (pod/vm/k8s/local/
	// android/group) take. Recognized via a connected OR pre-scanned deploy provider (t.DeploySubstrates,
	// the Threaded snapshot), so the bed parses before the out-of-process provider connects.
	if t.DeploySubstrates[pn.Disc] {
		return seams.BuildBundleEntity(pn, acc)
	}
	// An external KIND (F4): declared by a project plugin candy whose out-of-process provider has
	// not registered. During the connect pre-pass's nested scan the provider is not connected YET —
	// DEFER (skip, no error) so the nested scan succeeds; the OUTER load decodes it after the
	// connect pass runs. OUTSIDE the pre-pass a still-missing provider means it FAILED to build or
	// connect. GRACEFULLY SKIP the node with a loud warning — never a hard load error — so
	// read-only commands still work in a degraded environment; a command that actually USES this
	// kind resolves the entity by name and fails loudly there (the node is absent).
	if t.Kinds[pn.Disc] {
		if seams.InKindConnectPass() {
			return nil
		}
		if cerr := seams.DeclaredKindConnectError(pn.Disc); cerr != nil {
			fmt.Fprintf(os.Stderr, "Warning: node %q: kind %q provider build/connect failed: %v; skipping the node — any command that uses it will fail loudly at that point\n", pn.Name, pn.Disc, cerr)
			return nil
		}
		fmt.Fprintf(os.Stderr, "Warning: node %q: kind %q is declared by a plugin whose provider did not connect (build/connect failed); skipping the node — any command that uses it will fail loudly at that point\n", pn.Name, pn.Disc)
		return nil
	}
	return fmt.Errorf("node %q: unsupported discriminator %q", pn.Name, pn.Disc)
}
