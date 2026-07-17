package loaderkit

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/opencharly/sdk/deploykit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// scan_candy.go — the candy-scan MECHANISM (W9: the type-Candy + scan-machinery move), ported
// verbatim from charly/layers.go (scanCandy/derivePackageSectionsFromCalamares) and
// charly/unified.go (populateCandyFromYAML). It reads a candy directory + its manifest and
// constructs the two envelope views DIRECTLY (spec.CandyModel + spec.CandyView) — the SAME
// resolved shape sdk/deploykit.NewSpecCandyModel already consumes to build a spec.CandyReader, so
// core never needs a concrete Candy struct again: candy/plugin-loader's ScanCandyManifest seam
// calls ScanCandyManifest below and returns the two views; the host wraps them via NewSpecCandyModel.
//
// Every derived-logic branch below (bake_plugin→require implication, per-distro package-section
// derivation, port protocol-suffix normalization) is preserved byte-for-byte from the original —
// this is genuine business logic, not field-copying (R1/RDD: proven via the byte-parity spike
// comparing this path against the pre-move charly/layers.go+unified.go pair on real candies,
// including a malformed-manifest negative case for identical error-path behavior).

// ScanCandyManifest scans a single candy DIRECTORY (the `from:`-directory / legacy-discovered-candy
// path) and returns its two resolved envelope views. Named distinctly from the ESTABLISHED exported
// charly.ScanCandy(dir) (the whole-project scan-all-candies entry point) to avoid confusion between
// the two similarly-shaped-but-different scans while both exist side by side during the cutover.
// parseDoc is the injected per-document parse seam (charly's parseCandyYAML — registry-coupled, so
// it stays host-side; see the CandyScanner interface doc in sdk/spec/loader_seam.go).
func ScanCandyManifest(path, name, manifestName string, parseDoc func(path string) (*spec.CandyYAML, error)) (spec.CandyModel, spec.CandyView, spec.CandyRefs, error) {
	yamlPath := filepath.Join(path, manifestName)
	var ly *spec.CandyYAML
	if kit.FileExists(yamlPath) {
		parsed, err := parseDoc(yamlPath)
		if err != nil {
			return spec.CandyModel{}, spec.CandyView{}, spec.CandyRefs{}, fmt.Errorf("parsing %s: %w", manifestName, err)
		}
		ly = parsed
	}
	m, v, refs := scanFromParsed(name, path, ly)
	return m, v, refs, nil
}

// ScanInlineCandy builds the two resolved envelope views + the rich pre-qualification refs for a
// candy declared INLINE in a unified charly.yml (no separate manifest file — ly is already the
// parsed body). sourceDir is the charly.yml's own directory (an inline candy's SourceDir always
// equals its declaring file's dir). Mirrors the pre-move charly/unified.go synthesizeInlineCandy,
// which called the SAME populateCandyFromYAML + fs-probe pair scanCandy did — the two paths can
// never drift because they share this one function.
func ScanInlineCandy(name, sourceDir string, ly *spec.CandyYAML) (spec.CandyModel, spec.CandyView, spec.CandyRefs) {
	return scanFromParsed(name, sourceDir, ly)
}

