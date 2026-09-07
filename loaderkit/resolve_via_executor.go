package loaderkit

import (
	"context"
	"encoding/json"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// resolve_via_executor.go — the plugin-side substrate/resource RESOLVE callbacks the plugin-side loader
// (execLoaderExecutor in plugin-fleet/plugin-build/plugin-vm) threads into loaderkit.ValidateAndroidDevices
// / ValidatePreemptible, so the plugin runs those loader validators ITSELF instead of calling back to the
// host over a HostBuild leg (the former host android/preempt-validate legs, now dissolved). Each is
// InvokeProvider(class:kind, word, OpResolve) over the plugin's reverse channel — the SAME plugin↔plugin
// dispatch candy/plugin-build's build-resolve already uses in production (resolve_legs.go's
// resolveResourceLeg/resolveDistroLeg/resolveInitLeg). ONE home (R3): reused by build-resolve AND the 3
// plugins' loader-validate. The compiled-in host loader (charly.LoadUnified via hostLoaderExecutor) keeps
// its own registry-side callbacks (resolveResource/Android/VmViaPlugin) — this is the PLUGIN-side twin.
//
// The wrappers are one-liners over the shared resolveSubstrateViaExecutor/decodeResolveReply envelope
// (resolve_substrate_executor.go — parser consolidation F1.1): the same typed request/reply shape
// resolve_kind_entity_via_executor.go's Resolve*EntityViaExecutor and refs_seams_executor.go's
// resolveLocalViaPeer dispatch through.

// ResolveResourceViaExecutor builds the resolveResource callback ValidatePreemptible needs, dispatching
// each opaque resource body to the resource kind's OpResolve leg over InvokeProvider.
func ResolveResourceViaExecutor(ctx context.Context, ex *sdk.Executor) func(json.RawMessage) (*spec.ResolvedResource, error) {
	return func(body json.RawMessage) (*spec.ResolvedResource, error) {
		res, err := resolveSubstrateViaExecutor(ctx, ex, "resource", spec.ResourceResolveInput{Resource: body})
		if err != nil {
			return nil, err
		}
		var reply spec.ResourceResolveReply
		if err := decodeResolveReply(res, &reply, "resource resolve"); err != nil {
			return nil, err
		}
		return reply.Resolved, nil
	}
}

// ResolveAndroidViaExecutor builds the resolveAndroid callback ValidateAndroidDevices needs, dispatching
// each opaque android body to the substrate plugin's OpResolve leg (any of its 5 substrate words serves;
// "local" is the canonical entry, mirroring the host's invokeSubstrateTemplateResolve).
func ResolveAndroidViaExecutor(ctx context.Context, ex *sdk.Executor) func(json.RawMessage) (*spec.ResolvedAndroid, error) {
	return func(body json.RawMessage) (*spec.ResolvedAndroid, error) {
		res, err := resolveSubstrateViaExecutor(ctx, ex, "local", spec.SubstrateTemplateResolveRequest{Android: &spec.AndroidResolveInput{Android: body}})
		if err != nil {
			return nil, err
		}
		var reply spec.AndroidResolveReply
		if err := decodeResolveReply(res, &reply, "android resolve"); err != nil {
			return nil, err
		}
		return reply.Resolved, nil
	}
}

// ResolveVmViaExecutor builds the resolveVm callback ValidatePreemptible needs. Returns nil for an
// empty/absent body (matching the host resolveVmViaPlugin).
func ResolveVmViaExecutor(ctx context.Context, ex *sdk.Executor) func(json.RawMessage) (*vmshared.VmSpec, error) {
	return func(body json.RawMessage) (*vmshared.VmSpec, error) {
		if len(body) == 0 {
			return nil, nil
		}
		res, err := resolveSubstrateViaExecutor(ctx, ex, "local", spec.SubstrateTemplateResolveRequest{Vm: &spec.VmResolveInput{Vm: body}})
		if err != nil {
			return nil, err
		}
		var reply spec.VmResolveReply
		if err := decodeResolveReply(res, &reply, "vm resolve"); err != nil {
			return nil, err
		}
		return reply.Resolved, nil
	}
}
