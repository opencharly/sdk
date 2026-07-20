package deploykit

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
)

// TaskAutoExports are the auto-exported variable names reserved for the generator;
// `vars:` entries may not shadow these, and every `${VAR}` reference resolves
// against (auto-exports ∪ candy.Vars).
var TaskAutoExports = map[string]bool{
	"USER":       true,
	"UID":        true,
	"GID":        true,
	"HOME":       true,
	"ARCH":       true,
	"BUILD_ARCH": true,
}

// TaskVarRefPattern matches ${NAME} references in task fields.
var TaskVarRefPattern = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// TaskKnownNames returns the ${NAME} references that resolve cleanly for this
// candy: auto-exports ∪ candy.Vars keys.
func TaskKnownNames(vars map[string]string) map[string]bool {
	known := make(map[string]bool, len(TaskAutoExports)+len(vars))
	for k := range TaskAutoExports {
		known[k] = true
	}
	for k := range vars {
		known[k] = true
	}
	return known
}

// TaskUnresolvedRefs returns the names of ${VAR} references in s not in known.
func TaskUnresolvedRefs(s string, known map[string]bool) []string {
	matches := TaskVarRefPattern.FindAllStringSubmatch(s, -1)
	var out []string
	seen := make(map[string]bool)
	for _, m := range matches {
		name := m[1]
		if !known[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// ResolveUserSpec converts a task's `user:` field to (userDirective, chownPair).
func ResolveUserSpec(userField string, img *buildkit.ResolvedBox) (directive, chown string) {
	u := strings.TrimSpace(userField)
	if u == "${USER}" {
		return strconv.Itoa(img.UID), fmt.Sprintf("%d:%d", img.UID, img.GID)
	}
	u = TaskSubstAutoExports(u, img)
	if u == "" || u == "root" || u == "0" {
		return "0", ""
	}
	if strings.Contains(u, ":") {
		return u, u
	}
	if _, err := strconv.Atoi(u); err == nil {
		return u, u + ":" + u
	}
	return u, u + ":" + u
}

// TaskSubstAutoExports substitutes the image-level auto-exports (USER/UID/GID/HOME)
// in s. ARCH/BUILD_ARCH are NOT substituted here.
func TaskSubstAutoExports(s string, img *buildkit.ResolvedBox) string {
	if s == "" || img == nil {
		return s
	}
	repl := map[string]string{
		"USER": img.User,
		"UID":  strconv.Itoa(img.UID),
		"GID":  strconv.Itoa(img.GID),
		"HOME": img.Home,
	}
	return TaskVarRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		if v, ok := repl[name]; ok {
			return v
		}
		return match
	})
}

// TaskSubstPath resolves a destination/path field: auto-export substitution plus
// leading ~/ expansion to the resolved HOME.
func TaskSubstPath(p string, img *buildkit.ResolvedBox) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") && img != nil && img.Home != "" {
		p = img.Home + p[1:]
	}
	return TaskSubstAutoExports(p, img)
}

// EmitVarsEnv writes ENV directives for candy.Vars and the build-arg-sourced ARCH
// auto-export. Sorts vars by key for deterministic output; emitted once per candy.
func EmitVarsEnv(b *strings.Builder, vars map[string]string) {
	b.WriteString("ARG TARGETARCH\n")
	b.WriteString("ENV ARCH=${TARGETARCH}\n")

	if len(vars) == 0 {
		return
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := vars[k]
		escaped := strings.ReplaceAll(v, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		fmt.Fprintf(b, "ENV %s=\"%s\"\n", k, escaped)
	}
}

// EmitMkdirBatch emits a single RUN mkdir -p for a batch of adjacent same-user
// mkdir tasks, splitting per-mode chmod at the tail when modes differ.
func EmitMkdirBatch(b *strings.Builder, tasks []vmshared.Op, img *buildkit.ResolvedBox) {
	if len(tasks) == 0 {
		return
	}
	byMode := make(map[string][]string)
	var modeOrder []string
	var allPaths []string
	for _, t := range tasks {
		pth := TaskSubstPath(t.Mkdir, img)
		allPaths = append(allPaths, pth)
		mode := t.Mode
		if _, ok := byMode[mode]; !ok {
			modeOrder = append(modeOrder, mode)
		}
		byMode[mode] = append(byMode[mode], pth)
	}
	parts := []string{"mkdir -p " + strings.Join(allPaths, " ")}
	for _, m := range modeOrder {
		if m == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("chmod %s %s", m, strings.Join(byMode[m], " ")))
	}
	b.WriteString("RUN " + strings.Join(parts, " && ") + "\n")
}

