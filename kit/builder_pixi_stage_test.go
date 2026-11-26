package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestPixiStageDoesNotSetRattlerCacheDir is the regression witness for the dead cache root:
// measured on pixi 0.70.2 AND 0.84.2, a `--frozen` install writes its package cache to
// PIXI_CACHE_DIR (`pkgs`, `uv-cache`) and NEVER creates RATTLER_CACHE_DIR, so declaring the
// second variable only produced a cache root nothing writes. The emitted stage must set
// PIXI_CACHE_DIR and must not name RATTLER_CACHE_DIR.
func TestPixiStageDoesNotSetRattlerCacheDir(t *testing.T) {
	in := spec.BuilderResolveInput{
		Candy:      "openwebui",
		Builder:    "pixi",
		BuilderRef: "builder:test",
		StageName:  "pixi-build",
		CopySrc:    ".build/_candy/openwebui.v1",
		UID:        1000,
		GID:        1000,
		Home:       "/home/user",
		Manifest:   "pixi.toml",
		InstallCmd: "pixi install --frozen",
	}
	reply, err := BuilderResolve("pixi", in)
	if err != nil {
		t.Fatalf("BuilderResolve(pixi): %v", err)
	}
	if !strings.Contains(reply.Stage, "PIXI_CACHE_DIR=/tmp/pixi-cache") {
		t.Errorf("the pixi stage does not set PIXI_CACHE_DIR:\n%s", reply.Stage)
	}
	if strings.Contains(reply.Stage, "RATTLER_CACHE_DIR=") {
		t.Errorf("the pixi stage still declares RATTLER_CACHE_DIR, which pixi never creates:\n%s", reply.Stage)
	}
}
