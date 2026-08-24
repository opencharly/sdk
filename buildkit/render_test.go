package buildkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestRpmTemplateWithModules / TestPacTemplateBasic / TestAurInstallPhase were
// relocated here from charly/generate_test.go (K3 cone2 test closure): the tests
// exercise RenderTemplate — already a pure buildkit function taking only
// `*spec.Format`/`*spec.InstallContext` — but the ORIGINAL charly-side tests built
// their `*spec.Format` fixture via `testDistroDef(tag).Format["fmt"]`, which loads
// charly/testdata/build.yml through charly's own `LoadBuildConfigForBox` (a
// charly-core-only loader, unreachable from sdk/buildkit). Each fixture below is a
// literal `*spec.Format` carrying the SAME `phase.install.container` TEXT the testdata
// distro vocabulary declares for that format (rpm/pac/aur), so the render
// assertions are byte-for-byte the same test, decoupled from the charly loader.

// TestUnifiedInstallBodyRendersBothVenues proves the R3 shape end to end: a
// venue-agnostic `phase.install.install` body (written with `&& \`
// continuations, valid plain shell AND inside a Dockerfile RUN) renders as a
// plain shell command for the host venue and as a BuildKit RUN for the
// container venue — one canonical body, venue applied at render by
// spec.FormatPhaseTemplate (sdk#…).
func TestUnifiedInstallBodyRendersBothVenues(t *testing.T) {
	rpm := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/libdnf5", Sharing: "locked"}},
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Install: `{{- range .Repos}}{{if .url}}
    dnf5 config-manager addrepo --from-repofile={{quote .url}} 2>/dev/null || true && \
{{- end}}{{end}}
    dnf install -y{{range .Options}} {{.}}{{end}}{{range .Packages}} {{.}}{{end}}
`},
		},
	}
	ctx := &spec.InstallContext{
		CacheMounts: rpm.CacheMount,
		Packages:    []string{"charly"},
		Repos: []map[string]any{{
			"name": "charly",
			"url":  "https://opencharly.github.io/charly-fedora/amd64/",
		}},
	}

	// Host venue: the body verbatim, runnable as plain shell.
	hostTmpl := FormatPhaseTemplate(rpm, spec.PhaseInstall, spec.VenueHostNative)
	host, err := RenderTemplate("rpm-host-unified", hostTmpl, ctx)
	if err != nil {
		t.Fatalf("host render error: %v", err)
	}
	if !strings.Contains(host, "dnf5 config-manager addrepo --from-repofile=\"https://opencharly.github.io/charly-fedora/amd64/\"") {
		t.Errorf("host render missing the repo add; got:\n%s", host)
	}
	if !strings.Contains(host, "dnf install -y charly") {
		t.Errorf("host render missing the package install; got:\n%s", host)
	}

	// Container venue: the body under a BuildKit RUN + cacheMounts.
	containerTmpl := FormatPhaseTemplate(rpm, spec.PhaseInstall, spec.VenueContainerBuilder)
	container, err := RenderTemplate("rpm-container-unified", containerTmpl, ctx)
	if err != nil {
		t.Fatalf("container render error: %v", err)
	}
	if !strings.Contains(container, "RUN --mount=type=cache,id=charly-var-cache-libdnf5,dst=/var/cache/libdnf5,sharing=locked") {
		t.Errorf("container render missing the cacheMounts RUN prefix; got:\n%s", container)
	}
	if !strings.Contains(container, "dnf install -y charly") {
		t.Errorf("container render missing the package install; got:\n%s", container)
	}
}