// EmitCopy emits a COPY --from=<layer-stage> for an existing file in the candy dir.
func EmitCopy(b *strings.Builder, t vmshared.Op, layerStage string, img *buildkit.ResolvedBox) {
	src := t.Copy
	dest := TaskSubstPath(t.To, img)
	mode := t.Mode
	if mode == "" {
		mode = "0755"
	}
	_, chown := ResolveUserSpec(t.RunAs, img)
	flags := []string{fmt.Sprintf("--from=%s", layerStage), fmt.Sprintf("--chmod=%s", mode)}
	if chown != "" {
		flags = append(flags, fmt.Sprintf("--chown=%s", chown))
	}
	fmt.Fprintf(b, "COPY %s %s %s\n", strings.Join(flags, " "), src, dest)
}

// EmitWrite emits a COPY from the staged inline-content dir to the destination.
func EmitWrite(b *strings.Builder, t vmshared.Op, srcPath string, img *buildkit.ResolvedBox) {
	dest := TaskSubstPath(t.Write, img)
	mode := t.Mode
	if mode == "" {
		mode = "0644"
	}
	_, chown := ResolveUserSpec(t.RunAs, img)
	flags := []string{fmt.Sprintf("--chmod=%s", mode)}
	if chown != "" {
		flags = append(flags, fmt.Sprintf("--chown=%s", chown))
	}
	fmt.Fprintf(b, "COPY %s %s %s\n", strings.Join(flags, " "), srcPath, dest)
}

// EmitLinkBatch emits a single RUN with chained ln -sf for a batch of adjacent
// same-user link tasks.
func EmitLinkBatch(b *strings.Builder, tasks []vmshared.Op, img *buildkit.ResolvedBox) {
	if len(tasks) == 0 {
		return
	}
	parts := make([]string, 0, len(tasks))
	for _, t := range tasks {
		link := TaskSubstPath(t.Link, img)
		target := TaskSubstPath(t.Target, img)
		parts = append(parts, fmt.Sprintf("ln -sf %s %s", target, link))
	}
	b.WriteString("RUN " + strings.Join(parts, " && ") + "\n")
}

// EmitSetcapBatch emits a single RUN setcap … for a batch of adjacent setcap tasks
// (strip via -r for empty caps, set otherwise), chained with &&.
func EmitSetcapBatch(b *strings.Builder, tasks []vmshared.Op, img *buildkit.ResolvedBox) {
	if len(tasks) == 0 {
		return
	}
	parts := make([]string, 0, len(tasks))
	for _, t := range tasks {
		pth := TaskSubstPath(t.Setcap, img)
		if strings.TrimSpace(t.Caps) == "" {
			parts = append(parts, fmt.Sprintf("setcap -r %s 2>/dev/null || true", pth))
		} else {
			parts = append(parts, fmt.Sprintf("setcap %s %s", t.Caps, pth))
		}
	}
	b.WriteString("RUN " + strings.Join(parts, " && ") + "\n")
}

// TaskCacheMounts renders a task's candy-declared `cache:` paths as BuildKit
// cache-mount flags. Ownership follows the task's stage user (root → shared,
// non-root → uid/gid-owned).
func TaskCacheMounts(t vmshared.Op, img *buildkit.ResolvedBox) []string {
	if len(t.Cache) == 0 {
		return nil
	}
	root := taskRunsAsRoot(t.RunAs, img)
	out := make([]string, 0, len(t.Cache))
	for _, p := range t.Cache {
		p = TaskSubstPath(p, img)
		if root {
			out = append(out, buildkit.SharedCacheMount(p, "").String())
		} else {
			out = append(out, buildkit.OwnedCacheMount(p, img.UID, img.GID).String())
		}
	}
	return out
}

