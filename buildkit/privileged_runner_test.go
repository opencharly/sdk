package buildkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// privileged_runner_test.go — coverage for the pure pieces of the shared privileged-container
// exec primitive. RunPrivileged itself is NOT unit-tested here: it execs a real `podman run
// --privileged` (pacstrap/debootstrap/bootc-install), which needs a live container engine and
// root-capable namespace behavior no hermetic unit test can fake honestly — it is
// integration-covered by the check-bed roster's arch-pacstrap privileged-bootstrap build (R10).

func TestCopyFileBytes(t *testing.T) {
	t.Run("round-trips real file bytes through a temp dir", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src.bin")
		dst := filepath.Join(dir, "sub", "dst.bin")
		want := []byte("charly privileged bootstrap payload\x00\x01\x02")
		if err := os.WriteFile(src, want, 0o600); err != nil {
			t.Fatalf("seeding src: %v", err)
		}
		// CopyFileBytes must NOT create the destination's parent dir itself — mirroring
		// RunPrivileged's own contract, which calls os.MkdirAll(filepath.Dir(OutputDest))
		// before invoking this helper. Prove that by pre-creating it here.
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("seeding dst dir: %v", err)
		}
		if err := CopyFileBytes(src, dst); err != nil {
			t.Fatalf("CopyFileBytes: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("reading dst: %v", err)
		}
		if string(got) != string(want) {
			t.Errorf("round-tripped content = %q, want %q", got, want)
		}
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("stat dst: %v", err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("dst perm = %o, want 0644 (CopyFileBytes' documented WriteFile mode)", info.Mode().Perm())
		}
	})

	t.Run("missing source file surfaces the read error", func(t *testing.T) {
		dir := t.TempDir()
		err := CopyFileBytes(filepath.Join(dir, "does-not-exist"), filepath.Join(dir, "dst"))
		if err == nil {
			t.Fatal("expected an error for a missing source file")
		}
	})
}

func TestCheckFreeSpace(t *testing.T) {
	dir := t.TempDir()

	t.Run("zero min is a no-op", func(t *testing.T) {
		if err := checkFreeSpace(dir, 0); err != nil {
			t.Errorf("checkFreeSpace(dir, 0) = %v, want nil", err)
		}
		if err := checkFreeSpace(dir, -1); err != nil {
			t.Errorf("checkFreeSpace(dir, -1) = %v, want nil", err)
		}
	})

	t.Run("small min passes on a real filesystem", func(t *testing.T) {
		if err := checkFreeSpace(dir, 1<<20); err != nil {
			t.Errorf("checkFreeSpace(dir, 1MiB) = %v, want nil (temp dir has free space)", err)
		}
	})

	t.Run("impossible min fails with the actionable error", func(t *testing.T) {
		err := checkFreeSpace(dir, 1<<60)
		if err == nil {
			t.Fatal("expected an error for an impossible free-space requirement")
		}
		msg := err.Error()
		for _, want := range []string{"insufficient free space on", "GiB free", "GiB required", "free disk space and retry"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q missing expected fragment %q", msg, want)
			}
		}
	})
}

func TestRenderBootstrapScript(t *testing.T) {
	t.Run("resolves ctx fields + funcs into the rendered script", func(t *testing.T) {
		builder := &BuilderDef{
			Inline:          true,
			InstallTemplate: `home={{.Home}} pkgs={{join .Packages ","}} q={{quote .Quoted}}`,
		}
		ctx := struct {
			Home     string
			Packages []string
			Quoted   string
		}{
			Home:     "/root",
			Packages: []string{"base", "linux", "sudo"},
			Quoted:   "a b",
		}
		got, err := RenderBootstrapScript(builder, ctx)
		if err != nil {
			t.Fatalf("RenderBootstrapScript: %v", err)
		}
		want := `home=/root pkgs=base,linux,sudo q="a b"`
		if got != want {
			t.Errorf("RenderBootstrapScript = %q, want %q", got, want)
		}
	})

	t.Run("uses the phase.install.container template over the legacy fallback when both set", func(t *testing.T) {
		builder := &BuilderDef{
			Inline:          true,
			InstallTemplate: "legacy={{.X}}",
			Phases: &spec.PhaseSet{
				Install: &spec.PhaseTemplates{Container: "phased={{.X}}"},
			},
		}
		ctx := struct{ X string }{X: "v"}
		got, err := RenderBootstrapScript(builder, ctx)
		if err != nil {
			t.Fatalf("RenderBootstrapScript: %v", err)
		}
		if got != "phased=v" {
			t.Errorf("RenderBootstrapScript = %q, want %q (phase template must win)", got, "phased=v")
		}
	})

	t.Run("no install template at all is an error", func(t *testing.T) {
		builder := &BuilderDef{}
		_, err := RenderBootstrapScript(builder, struct{}{})
		if err == nil {
			t.Fatal("expected an error for a builder with no phase.install.container template")
		}
		if !strings.Contains(err.Error(), "no phase.install.container template") {
			t.Errorf("error %q missing expected diagnostic text", err.Error())
		}
	})

	t.Run("InstallTemplate set but Inline false does not fall back (multi-stage builders resolve via their plugin, not this legacy path)", func(t *testing.T) {
		builder := &BuilderDef{InstallTemplate: "should-not-render={{.X}}"}
		_, err := RenderBootstrapScript(builder, struct{ X string }{X: "v"})
		if err == nil {
			t.Fatal("expected an error: non-inline builders must not use the legacy InstallTemplate fallback")
		}
		if !strings.Contains(err.Error(), "no phase.install.container template") {
			t.Errorf("error %q missing expected diagnostic text", err.Error())
		}
	})

	t.Run("malformed template text is a parse error", func(t *testing.T) {
		builder := &BuilderDef{Inline: true, InstallTemplate: "{{ .Unclosed "}
		_, err := RenderBootstrapScript(builder, struct{}{})
		if err == nil {
			t.Fatal("expected a template parse error")
		}
		if !strings.Contains(err.Error(), "parsing bootstrap script template") {
			t.Errorf("error %q missing expected diagnostic text", err.Error())
		}
	})

	t.Run("referencing a field absent from ctx is an execute error", func(t *testing.T) {
		builder := &BuilderDef{Inline: true, InstallTemplate: "{{.NoSuchField}}"}
		ctx := struct{ Present string }{Present: "x"}
		_, err := RenderBootstrapScript(builder, ctx)
		if err == nil {
			t.Fatal("expected a template execute error for a missing field")
		}
		if !strings.Contains(err.Error(), "rendering bootstrap script") {
			t.Errorf("error %q missing expected diagnostic text", err.Error())
		}
	})
}