// TestPacUnifiedInstallKeyBranchUsesHasPrefix proves the hasPrefix template
// func routes a repo key: that is an http(s) URL (a published key FILE — curl +
// pacman-key --add, the deterministic keyserver-free mechanism) from a bare
// fingerprint (pacman-key --recv-keys). This test FAILS without the hasPrefix
// func (the template errors: function "hasPrefix" not defined) — the
// failing-without-fix coverage for the render.go change.
func TestPacUnifiedInstallKeyBranchUsesHasPrefix(t *testing.T) {
	pac := &spec.Format{
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Install: `{{- range .Repos}}
    printf '[{{.name}}]\nServer = {{.server}}\nSigLevel = {{default .siglevel "Optional TrustAll"}}\n' >> /etc/pacman.conf && \
{{- if .key}}
{{- if hasPrefix (printf "%v" .key) "http"}}
    curl -fsSL {{quote .key}} -o /tmp/{{.name}}.gpg && pacman-key --add /tmp/{{.name}}.gpg && \
{{- else}}
    pacman-key --recv-keys {{.key}} && pacman-key --lsign-key {{.key}} && \
{{- end}}
{{- end}}
{{- end}}
    pacman -Syu --noconfirm --needed{{range .Packages}} {{.}}{{end}}
`},
		},
	}

	urlCtx := &spec.InstallContext{
		Packages: []string{"charly"},
		Repos: []map[string]any{{
			"name":   "charly",
			"server": "https://opencharly.github.io/charly-arch/amd64/",
			"key":    "https://opencharly.github.io/charly-arch/charly.gpg",
		}},
	}
	urlOut, err := RenderTemplate("pac-url-key", pac.Phases.Install.Install, urlCtx)
	if err != nil {
		t.Fatalf("URL-key render error (hasPrefix not wired?): %v", err)
	}
	if !strings.Contains(urlOut, "curl -fsSL \"https://opencharly.github.io/charly-arch/charly.gpg\" -o /tmp/charly.gpg && pacman-key --add") {
		t.Errorf("URL key: want the curl+add branch, got:\n%s", urlOut)
	}
	if strings.Contains(urlOut, "pacman-key --recv-keys") {
		t.Errorf("URL key: must NOT take the recv-keys branch; got:\n%s", urlOut)
	}

	fprCtx := &spec.InstallContext{
		Packages: []string{"charly"},
		Repos: []map[string]any{{
			"name":   "charly",
			"server": "https://opencharly.github.io/charly-arch/amd64/",
			"key":    "978DFF11A951A830F7ADA2D4062B073E9D1BAE2E",
		}},
	}
	fprOut, err := RenderTemplate("pac-fpr-key", pac.Phases.Install.Install, fprCtx)
	if err != nil {
		t.Fatalf("fingerprint-key render error: %v", err)
	}
	if !strings.Contains(fprOut, "pacman-key --recv-keys 978DFF11A951A830F7ADA2D4062B073E9D1BAE2E") {
		t.Errorf("fingerprint key: want the recv-keys branch, got:\n%s", fprOut)
	}
	if strings.Contains(fprOut, "curl -fsSL") {
		t.Errorf("fingerprint key: must NOT take the curl branch; got:\n%s", fprOut)
	}
}

// TestRpmHostCellHandlesRepos proves the rpm phase.install.host cell renders the
// repo setup the container cell has always had: the .repo file write (with the
// gpgkey), the key import, and --enable-repo on the install line. Regression for
// the check-fedora-vm `No match for argument: charly` — the host cell was bare
// `dnf install -y ...` with no repo handling, so a candy's distro repo was never
// added on the host/VM venue. The fixture is a literal copy of the host cell in
// charly/charly.yml (the embedded vocabulary), mirroring TestRpmTemplateWithModules'
// container-cell fixture; the charly-side presence check lives in
// charly/format_config_test.go (charly/ core must not import sdk).
func TestRpmHostCellHandlesRepos(t *testing.T) {
	rpm := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/libdnf5", Sharing: "locked"}},
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Host: `{{- range .Repos}}{{if .rpm}}
    dnf install -y {{quote .rpm}} && \
{{- end}}{{end}}
{{- if or (anyRepoHasURL .Repos) .Copr}}
    dnf install -y dnf5-plugins && \
{{- end}}
{{- range .Repos}}{{if .url}}
{{- if hasSuffix (printf "%s" .url) ".repo"}}
    dnf5 config-manager addrepo --from-repofile={{quote .url}} 2>/dev/null || true && \
    dnf5 config-manager setopt {{quote (printf "%s.enabled=0" .name)}} && \
{{- else}}
    printf '%s\n' '[{{.name}}]' 'name={{.name}}' 'baseurl={{.url}}' 'enabled=0' 'gpgcheck={{if eq (default .gpgcheck "true") "false"}}0{{else}}1{{end}}'{{if .repo_gpgcheck}} 'repo_gpgcheck={{if eq (printf "%v" .repo_gpgcheck) "false"}}0{{else}}1{{end}}'{{end}}{{if .gpgkey}} 'gpgkey={{.gpgkey}}'{{end}} > /etc/yum.repos.d/{{.name}}.repo && \
{{- end}}
{{- if .gpgkey}}
    rpm --import {{.gpgkey}} || true && \
{{- end}}
{{- end}}{{end}}
    dnf install -y{{range .Options}} {{.}}{{end}}{{range .Repos}}{{if .url}} --enable-repo={{quote .name}}{{end}}{{end}}{{range .Exclude}} --exclude='{{.}}'{{end}}{{range .Packages}} {{.}}{{end}}
`},
		},
	}
	ctx := &spec.InstallContext{
		CacheMounts: rpm.CacheMount,
		Packages:    []string{"charly"},
		Repos: []map[string]any{{
			"name":   "charly",
			"url":    "https://opencharly.github.io/charly-fedora/amd64/",
			"gpgkey": "https://opencharly.github.io/charly-fedora/RPM-GPG-KEY-charly",
		}},
	}
	out, err := RenderTemplate("rpm-host-test", rpm.Phases.Install.Host, ctx)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	for _, want := range []string{
		"/etc/yum.repos.d/charly.repo",
		"gpgkey=https://opencharly.github.io/charly-fedora/RPM-GPG-KEY-charly",
		"rpm --import https://opencharly.github.io/charly-fedora/RPM-GPG-KEY-charly",
		"--enable-repo=\"charly\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rpm host install missing %q; got:\n%s", want, out)
		}
	}
}