// taskRunsAsRoot reports whether a task's stage user is root: an explicit root
// RunAs, or — with RunAs empty — the image's build user being root (a task RUN
// inherits the stage's USER, which is the image build user). ResolveUserSpec
// alone cannot decide this: it maps an EMPTY RunAs to "0" for USER-directive
// purposes, which would wrongly root-ify every default task's cache ownership
// (the curl-23 build failure: a root-owned shared downloads cache breaks every
// non-root stage's download).
func taskRunsAsRoot(runAs string, img *buildkit.ResolvedBox) bool {
	if strings.TrimSpace(runAs) == "" {
		return img.UID == 0
	}
	directive, _ := ResolveUserSpec(runAs, img)
	return directive == "0"
}

// EmitDownload emits one RUN per download task: fetch to a content-addressed
// /tmp/downloads cache, then extract. Honors candy-declared `cache:` mounts.
func EmitDownload(b *strings.Builder, t vmshared.Op, img *buildkit.ResolvedBox) error {
	url := t.Download
	dest := TaskSubstPath(t.To, img)
	extract := strings.TrimSpace(t.Extract)

	var envPrefix strings.Builder
	envPrefix.WriteString("export BUILD_ARCH=$(uname -m);")
	var envForSh strings.Builder
	envForSh.WriteString("BUILD_ARCH=$(uname -m)")
	if len(t.Env) > 0 {
		keys := make([]string, 0, len(t.Env))
		for k := range t.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&envPrefix, " export %s=%s;", k, kit.ShellQuote(t.Env[k]))
			fmt.Fprintf(&envForSh, " %s=%s", k, kit.ShellQuote(t.Env[k]))
		}
	}

	stripFlag := ""
	if t.StripComponents > 0 {
		stripFlag = fmt.Sprintf(" --strip-components=%d", t.StripComponents)
	}

	if extract == "" {
		if strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
			extract = "tar.gz"
		} else {
			extract = "none"
		}
	}
	if extract == "none" && dest == "" {
		return fmt.Errorf("download %q: extract=none requires `to:` destination", url)
	}

	fetch := fmt.Sprintf(`%s mkdir -p /tmp/downloads; __u=%q; __c=/tmp/downloads/$(printf %%s "$__u" | sha256sum | cut -c1-64); [ -s "$__c" ] || { curl -fsSL "$__u" -o "$__c.part" && mv -f "$__c.part" "$__c"; }`, envPrefix.String(), url)

	var extractCmd string
	switch extract {
	case "tar.gz":
		extractCmd = fmt.Sprintf(`tar -xzf "$__c"%s -C %s %s`, stripFlag, dest, strings.Join(t.ExtractInclude, " "))
	case "tar.xz":
		extractCmd = fmt.Sprintf(`tar -xJf "$__c"%s -C %s %s`, stripFlag, dest, strings.Join(t.ExtractInclude, " "))
	case "tar.zst":
		extractCmd = fmt.Sprintf(`tar --zstd -xf "$__c"%s -C %s %s`, stripFlag, dest, strings.Join(t.ExtractInclude, " "))
	case "zip":
		extractCmd = fmt.Sprintf(`unzip -o "$__c" -d %s`, dest)
	case "sh":
		extractCmd = fmt.Sprintf(`%s sh "$__c"`, envForSh.String())
	case "none":
		extractCmd = fmt.Sprintf(`cp -f "$__c" %s`, dest)
	default:
		return fmt.Errorf("download %q: unknown extract %q", url, extract)
	}

	cmd := fetch + " && " + extractCmd
	if t.Mode != "" {
		if extract == "none" {
			cmd += fmt.Sprintf(" && chmod %s %s", t.Mode, dest)
		} else if extract != "sh" {
			cmd += fmt.Sprintf(" && chmod -R %s %s", t.Mode, dest)
		}
	}

	cacheMounts := TaskCacheMounts(t, img)
	mounts := make([]string, 0, 1+len(cacheMounts))
	// The downloads cache follows the task's stage user exactly like a
	// candy-declared `cache:` mount (TaskCacheMounts, taskRunsAsRoot): a
	// NON-ROOT stage gets an uid/gid-owned cache (per-uid id namespace, the
	// pixi/npm-cache pattern) — a shared root-owned backing permanently breaks
	// non-root downloads the moment any root stage has written to it (the
	// curl-23 build failure).
	if taskRunsAsRoot(t.RunAs, img) {
		mounts = append(mounts, buildkit.SharedCacheMount("/tmp/downloads", "").String())
	} else {
		mounts = append(mounts, buildkit.OwnedCacheMount("/tmp/downloads", img.UID, img.GID).String())
	}
	mounts = append(mounts, cacheMounts...)
	b.WriteString("RUN " + strings.Join(mounts, " ") + " bash -c " + kit.ShellQuote(cmd) + "\n")
	return nil
}

