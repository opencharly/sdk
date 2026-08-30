package vmshared

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/opencharly/spec/spec"
)

// RenderInstallerSeed renders a distro's unattended-installer answers volume.
//
// The split is deliberate and is the whole design: the DISTRO owns the FORMAT (which files,
// with what names, in what syntax — archinstall JSON, kickstart, preseed, Subiquity
// autoinstall), and the VM ENTITY owns the DATA (this username, this disk, these SSH keys).
// A renderer that guessed the format from the distro name is the failure mode
// validateSourceDistro's own comment records as having burned this codebase already.
//
// Pure: no filesystem, no network, no crypt(). The caller hashes the password before
// building ctx, so a PLAINTEXT password never reaches a template, a log, or a temp file.
//
// Returns path-inside-the-volume -> rendered content, ready for WriteLabeledISO.
func RenderInstallerSeed(inst *spec.DistroInstaller, ctx spec.InstallerSeedContext) (map[string]string, error) {
	if inst == nil {
		return nil, fmt.Errorf("RenderInstallerSeed: distro declares no installer")
	}
	if len(inst.Files) == 0 {
		return nil, fmt.Errorf("RenderInstallerSeed: installer declares no files")
	}

	out := make(map[string]string, len(inst.Files))
	for _, f := range inst.Files {
		if f.Path == "" {
			return nil, fmt.Errorf("installer declares a file with no path")
		}
		// The `when:` guard is what lets ONE vocabulary express a whole optional matrix —
		// an authorized_keys file only when keys were given, a defer-provisioning sentinel
		// INSTEAD of credentials, an encryption marker only when encrypting — with no Go
		// branch anywhere. A file whose guard renders empty or "false" is simply absent.
		if f.When != "" {
			got, err := renderSeedTemplate("when:"+f.Path, f.When, ctx)
			if err != nil {
				return nil, err
			}
			if !seedGuardPasses(got) {
				continue
			}
		}
		body, err := renderSeedTemplate(f.Path, f.Content, ctx)
		if err != nil {
			return nil, err
		}
		// A mis-keyed template renders the literal "<no value>" rather than failing, which
		// would put a broken answers file on the volume and surface as an installer that
		// hangs at a prompt nobody is watching. Caught here instead.
		if strings.Contains(body, "<no value>") {
			return nil, fmt.Errorf("rendering %s: template referenced a field absent from the seed context (rendered %q)", f.Path, "<no value>")
		}
		if _, dup := out[f.Path]; dup {
			return nil, fmt.Errorf("installer declares %s twice", f.Path)
		}
		out[f.Path] = body
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("RenderInstallerSeed: every file was excluded by its when: guard")
	}
	return out, nil
}

// seedGuardPasses reports whether a rendered `when:` guard means "emit this file".
// Empty, "false" and "0" mean no; anything else means yes. Whitespace is trimmed because a
// template that spans lines for readability would otherwise never match "false".
func seedGuardPasses(rendered string) bool {
	switch strings.ToLower(strings.TrimSpace(rendered)) {
	case "", "false", "0", "no":
		return false
	}
	return true
}

// seedTemplateFuncs is the ARITHMETIC an answer template needs and nothing else.
//
// Installer answer formats state partition geometry as absolute numbers — archinstall
// indexes partition['size'] with no default and has no fill-remaining sentinel — so a
// template given only the disk size must be able to subtract the ESP from it. Go templates
// cannot add or subtract at all without this.
//
// Deliberately FOUR functions and no more. This is not a general-purpose expression
// language: everything a seed needs is integer byte arithmetic, and each addition here is
// a thing a distro author can hide logic in, where it is invisible to every gate. `mib` and
// `gib` exist so a template says 2 GiB rather than 2147483648, which is the number a human
// can check against upstream's own script.
var seedTemplateFuncs = template.FuncMap{
	"add": func(a, b int64) int64 { return a + b },
	"sub": func(a, b int64) int64 { return a - b },
	"mib": func(n int64) int64 { return n * 1024 * 1024 },
	"gib": func(n int64) int64 { return n * 1024 * 1024 * 1024 },
}

func renderSeedTemplate(name, text string, ctx spec.InstallerSeedContext) (string, error) {
	t, err := template.New(name).Funcs(seedTemplateFuncs).Option("missingkey=default").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, ctx); err != nil {
		return "", fmt.Errorf("rendering %s: %w", name, err)
	}
	return b.String(), nil
}

// SeedFileNames returns the rendered file paths in sorted order. Only for diagnostics and
// tests — the ISO writer sorts internally so its output is reproducible.
func SeedFileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