// TestPacHostCellHandlesRepos proves the pac phase.install.host cell renders the
// Keys/Repos setup the container cell has always had: the repo appended to
// /etc/pacman.conf and the key imported. Regression for the check-cachyos-vm
// `target not found: charly` — the host cell was bare `pacman -Syu ...` with no
// repo handling, so a candy's distro repo was never added on the host/VM venue.
// The fixture is a literal copy of the host cell in charly/charly.yml; the
// charly-side presence check lives in charly/format_config_test.go.
func TestPacHostCellHandlesRepos(t *testing.T) {
	pac := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/pacman/pkg", Sharing: "locked"}},
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Host: `{{- range .Keys}}
    pacman-key --recv-keys {{.}} && pacman-key --lsign-key {{.}} && \
{{- end}}
{{- range .Repos}}
    printf '[{{.name}}]\nServer = {{.server}}\nSigLevel = {{default .siglevel "Optional TrustAll"}}\n' >> /etc/pacman.conf && \
{{- if .key}}
    pacman-key --recv-keys {{.key}} && pacman-key --lsign-key {{.key}} && \
{{- end}}
{{- end}}
    pacman -Syu --noconfirm --needed{{range .Options}} {{.}}{{end}}{{range .Packages}} {{.}}{{end}}
`},
		},
	}
	ctx := &spec.InstallContext{
		CacheMounts: pac.CacheMount,
		Packages:    []string{"charly"},
		Keys:        []string{"978DFF11A951A830F7ADA2D4062B073E9D1BAE2E"},
		Repos: []map[string]any{{
			"name":     "charly",
			"server":   "https://opencharly.github.io/charly-arch/amd64/",
			"key":      "978DFF11A951A830F7ADA2D4062B073E9D1BAE2E",
			"siglevel": "Required DatabaseOptional",
		}},
	}
	out, err := RenderTemplate("pac-host-test", pac.Phases.Install.Host, ctx)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	for _, want := range []string{
		"Server = https://opencharly.github.io/charly-arch/amd64/",
		"/etc/pacman.conf",
		"pacman-key --recv-keys 978DFF11A951A830F7ADA2D4062B073E9D1BAE2E",
		"pacman -Syu --noconfirm --needed charly",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pac host install missing %q; got:\n%s", want, out)
		}
	}
}