// EmitCmd emits a single RUN for a cmd task, with the layer-stage /ctx bind mount
// plus cache mounts appropriate to the user (distro format caches for root, npm
// cache for non-root). Shell is bash via a single-quoted heredoc so authors'
// $(cmd) / ${VAR} stay intact for bash; BUILD_ARCH is injected as a shell var.
func EmitCmd(b *strings.Builder, t vmshared.Op, layerStage string, img *buildkit.ResolvedBox, userIsRoot bool) {
	var mounts []string
	mounts = append(mounts, fmt.Sprintf("--mount=type=bind,from=%s,source=/,target=/ctx", layerStage))

	if userIsRoot && img != nil && img.DistroDef != nil {
		if formatDef, ok := img.DistroDef.Format[img.Pkg]; ok {
			if cm := buildkit.RenderCacheMounts(formatDef.CacheMount, -1, 0, " ", false); cm != "" {
				mounts = append(mounts, cm)
			}
		}
	} else {
		mounts = append(mounts, buildkit.OwnedCacheMount("/tmp/npm-cache", img.UID, img.GID).String())
	}

	mounts = append(mounts, TaskCacheMounts(t, img)...)

	b.WriteString("RUN ")
	for _, m := range mounts {
		b.WriteString(m + " ")
	}
	b.WriteString("bash <<'OVCMD'\n")
	b.WriteString(BuildArchExports())
	if len(t.Env) > 0 {
		keys := make([]string, 0, len(t.Env))
		for k := range t.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "export %s=%s\n", k, kit.ShellQuote(t.Env[k]))
		}
	}
	b.WriteString("set -e\n")
	b.WriteString(t.Command)
	if !strings.HasSuffix(t.Command, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("OVCMD\n")
}

// Build-mode Containerfile emit helpers, relocated verbatim from charly/tasks.go
// (P8). Op = vmshared.Op; ResolvedBox = buildkit.ResolvedBox. Behaviour is
// byte-identical to the former charly package-main helpers; charly keeps thin
// var-alias shims (var buildArchExports = deploykit.BuildArchExports, …) so the
// other build/check/deploy callers stay unchanged until they relocate.

// BuildArchExports emits the shell prelude that maps `uname -m` to a Go-style
// ARCH (amd64/arm64/arm) for download/build steps.
func BuildArchExports() string {
	return "BUILD_ARCH=$(uname -m)\n" +
		"case \"$BUILD_ARCH\" in\n" +
		"  x86_64) ARCH=amd64 ;;\n" +
		"  aarch64) ARCH=arm64 ;;\n" +
		"  armv7l|armv7|armhf) ARCH=arm ;;\n" +
		"  *) ARCH=$BUILD_ARCH ;;\n" +
		"esac\n" +
		"export BUILD_ARCH ARCH\n"
}

// ParentDirForDest returns the parent directory of a copy/write destination, or
// "" when it is the root or current dir (no auto-mkdir needed).
func ParentDirForDest(dest string) string {
	clean := path.Clean(dest)
	dir := path.Dir(clean)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// TaskCoalescesWith returns true if next can be batched with current under the
// adjacent-coalescing rule: same verb, same user, both verbs support batching.
func TaskCoalescesWith(current, next vmshared.Op, currentVerb string) bool {
	nextVerb, err := next.Kind()
	if err != nil || nextVerb != currentVerb {
		return false
	}
	if current.RunAs != next.RunAs {
		return false
	}
	switch currentVerb {
	case "mkdir", "link", "setcap":
		return true
	}
	return false
}
