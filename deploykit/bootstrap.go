package deploykit

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// RenderDnfConfWrite renders the dnf download-tuning conf snippet (appended to
// /etc/dnf/dnf.conf before the bootstrap install). Relocated from charly (P8).
func RenderDnfConfWrite(d *spec.Dnf) string {
	if d == nil {
		return ""
	}
	var lines []string
	if d.MaxParallelDownloads > 0 {
		lines = append(lines, fmt.Sprintf("max_parallel_downloads=%d", d.MaxParallelDownloads))
	}
	if d.Fastestmirror {
		lines = append(lines, "fastestmirror=True")
	}
	if len(lines) == 0 {
		return ""
	}
	body := strings.Join(lines, "\\n") + "\\n"
	return fmt.Sprintf("printf '%s' >> /etc/dnf/dnf.conf && \\\n    ", body)
}

// WriteBootstrap emits the base-image bootstrap block: cache mounts, dnf tuning,
// bootstrap package install, distro workarounds, the go-task install, user/group
// creation (or adoption), and WORKDIR. Relocated from charly (P8); byte-identical.
func (g *Generator) WriteBootstrap(b *strings.Builder, img *buildkit.ResolvedBox) {
	b.WriteString("# Bootstrap\n")

	var distroDef *buildkit.DistroDef
	if img.DistroConfig != nil {
		distroDef = img.DistroConfig.ResolveDistro(img.Distro)
	}

	b.WriteString("RUN ")
	var cacheMounts []spec.CacheMount
	if distroDef != nil {
		cacheMounts = distroDef.Bootstrap.CacheMount
	} else if img.DistroDef != nil {
		if formatDef, ok := img.DistroDef.Format[img.Pkg]; ok {
			cacheMounts = formatDef.CacheMount
		}
	}
	b.WriteString(buildkit.RenderCacheMounts(cacheMounts, -1, 0, " \\\n    ", true))

	if distroDef != nil {
		b.WriteString(RenderDnfConfWrite(distroDef.Dnf))
	}

	if distroDef != nil && distroDef.Bootstrap.InstallCmd != "" && len(distroDef.Bootstrap.Package) > 0 {
		fmt.Fprintf(b, "%s %s && \\\n    ", distroDef.Bootstrap.InstallCmd, strings.Join(distroDef.Bootstrap.Package, " "))
	}

	if distroDef != nil {
		for _, w := range distroDef.Workarounds {
			b.WriteString(w + " && \\\n    ")
		}
	}

	b.WriteString("{ [ -L /usr/local ] && mkdir -p \"$(readlink /usr/local)\"; mkdir -p /usr/local/bin; } && \\\n")
	b.WriteString("    ARCH=$(uname -m) && \\\n")
	b.WriteString("    case \"$ARCH\" in x86_64) ARCH=amd64;; aarch64) ARCH=arm64;; esac && \\\n")
	b.WriteString("    curl -fsSL \"https://github.com/go-task/task/releases/latest/download/task_linux_${ARCH}.tar.gz\" | tar -xzf - -C /usr/local/bin task\n\n")

	if img.UserAdopted {
		fmt.Fprintf(b, "# User %s (uid=%d) adopted from base image (declared in the embedded distro.base_user) — no useradd needed\n\n", img.User, img.UID)
	} else {
		fmt.Fprintf(b, "RUN if ! getent passwd %d >/dev/null 2>&1; then \\\n", img.UID)
		fmt.Fprintf(b, "      (getent group %d >/dev/null 2>&1 || groupadd -g %d %s) && \\\n", img.GID, img.GID, img.User)
		fmt.Fprintf(b, "      useradd -m -u %d -g %d -s /bin/bash %s; \\\n", img.UID, img.GID, img.User)
		b.WriteString("    fi\n\n")
	}

	fmt.Fprintf(b, "WORKDIR %s\n\n", img.Home)
}

// EmitBaseBootstrap emits the base-stage bootstrap: a builder-rootfs ADD for a
// `from: builder:` base, then either the full WriteBootstrap (external base) or a
// USER root reset (internal base). Relocated from charly (P8); byte-identical.
func (g *Generator) EmitBaseBootstrap(b *strings.Builder, boxName string, img *buildkit.ResolvedBox) {
	if after, ok := strings.CutPrefix(img.From, "builder:"); ok {
		builderName := after
		fmt.Fprintf(b, "ADD .build/%s/%s.tar.gz /\n\n", boxName, builderName)
	}
	if img.IsExternalBase && !strings.HasPrefix(img.From, "builder:") {
		g.WriteBootstrap(b, img)
	} else {
		// Internal base or builder rootfs - reset to root for candy processing
		b.WriteString("USER root\n\n")
	}
}
