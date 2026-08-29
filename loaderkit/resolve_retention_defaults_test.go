package loaderkit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
	"google.golang.org/grpc"

	"github.com/opencharly/spec/spec"
)

// stubRetentionExecutorClient is a minimal ExecutorServiceClient stub that answers
// HostBuild("retention-defaults") with a canned RetentionReply — proving the seam
// round-trips the request and returns the project's defaults WITHOUT walking the
// project or resolving its @github refs (issue #423: the clean hang).
type stubRetentionExecutorClient struct {
	pb.ExecutorServiceClient
	reply spec.RetentionReply
}

func (s stubRetentionExecutorClient) HostBuild(_ context.Context, in *pb.HostBuildRequest, _ ...grpc.CallOption) (*pb.HostBuildReply, error) {
	if in.GetKind() != "retention-defaults" {
		return nil, fmt.Errorf("stubRetentionExecutorClient: unexpected HostBuild(%q)", in.GetKind())
	}
	b, err := json.Marshal(s.reply)
	if err != nil {
		return nil, err
	}
	return &pb.HostBuildReply{ResultJson: b}, nil
}

// TestResolveRetentionDefaultsViaSeam proves the seam reads the project's retention
// defaults through the lightweight HostBuild seam: the request carries the project
// dir, and the reply's keep_images / keep_check_runs come back verbatim. A nil
// executor (a placement without a reverse channel) degrades to 0/0.
func TestResolveRetentionDefaultsViaSeam(t *testing.T) {
	ctx := context.Background()

	// The seam must round-trip the defaults.
	ex := sdk.NewInProcExecutor(stubRetentionExecutorClient{
		reply: spec.RetentionReply{KeepImages: 3, KeepCheckRuns: 5},
	})
	images, runs := ResolveRetentionDefaultsViaSeam(ctx, ex, "/tmp/proj")
	if images != 3 || runs != 5 {
		t.Fatalf("ResolveRetentionDefaultsViaSeam = (%d, %d), want (3, 5)", images, runs)
	}

	// A nil executor degrades to 0/0 (retention disabled) — the deleted seam's
	// best-effort contract.
	if images, runs := ResolveRetentionDefaultsViaSeam(ctx, nil, "/tmp/proj"); images != 0 || runs != 0 {
		t.Fatalf("ResolveRetentionDefaultsViaSeam(nil) = (%d, %d), want (0, 0)", images, runs)
	}
}
