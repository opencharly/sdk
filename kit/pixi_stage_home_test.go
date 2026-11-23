package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestPixiStage_PinsHomeAndEnvRoot is the coverage for the regression that made every ComfyUI
// image check read a missing interpreter: `pixi install` stores its environment under
// $PIXI_HOME (default $HOME/.pixi), while USER+WORKDIR do NOT set HOME — it comes from the
// base image's passwd for that uid. A base whose uid home moved silently put the env outside
// the box's home, and the final stage's `COPY --from=<stage> <home> <home>` carried no .pixi.
//
// The stage must therefore SET HOME and PIXI_HOME from the input home, and assert the env root
// exists after installing. Removing either line makes this test fail.
func TestPixiStage_PinsHomeAndEnvRoot(t *testing.T) {
	for _, home := range []string{"/home/user", "/root", "/home/u"} {
		reply, err := BuilderResolve("pixi", spec.BuilderResolveInput{
			Candy:      "example",
			Builder:    "pixi",
			BuilderRef: "arch-builder:latest",
			StageName:  "example-pixi",
			UID:        1000,
			GID:        1000,
			Home:       home,
			User:       "user",
			Manifest:   "pixi.toml",
			CopySrc:    "candy/example",
		})
		if err != nil {
			t.Fatalf("BuilderResolve(pixi): %v", err)
		}
		stage := reply.Stage
		wantEnv := "ENV HOME=" + home + " PIXI_HOME=" + home + "/.pixi"
		if !strings.Contains(stage, wantEnv) {
			t.Fatalf("pixi stage for home %q missing %q:\n%s", home, wantEnv, stage)
		}
		wantAssert := "test -d " + home + "/.pixi"
		if !strings.Contains(stage, wantAssert) {
			t.Fatalf("pixi stage for home %q missing the env-root assertion %q:\n%s", home, wantAssert, stage)
		}
		if strings.Contains(stage, "PIXI_HOME=/tmp") {
			t.Fatalf("pixi stage pins PIXI_HOME outside the box home:\n%s", stage)
		}
	}
}
