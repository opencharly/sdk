package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// `cargo install` installs BINARIES ONLY — on a crate declaring no [[bin]] it fails with "no
// binaries to install". Any candy shipping a Rust LIBRARY (a cdylib GStreamer plugin, a
// staticlib, a .so for LD_PRELOAD) therefore could not use this builder at all.
func TestCargoInline_CarriesBothModes(t *testing.T) {
	reply, err := BuilderResolve("cargo", spec.BuilderResolveInput{
		Candy: "gst-wayland-display", LayerStage: "layer-x", CacheMountsOwned: "--mount=type=cache,dst=/c ",
	})
	if err != nil {
		t.Fatalf("BuilderResolve: %v", err)
	}
	frag := reply.InlineFragment

	// The binary path is preserved verbatim — this must stay a no-op for every existing
	// cargo candy, all of which install binaries.
	if !strings.Contains(frag, "cargo install --path /tmp/charly-cargo-src") {
		t.Errorf("the binary install path is gone:\n%s", frag)
	}
	// The library path.
	if !strings.Contains(frag, "cargo build --release --manifest-path /tmp/charly-cargo-src/Cargo.toml") {
		t.Errorf("no library build path:\n%s", frag)
	}
	// Artifacts land in a per-candy directory, so the install is identifiable and therefore
	// removable. A flat /usr/local/lib would leave charly unable to tell its own .so files
	// from anyone else's.
	if !strings.Contains(frag, "/usr/local/lib/charly/gst-wayland-display") {
		t.Errorf("artifacts do not go to the per-candy lib dir:\n%s", frag)
	}
	// ...which is invisible to the dynamic loader unless the drop-in is written with it.
	if !strings.Contains(frag, "/etc/ld.so.conf.d/charly-gst-wayland-display.conf") ||
		!strings.Contains(frag, "ldconfig") {
		t.Errorf("no loader drop-in beside the per-candy lib dir:\n%s", frag)
	}
	// The cache mounts still ride on the RUN.
	if !strings.Contains(frag, "--mount=type=cache,dst=/c") {
		t.Errorf("cache mounts dropped:\n%s", frag)
	}
}

// Cargo's binary detection is THREE rules, not one. Treating a binary crate as a library
// would build it and then install nothing at all, so all three must be tested for.
func TestCargoInline_DetectsAllThreeBinaryShapes(t *testing.T) {
	reply, err := BuilderResolve("cargo", spec.BuilderResolveInput{Candy: "c"})
	if err != nil {
		t.Fatalf("BuilderResolve: %v", err)
	}
	for _, want := range []string{
		`[[bin]]`,                           // an explicit section
		`/tmp/charly-cargo-src/src/main.rs`, // the default binary
		`/tmp/charly-cargo-src/src/bin/`,    // additional binaries
	} {
		if !strings.Contains(reply.InlineFragment, want) {
			t.Errorf("binary detection does not test for %q:\n%s", want, reply.InlineFragment)
		}
	}
}

// A library crate that produces no cdylib/staticlib must FAIL, not install nothing quietly.
// The aur builder already sets this precedent for exactly the same reason.
func TestCargoInline_EmptyLibraryBuildIsFatal(t *testing.T) {
	reply, err := BuilderResolve("cargo", spec.BuilderResolveInput{Candy: "c"})
	if err != nil {
		t.Fatalf("BuilderResolve: %v", err)
	}
	frag := reply.InlineFragment
	if !strings.Contains(frag, "exit 1") {
		t.Errorf("an empty library build does not fail the build:\n%s", frag)
	}
	// The message must tell the author what to do, not merely that something is wrong.
	if !strings.Contains(frag, `crate-type = ["cdylib"]`) {
		t.Errorf("the failure message does not name the fix:\n%s", frag)
	}
	// `|| true` anywhere in the install loop would re-introduce the silent no-op.
	if strings.Contains(frag, "|| true") {
		t.Errorf("the library branch swallows a failure with `|| true`:\n%s", frag)
	}
}

// The library install is two files in two places, so the teardown is two ops. Leaving the
// ld.so.conf.d drop-in behind would point the loader at a directory that no longer exists.
func TestBuilderReverse_CargoLibrary(t *testing.T) {
	ctx := BuilderCollectContext("cargo", spec.BuilderCollectInput{Candy: "gwd"})
	ops := BuilderReverse("cargo", spec.BuilderReverseInput{Candy: "gwd", Context: ctx})
	if len(ops) != 2 {
		t.Fatalf("cargo library reverse = %+v, want 2 ops", ops)
	}
	if ops[0].Kind != spec.ReverseOpRmDirRecursive || ops[0].Targets[0] != "/usr/local/lib/charly/gwd" {
		t.Errorf("op 0 = %+v, want rm-dir-recursive on the per-candy lib dir", ops[0])
	}
	if ops[1].Kind != spec.ReverseOpRmFileSystem || ops[1].Targets[0] != "/etc/ld.so.conf.d/charly-gwd.conf" {
		t.Errorf("op 1 = %+v, want rm-file-system on the loader drop-in", ops[1])
	}
	for i, op := range ops {
		if op.Scope != spec.ScopeSystem {
			t.Errorf("op %d scope = %q, want system — both paths are outside any user's home", i, op.Scope)
		}
	}
}

// The binary leg still produces its own op, and the two legs coexist: a context carrying both
// yields three ops, not one silently replacing the other.
func TestBuilderReverse_CargoBinaryAndLibraryCoexist(t *testing.T) {
	ops := BuilderReverse("cargo", spec.BuilderReverseInput{Candy: "c", Context: map[string]any{
		"binaries": []any{"rg"},
		"lib_dir":  "/usr/local/lib/charly/c",
		"ld_conf":  "/etc/ld.so.conf.d/charly-c.conf",
	}})
	if len(ops) != 3 {
		t.Fatalf("reverse = %+v, want 3 ops (cargo-uninstall + rm-dir + rm-file)", ops)
	}
	if ops[0].Kind != spec.ReverseOpCargoUninstall {
		t.Errorf("the binary leg's cargo-uninstall was displaced: %+v", ops)
	}
}

// Both cargo modes write INTO the crate directory — `cargo build` its Cargo.lock, `cargo
// install` its intermediate artifacts — and a Containerfile `--mount=type=bind` is READ-ONLY.
// Building anywhere under /ctx therefore fails at image-build time with an "os error 30" that
// names a path, not a cause. The source is copied out ONCE, before the branch, so neither mode
// can regress into building in place.
func TestCargoInline_NeverBuildsInTheReadOnlyMount(t *testing.T) {
	reply, err := BuilderResolve("cargo", spec.BuilderResolveInput{Candy: "c", LayerStage: "L"})
	if err != nil {
		t.Fatalf("BuilderResolve: %v", err)
	}
	frag := reply.InlineFragment
	if !strings.Contains(frag, "cp -a /ctx /tmp/charly-cargo-src") {
		t.Errorf("the source is not copied out of the read-only bind mount:\n%s", frag)
	}
	// /ctx may appear ONLY as the mount target and as the copy source — never as a build or
	// install path.
	for _, forbidden := range []string{
		"cargo install --path /ctx",
		"--manifest-path /ctx",
		"--target-dir /ctx",
	} {
		if strings.Contains(frag, forbidden) {
			t.Errorf("cargo would write into the read-only mount (%q):\n%s", forbidden, frag)
		}
	}
}
