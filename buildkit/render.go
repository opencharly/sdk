// Package buildkit holds the pure Containerfile render/compute machinery that
// any build front-end (the charly host engine, an out-of-tree build plugin) can
// import — the SDK-library half of the "core is the kernel; every capability is a
// plugin" architecture. This file is the format/builder TEMPLATE render surface:
// cache-mount rendering, the shared template.FuncMap, the text/template driver,
// and the InstallContext constructor. It depends only on sdk/spec (the wire types)
// and sdk/kit (shell quoting) — never on charly core.
package buildkit

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// CacheMount is the RENDER-time cache-mount value (a resolved Containerfile
// `--mount=type=cache,…` directive). Distinct from the authoring
// spec.CacheMount (dst/sharing/owned): this one carries the concrete uid/gid the
// owned form resolved to (sentinel UID == -1 = the shared form). Produced by
// SharedCacheMount/OwnedCacheMount from the authoring def.
type CacheMount struct {
	Dst     string // Container-side mount path; canonical id source.
	Sharing string // "locked"/"shared"/"private" for shared caches; ignored for owned.
	UID     int    // For owned caches; sentinel -1 means "shared" form.
	GID     int
}

// SharedCacheMount returns a cache-mount value for root-installed system
// caches (dnf/apt/pacman/downloads). Sharing defaults to "locked".
func SharedCacheMount(dst, sharing string) CacheMount {
	if sharing == "" {
		sharing = "locked"
	}
	return CacheMount{Dst: dst, Sharing: sharing, UID: -1}
}

// OwnedCacheMount returns a cache-mount value for non-root user caches
// (pixi/npm/cargo). UID becomes part of the id namespace.
func OwnedCacheMount(dst string, uid, gid int) CacheMount {
	return CacheMount{Dst: dst, UID: uid, GID: gid}
}

// String renders the CacheMount as a Containerfile `--mount=type=cache,...`
// flag, delegating to spec.FormatCacheMount — the single-source formatter (#55
// coneK3tasks relocated the slice renderers + the per-mount formatter to spec/spec so
// charly core can render cache mounts without an sdk/buildkit import). The `id=` field
// is derived from Dst (and UID for owned caches), keeping the cache stable across
// layer-hash changes during iterative builds.
func (m CacheMount) String() string {
	return spec.FormatCacheMount(m.Dst, m.Sharing, m.UID, m.GID)
}

