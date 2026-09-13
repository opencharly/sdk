package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModuleVersion(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{"v2026.239.1615", "v0.2026239.1615"},
		{"v2026.240.0122", "v0.2026240.122"},
		{"v2026.250.0721", "v0.2026250.721"},
		{"v2026.203.0007", "v0.2026203.7"},
	}
	for _, c := range cases {
		got, err := goModuleVersion(c.tag)
		if err != nil {
			t.Fatalf("goModuleVersion(%q): %v", c.tag, err)
		}
		if got != c.want {
			t.Errorf("goModuleVersion(%q) = %q, want %q", c.tag, got, c.want)
		}
	}
	for _, bad := range []string{"2026.239.1615", "v2026.239", "v2026.239.16x5", "v26.239.1615"} {
		if _, err := goModuleVersion(bad); err == nil {
			t.Errorf("goModuleVersion(%q) = nil error, want failure", bad)
		}
	}
}

func writePins(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pins.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePins(t *testing.T) {
	p := writePins(t, "# a comment\n\nplugin-clean@v2026.239.1615\nplugin-secrets@v2026.240.0122\n")
	pins, err := parsePins(p)
	if err != nil {
		t.Fatalf("parsePins: %v", err)
	}
	if len(pins) != 2 {
		t.Fatalf("got %d pins, want 2", len(pins))
	}
	// sorted by name
	if pins[0].name != "plugin-clean" || pins[1].name != "plugin-secrets" {
		t.Fatalf("pins not name-sorted: %+v", pins)
	}
	if pins[0].module != "github.com/opencharly/plugin-clean/candy/plugin-clean" {
		t.Errorf("module = %q", pins[0].module)
	}
	if pins[1].version != "v0.2026240.122" {
		t.Errorf("version = %q, want v0.2026240.122", pins[1].version)
	}
}

func TestParsePinsRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"no-at":     "plugin-clean\n",
		"duplicate": "plugin-clean@v2026.239.1615\nplugin-clean@v2026.239.1615\n",
		"badtag":    "plugin-clean@nope\n",
	} {
		if _, err := parsePins(writePins(t, body)); err == nil {
			t.Errorf("%s: parsePins = nil error, want failure", name)
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	pins, err := parsePins(writePins(t, "plugin-clean@v2026.239.1615\nplugin-secrets@v2026.240.0122\n"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := renderGoMod("github.com/opencharly/charly-lib", "v0.2026255.2249", "1.26.4", pins)
	main := renderMain("host-command-plugins.txt", pins)

	if again := string(renderGoMod("github.com/opencharly/charly-lib", "v0.2026255.2249", "1.26.4", pins)); again != string(gomod) {
		t.Fatal("renderGoMod is not deterministic")
	}
	if again := string(renderMain("host-command-plugins.txt", pins)); again != string(main) {
		t.Fatal("renderMain is not deterministic")
	}

	gomodS := string(gomod)
	for _, want := range []string{
		"module github.com/opencharly/charly-lib",
		"github.com/opencharly/sdk v0.2026255.2249",
		"github.com/opencharly/plugin-clean/candy/plugin-clean v0.2026239.1615",
		"github.com/opencharly/plugin-secrets/candy/plugin-secrets v0.2026240.122",
	} {
		if !strings.Contains(gomodS, want) {
			t.Errorf("go.mod missing %q\n%s", want, gomodS)
		}
	}
	mainS := string(main)
	for _, want := range []string{
		`plugin_clean "github.com/opencharly/plugin-clean/candy/plugin-clean"`,
		`"github.com/opencharly/sdk/charlylib"`,
		`"plugin-clean": {Provider: plugin_clean.NewProvider, Meta: plugin_clean.NewMeta, CLI: plugin_clean.CliMain}`,
	} {
		if !strings.Contains(mainS, want) {
			t.Errorf("main.go missing %q\n%s", want, mainS)
		}
	}
}

func TestCheckGeneratedDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"go.mod": []byte("x"), "main.go": []byte("y")}
	if err := checkGenerated(dir, files); err == nil {
		t.Fatal("checkGenerated on an empty dir = nil error, want missing-file failure")
	}
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkGenerated(dir, files); err != nil {
		t.Fatalf("checkGenerated on matching files: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkGenerated(dir, files); err == nil {
		t.Fatal("checkGenerated on drifted content = nil error, want failure")
	}
}
