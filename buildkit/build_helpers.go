package buildkit

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"text/template"

	"github.com/opencharly/sdk/spec"
)

// build_helpers.go — pure build-engine helpers relocated out of charly core (the BUILD-cone
// "every capability is a plugin" cutover): box-arg normalization, podman-jobs resolution, host
// platform detection, the charly-binary freshness check, and the pacstrap bootstrap-conf
// renderers. None of these touch the loader, the provider registry, or any charly-core-only
// type — every dependency is stdlib or a buildkit type already in this package
// (BuilderDef/ResolvedBox), so they are genuine floor-of-the-build-cone pure functions.

// NormalizeBoxArgs canonicalises the positional box selection shared by `charly box build` and
// `charly box generate`. The lone sentinel `all` (case-insensitive) collapses to nil — i.e.
// "every enabled box" — so `charly box build all` / `charly box generate all` behave identically
// to the bare no-argument form. Any other slice (including a literal "all" alongside other
// names) passes through unchanged: the sentinel fires ONLY when it is the sole argument, so a
// box that happens to be named "all" is still reachable via an explicit two-name invocation.
func NormalizeBoxArgs(boxes []string) []string {
	if len(boxes) == 1 && strings.EqualFold(boxes[0], "all") {
		return nil
	}
	return boxes
}

// PodmanJobsCapFallback is the ceiling on the auto-computed `podman build --jobs` value, used
// ONLY when defaults.podman_jobs_cap is absent from project config. The operative ceiling is
// charly.yml `defaults.podman_jobs_cap`; this conservative constant just keeps configs that
// don't declare the key on a safe value. The per-build override is --podman-jobs /
// CHARLY_PODMAN_JOBS. (See CHANGELOG/ for the podman-5.7.x blob-reuse SIGABRT race that
// originally motivated a hard cap.)
const PodmanJobsCapFallback = 4

// NumCPU is a package-level override point for runtime.NumCPU so tests can inject a fixed value.
var NumCPU = runtime.NumCPU

// ResolvePodmanJobs returns the --jobs value to pass to `podman build`. An explicit override
// (>0, from --podman-jobs / CHARLY_PODMAN_JOBS / defaults.podman_jobs) wins. Otherwise the value
// is CPU-proportional, capped at `jobsCap` (defaults.podman_jobs_cap, else PodmanJobsCapFallback):
// min(NumCPU(), jobsCap). A jobsCap < 1 falls back to PodmanJobsCapFallback.
func ResolvePodmanJobs(override, jobsCap int) int {
	if override > 0 {
		return override
	}
	if jobsCap < 1 {
		jobsCap = PodmanJobsCapFallback
	}
	n := NumCPU()
	if n < jobsCap {
		return n
	}
	return jobsCap
}

// HostPlatform returns the host platform in OCI format.
func HostPlatform() string {
	return "linux/" + runtime.GOARCH
}

// CharlyBinaryUpToDate returns true when binPath exists and is newer than every .go file under
// srcDir. Returns (false, nil) for any file system state that warrants a rebuild (missing
// binary, missing source dir).
func CharlyBinaryUpToDate(binPath, srcDir string) (bool, error) {
	binStat, err := os.Stat(binPath)
	if err != nil {
		return false, nil
	}
	binMtime := binStat.ModTime()
	upToDate := true
	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(binMtime) {
			upToDate = false
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return false, walkErr
	}
	return upToDate, nil
}

// BootstrapPackagesForBox returns base + per-image bootstrap packages. Per-image overrides
// aren't currently surfaced via charly.yml; this returns just the distro defaults for now.
// Mirrors the VM-bootstrap engine's equivalent lookup at the OCI-image build path (the box
// config `from: builder:<name>` consumers). Same dispatch rules: Pacstrap.BasePackages for
// pacstrap-flavored, Debootstrap.BasePackages for debootstrap-flavored.
func BootstrapPackagesForBox(img *ResolvedBox) []string {
	if img.DistroDef == nil {
		return nil
	}
	if img.DistroDef.Pacstrap != nil {
		return img.DistroDef.Pacstrap.BasePackages
	}
	if img.DistroDef.Debootstrap != nil {
		return img.DistroDef.Debootstrap.BasePackages
	}
	return nil
}

// pacstrapMicroarchRe matches pacman microarchitecture-level tokens (e.g. x86_64_v3) embedded
// in a repo Server URL. CachyOS's cachyos-v3 repos serve such packages; pacman rejects them
// unless the matching token is in Architecture.
var pacstrapMicroarchRe = regexp.MustCompile(`x86_64_v[0-9]+`)

// RenderPacstrapExtraConf builds the pacman.conf fragment appended to /etc/pacman.conf inside
// the bootstrap container before `pacstrap` runs. It is the SINGLE source of truth for both the
// image bootstrap path (candy/plugin-build's runPrivilegedBootstrap) and the VM bootstrap path
// (candy/plugin-vm) — these previously each open-coded the rendering and drifted: the VM path
// dropped the per-repo SigLevel, so a SigLevel=Never repo (CachyOS) fell back to the default
// Required and `pacman -Sy` failed with "GPGME error: No data / corrupted PGP signature". Both
// paths share this function.
//
// It emits, in order:
//  1. an [options] Architecture directive whenever any repo Server declares a microarch variant
//     (e.g. x86_64_v3). pacman's default Architecture (auto → x86_64) otherwise rejects those
//     packages with "package architecture is not valid". Architecture is cumulative in pacman,
//     so appending this to the base config widens the accepted set rather than replacing it.
//  2. each [repo] block with its Server and (when set) SigLevel.
func RenderPacstrapExtraConf(p *spec.Pacstrap) string {
	if p == nil || len(p.ExtraRepos) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var microarch []string
	for _, r := range p.ExtraRepos {
		for _, m := range pacstrapMicroarchRe.FindAllString(r.Server, -1) {
			if !seen[m] {
				seen[m] = true
				microarch = append(microarch, m)
			}
		}
	}
	sort.Strings(microarch)

	var b strings.Builder
	if len(microarch) > 0 {
		fmt.Fprintf(&b, "[options]\nArchitecture = x86_64 %s\n", strings.Join(microarch, " "))
	}
	for _, r := range p.ExtraRepos {
		fmt.Fprintf(&b, "[%s]\nServer = %s\n", r.Name, r.Server)
		if r.SigLevel != "" {
			fmt.Fprintf(&b, "SigLevel = %s\n", r.SigLevel)
		}
	}
	return b.String()
}

// RenderRuntimePacmanConf renders the booted-guest /etc/pacman.conf for a pacstrap distro.
// `runtime_pacman_conf` is a Go text/template evaluated against the PacstrapDef, so the repo
// list is derived from the SINGLE `extra_repo` source (`{{ range .ExtraRepos }}`) rather than a
// second hand-maintained verbatim copy. The template adds only the runtime-specific framing
// ([options] header + Arch core/extra). A legacy verbatim config (no template actions) renders
// to itself. Returns "" when unset; surfaces malformed-template errors.
func RenderRuntimePacmanConf(p *spec.Pacstrap) (string, error) {
	if p == nil || strings.TrimSpace(p.RuntimePacmanConf) == "" {
		return "", nil
	}
	tmpl, err := template.New("runtime_pacman_conf").Parse(p.RuntimePacmanConf)
	if err != nil {
		return "", fmt.Errorf("parsing runtime_pacman_conf template: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, p); err != nil {
		return "", fmt.Errorf("rendering runtime_pacman_conf: %w", err)
	}
	return b.String(), nil
}