// TemplateFuncs provides helper functions for format/builder templates. The cache-mount
// funcs delegate to spec.RenderCacheMounts/RenderCacheMountsAuto (relocated to spec/spec,
// #55 coneK3tasks) — buildkit imports spec, so the shared spec primitives are the single
// source (R3) for every render site.
var TemplateFuncs = template.FuncMap{
	"cacheMounts": func(m []spec.CacheMount) string { return spec.RenderCacheMounts(m, -1, 0, " \\\n    ", false) },
	"cacheMountsOwned": func(m []spec.CacheMount, uid, gid int) string {
		return spec.RenderCacheMounts(m, uid, gid, " \\\n    ", true)
	},
	"cacheMountsAuto": func(m []spec.CacheMount, uid, gid int) string {
		return spec.RenderCacheMountsAuto(m, uid, gid, " \\\n    ", false)
	},

	// quote returns a shell-safe quoted string.
	"quote": func(s any) string {
		return fmt.Sprintf("%q", fmt.Sprint(s))
	},

	// default returns the value if non-empty, otherwise the fallback.
	"default": func(val, fallback any) any {
		s := fmt.Sprint(val)
		if s == "" || s == "<nil>" {
			return fallback
		}
		return val
	},

	// splitFirst splits a string by sep and returns the first part.
	"splitFirst": func(s, sep string) string {
		parts := strings.SplitN(s, sep, 2)
		return parts[0]
	},

	// replace performs string replacement.
	"replace": strings.ReplaceAll,

	// join joins a string slice with a separator.
	"join": func(elems any, sep string) string {
		switch v := elems.(type) {
		case []string:
			return strings.Join(v, sep)
		case []any:
			strs := make([]string, len(v))
			for i, e := range v {
				strs[i] = fmt.Sprint(e)
			}
			return strings.Join(strs, sep)
		default:
			return fmt.Sprint(elems)
		}
	},

	// printf is a template-accessible Sprintf.
	"printf": fmt.Sprintf,

	// shquote shell-quotes a single argument (delegates to kit.ShQuoteArg), so a
	// host-venue builder/install template can emit a package or path argument
	// safely. Used by the aur builder's phase.install.host cell to quote each
	// AUR package name passed to yay.
	"shquote": kit.ShQuoteArg,

	// hasSuffix reports whether a string ends with the given suffix.
	// Used by the rpm install template to distinguish a URL pointing at
	// a `.repo` configuration file (consumable by `dnf5 config-manager
	// addrepo --from-repofile`) from a yum baseurl that needs an
	// inline `.repo` file generated locally.
	"hasSuffix": strings.HasSuffix,

	// anyRepoHasURL reports whether any repo entry declares a `url` key
	// (i.e. needs `dnf5 config-manager addrepo`). Lets install_template
	// conditionally install `dnf5-plugins` — necessary on bootc bases
	// which strip it from the default install.
	"anyRepoHasURL": func(repos []map[string]any) bool {
		for _, r := range repos {
			if u, ok := r["url"]; ok && fmt.Sprint(u) != "" {
				return true
			}
		}
		return false
	},
}

// RenderTemplate renders a Go text/template with the given context.
func RenderTemplate(name, tmplStr string, ctx any) (string, error) {
	if tmplStr == "" {
		return "", nil
	}
	tmpl, err := template.New(name).Funcs(TemplateFuncs).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", name, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, ctx); err != nil {
		return "", fmt.Errorf("executing template %q: %w", name, err)
	}
	return b.String(), nil
}

// NewInstallContext creates a spec.InstallContext from a generic PackageSection.
func NewInstallContext(section map[string]any, cacheMounts []spec.CacheMount) *spec.InstallContext {
	ctx := &spec.InstallContext{
		CacheMounts: cacheMounts,
	}

	if pkgs, ok := section["package"]; ok {
		ctx.Packages = ToStringSlice(pkgs)
	}
	if repos, ok := section["repo"]; ok {
		ctx.Repos = ToMapSlice(repos)
	}
	if opts, ok := section["options"]; ok {
		ctx.Options = ToStringSlice(opts)
	}
	if copr, ok := section["copr"]; ok {
		ctx.Copr = ToStringSlice(copr)
	}
	if mods, ok := section["module"]; ok {
		ctx.Modules = ToStringSlice(mods)
	}
	if excl, ok := section["exclude"]; ok {
		ctx.Exclude = ToStringSlice(excl)
	}
	if keys, ok := section["keys"]; ok {
		ctx.Keys = ToStringSlice(keys)
	}

	return ctx
}

// ToStringSlice converts an interface{} to []string.
func ToStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, len(val))
		for i, e := range val {
			result[i] = fmt.Sprint(e)
		}
		return result
	default:
		return nil
	}
}

// ToMapSlice converts an interface{} to []map[string]any. Accepts both
// `[]interface{}` (legacy raw-YAML decode shape) and `[]map[string]any` (typed
// shape produced by the post-2026-05 derivePackageSectionsFromCalamares bridge
// that copies `DistroPackages.Repos` directly into PackageSection.Raw).
func ToMapSlice(v any) []map[string]any {
	switch val := v.(type) {
	case []any:
		result := make([]map[string]any, 0, len(val))
		for _, e := range val {
			if m, ok := e.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	case []map[string]any:
		// Already the right shape; just copy.
		result := make([]map[string]any, len(val))
		copy(result, val)
		return result
	default:
		return nil
	}
}
