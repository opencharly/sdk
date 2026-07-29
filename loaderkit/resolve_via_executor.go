package loaderkit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/spec"
)

// resolve_via_executor.go — the plugin-side substrate/resource RESOLVE callbacks the plugin-side loader
// (execLoaderExecutor in plugin-bundle/plugin-build/plugin-vm) threads into loaderkit.ValidateAndroidDevices
// / ValidatePreemptible, so the plugin runs those loader validators ITSELF instead of calling back to the
// host over a HostBuild leg (the former host android/preempt-validate legs, now dissolved). Each is
// InvokeProvider(class:kind, word, OpResolve) over the plugin's reverse channel — the SAME plugin↔plugin
// dispatch candy/plugin-build's build-resolve already uses in production (resolve_legs.go's
// resolveResourceLeg/resolveDistroLeg/resolveInitLeg). ONE home (R3): reused by build-resolve AND the 3
// plugins' loader-validate. The compiled-in host loader (charly.LoadUnified via hostLoaderExecutor) keeps
// its own registry-side callbacks (resolveResource/Android/VmViaPlugin) — this is the PLUGIN-side twin.

// ResolveResourceViaExecutor builds the resolveResource callback ValidatePreemptible needs, dispatching
// each opaque resource body to the resource kind's OpResolve leg over InvokeProvider.
func ResolveResourceViaExecutor(ctx context.Context, ex *sdk.Executor) func(json.RawMessage) (*spec.ResolvedResource, error) {
	return func(body json.RawMessage) (*spec.ResolvedResource, error) {
		params, err := json.Marshal(spec.ResourceResolveInput{Resource: body})
		if err != nil {
			return nil, err
		}
		res, err := ex.InvokeProvider(ctx, "kind", "resource", sdk.OpResolve, params, nil, sdk.InvokeProviderOpts{})
		if err != nil {
			return nil, err
		}
		var reply spec.ResourceResolveReply
		if len(res) > 0 {
			if err := json.Unmarshal(res, &reply); err != nil {
				return nil, fmt.Errorf("resource resolve: decode reply: %w", err)
			}
		}
		return reply.Resolved, nil
	}
}

// ResolveAndroidViaExecutor builds the resolveAndroid callback ValidateAndroidDevices needs, dispatching
// each opaque android body to the substrate plugin's OpResolve leg (any of its 5 substrate words serves;
// "local" is the canonical entry, mirroring the host's invokeSubstrateTemplateResolve).
func ResolveAndroidViaExecutor(ctx context.Context, ex *sdk.Executor) func(json.RawMessage) (*spec.ResolvedAndroid, error) {
	return func(body json.RawMessage) (*spec.ResolvedAndroid, error) {
		params, err := json.Marshal(spec.SubstrateTemplateResolveRequest{Android: &spec.AndroidResolveInput{Android: body}})
		if err != nil {
			return nil, err
		}
		res, err := ex.InvokeProvider(ctx, "kind", "local", sdk.OpResolve, params, nil, sdk.InvokeProviderOpts{})
		if err != nil {
			return nil, err
		}
		var reply spec.AndroidResolveReply
		if len(res) > 0 {
			if err := json.Unmarshal(res, &reply); err != nil {
				return nil, fmt.Errorf("android resolve: decode reply: %w", err)
			}
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
		params, err := json.Marshal(spec.SubstrateTemplateResolveRequest{Vm: &spec.VmResolveInput{Vm: body}})
		if err != nil {
			return nil, err
		}
		res, err := ex.InvokeProvider(ctx, "kind", "local", sdk.OpResolve, params, nil, sdk.InvokeProviderOpts{})
		if err != nil {
			return nil, err
		}
		var reply spec.VmResolveReply
		if len(res) > 0 {
			if err := json.Unmarshal(res, &reply); err != nil {
				return nil, fmt.Errorf("vm resolve: decode reply: %w", err)
			}
		}
		return reply.Resolved, nil
	}
}
