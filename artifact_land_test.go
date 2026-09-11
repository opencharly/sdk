package sdk

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
	"google.golang.org/grpc"
)

// fakeExecutorClient is a REAL GetFile over a temp "venue" dir: it embeds the
// generated gRPC client interface (satisfying the full ExecutorServiceClient
// shape) and overrides GetFile to read the requested file straight from the
// host filesystem under the venue dir — the same read the host's executor
// reverse-server performs for a venue file. Wrapped in NewInProcExecutor it
// yields a fully functional *Executor for LandArtifact's pull path without any
// sockets or servers.
type fakeExecutorClient struct {
	proto.ExecutorServiceClient
	venueDir string
}

func (f *fakeExecutorClient) GetFile(ctx context.Context, in *proto.GetFileRequest, opts ...grpc.CallOption) (*proto.GetFileReply, error) {
	data, err := os.ReadFile(filepath.Join(f.venueDir, in.GetPath()))
	if err != nil {
		return &proto.GetFileReply{Error: err.Error()}, nil
	}
	return &proto.GetFileReply{Content: data}, nil
}

// TestLandArtifact_VenuePull proves the venue-side path: bytes are pulled via
// the (fake) executor's GetFile, written to the host artifact path (creating
// parent dirs), and the declared validators then run on the HOST copy.
func TestLandArtifact_VenuePull(t *testing.T) {
	venueDir := t.TempDir()
	venuePath := filepath.Join(venueDir, "capture.png")
	writeMixedPNG(t, venuePath, 800, 600,
		color.RGBA{0, 0, 0, 255}, color.RGBA{255, 0, 0, 255}, 400, 300)

	hostDir := t.TempDir()
	hostPath := filepath.Join(hostDir, "nested", "capture.png") // nested dir must be created

	ex := NewInProcExecutor(&fakeExecutorClient{venueDir: venueDir})
	op := &spec.Op{PluginInput: map[string]any{
		"artifact":             hostPath,
		"artifact_min_bytes":   100,
		"artifact_not_uniform": true,
	}}

	if err := LandArtifact(context.Background(), ex, "capture.png", hostPath, op); err != nil {
		t.Fatalf("venue pull + validation: want nil, got %v", err)
	}

	want, err := os.ReadFile(venuePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("host artifact %q was not written: %v", hostPath, err)
	}
	if string(got) != string(want) {
		t.Fatalf("host artifact bytes differ from venue bytes (got %d bytes, want %d)", len(got), len(want))
	}
}

// TestLandArtifact_HostSideNoPull proves the host-side path: with a nil
// executor OR a blank venue path nothing is pulled and nothing is written —
// validation runs straight on the pre-existing host artifact. A nil executor
// with a non-empty venue path must NOT touch the executor (it would have
// errored), so the "not found" error below is the VALIDATOR's, not a pull's.
func TestLandArtifact_HostSideNoPull(t *testing.T) {
	dir := t.TempDir()
	hostPath := filepath.Join(dir, "shot.png")
	writeMixedPNG(t, hostPath, 640, 480,
		color.RGBA{0, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, 320, 240)

	opOk := &spec.Op{PluginInput: map[string]any{
		"artifact":             hostPath,
		"artifact_min_bytes":   50,
		"artifact_not_uniform": true,
	}}

	t.Run("nil-executor-with-venue-path-validates-existing", func(t *testing.T) {
		// ex == nil AND venuePath != "" → no pull, validate-only. The pre-seeded
		// host artifact passes; had a pull been attempted it would have failed
		// (no executor to pull with).
		if err := LandArtifact(context.Background(), nil, "/venue/shot.png", hostPath, opOk); err != nil {
			t.Fatalf("nil executor: want nil, got %v", err)
		}
	})

	t.Run("nil-executor-missing-artifact-is-validator-error", func(t *testing.T) {
		missing := filepath.Join(dir, "never-written.png")
		opMin := &spec.Op{PluginInput: map[string]any{
			"artifact":           missing,
			"artifact_min_bytes": 10,
		}}
		err := LandArtifact(context.Background(), nil, "/venue/shot.png", missing, opMin)
		if err == nil {
			t.Fatalf("missing host artifact: want validator error, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("missing host artifact: want validator 'not found' error (no pull happened), got %q", err.Error())
		}
	})

	t.Run("executor-with-blank-venue-path-validates-existing", func(t *testing.T) {
		// ex != nil AND venuePath == "" → no pull, validate-only.
		ex := NewInProcExecutor(&fakeExecutorClient{venueDir: t.TempDir()})
		if err := LandArtifact(context.Background(), ex, "", hostPath, opOk); err != nil {
			t.Fatalf("blank venue path: want nil, got %v", err)
		}
	})
}

// TestLandArtifact_ErrorPropagation proves both error legs: a missing VENUE
// file surfaces as a descriptive pull error naming both paths, and a pulled
// artifact that violates a validator surfaces that validator's error AFTER the
// bytes were landed host-side.
func TestLandArtifact_ErrorPropagation(t *testing.T) {
	t.Run("missing-venue-file", func(t *testing.T) {
		venueDir := t.TempDir()
		hostDir := t.TempDir()
		hostPath := filepath.Join(hostDir, "out.png")

		ex := NewInProcExecutor(&fakeExecutorClient{venueDir: venueDir})
		op := &spec.Op{PluginInput: map[string]any{
			"artifact":           hostPath,
			"artifact_min_bytes": 1,
		}}
		err := LandArtifact(context.Background(), ex, "missing.png", hostPath, op)
		if err == nil {
			t.Fatalf("missing venue file: want pull error, got nil")
		}
		msg := err.Error()
		for _, want := range []string{"missing.png", hostPath, "no such file"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("missing venue file: want error mentioning %q, got %q", want, msg)
			}
		}
		if _, statErr := os.Stat(hostPath); !os.IsNotExist(statErr) {
			t.Fatalf("missing venue file: host artifact must not exist after failed pull, stat err = %v", statErr)
		}
	})

	t.Run("pulled-too-small-fails-validator", func(t *testing.T) {
		venueDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(venueDir, "tiny.png"), []byte("tiny"), 0o644); err != nil {
			t.Fatal(err)
		}
		hostDir := t.TempDir()
		hostPath := filepath.Join(hostDir, "tiny.png")

		ex := NewInProcExecutor(&fakeExecutorClient{venueDir: venueDir})
		op := &spec.Op{PluginInput: map[string]any{
			"artifact":           hostPath,
			"artifact_min_bytes": 100, // 4-byte venue file can never pass
		}}
		err := LandArtifact(context.Background(), ex, "tiny.png", hostPath, op)
		if err == nil {
			t.Fatalf("too-small artifact: want validator error, got nil")
		}
		if !strings.Contains(err.Error(), "min_bytes") {
			t.Fatalf("too-small artifact: want min_bytes error, got %q", err.Error())
		}
		// The bytes WERE landed before validation rejected them.
		if info, statErr := os.Stat(hostPath); statErr != nil || info.Size() != 4 {
			t.Fatalf("pulled artifact should be on the host (size 4), stat err = %v", statErr)
		}
	})
}
