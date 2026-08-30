package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestBuilderResolve_Mise proves the mise detection-builder renders a
// DISTRO-AGNOSTIC stage from the host-computed input: FROM the BuilderRef (the
// image's builder box), the install command from the embedded builder: mise:
// vocabulary's install_command, and the Home-based artifacts — no hard-coded
// distro names, package managers, or install paths (the boundary law: distro
// knowledge is DATA in the host's vocabulary, never a builder template).
func TestBuilderResolve_Mise(t *testing.T) {
	reply, err := BuilderResolve("mise", spec.BuilderResolveInput{
		BuilderRef: "cachyos-builder",
		StageName:  "demo-mise-build",
		CopySrc:    "candy/demo",
		Manifest:   "mise.toml",
		UID:        1000,
		GID:        1000,
		Home:       "/home/user",
		InstallCmd: "mise install",
	})
	if err != nil {
		t.Fatalf("BuilderResolve(mise): %v", err)
	}
	if !strings.Contains(reply.Stage, "FROM cachyos-builder AS demo-mise-build") {
		t.Fatalf("stage missing BuilderRef FROM: %q", reply.Stage)
	}
	if !strings.Contains(reply.Stage, "COPY --chown=1000:1000 candy/demo/mise.toml mise.toml") {
		t.Fatalf("stage missing manifest COPY: %q", reply.Stage)
	}
	if !strings.Contains(reply.Stage, "mise install && mise reshim") {
		t.Fatalf("stage missing install+reshim: %q", reply.Stage)
	}
	// The pinned mise release is NAMED (miseVersion) and rendered into the
	// download URL — never a magic literal in the template.
	if !strings.Contains(reply.Stage, "mise-"+miseVersion+"-linux-$A$M.tar.gz") {
		t.Fatalf("stage missing the named pinned mise release in the download URL: %q", reply.Stage)
	}
	// The stage probes for the musl loader and appends the -musl suffix, so the
	// glibc binary never lands on a musl base where it cannot exec.
	if !strings.Contains(reply.Stage, "ld-musl-x86_64.so.1") || !strings.Contains(reply.Stage, "M=\"-musl\"") {
		t.Fatalf("stage missing the musl-loader probe: %q", reply.Stage)
	}
	if len(reply.CopyArtifacts) != 1 || !strings.Contains(reply.CopyArtifacts[0], "/home/user") {
		t.Fatalf("artifacts must copy the Home: %v", reply.CopyArtifacts)
	}
	if !strings.Contains(reply.CopyBinary, "/usr/local/bin/mise") {
		t.Fatalf("binary copy missing: %q", reply.CopyBinary)
	}
}
