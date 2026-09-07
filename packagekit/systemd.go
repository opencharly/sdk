package packagekit

// systemd.go — render the packaging section's systemd units + preset files into
// nfpm files.Contents. The NON-AUTOSTARTING contract: units are INSTALLED but
// never ENABLED (no post-install enable script ships); the preset files
// guarantee a distro that runs `systemctl preset` on package install (Debian's
// dh_installsystemd) cannot auto-enable them. The operator starts a unit on
// demand with `systemctl start <name>` / `systemctl --user start <name>`.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goreleaser/nfpm/v2/files"
	"github.com/opencharly/spec/spec"
)

// systemdUnitContents renders every pkg.Systemd unit + the scope preset files
// into nfpm contents. system scope → /usr/lib/systemd/system/<name>.service;
// user scope → /usr/lib/systemd/user/<name>.service. The preset files
// (/usr/lib/systemd/{system,user}-preset/50-charly.preset) carry
// `disable <name>.service` for every unit of that scope. The rendered bodies
// are written to a temp dir (nfpm contents reference on-disk sources) and
// referenced by Destination; the temp dir lives for the build process.
func systemdUnitContents(pkg *spec.Packaging) (files.Contents, error) {
	if len(pkg.Systemd) == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "charly-systemd-*")
	if err != nil {
		return nil, fmt.Errorf("create systemd temp dir: %w", err)
	}
	var contents files.Contents
	var systemPreset, userPreset []string
	for _, u := range pkg.Systemd {
		if u == nil {
			continue
		}
		if u.Name == "" || u.Exec == "" {
			return nil, fmt.Errorf("packaging.systemd unit: name and exec are required")
		}
		var unitDir, wantedBy string
		switch u.Scope {
		case "system":
			unitDir, wantedBy = "/usr/lib/systemd/system", "multi-user.target"
			systemPreset = append(systemPreset, u.Name+".service")
		case "user":
			unitDir, wantedBy = "/usr/lib/systemd/user", "default.target"
			userPreset = append(userPreset, u.Name+".service")
		default:
			return nil, fmt.Errorf("packaging.systemd unit %q: scope must be system or user, got %q", u.Name, u.Scope)
		}
		src := filepath.Join(dir, u.Scope+"-"+u.Name+".service")
		if err := os.WriteFile(src, []byte(renderSystemdUnit(u, wantedBy)), 0o644); err != nil {
			return nil, fmt.Errorf("write systemd unit %s: %w", src, err)
		}
		contents = append(contents, &files.Content{
			Source:      src,
			Destination: filepath.Join(unitDir, u.Name+".service"),
			FileInfo:    &files.ContentFileInfo{Mode: 0o644},
		})
	}
	if len(systemPreset) > 0 {
		src := filepath.Join(dir, "50-charly.system.preset")
		if err := os.WriteFile(src, []byte(renderPreset(systemPreset)), 0o644); err != nil {
			return nil, fmt.Errorf("write system preset: %w", err)
		}
		contents = append(contents, &files.Content{
			Source:      src,
			Destination: "/usr/lib/systemd/system-preset/50-charly.preset",
			FileInfo:    &files.ContentFileInfo{Mode: 0o644},
		})
	}
	if len(userPreset) > 0 {
		src := filepath.Join(dir, "50-charly.user.preset")
		if err := os.WriteFile(src, []byte(renderPreset(userPreset)), 0o644); err != nil {
			return nil, fmt.Errorf("write user preset: %w", err)
		}
		contents = append(contents, &files.Content{
			Source:      src,
			Destination: "/usr/lib/systemd/user-preset/50-charly.preset",
			FileInfo:    &files.ContentFileInfo{Mode: 0o644},
		})
	}
	return contents, nil
}

// renderSystemdUnit renders one unit file. The [Install] section is present so
// `systemctl enable` works when the operator opts in — but no post-install
// enable script ships (the non-autostarting contract).
func renderSystemdUnit(u *spec.PackagingSystemdUnit, wantedBy string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	if u.Description != "" {
		fmt.Fprintf(&b, "Description=%s\n", u.Description)
	}
	if len(u.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", strings.Join(u.After, " "))
	}
	if len(u.Wants) > 0 {
		fmt.Fprintf(&b, "Wants=%s\n", strings.Join(u.Wants, " "))
	}
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	if u.Working_directory != "" {
		fmt.Fprintf(&b, "WorkingDirectory=%s\n", u.Working_directory)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", u.Exec)
	restart := u.Restart
	if restart == "" {
		restart = "on-failure"
	}
	fmt.Fprintf(&b, "Restart=%s\n", restart)
	b.WriteString("RestartSec=5\n")
	if len(u.Environment) > 0 {
		keys := make([]string, 0, len(u.Environment))
		for k := range u.Environment {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "Environment=%s=%s\n", k, u.Environment[k])
		}
	}
	b.WriteString("\n[Install]\n")
	fmt.Fprintf(&b, "WantedBy=%s\n", wantedBy)
	return b.String()
}

// renderPreset renders a preset file: `disable <name>.service` per unit of the
// scope, so a distro that runs `systemctl preset` on install cannot
// auto-enable the units.
func renderPreset(units []string) string {
	var b strings.Builder
	for _, u := range units {
		fmt.Fprintf(&b, "disable %s\n", u)
	}
	return b.String()
}
