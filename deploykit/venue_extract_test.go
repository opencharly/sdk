package deploykit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// venue_extract_test.go — direct unit coverage for RunVenueExtractStep (the machine-venue
// materialization of a candy's `extract:` entries). The podman create/cp/rm leg is driven
// through the runEngineCmd package-level seam and the tar leg through tarCreate, so the FULL
// flow — stat → tar → PutFile → extract script → cleanup — is exercised without spawning a
// real engine. The CompileExtractSteps lowering is covered in plan_compile_test.go.
//
// The recorder materializes the copied path into the staging dir on the cp leg (a file or a
// dir, per the COPY semantics under test), so the subsequent tar has real content to pack.

// engineRecorder is a runEngineCmd substitute: it records every engine command and, for the cp
// leg, materializes the copied path (a file or a dir) into the staging dir. The cp args are
// [cp, <container>:<path>, <stageHost>/].
type engineRecorder struct {
	calls []string
	dir   bool // materialize the copied path as a directory (COPY dir semantics)
}

func (r *engineRecorder) run(_ context.Context, _ string, args ...string) error {
	r.calls = append(r.calls, strings.Join(args, " "))
	if len(args) >= 3 && args[0] == "cp" {
		stageHost := strings.TrimSuffix(args[len(args)-1], "/")
		path := strings.SplitN(args[1], ":", 2)[1]
		copied := filepath.Join(stageHost, filepath.Base(path))
		if r.dir {
			if err := os.MkdirAll(copied, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(copied, "marker"), []byte("x"), 0o644)
		}
		return os.WriteFile(copied, []byte("binary"), 0o755)
	}
	return nil
}

// tarRecorder is a tarCreate substitute: it records the tar args (the tarball root name is the
// last element — the file-vs-dir + trailing-slash decisions land there) and writes a valid
// empty tarball so the flow proceeds.
type tarRecorder struct {
	args [][]string
}

func (r *tarRecorder) run(_ context.Context, args ...string) error {
	r.args = append(r.args, args)
	// The tarball path is the element after -czf; write a valid (empty) gzip tarball so the
	// subsequent PutFile has real content to ship.
	for i, a := range args {
		if a == "-czf" && i+1 < len(args) {
			return writeEmptyTarGz(args[i+1])
		}
	}
	return nil
}

// writeEmptyTarGz writes a valid gzip-compressed empty tar archive (the smallest possible
// tarball) — enough for PutFile to ship without the flow caring about its contents.
func writeEmptyTarGz(path string) error {
	// A gzip stream wrapping an empty tar: 20-byte gzip header + 1024-byte zero tar EOF block.
	// gzip magic + method + flags + mtime + xfl + os.
	header := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}
	body := make([]byte, 1024)
	// gzip CRC32 of the empty tar (0) + ISIZE (0).
	trailer := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	out := make([]byte, 0, len(header)+len(body)+len(trailer))
	out = append(out, header...)
	out = append(out, body...)
	out = append(out, trailer...)
	return os.WriteFile(path, out, 0o644)
}

// noopImageSeams is reused from venue_builder_test.go (resolveImage/ensureImage closures that
// never reach the image-resolution branch).

// TestRunVenueExtractStep_Validation proves Source/Path/Dest are all required — a missing one
// is a hard error, never a silent partial extract.
func TestRunVenueExtractStep_Validation(t *testing.T) {
	resolveImage, ensureImage := noopImageSeams()
	for _, s := range []*ExtractStep{
		{Source: "", Path: "/x", Dest: "/y", CandyName: "c"},
		{Source: "img", Path: "", Dest: "/y", CandyName: "c"},
		{Source: "img", Path: "/x", Dest: "", CandyName: "c"},
	} {
		if err := RunVenueExtractStep(context.Background(), &localPkgRecExec{}, resolveImage, ensureImage, s, EmitOpts{}); err == nil {
			t.Errorf("RunVenueExtractStep(%+v): expected a validation error, got nil", s)
		}
	}
}

