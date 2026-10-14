package loaderkit

// resolve_cluster_via_executor_test.go — the `cluster: <profile>` resolver's arms, moved here
// from candy/plugin-kube (kube_verb_cluster_resolve_test.go) and candy/plugin-kubevirt when R3
// hoisted the mechanism into its home (opencharly/sdk#338). The loud-failure contract is the point:
// a FAILED resolve must never be read as "no cluster profile" (opencharly/plugin-kube#15).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
	"google.golang.org/grpc"
)

// stubClusterExecutorClient is a minimal pb.ExecutorServiceClient double: it EMBEDS the generated
// client interface (nil) so only HostBuild needs a body — the pattern
// loaderkit/resolve_retention_defaults_test.go established, kept here so the SDK never needs a
// package-var seam for this mechanism (R3).
type stubClusterExecutorClient struct {
	pb.ExecutorServiceClient
	projectDir string
	connectErr error
	raw        []byte
}

func (s stubClusterExecutorClient) HostBuild(_ context.Context, in *pb.HostBuildRequest, _ ...grpc.CallOption) (*pb.HostBuildReply, error) {
	if in.GetKind() != "deploy-plugins-connect" {
		panic("stubClusterExecutorClient: unexpected host-build kind " + in.GetKind())
	}
	if s.connectErr != nil {
		return nil, s.connectErr
	}
	if s.raw != nil {
		return &pb.HostBuildReply{ResultJson: s.raw}, nil
	}
	b, err := json.Marshal(spec.DeployPluginsConnectReply{Dir: s.projectDir})
	if err != nil {
		return nil, err
	}
	return &pb.HostBuildReply{ResultJson: b}, nil
}

func TestProjectDirViaExecutorResolvesTheSeam(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	dir, err := ProjectDirViaExecutor(context.Background(), ex, "")
	if err != nil {
		t.Fatalf("ProjectDirViaExecutor: %v", err)
	}
	if dir != "/proj" {
		t.Fatalf("dir = %q, want /proj", dir)
	}
}

func TestProjectDirViaExecutorNilExecutorIsLoud(t *testing.T) {
	if _, err := ProjectDirViaExecutor(context.Background(), nil, ""); err == nil {
		t.Fatal("want an error for a nil executor, got nil")
	} else if !strings.Contains(err.Error(), "deploy-plugins-connect") {
		t.Fatalf("the failure does not name the seam: %v", err)
	}
}

func TestProjectDirViaExecutorConnectFailureIsLoud(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{connectErr: errors.New("host seam down")})
	if _, err := ProjectDirViaExecutor(context.Background(), ex, ""); err == nil {
		t.Fatal("want an error when the seam fails, got nil (the silent-swallow class)")
	}
}

func TestProjectDirViaExecutorUndecodableReplyIsLoud(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{raw: []byte("not json")})
	if _, err := ProjectDirViaExecutor(context.Background(), ex, ""); err == nil {
		t.Fatal("want an error for an undecodable reply, got nil")
	}
}

func TestResolveClusterContextNilExecutorErrorsLoudly(t *testing.T) {
	kctx, err := ResolveClusterContextViaExecutor(context.Background(), nil, "check-k3s-vm-ctx")
	if err == nil {
		t.Fatal("want an error for a nil executor, got nil")
	}
	if !strings.Contains(err.Error(), "check-k3s-vm-ctx") {
		t.Fatalf("error must name the cluster, got: %v", err)
	}
	if kctx != "" {
		t.Fatalf("want an empty context on failure, got %q", kctx)
	}
}

func TestResolveClusterContextProjectDirResolveFailsErrorsLoudly(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{connectErr: errors.New("host seam down")})
	kctx, err := ResolveClusterContextViaExecutor(context.Background(), ex, "check-k3s-vm-ctx")
	if err == nil {
		t.Fatal("want an error when the project-dir resolve fails, got nil (the silent-swallow class)")
	}
	if !strings.Contains(err.Error(), "check-k3s-vm-ctx") {
		t.Fatalf("error must name the cluster, got: %v", err)
	}
	if kctx != "" {
		t.Fatalf("want an empty context on failure, got %q", kctx)
	}
}

func TestResolveClusterContextEntityResolveFailsErrorsLoudly(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	resolve := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return nil, errors.New("resolve boom")
	}
	if _, err := resolveClusterContext(context.Background(), ex, "check-k3s-vm-ctx", resolve); err == nil {
		t.Fatal("want an error when the entity resolve fails, got nil")
	} else if !strings.Contains(err.Error(), "check-k3s-vm-ctx") {
		t.Fatalf("error must name the cluster, got: %v", err)
	}
}

// TestResolveClusterContextResolvedButEmptyContextFallsBack pins the legitimate-miss arm: a
// resolved-but-empty context stays ("", nil) so the caller keeps its own current-context fallback.
// Turning it into a hard failure would break every bed that names a cluster without pinning a
// context.
func TestResolveClusterContextResolvedButEmptyContextFallsBack(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	resolve := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return &spec.ResolvedKubernetes{}, nil
	}
	kctx, err := resolveClusterContext(context.Background(), ex, "check-k3s-vm-ctx", resolve)
	if err != nil {
		t.Fatalf("a resolved-but-empty context must fall back, got error: %v", err)
	}
	if kctx != "" {
		t.Fatalf("want an empty context for the fallback, got %q", kctx)
	}
	// The nil-view arm is the same contract: no entity at all is a miss, not a failure.
	resolveNil := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return nil, nil
	}
	if kctx, err := resolveClusterContext(context.Background(), ex, "check-k3s-vm-ctx", resolveNil); err != nil || kctx != "" {
		t.Fatalf("nil view -> (%q, %v), want (\"\", nil)", kctx, err)
	}
}

