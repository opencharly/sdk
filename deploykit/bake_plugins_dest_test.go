package deploykit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestEmitBakedPluginsWritesOutsideThePackageOwnedDir is the #595 regression test.
//
// The distro package OWNS /usr/lib/charly/plugins/**: a build step that writes there collides
// with the transaction that installs charly in the SAME image —
//
//	charly: /usr/lib/charly/plugins/plugin-stub exists in filesystem
//	error: failed to commit transaction (conflicting files)
//
// — so the bake must land in bakedPluginImageDir (package-safe, /usr/local) and register it with
// the in-container charly through CHARLY_PLUGIN_DIR, which charly's loader searches FIRST.
func TestEmitBakedPluginsWritesOutsideThePackageOwnedDir(t *testing.T) {
	srcDir := t.TempDir()
	writeBakeTestFile(t, filepath.Join(srcDir, "go.mod"), "module example.com/plugin-stub\n\ngo 1.21\n")
	writeBakeTestFile(t, filepath.Join(srcDir, "main.go"), "package main\n\nfunc main() {}\n")

	consumer := NewSpecCandyModel(
		spec.CandyModel{BakePlugin: []string{"plugin-stub"}},
		spec.CandyView{Name: "consumer"},
	)
	plugin := NewSpecCandyModel(
		spec.CandyModel{SourceDir: srcDir},
		spec.CandyView{Name: "plugin-stub", IsPlugin: true, PluginProviders: []string{"verb:stub"}},
	)

	var b strings.Builder
	err := EmitBakedPlugins(
		context.Background(), &b, t.TempDir(), "box",
		[]string{"consumer"},
		map[string]CandyModel{"consumer": consumer, "plugin-stub": plugin},
	)
	if err != nil {
		t.Fatalf("EmitBakedPlugins: %v", err)
	}
	got := b.String()

	// THE INVARIANT: no emitted path may land on the distro package's own plugin directory,
	// or the pacman transaction that installs charly refuses the image build.
	if strings.Contains(got, " "+bakedPluginDir+"/") {
		t.Fatalf("bake emits into the package-owned dir %s, which the charly install refuses:\n%s",
			bakedPluginDir, got)
	}

	// The bare metal: binary AND providers manifest both land in the package-safe image dir.
	for _, want := range []string{
		"COPY .build/box/.plugins/plugin-stub " + bakedPluginImageDir + "/plugin-stub\n",
		"RUN chmod 0755 " + bakedPluginImageDir + "/plugin-stub\n",
		"COPY .build/box/.plugins/plugin-stub.providers " + bakedPluginImageDir + "/plugin-stub.providers\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted fragment is missing %q:\n%s", want, got)
		}
	}

	// The registration the in-container charly needs, emitted exactly once however many
	// plugins are baked.
	env := "ENV " + bakedPluginEnv + "=" + bakedPluginImageDir + "\n"
	if n := strings.Count(got, env); n != 1 {
		t.Errorf("emitted %d copies of %q, want exactly 1:\n%s", n, env, got)
	}
}

func writeBakeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