// ScanRemoteCandy scans specific candies out of a downloaded remote repository directory — only
// the bare refs in wantRefs (each "github.com/org/repo/candy/name" form, keyed by that same bare
// ref in the returned map). Ported verbatim from the pre-move charly/layers.go ScanRemoteCandy: for
// each wanted ref, ScanCandyManifest the candy dir, then set the resulting CandyView's
// Remote/RepoPath/SubPathPrefix (the post-scan "construct-then-mutate" step scan-time cannot do
// itself — a candy doesn't know it's REMOTE until the fetch layer resolves it there) and qualify
// its plain-name sibling deps (QualifyRemoteSiblingDeps) so the dependency graph + validator can
// resolve a remote candy's transitive deps against siblings pulled from the same repo, without
// per-call-site repo-path plumbing.
func ScanRemoteCandy(repoDir, repoPath string, wantRefs map[string]bool, parseDoc func(path string) (*spec.CandyYAML, error)) (map[string]spec.ScannedCandy, error) {
	out := make(map[string]spec.ScannedCandy, len(wantRefs))

	for bareRef := range wantRefs {
		// Extract sub-path from bare ref: "github.com/org/repo/candy/name" -> "candy/name"
		subPath := strings.TrimPrefix(bareRef, repoPath+"/")
		candyDir := filepath.Join(repoDir, subPath)

		// Derive name from last segment
		name := subPath
		if idx := strings.LastIndex(subPath, "/"); idx != -1 {
			name = subPath[idx+1:]
		}

		if _, err := os.Stat(candyDir); os.IsNotExist(err) {
			return nil, fmt.Errorf("remote candy %s not found at %s", bareRef, candyDir)
		}

		m, v, refs, err := ScanCandyManifest(candyDir, name, kit.UnifiedFileName, parseDoc)
		if err != nil {
			return nil, fmt.Errorf("scanning remote candy %s: %w", bareRef, err)
		}
		v.Remote = true
		v.RepoPath = repoPath
		// Compute sub-path prefix for sibling dep resolution (e.g. "candy/")
		if idx := strings.LastIndex(subPath, "/"); idx != -1 {
			v.SubPathPrefix = subPath[:idx+1]
		}
		QualifyRemoteSiblingDeps(v.RepoPath, v.SubPathPrefix, &refs)

		out[bareRef] = spec.ScannedCandy{Model: m, View: v, Refs: refs}
	}

	return out, nil
}

// scanFromParsed is the shared construction body: fs-probes + populateFromYAML + the partial
// #67 host-precomputed predicates — used by both ScanCandyManifest (file-based) and ScanInlineCandy
// (already-parsed). The returned spec.CandyRefs carries the RICH pre-qualification
// require:/candy:/bake_plugin: refs — CandyView.Require/.IncludedCandy and CandyModel.BakePlugin
// stay UNSET here (their bare-string FINAL form is written by the host's qualify-then-finalize
// step, mirroring the pre-move qualifyRemoteSiblingDeps → projectCandyView/projectCandyModel
// ordering — see spec.CandyRefs' doc comment in sdk/spec/loader_seam.go).
func scanFromParsed(name, sourceDir string, ly *spec.CandyYAML) (spec.CandyModel, spec.CandyView, spec.CandyRefs) {
	m := spec.CandyModel{Name: name, SourceDir: sourceDir}
	v := spec.CandyView{Name: name}

	// Install-file fs-probes (anchored at SourceDir, same as the pre-move scanCandy). These feed
	// HasInstallFiles below (host-precomputed on CandyModel — the #67 predicate-carrying pattern
	// every consumer already reads verbatim, so the probe result must be computed exactly here).
	// pixi.lock is NOT part of HasInstallFiles (matches the pre-move *Candy.HasInstallFiles() —
	// GetHasPixiLock() is a separate LIVE fs-probe specCandyAdapter runs at consumption time).
	hasPixiToml := kit.FileExists(filepath.Join(sourceDir, "pixi.toml"))
	hasPyprojectToml := kit.FileExists(filepath.Join(sourceDir, "pyproject.toml"))
	hasEnvironmentYml := kit.FileExists(filepath.Join(sourceDir, "environment.yml"))
	hasPackageJson := kit.FileExists(filepath.Join(sourceDir, "package.json"))
	hasCargoToml := kit.FileExists(filepath.Join(sourceDir, "Cargo.toml"))

	svcFiles, _ := filepath.Glob(filepath.Join(sourceDir, "*.service"))
	if len(svcFiles) > 0 {
		m.ServiceFiles = svcFiles
	}

	var refs spec.CandyRefs
	if ly != nil {
		refs = populateFromYAML(&m, &v, ly)
	}

	// PARTIAL host-precomputed predicates (#67): every term computable from THIS candy alone
	// (fs-probes, package derivation, Apk). RunOps and HasInit are NOT scan-computable — RunOps
	// needs opInContext/VerbCatalog (registry-adjacent D-data the host still owns, task #39);
	// HasInit needs PopulateCandyInitSystem (cross-candy InitConfig resolution, run once after
	// EVERY candy is scanned). The host completes both booleans by OR-ing in those two missing
	// terms once it has computed them (boolean OR is associative — the two-stage composition is
	// byte-identical to computing the whole predicate in one pass; proven by the byte-parity spike).
	hasFormatPackages := len(m.FormatSections) > 0 || len(m.TagSections) > 0 || len(m.TopPackages) > 0
	hasTagPackages := false
	for _, s := range m.TagSections {
		if len(s.Package) > 0 {
			hasTagPackages = true
			break
		}
	}
	hasApk := len(m.Apk) > 0
	m.HasInstallFiles = hasFormatPackages || hasTagPackages || len(m.TopPackages) > 0 ||
		hasPixiToml || hasPyprojectToml || hasEnvironmentYml || hasPackageJson || hasCargoToml ||
		hasApk
	m.HasContent = m.HasInstallFiles || m.Env != nil || len(m.Port) > 0 || m.Route != nil ||
		len(v.Volumes) > 0 || len(v.Aliases) > 0 || len(m.Extract) > 0 || len(m.Data) > 0 || len(m.Libvirt) > 0 ||
		len(m.PortRelayPorts) > 0 || len(m.ServiceFiles) > 0 || len(m.Service) > 0

	return m, v, refs
}

