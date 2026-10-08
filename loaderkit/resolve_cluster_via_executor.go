package loaderkit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	"github.com/opencharly/spec/spec"
)

// resolve_cluster_via_executor.go — the ONE plugin-side resolver for a `cluster: <profile>`.
//
// Two plugins carried their own copy of this mechanism: `hostProjectDir` existed
// byte-identical in candy/plugin-kube's preresolve.go and candy/plugin-kubevirt's cluster.go
// (four call sites), and each then wrapped it in its own five-step `cluster:` resolver
// (`resolveKubeVerbCluster`, `resolveClusterContext`). R3 hoists ONE declaration here and the
// plugins call it, deleting their copies (opencharly/sdk#338). THIS DOCSTRING IS THE SOURCE OF
// TRUTH for the mechanism — a caller's comment should point here, never restate it.

// ProjectDirViaExecutor resolves the project directory over the "deploy-plugins-connect" host
// seam — the same preamble `command:deploy`'s own resolveTreeViaLoader runs host-side: it returns
// the host's os.Getwd() and connects the deployment's plugins. A plugin-side leg with no
// dispatch-threaded directory of its own (a post-provision handler, a command leaf) uses it to
// feed the self-load helpers (LoadUnifiedViaExecutor, ResolveMergedTreeViaExecutor,
// Resolve{Kubernetes,Vm,Android}EntityViaExecutor). deployName names the deployment whose plugins
// the host connects; "" asks for the CURRENT project.
//
// Every failure is a real error — a missing host reverse channel, a failed seam call, or an
// undecodable reply — because an empty directory silently resolved here would make every caller
// load the wrong project.
func ProjectDirViaExecutor(ctx context.Context, ex *sdk.Executor, deployName string) (string, error) {
	if ex == nil {
		return "", fmt.Errorf("deploy-plugins-connect: no host reverse channel (an out-of-process plugin needs one; a host-resident caller has the seam in-proc)")
	}
	reqJSON, err := json.Marshal(spec.DeployPluginsConnectRequest{Path: deployName})
	if err != nil {
		return "", fmt.Errorf("deploy-plugins-connect: encode request: %w", err)
	}
	resJSON, err := ex.HostBuild(ctx, "deploy-plugins-connect", reqJSON)
	if err != nil {
		return "", fmt.Errorf("deploy-plugins-connect: %w", err)
	}
	var reply spec.DeployPluginsConnectReply
	if err := json.Unmarshal(resJSON, &reply); err != nil {
		return "", fmt.Errorf("deploy-plugins-connect: decode reply: %w", err)
	}
	return reply.Dir, nil
}

// resolveClusterContext is ResolveClusterContextViaExecutor's body, with the kind:kubernetes
// resolve leg as a parameter so every arm is testable without a live host (the seam a plugin's
// own package-var could only reach from its side). Production callers go through the exported
// wrapper, which passes the real helper.
func resolveClusterContext(
	ctx context.Context,
	ex *sdk.Executor,
	cluster string,
	resolveKubernetes func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error),
) (string, error) {
	if ex == nil {
		return "", fmt.Errorf("resolving cluster %q: no executor available", cluster)
	}
	dir, err := ProjectDirViaExecutor(ctx, ex, "")
	if err != nil {
		return "", fmt.Errorf("resolving the project dir to resolve cluster %q: %w", cluster, err)
	}
	view, err := resolveKubernetes(ctx, ex, dir, cluster)
	if err != nil {
		return "", fmt.Errorf("resolving cluster %q: %w", cluster, err)
	}
	if view == nil {
		return "", nil
	}
	return view.KubeconfigContext, nil
}

// ResolveClusterContextViaExecutor resolves a step's `cluster: <profile>` to the concrete
// kubeconfig context of the named kind:kubernetes entity, self-loading the project plugin-side
// over the reverse channel (no host round-trip beyond the two seams above).
//
// It returns ("", nil) for a legitimate MISS — the project declares no such entity — which the
// kube/kubevirt verbs read as "no cluster profile named", leaving the kubeconfig's own
// current-context in force. Every FAILURE is loud and names the cluster: a nil executor, a failing
// project-dir seam, or a failing entity resolve. That asymmetry is deliberate and load-bearing: a
// resolve FAILURE must never be read as "no cluster profile" and silently run against whatever
// current-context the kubeconfig happens to hold (the silent-degradation defect
// opencharly/plugin-kube#15 fixed, and the reason its tests must survive the move here).
func ResolveClusterContextViaExecutor(ctx context.Context, ex *sdk.Executor, cluster string) (string, error) {
	return resolveClusterContext(ctx, ex, cluster, ResolveKubernetesEntityViaExecutor)
}
