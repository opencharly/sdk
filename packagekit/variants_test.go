package packagekit

import (
	"reflect"
	"testing"

	"github.com/opencharly/spec/spec"
)

func testPackaging() *spec.Packaging {
	return &spec.Packaging{
		Name:       "charly",
		Maintainer: "Test Maintainer <test@example.com>",
		Variants: map[string]*spec.PackagingVariant{
			"default": {Plugins: []string{"plugin-doctor", "plugin-clean"}},
			"minimal": {Plugins: []string{"plugin-doctor"}},
			"full":    {Plugins: []string{"plugin-doctor", "plugin-clean", "plugin-secrets"}},
			"broken":  {Plugins: []string{"plugin-nope"}},
		},
		Formats: map[string]*spec.PackagingFormat{
			"deb":       {DefaultVariant: "default"},
			"archlinux": {DefaultVariant: "minimal", OptDepends: map[string]string{"qemu-full": "full-system VM support"}},
			"rpm":       {},
			"apk":       {},
			"ipk":       {},
			"msix":      {Publisher: "CN=OpenCharly"},
		},
	}
}

func TestResolveVariant(t *testing.T) {
	pkg := testPackaging()
	cases := []struct {
		format, variant, wantName string
		wantPlugins               []string
	}{
		{"deb", "", "charly", []string{"plugin-doctor", "plugin-clean"}}, // default_variant → plain name
		{"deb", "minimal", "charly-minimal", []string{"plugin-doctor"}},  // named variant → charly-<variant>
		{"archlinux", "", "charly", []string{"plugin-doctor"}},           // default_variant minimal → plain name
		{"archlinux", "full", "charly-full", []string{"plugin-doctor", "plugin-clean", "plugin-secrets"}},
		{"rpm", "", "charly", []string{"plugin-doctor", "plugin-clean"}}, // no default_variant → "default" variant
	}
	for _, c := range cases {
		name, plugins, err := ResolveVariant(pkg, c.format, c.variant)
		if err != nil {
			t.Errorf("ResolveVariant(%q, %q): %v", c.format, c.variant, err)
			continue
		}
		if name != c.wantName {
			t.Errorf("ResolveVariant(%q, %q) name = %q, want %q", c.format, c.variant, name, c.wantName)
		}
		if !reflect.DeepEqual(plugins, c.wantPlugins) {
			t.Errorf("ResolveVariant(%q, %q) plugins = %v, want %v", c.format, c.variant, plugins, c.wantPlugins)
		}
	}
}

func TestResolveVariantUnknown(t *testing.T) {
	pkg := testPackaging()
	if _, _, err := ResolveVariant(pkg, "deb", "nope"); err == nil {
		t.Fatal("expected error for unknown variant")
	}
	if _, _, err := ResolveVariant(pkg, "nope", ""); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestVariantNames(t *testing.T) {
	pkg := testPackaging()
	got := VariantNames(pkg, "deb")
	want := []string{"default", "broken", "full", "minimal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("VariantNames(deb) = %v, want %v", got, want)
	}
}

func TestFamilyNames(t *testing.T) {
	pkg := testPackaging()
	// deb: default_variant "default" → plain `charly`; every other variant → `charly-<v>`.
	got := FamilyNames(pkg, "deb")
	want := []string{"charly", "charly-broken", "charly-full", "charly-minimal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FamilyNames(deb) = %v, want %v", got, want)
	}
	// archlinux: default_variant "minimal" → the `minimal` variant IS the plain name,
	// and `default` becomes `charly-default` — the family is format-dependent.
	got = FamilyNames(pkg, "archlinux")
	want = []string{"charly", "charly-broken", "charly-default", "charly-full"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FamilyNames(archlinux) = %v, want %v", got, want)
	}
}
