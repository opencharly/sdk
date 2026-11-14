package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// TestEmitBaseBootstrapResetsRootForExternalBase pins sdk#515: every image charly
// builds ends with `USER <uid>` (deploykit/generate.go emits it for the workload
// user), so composing such an image as an EXTERNAL base carries that USER into the
// child build. The base stage's bootstrap RUN then executes as uid 1000 and dies on
// `/etc/dnf/dnf.conf: Permission denied` — measured live on
// `base: ghcr.io/opencharly/fedora-nonfree:2026.317.2254`.
//
// The internal-base branch already resets with `USER root`; the external branch must
// do the same, BEFORE the bootstrap RUN.
func TestEmitBaseBootstrapResetsRootForExternalBase(t *testing.T) {
	img := &buildkit.ResolvedBox{
		ResolvedBox: spec.ResolvedBox{
			Name:           "demo",
			From:           "ghcr.io/opencharly/fedora:2026.317.2254",
			IsExternalBase: true,
			User:           "user",
			UID:            1000,
			GID:            1000,
			Home:           "/home/user",
		},
	}
	var b strings.Builder
	(&Generator{}).EmitBaseBootstrap(&b, "demo", img)
	out := b.String()

	rootAt := strings.Index(out, "USER root")
	bootAt := strings.Index(out, "# Bootstrap")
	if rootAt < 0 {
		t.Fatalf("external base stage must reset to root (the base image ends `USER %d`); got:\n%s", img.UID, out)
	}
	if bootAt < 0 {
		t.Fatalf("bootstrap block missing from the external base stage; got:\n%s", out)
	}
	if rootAt > bootAt {
		t.Fatalf("USER root must come BEFORE the bootstrap RUN (root at %d, bootstrap at %d); got:\n%s", rootAt, bootAt, out)
	}
}

// TestEmitBaseBootstrapInternalBaseUnchanged keeps the internal-base arm honest: it
// resets to root and must NOT re-run the bootstrap (the parent image already did).
func TestEmitBaseBootstrapInternalBaseUnchanged(t *testing.T) {
	img := &buildkit.ResolvedBox{
		ResolvedBox: spec.ResolvedBox{
			Name:           "demo",
			From:           "fedora",
			IsExternalBase: false,
		},
	}
	var b strings.Builder
	(&Generator{}).EmitBaseBootstrap(&b, "demo", img)
	out := b.String()

	if !strings.Contains(out, "USER root") {
		t.Fatalf("internal base must reset to root; got:\n%s", out)
	}
	if strings.Contains(out, "# Bootstrap") {
		t.Fatalf("internal base must not re-run the bootstrap; got:\n%s", out)
	}
}
