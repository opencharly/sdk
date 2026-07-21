package buildkit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// config_resolve_test.go — sdk-level coverage for the box-resolution free functions relocated out
// of charly/config.go + charly/namespace.go (FLOOR-SLIM Unit 5). Proves the moved code LIVE in the
// sdk gate (each test would FAIL without it), independent of charly's own (much larger)
// config_test.go / namespace_test.go / resolver_unify_test.go coverage.

func boxMapOf(m map[string]spec.BoxConfig) spec.BoxMap {
	out := make(spec.BoxMap, len(m))
	for k, v := range m {
		out[k] = spec.EncodeBox(v)
	}
	return out
}

func TestResolveBox_InheritsDefaults(t *testing.T) {
	cfg := &spec.Config{
		Defaults: spec.BoxConfig{
			Registry:  "ghcr.io/test",
			Build:     []string{"rpm"},
			Platforms: []string{"linux/amd64", "linux/arm64"},
		},
		Box: boxMapOf(map[string]spec.BoxConfig{
			"base": {Base: "quay.io/fedora/fedora:43", Distro: []string{"fedora:43", "fedora"}, Candy: []string{}},
		}),
	}
	resolved, err := ResolveBox(cfg, "base", "2026.045.1415", "", ResolveOpts{})
	if err != nil {
		t.Fatalf("ResolveBox() error = %v", err)
	}
	if resolved.Base != "quay.io/fedora/fedora:43" {
		t.Errorf("Base = %q, want quay.io/fedora/fedora:43", resolved.Base)
	}
	if !resolved.IsExternalBase {
		t.Error("IsExternalBase = false, want true (external OCI base)")
	}
	if resolved.Pkg != "rpm" {
		t.Errorf("Pkg = %q, want rpm", resolved.Pkg)
	}
	if resolved.Tag != "2026.045.1415" {
		t.Errorf("Tag = %q, want the calver (auto default)", resolved.Tag)
	}
	if len(resolved.Platforms) != 2 || resolved.Platforms[0] != "linux/amd64" {
		t.Errorf("Platforms = %v, want inherited from defaults", resolved.Platforms)
	}
}

func TestResolveBox_NotFoundAndDisabled(t *testing.T) {
	disabled := false
	cfg := &spec.Config{
		Defaults: spec.BoxConfig{Build: []string{"rpm"}, Platforms: []string{"linux/amd64"}},
		Box: boxMapOf(map[string]spec.BoxConfig{
			"off": {Base: "quay.io/fedora/fedora:43", Enabled: &disabled, Candy: []string{}},
		}),
	}
	if _, err := ResolveBox(cfg, "nonexistent", "test", "", ResolveOpts{}); err == nil {
		t.Error("ResolveBox(nonexistent) should error")
	}
	if _, err := ResolveBox(cfg, "off", "test", "", ResolveOpts{}); err == nil {
		t.Error("ResolveBox(off) should error (disabled)")
	}
	if _, err := ResolveBox(cfg, "off", "test", "", ResolveOpts{IncludeDisabled: true}); err != nil {
		t.Errorf("ResolveBox(off, IncludeDisabled=true) should succeed, got: %v", err)
	}
}

func TestResolveBox_NamespaceDelegation(t *testing.T) {
	sub := &spec.Config{
		Box: boxMapOf(map[string]spec.BoxConfig{
			"widget": {Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Distro: []string{"fedora"}, Candy: []string{}},
		}),
	}
	root := &spec.Config{
		Defaults:   spec.BoxConfig{Platforms: []string{"linux/amd64"}},
		Namespaces: map[string]*spec.Config{"sub": sub},
	}
	resolved, err := ResolveBox(root, "sub.widget", "test", "", ResolveOpts{})
	if err != nil {
		t.Fatalf("ResolveBox(sub.widget) error = %v", err)
	}
	if resolved.Name != "widget" {
		t.Errorf("Name = %q, want widget (leaf, resolved in namespace context)", resolved.Name)
	}
}

func TestResolveEffectiveBuilder_Precedence(t *testing.T) {
	cfg := &spec.Config{
		Defaults: spec.BoxConfig{Builder: BuilderMap{"pixi": "default-builder"}},
		Box: boxMapOf(map[string]spec.BoxConfig{
			"default-builder": {Candy: []string{}},
			"custom-builder":  {Candy: []string{}},
		}),
	}
	out := ResolveEffectiveBuilder(cfg, "app", nil, "scratch", true, BuilderMap{"npm": "custom-builder"})
	if out.BuilderFor("pixi") != "default-builder" {
		t.Errorf("pixi = %q, want default-builder (inherited)", out.BuilderFor("pixi"))
	}
	if out.BuilderFor("npm") != "custom-builder" {
		t.Errorf("npm = %q, want custom-builder (per-image override)", out.BuilderFor("npm"))
	}
	// Self-reference filtered.
	self := ResolveEffectiveBuilder(cfg, "default-builder", nil, "scratch", true, nil)
	if self.HasBuilder("pixi") {
		t.Errorf("self-referencing builder should be filtered, got %v", self)
	}
}

func TestResolveAllBox_DisabledExcludedAndRequestedNamespacedTarget(t *testing.T) {
	disabled := false
	sub := &spec.Config{
		Box: boxMapOf(map[string]spec.BoxConfig{
			"widget": {Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Candy: []string{}},
		}),
	}
	root := &spec.Config{
		Defaults: spec.BoxConfig{Platforms: []string{"linux/amd64"}},
		Box: boxMapOf(map[string]spec.BoxConfig{
			"app": {Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Candy: []string{}},
			"off": {Base: "quay.io/fedora/fedora:43", Build: []string{"rpm"}, Enabled: &disabled, Candy: []string{}},
		}),
		Namespaces: map[string]*spec.Config{"sub": sub},
	}

	all, err := ResolveAllBox(root, "test", "", ResolveOpts{})
	if err != nil {
		t.Fatalf("ResolveAllBox() error = %v", err)
	}
	if _, ok := all["app"]; !ok {
		t.Error("app missing from resolved set")
	}
	if _, ok := all["off"]; ok {
		t.Error("disabled box 'off' should be excluded by default")
	}
	if _, ok := all["sub.widget"]; ok {
		t.Error("sub.widget should NOT be pulled without an explicit request (unreachable base)")
	}

	withReq, err := ResolveAllBox(root, "test", "", ResolveOpts{RequestedBoxes: []string{"sub.widget"}})
	if err != nil {
		t.Fatalf("ResolveAllBox(RequestedBoxes) error = %v", err)
	}
	if _, ok := withReq["sub.widget"]; !ok {
		t.Error("sub.widget should be pulled when explicitly requested")
	}
}