// TestRunVenueExtractStep_DryRunProceedsWithoutEngine proves the dry-run path returns before any
// engine command or venue write — the recorder errors loudly if runEngineCmd is ever called.
func TestRunVenueExtractStep_DryRunProceedsWithoutEngine(t *testing.T) {
	rec := &engineRecorder{}
	orig := runEngineCmd
	runEngineCmd = rec.run
	defer func() { runEngineCmd = orig }()

	s := &ExtractStep{Source: "img", Path: "/x", Dest: "/y", CandyName: "c"}
	resolveImage, ensureImage := noopImageSeams()
	if err := RunVenueExtractStep(context.Background(), &localPkgRecExec{}, resolveImage, ensureImage, s, EmitOpts{DryRun: true}); err != nil {
		t.Fatalf("RunVenueExtractStep dry-run: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("dry-run must not drive any engine command, got %#v", rec.calls)
	}
}

// TestRunVenueExtractStep_DirPathCopiesContentsIntoDest proves the COPY dir semantics: a
// directory Path extracts its CONTENTS into Dest (Dest is a directory). The tarball packs the
// dir's contents (tar -C <copied> .), the extract script mkdirs Dest and untars into it, and
// the venue tarball is cleaned up as root afterwards.
func TestRunVenueExtractStep_DirPathCopiesContentsIntoDest(t *testing.T) {
	rec := &engineRecorder{dir: true}
	orig := runEngineCmd
	runEngineCmd = rec.run
	defer func() { runEngineCmd = orig }()
	tarRec := &tarRecorder{}
	origTar := tarCreate
	tarCreate = tarRec.run
	defer func() { tarCreate = origTar }()

	exec := &localPkgRecExec{}
	s := &ExtractStep{
		Source:    "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1",
		Path:      "/etc/istio",
		Dest:      "/etc/istio",
		CandyName: "agentteams-higress",
	}
	resolveImage, ensureImage := noopImageSeams()
	if err := RunVenueExtractStep(context.Background(), exec, resolveImage, ensureImage, s, EmitOpts{}); err != nil {
		t.Fatalf("RunVenueExtractStep: %v", err)
	}

	// create + cp + rm must all have been driven through the seam, in order.
	if len(rec.calls) != 3 {
		t.Fatalf("engine calls = %d, want 3 (create/cp/rm); got %#v", len(rec.calls), rec.calls)
	}
	if !strings.HasPrefix(rec.calls[0], "create ") || !strings.HasPrefix(rec.calls[2], "rm ") {
		t.Errorf("engine calls = %#v, want create first and rm last", rec.calls)
	}

	// One tarball PutFile + one extract script + one cleanup script.
	if len(exec.putDests) != 1 {
		t.Fatalf("PutFile calls = %d, want 1 (the transfer tarball); got %#v", len(exec.putDests), exec.putDests)
	}
	if !strings.HasPrefix(exec.putDests[0], "/tmp/charly-extract-agentteams-higress-") || !strings.HasSuffix(exec.putDests[0], ".tar.gz") {
		t.Errorf("PutFile dest = %q, want /tmp/charly-extract-agentteams-higress-<scope>.tar.gz", exec.putDests[0])
	}
	if len(exec.systemScripts) != 2 {
		t.Fatalf("RunSystem calls = %d, want 2 (extract + cleanup); got %#v", len(exec.systemScripts), exec.systemScripts)
	}
	if !strings.Contains(exec.systemScripts[0], "mkdir -p '/etc/istio'") || !strings.Contains(exec.systemScripts[0], "tar -C '/etc/istio' -xzf") {
		t.Errorf("extract script = %q, want mkdir -p '/etc/istio' + tar -C '/etc/istio' -xzf", exec.systemScripts[0])
	}
	if !strings.Contains(exec.systemScripts[1], "rm -f") {
		t.Errorf("cleanup script = %q, want rm -f of the venue tarball", exec.systemScripts[1])
	}

	// The dir tarball packs the CONTENTS (tar -C <copied> .), not the dir itself.
	if len(tarRec.args) != 1 {
		t.Fatalf("tar calls = %d, want 1; got %#v", len(tarRec.args), tarRec.args)
	}
	if got := strings.Join(tarRec.args[0], " "); !strings.Contains(got, "-C ") || !strings.HasSuffix(got, " .") {
		t.Errorf("dir tar args = %q, want -C <copied> -czf <tarball> . (contents, not the dir)", got)
	}
}

// TestRunVenueExtractStep_FilePathLandsAtDest proves the COPY file semantics: a file Path lands
// at Dest (Dest is the file path) — the tarball root is basename(Dest) and the extract untars
// into Dir(Dest).
func TestRunVenueExtractStep_FilePathLandsAtDest(t *testing.T) {
	rec := &engineRecorder{}
	orig := runEngineCmd
	runEngineCmd = rec.run
	defer func() { runEngineCmd = orig }()
	tarRec := &tarRecorder{}
	origTar := tarCreate
	tarCreate = tarRec.run
	defer func() { tarCreate = origTar }()

	exec := &localPkgRecExec{}
	s := &ExtractStep{
		Source:    "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1",
		Path:      "/usr/local/bin/envoy",
		Dest:      "/usr/local/bin/envoy",
		CandyName: "agentteams-higress",
	}
	resolveImage, ensureImage := noopImageSeams()
	if err := RunVenueExtractStep(context.Background(), exec, resolveImage, ensureImage, s, EmitOpts{}); err != nil {
		t.Fatalf("RunVenueExtractStep: %v", err)
	}

	if len(exec.systemScripts) != 2 {
		t.Fatalf("RunSystem calls = %d, want 2; got %#v", len(exec.systemScripts), exec.systemScripts)
	}
	if !strings.Contains(exec.systemScripts[0], "mkdir -p '/usr/local/bin'") || !strings.Contains(exec.systemScripts[0], "tar -C '/usr/local/bin' -xzf") {
		t.Errorf("extract script = %q, want mkdir -p '/usr/local/bin' + tar -C '/usr/local/bin' -xzf", exec.systemScripts[0])
	}

	// The file tarball root must be basename(Dest) — the last tar arg.
	if len(tarRec.args) != 1 {
		t.Fatalf("tar calls = %d, want 1; got %#v", len(tarRec.args), tarRec.args)
	}
	args := tarRec.args[0]
	if len(args) < 2 || args[len(args)-1] != "envoy" {
		t.Errorf("file tar args = %q, want the tarball root to be envoy (basename of Dest)", strings.Join(args, " "))
	}
}

// TestRunVenueExtractStep_TrailingSlashDestKeepsSourceBasename proves the COPY trailing-slash
// semantics: a Dest ending in "/" means "into this directory, keeping the source basename".
// This is the regression guard for the filepath.Base trailing-slash trap — Base("/usr/local/bin/")
// is "bin", so without the explicit HasSuffix check the tarball root would be "bin" and the
// file would land at /usr/local/bin/bin instead of /usr/local/bin/envoy.
func TestRunVenueExtractStep_TrailingSlashDestKeepsSourceBasename(t *testing.T) {
	rec := &engineRecorder{}
	orig := runEngineCmd
	runEngineCmd = rec.run
	defer func() { runEngineCmd = orig }()
	tarRec := &tarRecorder{}
	origTar := tarCreate
	tarCreate = tarRec.run
	defer func() { tarCreate = origTar }()

	exec := &localPkgRecExec{}
	s := &ExtractStep{
		Source:    "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1",
		Path:      "/usr/local/bin/envoy",
		Dest:      "/usr/local/bin/", // trailing slash → "into this directory", keep basename
		CandyName: "agentteams-higress",
	}
	resolveImage, ensureImage := noopImageSeams()
	if err := RunVenueExtractStep(context.Background(), exec, resolveImage, ensureImage, s, EmitOpts{}); err != nil {
		t.Fatalf("RunVenueExtractStep: %v", err)
	}

	if len(exec.systemScripts) != 2 {
		t.Fatalf("RunSystem calls = %d, want 2; got %#v", len(exec.systemScripts), exec.systemScripts)
	}
	if !strings.Contains(exec.systemScripts[0], "mkdir -p '/usr/local/bin'") {
		t.Errorf("extract script = %q, want mkdir -p '/usr/local/bin' (Dir of the trailing-slash Dest)", exec.systemScripts[0])
	}

	// The tarball root must keep the SOURCE basename (envoy), never "bin".
	if len(tarRec.args) != 1 {
		t.Fatalf("tar calls = %d, want 1; got %#v", len(tarRec.args), tarRec.args)
	}
	args := tarRec.args[0]
	if len(args) < 2 || args[len(args)-1] != "envoy" {
		t.Errorf("trailing-slash tar args = %q, want the tarball root to be envoy (source basename), not bin", strings.Join(args, " "))
	}
}

// TestRunVenueExtractStep_ResolveImageApplied proves the injected resolveImage closure rewrites
// the source ref before the engine create (the caller's ONE genuine core dependency — a project
// Config + dir to resolve a short/namespace-qualified ref).
func TestRunVenueExtractStep_ResolveImageApplied(t *testing.T) {
	rec := &engineRecorder{}
	orig := runEngineCmd
	runEngineCmd = rec.run
	defer func() { runEngineCmd = orig }()
	origTar := tarCreate
	tarCreate = (&tarRecorder{}).run
	defer func() { tarCreate = origTar }()

	s := &ExtractStep{Source: "higress/all-in-one:2.2.1", Path: "/usr/local/bin/envoy", Dest: "/usr/local/bin/envoy", CandyName: "c"}
	resolveImage := func(string) (string, error) {
		return "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1", nil
	}
	ensureImage := func(context.Context, string) error { return nil }
	if err := RunVenueExtractStep(context.Background(), &localPkgRecExec{}, resolveImage, ensureImage, s, EmitOpts{}); err != nil {
		t.Fatalf("RunVenueExtractStep: %v", err)
	}
	if len(rec.calls) != 3 || !strings.Contains(rec.calls[0], "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.1") {
		t.Errorf("engine create = %q, want the RESOLVED source ref (not the short form)", rec.calls[0])
	}
}

// TestRunVenueExtractStep_EnsureImageErrorPropagates proves an ensureImage failure surfaces as a
// RunVenueExtractStep error (the image must be present before any engine command runs).
func TestRunVenueExtractStep_EnsureImageErrorPropagates(t *testing.T) {
	rec := &engineRecorder{}
	orig := runEngineCmd
	runEngineCmd = rec.run
	defer func() { runEngineCmd = orig }()

	s := &ExtractStep{Source: "img", Path: "/x", Dest: "/y", CandyName: "c"}
	resolveImage := func(string) (string, error) { return "", nil }
	ensureImage := func(context.Context, string) error { return spec.ErrNotSupported }
	if err := RunVenueExtractStep(context.Background(), &localPkgRecExec{}, resolveImage, ensureImage, s, EmitOpts{}); err == nil {
		t.Fatal("RunVenueExtractStep: expected an error when ensureImage fails, got nil")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("ensureImage failure must precede any engine command, got %#v", rec.calls)
	}
}
