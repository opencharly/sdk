package deploykit

import (
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
// from the format's install_template, dropped by an `if err == nil` with no
// else. Each produced a Containerfile that looked legitimate, exited 0, and
// built an image SILENTLY missing every package the candy declared — the
// failure only surfaced much later as an absent binary at runtime.
//
// These tests pin the loud behaviour. Against the pre-fix emitter all three
// return no error and emit nothing, so each fails without the change.

// installBox returns a box whose primary format's install_template is the
// caller's, so a test can drive the render outcome directly.
func installBox(format, template string) *buildkit.ResolvedBox {
	return &buildkit.ResolvedBox{
		ResolvedBox: spec.ResolvedBox{
			Name: "install-img", User: "user", UID: 1000, GID: 1000, Home: "/home/user",
			Pkg: format, BuildFormats: []string{format}, Tags: []string{"all", format},
		},
		DistroDef: &spec.ResolvedDistro{
			Format: map[string]*spec.Format{
				format: {InstallTemplate: template},
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

func TestWriteCandySteps_UnrenderableInstallTemplateIsLoud(t *testing.T) {
	g := packagedCandy(t)
	var b strings.Builder
	// `base` is not in buildkit.TemplateFuncs — exactly the defect that shipped
	// in the alpine apk install_template and rendered nothing, silently.
	_, err := g.WriteCandySteps(&b, "pkg-candy", installBox("apk", "RUN install {{base .key}}\n"), false)
	if err == nil {
		t.Fatalf("an install_template that cannot render must be a hard error, got nil (emitted %q)", b.String())
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
