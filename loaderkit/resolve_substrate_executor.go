package loaderkit

// resolve_substrate_executor.go — the ONE substrate/resource OpResolve envelope (parser
// consolidation F1.1): every typed Resolve*ViaExecutor wrapper here and in
// resolve_via_executor.go / resolve_kind_entity_via_executor.go, plus the refs seam's
// resolveLocalViaPeer, dispatched the same reverse-channel InvokeProvider(kind:<word>,
// OpResolve) call with a typed request envelope. The generic helper below is their single
// implementation (R3 — the former 7 inline copies); the host twin (charly/substrate_template_
// resolve.go) keeps its own registry-side invoke for the compiled-in path but speaks the SAME
// SubstrateTemplateResolveRequest/Reply wire shape.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
)

// resolveSubstrateViaExecutor dispatches ONE substrate/resource template resolve over the plugin's
// reverse channel: marshals the typed request envelope and InvokeProvider(kind, word, OpResolve)s
// it — the same plugin↔plugin dispatch candy/plugin-build's resolve legs use in production. word
// is the substrate word serving the resolve ("resource", or "local" — the substrate provider
// serves all its words indistinguishably). The RAW reply bytes return so the caller decodes its
// OWN typed reply — the per-kind reply envelope stays the caller's.
func resolveSubstrateViaExecutor(ctx context.Context, ex *sdk.Executor, word string, input any) (json.RawMessage, error) {
	params, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("%s resolve: marshal input: %w", word, err)
	}
	return ex.InvokeProvider(ctx, "kind", word, sdk.OpResolve, params, nil, sdk.InvokeProviderOpts{})
}

// decodeResolveReply decodes a non-empty OpResolve reply into the typed reply envelope — the
// "len(res) > 0 → unmarshal" dance every resolve reply shares. An empty reply leaves into zeroed.
func decodeResolveReply[T any](res json.RawMessage, into *T, what string) error {
	if len(res) == 0 {
		return nil
	}
	if err := json.Unmarshal(res, into); err != nil {
		return fmt.Errorf("%s: decode reply: %w", what, err)
	}
	return nil
}
