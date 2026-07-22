package deploykit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/sdk/proto"
	"github.com/opencharly/sdk/spec"
	"google.golang.org/grpc"
)

// fakeExecutorServiceClient is a minimal pb.ExecutorServiceClient test double: only HostBuild is
// implemented (it records the request and returns hostBuildReply/hostBuildErr); every other RPC
// panics if called, since LoadBundleConfigViaSeam touches ONLY HostBuild. Lets a test construct a
// real *sdk.Executor (sdk.NewInProcExecutor) without a live host process — mirrors
// candy/plugin-deploy-vm/lifecycle_test.go's identically-shaped fake (each consuming package names
// its own copy; there is no shared exported test double across modules, the established
// convention for this class of fixture).
type fakeExecutorServiceClient struct {
	pb.ExecutorServiceClient
	gotKind        string
	gotSpecJSON    []byte
	hostBuildReply *pb.HostBuildReply
	hostBuildErr   error
}

func (f *fakeExecutorServiceClient) HostBuild(ctx context.Context, in *pb.HostBuildRequest, opts ...grpc.CallOption) (*pb.HostBuildReply, error) {
	f.gotKind = in.GetKind()
	f.gotSpecJSON = in.GetSpecJson()
	if f.hostBuildErr != nil {
		return nil, f.hostBuildErr
	}
	return f.hostBuildReply, nil
}

// TestLoadBundleConfigViaSeam_ReadsNonEmptyOutOfProcess is the regression test for the
// bed-robustness batch's R3 hoist (charly#176 round-1): the read must go through the
// "pod-config-load-bundle" HostBuild seam and correctly decode a NON-EMPTY BundleConfig, proving
// the fix actually crosses the plugin↔host boundary rather than silently degrading like
// LoadBundleConfig() does outside the charly-core process.
func TestLoadBundleConfigViaSeam_ReadsNonEmptyOutOfProcess(t *testing.T) {
	wantDC := BundleConfig{Bundle: map[string]BundleNode{"demo": {Image: "demo-image"}}}
	dcJSON, err := json.Marshal(wantDC)
	if err != nil {
		t.Fatalf("marshal fixture BundleConfig: %v", err)
	}
	rep := spec.PodConfigLoadBundleReply{ConfigJSON: dcJSON}
	repJSON, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal fixture reply: %v", err)
	}
	fake := &fakeExecutorServiceClient{hostBuildReply: &pb.HostBuildReply{ResultJson: repJSON}}
	ex := sdk.NewInProcExecutor(fake)

	got, err := LoadBundleConfigViaSeam(context.Background(), ex, "test caller")
	if err != nil {
		t.Fatalf("LoadBundleConfigViaSeam() error = %v", err)
	}
	if got == nil {
		t.Fatal("LoadBundleConfigViaSeam() = nil, want the fake's non-empty BundleConfig")
	}
	entry, ok := got.Bundle["demo"]
	if !ok || entry.Image != "demo-image" {
		t.Errorf("LoadBundleConfigViaSeam().Bundle[demo].Image = %+v, want Image=demo-image", entry)
	}

	if fake.gotKind != "pod-config-load-bundle" {
		t.Errorf("HostBuild kind = %q, want %q", fake.gotKind, "pod-config-load-bundle")
	}
	var gotReq spec.PodConfigLoadDeployRequest
	if err := json.Unmarshal(fake.gotSpecJSON, &gotReq); err != nil {
		t.Fatalf("decode recorded HostBuild request: %v", err)
	}
	if gotReq.Caller != "test caller" {
		t.Errorf("PodConfigLoadDeployRequest.Caller = %q, want %q", gotReq.Caller, "test caller")
	}
}

// TestLoadBundleConfigViaSeam_EmptyOverlayReturnsNil covers the documented nil-on-empty contract
// (an absent/empty per-host overlay is not an error).
func TestLoadBundleConfigViaSeam_EmptyOverlayReturnsNil(t *testing.T) {
	rep := spec.PodConfigLoadBundleReply{}
	repJSON, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal fixture reply: %v", err)
	}
	fake := &fakeExecutorServiceClient{hostBuildReply: &pb.HostBuildReply{ResultJson: repJSON}}
	ex := sdk.NewInProcExecutor(fake)

	got, err := LoadBundleConfigViaSeam(context.Background(), ex, "test caller")
	if err != nil {
		t.Fatalf("LoadBundleConfigViaSeam() error = %v", err)
	}
	if got != nil {
		t.Errorf("LoadBundleConfigViaSeam() = %+v, want nil on an empty overlay", got)
	}
}

// TestLoadBundleConfigViaSeam_HostBuildErrorPropagates covers the failure path: a HostBuild
// transport error (e.g. no reverse channel) must surface as an error, never a silent nil.
func TestLoadBundleConfigViaSeam_HostBuildErrorPropagates(t *testing.T) {
	fake := &fakeExecutorServiceClient{hostBuildErr: errors.New("no host reverse channel")}
	ex := sdk.NewInProcExecutor(fake)
	_, err := LoadBundleConfigViaSeam(context.Background(), ex, "test caller")
	if err == nil {
		t.Fatal("LoadBundleConfigViaSeam() with a HostBuild transport error: want an error, got nil")
	}
}

// TestLoadBundleConfigViaSeam_NilExecutorErrors covers the nil-executor guard — a caller with no
// reverse channel (e.g. a command plugin's package-var stash never populated) gets a clean error
// instead of a nil-pointer panic on ex.HostBuild.
func TestLoadBundleConfigViaSeam_NilExecutorErrors(t *testing.T) {
	_, err := LoadBundleConfigViaSeam(context.Background(), nil, "test caller")
	if err == nil {
		t.Fatal("LoadBundleConfigViaSeam(nil executor): want an error, got nil")
	}
}