// populateFromYAML fans the parsed manifest out onto the two envelope views — the direct
// composition of the pre-move populateCandyFromYAML (Candy field writes) with
// resolved_project_host.go's projectCandyModel/projectCandyView (Candy→view reads), skipping the
// concrete Candy hop entirely. Field-by-field parity with both pre-move functions combined.
// Returns the RICH pre-qualification require:/candy:/bake_plugin: refs (spec.CandyRefs) — NOT
// bare-stringed onto v.Require/v.IncludedCandy here (see scanFromParsed's doc comment): the host's
// later qualify-then-finalize step sets .Resolved on a remote candy's plain-name sibling deps
// BEFORE the final bare-string projection, so bare-stringing at scan time would silently drop it.
func populateFromYAML(m *spec.CandyModel, v *spec.CandyView, ly *spec.CandyYAML) spec.CandyRefs {
	m.Version = ly.Version
	v.Version = ly.Version
	v.Description = ly.Description
	v.Status = ly.Status
	v.Info = deploykit.DescriptionInfo(ly.Description)
	v.IsPlugin = ly.Plugin != nil
	if ly.Plugin != nil {
		v.PluginSource = ly.Plugin.Source
		for _, cap := range ly.Plugin.Providers {
			v.PluginProviders = append(v.PluginProviders, string(cap))
		}
	}

	require := spec.ToCandyRefEntries(ly.Require)
	includedCandy := spec.ToCandyRefEntries(ly.Candy)
	bakePlugin := spec.ToCandyRefEntries(ly.BakePlugin)

	// `bake_plugin: <ref>` IMPLIES `require: <ref>` (see charly/unified.go's pre-move comment for
	// the full EffectiveVersion rationale) — dedupe by bare (map-key) name. This happens BEFORE
	// qualification (matches the pre-move populateCandyFromYAML, which ran before
	// qualifyRemoteSiblingDeps), so a bake_plugin-implied require gets qualified right alongside
	// every other require: entry.
	for _, bp := range bakePlugin {
		already := false
		for _, req := range require {
			if req.Bare() == bp.Bare() {
				already = true
				break
			}
		}
		if !already {
			require = append(require, bp)
		}
	}

	m.Service = ly.Service
	for _, s := range ly.Service {
		v.ServiceNames = append(v.ServiceNames, s.Name)
	}

	if len(ly.Package) > 0 || len(ly.Distro) > 0 {
		derivePackageSections(m, ly)
	}

	if len(ly.Port) > 0 {
		m.Port = ly.Port
		for _, p := range ly.Port {
			v.Ports = append(v.Ports, int64(p.Port))
		}
	}

	if len(ly.Env) > 0 || len(ly.PathAppend) > 0 {
		env := ly.Env
		if env == nil {
			env = make(map[string]string)
		}
		m.Env = &spec.EnvConfig{Vars: env, PathAppend: ly.PathAppend}
	}

	if ly.Route != nil {
		route := &spec.RouteConfig{Host: ly.Route.Host, Port: fmt.Sprintf("%d", ly.Route.Port)}
		m.Route = route
		v.Route = route
	}

	m.Volumes = ly.Volume
	v.Volumes = ly.Volume
	m.Aliases = ly.Alias
	v.Aliases = ly.Alias
	m.Extract = ly.Extract
	m.Data = ly.Data
	m.Security = ly.Security
	m.Libvirt = ly.Libvirt
	m.Hook = ly.Hook
	m.Plan = ly.Plan
	m.Artifact = ly.Artifact
	m.Capability = ly.Capability
	m.RequiresCapability = ly.RequiresCapability
	m.PortRelayPorts = ly.PortRelay
	v.PortRelayPorts = ly.PortRelay
	m.Secret = ly.SecretYAML
	m.EnvRequire = ly.EnvRequire
	m.EnvAccept = ly.EnvAccept
	m.SecretRequire = ly.SecretRequire
	m.SecretAccept = ly.SecretAccept
	m.MCPRequire = ly.MCPRequire
	m.MCPAccept = ly.MCPAccept
	v.EnvProvides = ly.EnvProvides
	v.MCPProvide = ly.MCPProvide
	m.Engine = ly.Engine
	m.Vars = ly.Vars
	m.Apk = ly.Apk
	m.ExternalBuilder = ly.ExternalBuilder
	v.SubPathPrefix = "" // filled by the resolve projector for remote candies (#67), not at scan time
	m.Reboot = ly.Reboot
	m.Shell = ly.Shell
	if len(ly.LocalPkg) > 0 {
		m.LocalPkg = ly.LocalPkg
	}
	if ly.Capability != nil {
		v.Capabilities = &spec.CandyCapabilitiesView{PreserveUser: ly.Capability.PreserveUser}
	}
	// v.HasInit is NOT set here — it needs PopulateCandyInitSystem's cross-candy InitConfig
	// resolution (see the ScanCandyManifest doc comment above), which runs once after every candy
	// in the project is scanned. The host sets it in that later pass.
	return spec.CandyRefs{Require: require, IncludedCandy: includedCandy, BakePlugin: bakePlugin}
}

