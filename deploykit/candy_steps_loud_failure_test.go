package deploykit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// candy_steps_loud_failure_test.go — the emitted-install integrity gate.
//
// WriteCandySteps used to swallow every reason a resolved package set could
// fail to render: a nil DistroDef, a primary build format with no definition in
// the box's distro, and (the one that actually shipped) a text/template error
// from the format's phase.install.container template, dropped by an `if err == nil` with no
// else. Each produced a Containerfile that looked legitimate, exited 0, and
// built an image SILENTLY missing every package the candy declared — the
// failure only surfaced much later as an absent binary at runtime.
//
// These tests pin the loud behaviour. Against the pre-fix emitter each returns
// no error and emits nothing, so each fails without the change.
//
// The package-resolution paths are only four of the five swallowed failures.
// The fifth — an EmitTasks error written into the Containerfile as a comment —
// is pinned by TestWriteCandySteps_EmitTasksFailureIsLoud at the bottom of this
// file. It was missed on the first pass precisely because the four above look
// like a complete set: they are the failures of ONE function, and the fifth
// belongs to the call BELOW them in the same emitter.

// installBox returns a box whose primary format's phase.install.container is the
// caller's, so a test can drive the render outcome directly.
func installBox(format, template string) *buildkit.ResolvedBox {
	return &buildkit.ResolvedBox{
		ResolvedBox: spec.ResolvedBox{
			Name: "install-img", User: "user", UID: 1000, GID: 1000, Home: "/home/user",
			Pkg: format, BuildFormats: []string{format}, Tags: []string{"all", format},
		},
		DistroDef: &spec.ResolvedDistro{
			Format: map[string]*spec.Format{
				format: {
					Phases: &spec.PhaseSet{
						Install: &spec.PhaseTemplates{Container: template},
					},
				},
			},
		},
	}
}

func packagedCandy(t *testing.T) *Generator {
	t.Helper()
	return &Generator{Candies: map[string]CandyModel{
		"pkg-candy": newTestCandy("pkg-candy", spec.CandyModel{TopPackages: []string{"sudo"}}),
	}}
}

func TestWriteCandySteps_UnrenderableInstallPhaseIsLoud(t *testing.T) {
	g := packagedCandy(t)
	var b strings.Builder
	// `base` is not in buildkit.TemplateFuncs — exactly the defect that shipped
	// in the alpine apk phase.install.container template and rendered nothing, silently.
	_, err := g.WriteCandySteps(&b, "pkg-candy", installBox("apk", "RUN install {{base .key}}\n"), false)
	if err == nil {
		t.Fatalf("an install.container template that cannot render must be a hard error, got nil (emitted %q)", b.String())
	}
	for _, want := range []string{"pkg-candy", "apk", "install-img"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q so the author can find it; got: %v", want, err)
		}
	}
}

func TestWriteCandySteps_MissingFormatDefIsLoud(t *testing.T) {
	g := packagedCandy(t)
	// The box's primary build format is apk, but its distro defines only rpm —
	// the shape a box hits when it inherits defaults.build without declaring
	// its own. Previously: packages resolved, nothing emitted, exit 0.
	img := installBox("rpm", "RUN dnf install\n")
	img.Pkg = "apk"
	img.BuildFormats = []string{"apk"}

	var b strings.Builder
	_, err := g.WriteCandySteps(&b, "pkg-candy", img, false)
	if err == nil {
		t.Fatalf("a primary format with no format definition must be a hard error, got nil (emitted %q)", b.String())
	}
	if !strings.Contains(err.Error(), "apk") {
		t.Errorf("error should name the unresolvable format; got: %v", err)
	}
}

func TestWriteCandySteps_NoDistroDefIsLoud(t *testing.T) {
	g := packagedCandy(t)
	img := installBox("apk", "RUN apk add\n")
	img.DistroDef = nil

	var b strings.Builder
	_, err := g.WriteCandySteps(&b, "pkg-candy", img, false)
	if err == nil {
		t.Fatalf("resolved packages with no distro definition must be a hard error, got nil (emitted %q)", b.String())
	}
}