func TestRpmTemplateWithModules(t *testing.T) {
	rpm := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/libdnf5", Sharing: "locked"}},
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Container: `RUN {{cacheMounts .CacheMounts}} \
{{- range .Repos}}{{if .rpm}}
    dnf install -y {{quote .rpm}} && \
{{- end}}{{end}}
{{- range .Repos}}{{if .url}}
    dnf5 config-manager addrepo --from-repofile={{quote .url}} 2>/dev/null || true && \
    dnf5 config-manager setopt {{quote (printf "%s.enabled=0" .name)}} && \
{{- if .gpgkey}}
    rpm --import {{.gpgkey}} || true && \
{{- end}}
{{- if eq (default .gpgcheck "true") "false"}}
    dnf5 config-manager setopt {{quote (printf "%s.gpgcheck=0" .name)}} && \
{{- end}}{{end}}{{end}}
{{- range .Copr}}
    dnf5 copr enable -y {{.}} && \
{{- end}}
{{- range .Modules}}
    dnf module reset -y {{splitFirst . ":"}} && \
    dnf module enable -y {{.}} && \
{{- end}}
    dnf install -y{{range .Options}} {{.}}{{end}}
{{- range .Repos}}{{if .url}} --enable-repo={{quote .name}}{{end}}{{end}}
{{- range .Exclude}} --exclude='{{.}}'{{end}}
{{- range .Packages}} \
      {{.}}{{end}}
{{- range .Copr}} && \
    dnf5 config-manager setopt "copr:copr.fedorainfracloud.org:{{replace . "/" ":"}}.enabled=0"
{{- end}}
`},
		},
	}
	ctx := &spec.InstallContext{
		CacheMounts: rpm.CacheMount,
		Packages:    []string{"valkey"},
		Modules:     []string{"valkey:remi-9.0"},
	}
	out, err := RenderTemplate("rpm-test", rpm.Phases.Install.Container, ctx)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if !strings.Contains(out, "dnf module reset -y valkey") {
		t.Error("should contain dnf module reset")
	}
	if !strings.Contains(out, "dnf module enable -y valkey:remi-9.0") {
		t.Error("should contain dnf module enable")
	}
	if !strings.Contains(out, "dnf install -y") {
		t.Error("should contain dnf install")
	}
	if !strings.Contains(out, "valkey") {
		t.Error("should contain package name")
	}
}

func TestPacTemplateBasic(t *testing.T) {
	pac := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/pacman/pkg", Sharing: "locked"}},
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Container: `RUN {{cacheMounts .CacheMounts}} \
{{- range .Keys}}
    pacman-key --recv-keys {{.}} && pacman-key --lsign-key {{.}} && \
{{- end}}
{{- range .Repos}}
    printf '[{{.name}}]\nServer = {{.server}}\nSigLevel = {{default .siglevel "Optional TrustAll"}}\n' >> /etc/pacman.conf && \
{{- if .key}}
    pacman-key --recv-keys {{.key}} && pacman-key --lsign-key {{.key}} && \
{{- end}}
{{- end}}
    pacman -Syu --noconfirm
{{- range .Options}} {{.}}{{end}}
{{- range .Packages}} \
      {{.}}{{end}}
`},
		},
	}
	ctx := &spec.InstallContext{
		CacheMounts: pac.CacheMount,
		Packages:    []string{"neovim", "ripgrep"},
	}
	out, err := RenderTemplate("pac-test", pac.Phases.Install.Container, ctx)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if !strings.Contains(out, "pacman -Syu --noconfirm") {
		t.Error("should contain pacman -Syu --noconfirm")
	}
	if !strings.Contains(out, "neovim") {
		t.Error("should contain neovim")
	}
	if !strings.Contains(out, "/var/cache/pacman/pkg") {
		t.Error("should use pacman cache mount")
	}
}

func TestAurInstallPhase(t *testing.T) {
	aur := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/pacman/pkg", Sharing: "locked"}},
		Phases: &spec.PhaseSet{
			Install: &spec.PhaseTemplates{Container: `COPY --from={{.StageName}} /tmp/aur-pkgs/ /tmp/aur-pkgs/
RUN {{cacheMounts .CacheMounts}} \
    pacman -U --noconfirm /tmp/aur-pkgs/*.pkg.tar.zst && \
    rm -rf /tmp/aur-pkgs
`},
		},
	}
	ctx := &spec.InstallContext{
		CacheMounts: aur.CacheMount,
		StageName:   "my-tool-aur-build",
	}
	out, err := RenderTemplate("aur-install-test", aur.Phases.Install.Container, ctx)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	if !strings.Contains(out, "COPY --from=my-tool-aur-build /tmp/aur-pkgs/") {
		t.Error("should COPY from AUR build stage")
	}
	if !strings.Contains(out, "pacman -U --noconfirm") {
		t.Error("should install with pacman -U")
	}
}
