package buildkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestRpmTemplateWithModules / TestPacTemplateBasic / TestAurInstallTemplate were
// relocated here from charly/generate_test.go (K3 cone2 test closure): the tests
// exercise RenderTemplate — already a pure buildkit function taking only
// `*spec.Format`/`*spec.InstallContext` — but the ORIGINAL charly-side tests built
// their `*spec.Format` fixture via `testDistroDef(tag).Format["fmt"]`, which loads
// charly/testdata/build.yml through charly's own `LoadBuildConfigForBox` (a
// charly-core-only loader, unreachable from sdk/buildkit). Each fixture below is a
// literal `*spec.Format` carrying the SAME `install_template:` TEXT the testdata
// distro vocabulary declares for that format (rpm/pac/aur), so the render
// assertions are byte-for-byte the same test, decoupled from the charly loader.

func TestRpmTemplateWithModules(t *testing.T) {
	rpm := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/libdnf5", Sharing: "locked"}},
		InstallTemplate: `RUN {{cacheMounts .CacheMounts}} \
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
`,
	}
	ctx := &spec.InstallContext{
		CacheMounts: rpm.CacheMount,
		Packages:    []string{"valkey"},
		Modules:     []string{"valkey:remi-9.0"},
	}
	out, err := RenderTemplate("rpm-test", rpm.InstallTemplate, ctx)
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
		InstallTemplate: `RUN {{cacheMounts .CacheMounts}} \
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
`,
	}
	ctx := &spec.InstallContext{
		CacheMounts: pac.CacheMount,
		Packages:    []string{"neovim", "ripgrep"},
	}
	out, err := RenderTemplate("pac-test", pac.InstallTemplate, ctx)
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

func TestAurInstallTemplate(t *testing.T) {
	aur := &spec.Format{
		CacheMount: []spec.CacheMount{{Dst: "/var/cache/pacman/pkg", Sharing: "locked"}},
		InstallTemplate: `COPY --from={{.StageName}} /tmp/aur-pkgs/ /tmp/aur-pkgs/
RUN {{cacheMounts .CacheMounts}} \
    pacman -U --noconfirm /tmp/aur-pkgs/*.pkg.tar.zst && \
    rm -rf /tmp/aur-pkgs
`,
	}
	ctx := &spec.InstallContext{
		CacheMounts: aur.CacheMount,
		StageName:   "my-tool-aur-build",
	}
	out, err := RenderTemplate("aur-install-test", aur.InstallTemplate, ctx)
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