// A candy that resolves NO packages for this box must still pass through
// untouched — the common per-distro case (a `distro:` map with no section for
// this box). This is the presence control for the three tests above: without
// it, "every path errors" would satisfy them just as well.
func TestWriteCandySteps_NoPackagesStillRenders(t *testing.T) {
	g := &Generator{Candies: map[string]CandyModel{
		"bare": newTestCandy("bare", spec.CandyModel{}),
	}}
	var b strings.Builder
	if _, err := g.WriteCandySteps(&b, "bare", installBox("apk", "RUN apk add\n"), false); err != nil {
		t.Fatalf("a candy declaring no packages must not error: %v", err)
	}
	if strings.Contains(b.String(), "apk add") {
		t.Errorf("a candy declaring no packages must emit no install; got %q", b.String())
	}
}

// pluginCandy returns a generator whose one candy carries a single `plugin:` op,
// with EmitPluginOp driven by the caller — the seam that decides whether the
// task-emit leg succeeds or fails.
func pluginCandy(emit func(op *spec.Op, img *spec.ResolvedBox) (string, bool, error)) *Generator {
	g := &Generator{Candies: map[string]CandyModel{
		"task-candy": newTestCandy("task-candy", spec.CandyModel{
			RunOps: []spec.Op{{Plugin: "no-such-verb", RunAs: "root"}},
		}),
	}}
	g.EmitPluginOp = emit
	return g
}

// The fifth swallowed failure: WriteCandySteps calls EmitTasks and used to write
// its error into the Containerfile as `# emitTasks error: …`. A comment is not a
// failure — podman builds it happily, the image ships without those steps, and
// the defect surfaces later as a missing file at runtime. Exactly the shape of
// the four above, one call further down.
//
// This is the case a validator caught by mutation after the first round claimed
// the family was complete: reverting the hard error to the comment left the
// whole suite green.
func TestWriteCandySteps_EmitTasksFailureIsLoud(t *testing.T) {
	g := pluginCandy(func(op *spec.Op, _ *spec.ResolvedBox) (string, bool, error) {
		return "", false, fmt.Errorf("verb %q: no provider registered", op.Plugin)
	})
	var b strings.Builder
	_, err := g.WriteCandySteps(&b, "task-candy", testResolvedBox(), false)
	if err == nil {
		t.Fatalf("a task-emit failure must be a hard error, got nil (emitted %q)", b.String())
	}
	// Naming the candy and the image is what makes the error actionable — the
	// author gets told WHERE, not just that something failed.
	for _, want := range []string{"task-candy", "test-img"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q so the author can find it; got: %v", want, err)
		}
	}
	// The pre-fix form is specifically a Containerfile COMMENT. Assert the
	// emitted text never carries one: a future refactor could restore the error
	// return AND keep writing the comment, which would satisfy the check above
	// while still shipping a misleading Containerfile.
	if strings.Contains(b.String(), "# emitTasks error") {
		t.Errorf("the failure was written into the Containerfile as a comment; emitted:\n%s", b.String())
	}
}

// Presence control for the test above. Without it, "any candy carrying a plugin
// op errors" would satisfy it just as well — and that would break every image
// that uses a plugin verb, which is most of them.
func TestWriteCandySteps_SucceedingPluginOpStillRenders(t *testing.T) {
	g := pluginCandy(func(*spec.Op, *spec.ResolvedBox) (string, bool, error) {
		return "RUN echo ok", false, nil // ActScript=false → spliced verbatim
	})
	var b strings.Builder
	if _, err := g.WriteCandySteps(&b, "task-candy", testResolvedBox(), false); err != nil {
		t.Fatalf("a plugin op whose provider resolves must render cleanly, got: %v", err)
	}
	if !strings.Contains(b.String(), "RUN echo ok") {
		t.Errorf("the provider's fragment did not reach the Containerfile; emitted:\n%s", b.String())
	}
}