// TestResolveClusterContextPassesTheProjectDirAndReturnsTheContext proves the two legs are wired
// together: the resolved project directory reaches the entity resolve, and the entity's
// KubeconfigContext is what comes back.
func TestResolveClusterContextPassesTheProjectDirAndReturnsTheContext(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	var gotDir, gotName string
	resolve := func(_ context.Context, _ *sdk.Executor, dir, name string) (*spec.ResolvedKubernetes, error) {
		gotDir, gotName = dir, name
		return &spec.ResolvedKubernetes{KubeconfigContext: "k3s-check"}, nil
	}
	kctx, err := resolveClusterContext(context.Background(), ex, "check-k3s-vm-ctx", resolve)
	if err != nil {
		t.Fatalf("resolveClusterContext: %v", err)
	}
	if gotDir != "/proj" || gotName != "check-k3s-vm-ctx" {
		t.Fatalf("resolve saw (dir=%q, name=%q), want (/proj, check-k3s-vm-ctx)", gotDir, gotName)
	}
	if kctx != "k3s-check" {
		t.Fatalf("kctx = %q, want k3s-check", kctx)
	}
}

// TestResolveClusterCarriesTheKubeconfigFileBesideTheContext pins the reason the full view
// exists. A cluster template addresses a per-cluster kubeconfig FILE; a consumer that only ever
// sees the context NAME is structurally unable to honour it, so the file must ride through the
// same resolve, unchanged and un-truncated.
func TestResolveClusterCarriesTheKubeconfigFileBesideTheContext(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	resolve := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return &spec.ResolvedKubernetes{
			Kubeconfig:        "/home/op/.kube/quant-dev.conf",
			KubeconfigContext: "kubernetes-admin@quant-dev",
		}, nil
	}
	view, err := resolveCluster(context.Background(), ex, "quant-dev", resolve)
	if err != nil {
		t.Fatalf("resolveCluster: %v", err)
	}
	if view == nil {
		t.Fatal("a resolved cluster must not come back nil")
	}
	if view.Kubeconfig != "/home/op/.kube/quant-dev.conf" {
		t.Fatalf("kubeconfig = %q, want the template's file", view.Kubeconfig)
	}
	if view.KubeconfigContext != "kubernetes-admin@quant-dev" {
		t.Fatalf("context = %q, want the template's context", view.KubeconfigContext)
	}
}

// TestResolveClusterNilViewIsAMissNotAFailure pins the miss semantics: a project that declares no
// such entity yields (nil, nil), which the verbs read as "no cluster profile named".
func TestResolveClusterNilViewIsAMissNotAFailure(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	resolve := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return nil, nil
	}
	view, err := resolveCluster(context.Background(), ex, "nope", resolve)
	if err != nil {
		t.Fatalf("a miss must not be a failure, got: %v", err)
	}
	if view != nil {
		t.Fatalf("want a nil view for a miss, got %+v", view)
	}
}

// TestResolveClusterFailuresAreLoud pins the asymmetry the context-only wrapper also relies on:
// every failure names the cluster and yields a nil view, so no caller can mistake it for a miss.
func TestResolveClusterFailuresAreLoud(t *testing.T) {
	if view, err := resolveCluster(context.Background(), nil, "quant-dev", nil); err == nil || view != nil {
		t.Fatalf("nil executor -> (%+v, %v), want (nil, loud error)", view, err)
	} else if !strings.Contains(err.Error(), "quant-dev") {
		t.Fatalf("error must name the cluster, got: %v", err)
	}

	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{connectErr: errors.New("host seam down")})
	if view, err := resolveCluster(context.Background(), ex, "quant-dev", nil); err == nil || view != nil {
		t.Fatalf("failing project-dir seam -> (%+v, %v), want (nil, loud error)", view, err)
	}

	ex = sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	boom := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return nil, errors.New("entity resolve boom")
	}
	if view, err := resolveCluster(context.Background(), ex, "quant-dev", boom); err == nil || view != nil {
		t.Fatalf("failing entity resolve -> (%+v, %v), want (nil, loud error)", view, err)
	} else if !strings.Contains(err.Error(), "quant-dev") || !strings.Contains(err.Error(), "entity resolve boom") {
		t.Fatalf("error must name the cluster and carry the cause, got: %v", err)
	}
}

// TestResolveClusterContextViewAgreesWithTheFullView proves the two views are ONE resolution: the
// context-only wrapper reports exactly what the full view carries, so no caller's behaviour can
// differ based on which of the two it was written against.
func TestResolveClusterContextViewAgreesWithTheFullView(t *testing.T) {
	ex := sdk.NewInProcExecutor(stubClusterExecutorClient{projectDir: "/proj"})
	resolve := func(context.Context, *sdk.Executor, string, string) (*spec.ResolvedKubernetes, error) {
		return &spec.ResolvedKubernetes{
			Kubeconfig:        "/etc/kubernetes/admin.conf",
			KubeconfigContext: "kubernetes-admin@quant-dev",
		}, nil
	}
	kctx, err := resolveClusterContext(context.Background(), ex, "quant-dev", resolve)
	if err != nil {
		t.Fatalf("resolveClusterContext: %v", err)
	}
	view, err := resolveCluster(context.Background(), ex, "quant-dev", resolve)
	if err != nil {
		t.Fatalf("resolveCluster: %v", err)
	}
	if kctx != view.KubeconfigContext {
		t.Fatalf("context-only view %q != full view's %q", kctx, view.KubeconfigContext)
	}
}
