package deploykit

import (
	"reflect"
	"testing"

	"github.com/opencharly/spec/spec"
)

func mkCandy(name, src, sub string, remote bool) CandyModel {
	return NewSpecCandyModel(
		spec.CandyModel{Name: name, SourceDir: src},
		spec.CandyView{Name: name, Remote: remote, SubPathPrefix: sub},
	)
}

func TestRemoteBuildConfigCacheRoots_RootLevel(t *testing.T) {
	// root-level standalone candy (the de-submodule cutover): SourceDir IS the repo@version cache root
	root := "/home/u/.cache/charly/repos/github.com/opencharly/layer-supervisord@v2026.239.1300"
	got := remoteBuildConfigCacheRoots(map[string]CandyModel{
		"layer-supervisord": mkCandy("layer-supervisord", root, "", true),
	})
	want := []string{root}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root-level: got %v want %v", got, want)
	}
}

func TestRemoteBuildConfigCacheRoots_Subpath(t *testing.T) {
	// subpath candy (old charly-repo shape): strip candy/<name> to reach the shared cache root
	src := "/home/u/.cache/charly/repos/github.com/opencharly/charly@v2026.238.1242/candy/layer-supervisord"
	want := "/home/u/.cache/charly/repos/github.com/opencharly/charly@v2026.238.1242"
	got := remoteBuildConfigCacheRoots(map[string]CandyModel{
		"candy/layer-supervisord": mkCandy("layer-supervisord", src, "candy/", true),
	})
	if !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("subpath: got %v want [%v]", got, want)
	}
}

func TestRemoteBuildConfigCacheRoots_MultiRepo(t *testing.T) {
	// post-cutover: two candies in two different repos → two distinct cache roots
	a := "/home/u/.cache/charly/repos/github.com/opencharly/layer-supervisord@v2026.239.1300"
	b := "/home/u/.cache/charly/repos/github.com/opencharly/layer-pixi@v2026.237.1416"
	got := remoteBuildConfigCacheRoots(map[string]CandyModel{
		"layer-supervisord": mkCandy("layer-supervisord", a, "", true),
		"layer-pixi":        mkCandy("layer-pixi", b, "", true),
	})
	if len(got) != 2 {
		t.Fatalf("multi-repo: got %v want 2 roots", got)
	}
}

func TestRemoteBuildConfigCacheRoots_NoRemote(t *testing.T) {
	got := remoteBuildConfigCacheRoots(map[string]CandyModel{
		"local": mkCandy("local", "/proj/candy/local", "", false),
	})
	if len(got) != 0 {
		t.Fatalf("no-remote: got %v want empty", got)
	}
}