// QualifyRemoteSiblingDeps records, for a freshly-scanned remote candy, the fully-qualified
// "<repo>/<subpathprefix><dep>" map key of each plain-name require:/candy:/bake_plugin: dep — the
// same form the fetched-sibling scan keys under. It sets each ref's resolved key
// (CandyRefEntry.Resolved) and leaves CandyRefEntry.Raw intact, so the graph resolves on .Bare()
// (qualified) while the transitive fetch loop still keys on the original .Raw plain name. @-ref
// deps are left untouched — their bare path already resolves directly. Ported verbatim from the
// pre-move charly/layers.go qualifyRemoteSiblingDeps, retargeted at the CandyRefs carrier (the
// live *Candy's Require/IncludedCandy/BakePlugin fields this mutated in place pre-move).
func QualifyRemoteSiblingDeps(repoPath, subPathPrefix string, refs *spec.CandyRefs) {
	qualify := func(list []spec.CandyRefEntry) {
		for i := range list {
			if !list[i].IsRemote() {
				list[i].Resolved = repoPath + "/" + subPathPrefix + list[i].Raw
			}
		}
	}
	qualify(refs.Require)
	qualify(refs.IncludedCandy)
	qualify(refs.BakePlugin)
}

// FinalizeCandyRefs projects the RICH pre-qualification refs (spec.CandyRefs) down to their
// bare-string FINAL wire form — v.Require / v.IncludedCandy (spec.CandyView) and m.BakePlugin
// (spec.CandyModel) — run ONCE, after any remote-sibling qualification (QualifyRemoteSiblingDeps)
// has had the chance to set .Resolved. Mirrors the pre-move projectCandyView/projectCandyModel
// bare-ref projection (charly/resolved_project_host.go's bareRefs() calls), which ran AFTER
// qualifyRemoteSiblingDeps on the live *Candy — this is the scan-machinery-move analogue.
func FinalizeCandyRefs(m *spec.CandyModel, v *spec.CandyView, refs spec.CandyRefs) {
	v.Require = bareCandyRefEntries(refs.Require)
	v.IncludedCandy = bareCandyRefEntries(refs.IncludedCandy)
	m.BakePlugin = bareCandyRefEntries(refs.BakePlugin)
}

