package loaderkit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	"github.com/opencharly/spec/spec"
)

// resolve_kind_entity_via_executor.go — the plugin-side "resolve a named kind:<word> template
// entity" self-load, K-wave W3a A3-phase-2's unblock of the charly/host_build_deploy_entity_resolve.go
// seam's kind:<word> template-lookup branch: NOW that LoadUnifiedViaExecutor (W1/#12) lets a plugin
// load the project itself, the ONE remaining reason this branch was a host seam — a separate-module
// plugin cannot import LoadUnified — no longer holds. resolveKindTemplateBodyViaExecutor mirrors the
// deleted host body's own uf.ProjectTemplates().ByKind(kind)[name] lookup (kind-blind, DATA-indexed);
// the 3 typed Resolve<Kind>EntityViaExecutor wrappers below combine it with the SAME typed
// spec.SubstrateTemplateResolveRequest discriminated-union dispatch resolve_via_executor.go's
// Resolve{Resource,Android,Vm}ViaExecutor already establish (a STRONGER-typed replacement for the
// deleted seam's own untyped map[string]map[string]json.RawMessage wire shape) — no new mechanism,
// same InvokeProvider(kind, "local", OpResolve) peer-dispatch already proven live throughout candy/.
// The wrappers are one-liners over the shared resolveSubstrateViaExecutor/decodeResolveReply
// envelope (resolve_substrate_executor.go — parser consolidation F1.1).

// resolveKindTemplateBodyViaExecutor loads the project (PLUGIN-SIDE, over the reverse channel) and
// returns the opaque kind:<word> template body named name — the raw, unresolved bytes exactly as
// authored in charly.yml. kind is a plain DATA key into ProjectTemplates().ByKind, never a behavior
// branch (mirrors the deleted host seam's own kind-blind lookup).
func resolveKindTemplateBodyViaExecutor(ctx context.Context, ex *sdk.Executor, dir, kind, name string) (json.RawMessage, error) {
	uf, ok, err := LoadUnifiedViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, fmt.Errorf("resolve kind:%s entity %q: %w", kind, name, err)
	}
	if !ok || uf == nil {
		return nil, fmt.Errorf("resolve kind:%s entity %q: no charly.yml or no kind:%s entities declared", kind, name, kind)
	}
	body := uf.ProjectTemplates().ByKind(kind)[name]
	if len(body) == 0 {
		// The ONE canonical reference-resolution surface (R3): the template
		// lookup is namespace-aware (ProjectTemplates().ByKind), and the
		// deploy-hop (a kind:check bed) is resolved by the same resolver the
		// load-time validator uses — a ref that validates must resolve here.
		if !ResolveEntityRef(uf, kind, name) {
			return nil, fmt.Errorf("resolve kind:%s entity %q: not found", kind, name)
		}
		return nil, fmt.Errorf("resolve kind:%s entity %q: found but has no template body (a clone-base bed hop — resolve via the bed's own from:)", kind, name)
	}
	return body, nil
}

// ResolveVmEntityViaExecutor loads the project and resolves the named kind:vm template entity via
// candy/plugin-substrate's OpResolve leg — the plugin-side self-load twin of the deleted
// "deploy-entity-resolve" seam's kind="vm" branch.
func ResolveVmEntityViaExecutor(ctx context.Context, ex *sdk.Executor, dir, name string) (*spec.ResolvedVm, error) {
	body, err := resolveKindTemplateBodyViaExecutor(ctx, ex, dir, "vm", name)
	if err != nil {
		return nil, err
	}
	res, err := resolveSubstrateViaExecutor(ctx, ex, "local", spec.SubstrateTemplateResolveRequest{Vm: &spec.VmResolveInput{Vm: body}})
	if err != nil {
		return nil, err
	}
	var reply spec.VmResolveReply
	if err := decodeResolveReply(res, &reply, fmt.Sprintf("vm entity %q", name)); err != nil {
		return nil, err
	}
	return reply.Resolved, nil
}

// ResolveKubernetesEntityViaExecutor loads the project and resolves the named kind:kubernetes
// template entity via candy/plugin-substrate's OpResolve leg — the plugin-side self-load twin of
// the deleted "deploy-entity-resolve" seam's kind="kubernetes" branch.
func ResolveKubernetesEntityViaExecutor(ctx context.Context, ex *sdk.Executor, dir, name string) (*spec.ResolvedKubernetes, error) {
	body, err := resolveKindTemplateBodyViaExecutor(ctx, ex, dir, "kubernetes", name)
	if err != nil {
		return nil, err
	}
	res, err := resolveSubstrateViaExecutor(ctx, ex, "local", spec.SubstrateTemplateResolveRequest{Kubernetes: &spec.KubernetesResolveInput{Kubernetes: body}})
	if err != nil {
		return nil, err
	}
	var reply spec.KubernetesResolveReply
	if err := decodeResolveReply(res, &reply, fmt.Sprintf("kubernetes entity %q", name)); err != nil {
		return nil, err
	}
	return reply.Resolved, nil
}

// ResolveAndroidEntityViaExecutor loads the project and resolves the named kind:android template
// entity via candy/plugin-substrate's OpResolve leg — the plugin-side self-load twin of the deleted
// "deploy-entity-resolve" seam's kind="android" branch.
func ResolveAndroidEntityViaExecutor(ctx context.Context, ex *sdk.Executor, dir, name string) (*spec.ResolvedAndroid, error) {
	body, err := resolveKindTemplateBodyViaExecutor(ctx, ex, dir, "android", name)
	if err != nil {
		return nil, err
	}
	res, err := resolveSubstrateViaExecutor(ctx, ex, "local", spec.SubstrateTemplateResolveRequest{Android: &spec.AndroidResolveInput{Android: body}})
	if err != nil {
		return nil, err
	}
	var reply spec.AndroidResolveReply
	if err := decodeResolveReply(res, &reply, fmt.Sprintf("android entity %q", name)); err != nil {
		return nil, err
	}
	return reply.Resolved, nil
}
