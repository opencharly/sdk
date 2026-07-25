package buildkit

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/sdk/spec"
)

// TestNormalizeBoxArgs asserts the `all` sentinel collapses to nil ONLY when it is the sole
// argument — the canonical "every enabled box" shape shared by `charly box build` and `charly
// box generate` (relocated from charly/box_selection_test.go with the BUILD-cone cutover).
func TestNormalizeBoxArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil stays nil", nil, nil},
		{"empty stays empty", []string{}, []string{}},
		{"lone all → nil", []string{"all"}, nil},
		{"lone ALL (case-insensitive) → nil", []string{"ALL"}, nil},
		{"lone All → nil", []string{"All"}, nil},
		{"single named box passes through", []string{"fedora"}, []string{"fedora"}},
		{"all alongside another name is literal", []string{"all", "fedora"}, []string{"all", "fedora"}},
		{"two named boxes pass through", []string{"fedora", "arch"}, []string{"fedora", "arch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeBoxArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeBoxArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolvePodmanJobs verifies the config-driven --jobs capping logic (relocated from
// charly/build_jobs_test.go). The cap is sourced from defaults.podman_jobs_cap (passed as
// jobsCap); a jobsCap of 0 falls back to PodmanJobsCapFallback. The helper must:
//   - honor an explicit override (>0) verbatim, ignoring cap + ncpu
//   - when no override: return min(NumCPU(), cap)
//   - treat jobsCap < 1 as PodmanJobsCapFallback
func TestResolvePodmanJobs(t *testing.T) {
	origNumCPU := NumCPU
	defer func() { NumCPU = origNumCPU }()

	cases := []struct {
		name     string
		override int
		jobsCap  int
		ncpu     int
		want     int
	}{
		{"override wins over large ncpu + cap", 8, 4, 16, 8},
		{"override wins over small ncpu", 1, 8, 16, 1},
		{"override wins regardless of cap", 12, 8, 16, 12},
		{"no override, configured cap 8, ncpu above cap", 0, 8, 16, 8},
		{"no override, configured cap 8, ncpu below cap returns ncpu", 0, 8, 4, 4},
		{"no override, configured cap 2 below ncpu", 0, 2, 16, 2},
		{"no override, cap 0 falls back to PodmanJobsCapFallback", 0, 0, 16, PodmanJobsCapFallback},
		{"no override, cap negative falls back", 0, -1, 16, PodmanJobsCapFallback},
		{"no override, cap 8 but ncpu 1", 0, 8, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			NumCPU = func() int { return tc.ncpu }
			got := ResolvePodmanJobs(tc.override, tc.jobsCap)
			if got != tc.want {
				t.Errorf("ResolvePodmanJobs(%d, %d) with ncpu=%d = %d, want %d",
					tc.override, tc.jobsCap, tc.ncpu, got, tc.want)
			}
		})
	}
}

// TestHostPlatform pins the OCI-format platform string derivation: "linux/" + the actual
// GOARCH this test binary was built for. Would fail if HostPlatform hardcoded a wrong prefix
// or a wrong GOARCH source.
func TestHostPlatform(t *testing.T) {
	want := "linux/" + runtime.GOARCH
	if got := HostPlatform(); got != want {
		t.Errorf("HostPlatform() = %q, want %q", got, want)
	}
}

// TestBootstrapPackagesForBox covers the pacstrap/debootstrap dispatch + the nil-distro and
// neither-set fallbacks. Would fail if the function picked the wrong field or didn't guard a
// nil distro/nil sub-struct.
func TestBootstrapPackagesForBox(t *testing.T) {
	t.Run("nil distro returns nil", func(t *testing.T) {
		if got := BootstrapPackagesForBox(nil); got != nil {
			t.Errorf("BootstrapPackagesForBox(nil) = %v, want nil", got)
		}
	})

	t.Run("pacstrap-flavored distro returns Pacstrap.BasePackages", func(t *testing.T) {
		distro := &spec.ResolvedDistro{Pacstrap: &spec.Pacstrap{BasePackages: []string{"base", "linux", "sudo"}}}
		got := BootstrapPackagesForBox(distro)
		want := []string{"base", "linux", "sudo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("BootstrapPackagesForBox(pacstrap) = %v, want %v", got, want)
		}
	})

	t.Run("debootstrap-flavored distro returns Debootstrap.BasePackages", func(t *testing.T) {
		distro := &spec.ResolvedDistro{Debootstrap: &spec.Debootstrap{BasePackages: []string{"apt-utils", "sudo"}}}
		got := BootstrapPackagesForBox(distro)
		want := []string{"apt-utils", "sudo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("BootstrapPackagesForBox(debootstrap) = %v, want %v", got, want)
		}
	})

	t.Run("neither pacstrap nor debootstrap set returns nil", func(t *testing.T) {
		distro := &spec.ResolvedDistro{Version: "13"}
		if got := BootstrapPackagesForBox(distro); got != nil {
			t.Errorf("BootstrapPackagesForBox(neither) = %v, want nil", got)
		}
	})

	t.Run("pacstrap takes priority when both are (implausibly) set", func(t *testing.T) {
		distro := &spec.ResolvedDistro{
			Pacstrap:    &spec.Pacstrap{BasePackages: []string{"pac"}},
			Debootstrap: &spec.Debootstrap{BasePackages: []string{"deb"}},
		}
		got := BootstrapPackagesForBox(distro)
		if len(got) != 1 || got[0] != "pac" {
			t.Errorf("BootstrapPackagesForBox(both set) = %v, want [pac] (pacstrap checked first)", got)
		}
	})
}

// TestCharlyBinaryUpToDate exercises the real mtime-comparison walk against actual files on
// disk — would fail if the freshness check compared the wrong timestamps or didn't short-
// circuit correctly on missing paths.
func TestCharlyBinaryUpToDate(t *testing.T) {
	t.Run("missing binary returns false, nil (warrants a rebuild, not an error)", func(t *testing.T) {
		dir := t.TempDir()
		up, err := CharlyBinaryUpToDate(filepath.Join(dir, "no-such-binary"), dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if up {
			t.Error("expected false for a missing binary")
		}
	})

	t.Run("binary newer than every .go file is up to date", func(t *testing.T) {
		dir := t.TempDir()
		srcFile := filepath.Join(dir, "main.go")
		if err := os.WriteFile(srcFile, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(srcFile, old, old); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "charly")
		if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		// bin's mtime defaults to "now", strictly after srcFile's backdated mtime.
		up, err := CharlyBinaryUpToDate(bin, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !up {
			t.Error("expected true: binary is newer than every .go file")
		}
	})

	t.Run("a .go file touched after the binary means stale", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "charly")
		if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(bin, old, old); err != nil {
			t.Fatal(err)
		}
		srcFile := filepath.Join(dir, "changed.go")
		if err := os.WriteFile(srcFile, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// srcFile's mtime defaults to "now", strictly after bin's backdated mtime.
		up, err := CharlyBinaryUpToDate(bin, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if up {
			t.Error("expected false: a .go file is newer than the binary")
		}
	})

	t.Run("non-.go files newer than the binary are ignored", func(t *testing.T) {
		dir := t.TempDir()
		srcFile := filepath.Join(dir, "main.go")
		if err := os.WriteFile(srcFile, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(srcFile, old, old); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "charly")
		if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(bin, old, old); err != nil {
			t.Fatal(err)
		}
		// A freshly-written non-.go file is newer than both, but must be ignored.
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		up, err := CharlyBinaryUpToDate(bin, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !up {
			t.Error("expected true: only .go files should count toward staleness")
		}
	})

	t.Run("missing source dir surfaces the walk error", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "charly")
		if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := CharlyBinaryUpToDate(bin, filepath.Join(dir, "no-such-src-dir"))
		if err == nil {
			t.Fatal("expected a walk error for a missing source dir")
		}
	})
}

// TestRenderPacstrapExtraConf covers the pacman.conf-fragment renderer: the microarch
// Architecture directive, the per-repo Server/SigLevel blocks, and the nil/empty short
// circuits. Would fail if the microarch detection regex, the repo ordering, or the SigLevel
// conditional broke.
func TestRenderPacstrapExtraConf(t *testing.T) {
	t.Run("nil pacstrap returns empty string", func(t *testing.T) {
		if got := RenderPacstrapExtraConf(nil); got != "" {
			t.Errorf("RenderPacstrapExtraConf(nil) = %q, want empty", got)
		}
	})

	t.Run("no extra repos returns empty string", func(t *testing.T) {
		if got := RenderPacstrapExtraConf(&spec.Pacstrap{}); got != "" {
			t.Errorf("RenderPacstrapExtraConf(no repos) = %q, want empty", got)
		}
	})

	t.Run("repo without microarch server: no Architecture directive, SigLevel omitted when unset", func(t *testing.T) {
		p := &spec.Pacstrap{ExtraRepos: []spec.PacstrapRepo{
			{Name: "myrepo", Server: "https://example.com/$repo/os/$arch"},
		}}
		got := RenderPacstrapExtraConf(p)
		if strings.Contains(got, "[options]") {
			t.Errorf("RenderPacstrapExtraConf without microarch server should not emit [options], got %q", got)
		}
		want := "[myrepo]\nServer = https://example.com/$repo/os/$arch\n"
		if got != want {
			t.Errorf("RenderPacstrapExtraConf() = %q, want %q", got, want)
		}
	})

	t.Run("microarch server triggers an [options] Architecture directive + SigLevel is emitted when set", func(t *testing.T) {
		p := &spec.Pacstrap{ExtraRepos: []spec.PacstrapRepo{
			{Name: "cachyos-v3", Server: "https://mirror.cachyos.org/repo/x86_64_v3/$repo", SigLevel: "Never"},
		}}
		got := RenderPacstrapExtraConf(p)
		if !strings.HasPrefix(got, "[options]\nArchitecture = x86_64 x86_64_v3\n") {
			t.Errorf("RenderPacstrapExtraConf() = %q, want an [options] Architecture header naming x86_64_v3", got)
		}
		if !strings.Contains(got, "[cachyos-v3]\nServer = https://mirror.cachyos.org/repo/x86_64_v3/$repo\nSigLevel = Never\n") {
			t.Errorf("RenderPacstrapExtraConf() = %q, missing the repo block with SigLevel", got)
		}
	})

	t.Run("multiple distinct microarch tokens are deduped and sorted", func(t *testing.T) {
		p := &spec.Pacstrap{ExtraRepos: []spec.PacstrapRepo{
			{Name: "r1", Server: "https://a/x86_64_v3/repo"},
			{Name: "r2", Server: "https://b/x86_64_v2/repo"},
			{Name: "r3", Server: "https://c/x86_64_v3/repo-again"}, // duplicate token, must not repeat
		}}
		got := RenderPacstrapExtraConf(p)
		wantHeader := "[options]\nArchitecture = x86_64 x86_64_v2 x86_64_v3\n"
		if !strings.HasPrefix(got, wantHeader) {
			t.Errorf("RenderPacstrapExtraConf() header = %q, want prefix %q", got, wantHeader)
		}
	})
}

// TestRenderRuntimePacmanConf covers the booted-guest pacman.conf template renderer: the
// nil/empty short circuit, a template deriving its repo list from .ExtraRepos, a legacy
// verbatim (no template actions) config rendering to itself, and the parse-error path.
func TestRenderRuntimePacmanConf(t *testing.T) {
	t.Run("nil pacstrap returns empty string, no error", func(t *testing.T) {
		got, err := RenderRuntimePacmanConf(nil)
		if err != nil || got != "" {
			t.Errorf("RenderRuntimePacmanConf(nil) = (%q, %v), want (\"\", nil)", got, err)
		}
	})

	t.Run("blank runtime_pacman_conf returns empty string, no error", func(t *testing.T) {
		got, err := RenderRuntimePacmanConf(&spec.Pacstrap{RuntimePacmanConf: "   "})
		if err != nil || got != "" {
			t.Errorf("RenderRuntimePacmanConf(blank) = (%q, %v), want (\"\", nil)", got, err)
		}
	})

	t.Run("legacy verbatim config (no template actions) renders to itself", func(t *testing.T) {
		verbatim := "[options]\nArchitecture = auto\n[core]\nInclude = /etc/pacman.d/mirrorlist\n"
		got, err := RenderRuntimePacmanConf(&spec.Pacstrap{RuntimePacmanConf: verbatim})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != verbatim {
			t.Errorf("RenderRuntimePacmanConf(verbatim) = %q, want unchanged %q", got, verbatim)
		}
	})

	t.Run("template derives the repo list from .ExtraRepos", func(t *testing.T) {
		p := &spec.Pacstrap{
			RuntimePacmanConf: "{{ range .ExtraRepos }}[{{ .Name }}]\nServer = {{ .Server }}\n{{ end }}",
			ExtraRepos: []spec.PacstrapRepo{
				{Name: "cachyos", Server: "https://mirror.cachyos.org/repo"},
				{Name: "cachyos-extra", Server: "https://mirror.cachyos.org/extra"},
			},
		}
		got, err := RenderRuntimePacmanConf(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "[cachyos]\nServer = https://mirror.cachyos.org/repo\n" +
			"[cachyos-extra]\nServer = https://mirror.cachyos.org/extra\n"
		if got != want {
			t.Errorf("RenderRuntimePacmanConf(template) = %q, want %q", got, want)
		}
	})

	t.Run("malformed template text is a parse error", func(t *testing.T) {
		_, err := RenderRuntimePacmanConf(&spec.Pacstrap{RuntimePacmanConf: "{{ .Unclosed "})
		if err == nil {
			t.Fatal("expected a template parse error")
		}
		if !strings.Contains(err.Error(), "parsing runtime_pacman_conf template") {
			t.Errorf("error %q missing expected diagnostic text", err.Error())
		}
	})
}
