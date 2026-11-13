package deploykit

// bake_plugins_manifest_bytes_test.go — the BYTE-FOR-BYTE half of the ONE-manifest-format cutover
// (opencharly/charly#1131): repointing the baked-image emitter at spec.PluginManifest must not
// change a single byte of what the image carries, because a plugin tree is read by ONE reader.
//
// The expectation below is a LITERAL, not spec.PluginManifest(...) — asserting the function against
// itself would pass no matter what either side became. The format is `class:word` per line,
// newline-terminated, staged beside the binary as `<leaf>.providers`.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestEmitBakedPluginsWritesTheManifestByteForByte(t *testing.T) {
	srcDir := t.TempDir()
	writeBakeTestFile(t, filepath.Join(srcDir, "go.mod"), "module example.com/plugin-stub\n\ngo 1.21\n")
	writeBakeTestFile(t, filepath.Join(srcDir, "main.go"), "package main\n\nfunc main() {}\n")

	consumer := NewSpecCandyModel(
		spec.CandyModel{BakePlugin: []string{"plugin-stub"}},
		spec.CandyView{Name: "consumer"},
	)
	plugin := NewSpecCandyModel(
		spec.CandyModel{SourceDir: srcDir},
		spec.CandyView{
			Name: "plugin-stub", IsPlugin: true,
			PluginProviders: []string{"verb:stub", "command:stub"},
		},
	)

	buildDir := t.TempDir()
	var b strings.Builder
	if err := EmitBakedPlugins(
		context.Background(), &b, buildDir, "box",
		[]string{"consumer"},
		map[string]CandyModel{"consumer": consumer, "plugin-stub": plugin},
	); err != nil {
		t.Fatalf("EmitBakedPlugins: %v", err)
	}

	// BYTE-FOR-BYTE, against a literal: one `class:word` per line, newline-terminated.
	want := []byte("verb:stub\ncommand:stub\n")
	staged := filepath.Join(buildDir, "box", ".plugins", "plugin-stub.providers")
	gotBytes, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("the baked plugin's manifest is not staged at %s: %v", staged, err)
	}
	if string(gotBytes) != string(want) {
		t.Fatalf("staged manifest = %q, want %q", gotBytes, want)
	}
	if gotBytes[len(gotBytes)-1] != '\n' {
		t.Fatalf("staged manifest does not end with a newline: %q", gotBytes)
	}

	// The manifest is DATA, not a program: 0644, whatever the binary's mode is.
	info, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("stat the staged manifest: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("staged manifest mode = %#o, want 0o644", perm)
	}

	// And the fragment carries the matching COPY, on both sides of the same suffix.
	got := b.String()
	wantCopy := "COPY .build/box/.plugins/plugin-stub.providers " + bakedPluginImageDir + "/plugin-stub.providers\n"
	if !strings.Contains(got, wantCopy) {
		t.Fatalf("emitted fragment is missing %q:\n%s", wantCopy, got)
	}
	// No second spelling of the suffix anywhere in the fragment (the repoint removed the literal).
	if strings.Contains(got, ".providers.providers") {
		t.Fatalf("the suffix was appended twice:\n%s", got)
	}
}