// bareCandyRefEntries projects a []spec.CandyRefEntry down to its bare-string []spec.CandyRef
// form (the CandyView wire shape — identity/graph refs are bare strings, resolved-key details
// like a remote candy's Resolved field are a build-model concern only).
func bareCandyRefEntries(refs []spec.CandyRefEntry) []spec.CandyRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]spec.CandyRef, len(refs))
	for i, r := range refs {
		out[i] = r.Bare()
	}
	return out
}

// derivePackageSections is the SOLE populator of the package surface (m.TagSections +
// m.TopPackages, plus the arch `aur` format section) from package: + distro: — ported verbatim
// from charly/layers.go's derivePackageSectionsFromCalamares, retargeted from the *Candy's
// pointer-valued maps to CandyModel's VALUE-valued maps (ensureTag/ensureFormat mutate a local
// copy and re-store it, since a Go map of struct VALUES can't be mutated in place through a read).
func derivePackageSections(m *spec.CandyModel, ly *spec.CandyYAML) {
	m.TopPackages = spec.PackageNames(ly.Package)

	ensureTag := func(tagKey string) spec.TagPkgConfig {
		if m.TagSections == nil {
			m.TagSections = map[string]spec.TagPkgConfig{}
		}
		cfg := m.TagSections[tagKey]
		if cfg.Raw == nil {
			cfg.Raw = map[string]any{}
		}
		return cfg
	}
	ensureFormat := func(fmtName string) spec.PackageSection {
		if m.FormatSections == nil {
			m.FormatSections = map[string]spec.PackageSection{}
		}
		ps := m.FormatSections[fmtName]
		if ps.FormatName == "" {
			ps.FormatName = fmtName
		}
		if ps.Raw == nil {
			ps.Raw = map[string]any{}
		}
		return ps
	}
	// addPackages unions pkgs into *dst (dedup, first-seen order).
	addPackages := func(dst *[]string, pkgs []string) {
		seen := map[string]bool{}
		for _, p := range *dst {
			seen[p] = true
		}
		for _, p := range pkgs {
			if !seen[p] {
				*dst = append(*dst, p)
				seen[p] = true
			}
		}
	}
	// setRaw records a non-nil extra (repo/copr/options/exclude/module) into a section's Raw. See
	// the pre-move comment (charly/layers.go) for the nil-interface-slice rationale (the K4-B
	// resolved-project JSON round-trip precedent this scan path now IS, not merely mirrors).
	setRaw := func(raw map[string]any, key string, val any) {
		if val == nil {
			return
		}
		if rv := reflect.ValueOf(val); rv.Kind() == reflect.Slice && rv.IsNil() {
			return
		}
		raw[key] = val
	}

	// Sorted iteration → deterministic regardless of Go map order.
	distroKeys := make([]string, 0, len(ly.Distro))
	for k := range ly.Distro {
		distroKeys = append(distroKeys, k)
	}
	kit.SortStrings(distroKeys)

	for _, distroKey := range distroKeys {
		dp := ly.Distro[distroKey]
		if dp == nil {
			continue
		}
		for part := range strings.SplitSeq(distroKey, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			tagKey := part
			if i := strings.IndexByte(part, '-'); i > 0 {
				tagKey = part[:i] + ":" + part[i+1:]
			}
			cfg := ensureTag(tagKey)
			addPackages(&cfg.Package, spec.PackageNames(dp.Package))
			cfg.Raw["package"] = cfg.Package
			setRaw(cfg.Raw, "repo", dp.Repo)
			setRaw(cfg.Raw, "copr", dp.Copr)
			setRaw(cfg.Raw, "options", dp.Options)
			setRaw(cfg.Raw, "exclude", dp.Exclude)
			setRaw(cfg.Raw, "module", dp.Module)
			m.TagSections[tagKey] = cfg

			if dp.AUR != nil {
				aurPS := ensureFormat("aur")
				addPackages(&aurPS.Packages, spec.PackageNames(dp.AUR.Package))
				aurPS.Raw["package"] = aurPS.Packages
				setRaw(aurPS.Raw, "options", dp.AUR.Options)
				setRaw(aurPS.Raw, "replaces", dp.AUR.Replaces)
				m.FormatSections["aur"] = aurPS
			}
		}
	}
}
