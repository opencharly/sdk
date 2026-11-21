package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
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
	if !strings.Contains(stage, "ls /tmp/aur-pkgs/example/*.pkg.tar.zst") || !strings.Contains(stage, "exit 1") {
		t.Fatalf("aur stage missing the zero-output guard (ls … || exit 1):\n%s", stage)
	}
	// The guard must come AFTER the copy (so it checks the copy's result).
	if strings.Index(stage, "ls /tmp/aur-pkgs/example/*.pkg.tar.zst") < strings.Index(stage, "-exec cp") {
		t.Fatal("guard must follow the find|cp, not precede it")
	}
}

// Each candy's packages and payload live in the candy's OWN directories — the aur builder plugin's
// own knowledge, declared in its reply (#BuilderResolveReply.package), never authored on the builder
// node (opencharly/sdk#608, DESIGN.cue #BuilderAlt). A shared /tmp/aur-pkgs made the first candy's
// install pick up every other candy's packages too.
func TestAurStage_PerCandyDirectoriesDeclaredByThePlugin(t *testing.T) {
	reply, err := BuilderResolve("aur", spec.BuilderResolveInput{
		Candy: "kde-shell", StageName: "kde-shell-aur-build", Packages: []string{"kdotool"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-exec cp {} /tmp/aur-pkgs/kde-shell/", "bsdtar -xpf \"$p\" -C /tmp/aur-root/kde-shell"} {
		if !strings.Contains(reply.Stage, want) {
			t.Errorf("aur stage missing %q:\n%s", want, reply.Stage)
		}
	}
	if len(reply.CopyArtifacts) != 1 || reply.CopyArtifacts[0] != "COPY --from=kde-shell-aur-build /tmp/aur-root/kde-shell/ /" {
		t.Errorf("aur payload copy = %v, want the candy's extracted package tree", reply.CopyArtifacts)
	}
	want := spec.BuilderPackage{Format: "pac", Dir: "/tmp/aur-pkgs/kde-shell", Glob: "*.pkg.tar.zst"}
	if reply.Package == nil || *reply.Package != want {
		t.Errorf("aur reply package = %+v, want %+v", reply.Package, want)
	}
}

// Every stage builder's payload is the plugin's own (pixi/npm/mise copy the stage home, aur its
// package tree); only aur declares a package output; a stage word with no payload is an error,
// never a stage whose output reaches no image.
func TestStageBuildersCarryThePluginsPayload(t *testing.T) {
	for _, word := range []string{"pixi", "npm", "mise"} {
		in := spec.BuilderResolveInput{Candy: "c", Builder: word, StageName: "c-" + word + "-build", Home: "/home/u", UID: 1000, GID: 1000}
		reply, err := BuilderResolve(word, in)
		if err != nil {
			t.Errorf("%s: %v", word, err)
			continue
		}
		if len(reply.CopyArtifacts) != 1 || reply.CopyArtifacts[0] != "COPY --from=c-"+word+"-build --chown=1000:1000 /home/u /home/u" {
			t.Errorf("%s: payload = %v, want the stage home", word, reply.CopyArtifacts)
		}
		if reply.Package != nil {
			t.Errorf("%s declares a package output %+v; only aur produces OS packages", word, reply.Package)
		}
	}
	// aur's payload is the candy's extracted package tree, copied to the image root as root-owned
	// system files: it is what a target without a pacman local_pkg contract receives, so a payload
	// that copied the stage home instead would ship none of the packages' files.
	aur := spec.BuilderResolveInput{Candy: "c", Builder: "aur", StageName: "c-aur-build", Home: "/home/u", UID: 1000, GID: 1000}
	reply, err := BuilderResolve("aur", aur)
	if err != nil {
		t.Fatalf("aur: %v", err)
	}
	if want := "COPY --from=c-aur-build " + AurPayloadDir("c") + "/ /"; len(reply.CopyArtifacts) != 1 || reply.CopyArtifacts[0] != want {
		t.Errorf("aur: payload = %v, want [%s] (the extracted package tree, root-owned)", reply.CopyArtifacts, want)
	}
	if _, err := builderPayloadCopies("cargo", spec.BuilderResolveInput{Candy: "c"}); err == nil {
		t.Error("a word with no stage payload (cargo is inline) returned no error")
	}
}
