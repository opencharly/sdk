package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestAurStage_EmptyOutputGuard proves the aur builder's rendered stage carries
// the zero-output guard: after the `find … -exec cp` (which silently succeeds on
// zero matches), the stage fails loudly if no .pkg.tar.zst landed in
// /tmp/aur-pkgs. Without it, a package that moved from the AUR into a repo
// (yay -S --needed installs it without building) produced an EMPTY copy layer
// that was committed + cached (the pgvector / check-cachyos-immich-ml-pod
// incident).
func TestAurStage_EmptyOutputGuard(t *testing.T) {
	reply, err := BuilderResolve("aur", spec.BuilderResolveInput{
		Candy:      "example",
		BuilderRef: "arch-builder:latest",
		StageName:  "example-aur",
		UID:        1000,
		GID:        1000,
		Home:       "/home/user",
		User:       "user",
		Packages:   []string{"yay-bin"},
	})
	if err != nil {
		t.Fatalf("BuilderResolve(aur): %v", err)
	}
	stage := reply.Stage
	if !strings.Contains(stage, "find /tmp/aur-build -name '*.pkg.tar.zst'") {
		t.Fatalf("aur stage missing the artifact-copy step:\n%s", stage)
	}
	// The guard: an `ls … || { echo …; exit 1; }` after the copy.
	if !strings.Contains(stage, "ls /tmp/aur-pkgs/*.pkg.tar.zst") || !strings.Contains(stage, "exit 1") {
		t.Fatalf("aur stage missing the zero-output guard (ls … || exit 1):\n%s", stage)
	}
	// The guard must come AFTER the copy (so it checks the copy's result).
	if strings.Index(stage, "ls /tmp/aur-pkgs/*.pkg.tar.zst") < strings.Index(stage, "-exec cp") {
		t.Fatal("guard must follow the find|cp, not precede it")
	}
}
