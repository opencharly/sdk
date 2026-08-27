package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

func mkCandy(name, src, sub string, remote bool) CandyModel {
	return NewSpecCandyModel(
		spec.CandyModel{Name: name, SourceDir: src},
		spec.CandyView{Name: name, Remote: remote, SubPathPrefix: sub},
	)
}

func TestRemoteBuildConfigCacheRoot_RootLevel(t *testing.T) {
	// root-level standalone candy (the de-submodule cutover): SourceDir IS the repo@version cache root
	root := "/home/u/.cache/charly/repos/github.com/opencharly/layer-supervisord@v2026.239.1300"
	got := remoteBuildConfigCacheRoot(map[string]CandyModel{
		"layer-supervisord": mkCandy("layer-supervisord", root, "", true),
	})
	if got != root {
		t.Fatalf("root-level: got %q want %q", got, root)
	}
}

func TestRemoteBuildConfigCacheRoot_Subpath(t *testing.T) {
	// subpath candy (old charly-repo shape): strip candy/<name> to reach the shared cache root
	src := "/home/u/.cache/charly/repos/github.com/opencharly/charly@v2026.238.1242/candy/layer-supervisord"
	want := "/home/u/.cache/charly/repos/github.com/opencharly/charly@v2026.238.1242"
	got := remoteBuildConfigCacheRoot(map[string]CandyModel{
		"candy/layer-supervisord": mkCandy("layer-supervisord", src, "candy/", true),
	})
	if got != want {
		t.Fatalf("subpath: got %q want %q", got, want)
	}
}

func TestRemoteBuildConfigCacheRoot_NoRemote(t *testing.T) {
	got := remoteBuildConfigCacheRoot(map[string]CandyModel{
		"local": mkCandy("local", "/proj/candy/local", "", false),
	})
	if got != "" {
		t.Fatalf("no-remote: got %q want \"\"", got)
	}
}
