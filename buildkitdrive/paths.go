// Package buildkitdrive drives a rootless BuildKit daemon and bridges what it builds
// into the run engine's image store.
//
// ⚑ It is deliberately not named for the word "builder". In this tree that word already
// carries three distinct meanings — the `builder:` PROVIDER class (multi-stage stage
// definitions such as pixi/npm/cargo), the `builder:` selection map in charly.yml, and
// the multi-stage build stages themselves — and `sdk/buildkit` is the Containerfile
// RENDER machinery. This package is the DRIVE: the daemon, the `buildctl` argv model,
// and the store bridge that makes a BuildKit build reachable by the run engine.
package buildkitdrive

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencharly/sdk/storekit"
)

const (
	// SocketName is the daemon's socket inside the runtime root.
	SocketName = "buildkitd.sock"
	// DirName is the daemon's state root inside the cache root.
	DirName = "buildkitd"
	// ConfigName is the rendered daemon config beside the state root.
	ConfigName = "buildkitd.toml"
	// LogName is the daemon's log, beside its state root. It exists because a daemon
	// outlives whoever started it and so must not write to that caller's streams.
	LogName = "buildkitd.log"
	// LockName is the daemon's own one-instance-per-root lock, inside the state root.
	// BuildKit enforces it: a second daemon on the same --root fails loudly (rc=1), which
	// is the primitive that makes one-daemon-per-uid safe.
	LockName = "buildkitd.lock"
)

// SocketPath is the daemon address, resolved through the ONE roots resolver.
//
// The uid-1000-hardcoded `/run/user/1000/buildkit/buildkitd.sock` literal is the
// deviation this replaces: it breaks on any uid other than 1000, and two conventions
// for one socket is one too many.
func SocketPath() (string, error) {
	dir, err := storekit.Dir(storekit.RootRuntime)
	if err != nil {
		return "", fmt.Errorf("buildkitdrive: socket: runtime root: %w", err)
	}
	return filepath.Join(dir, SocketName), nil
}

// StateRoot is the daemon's persistent `--root`.
//
// It MUST be on a real filesystem. A tmpfs-backed rootless root cannot mount its
// overlays (`mount source: "overlay" … err: operation not permitted`), so the cache
// root — resolved by the same resolver — is the only correct home.
func StateRoot() (string, error) {
	cache, err := storekit.CacheDir()
	if err != nil {
		return "", fmt.Errorf("buildkitdrive: state root: cache root: %w", err)
	}
	return filepath.Join(cache, DirName), nil
}

// ConfigPath is the rendered buildkitd.toml beside the state root.
func ConfigPath() (string, error) {
	root, err := StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ConfigName), nil
}

// EnsureDirs creates the socket's directory and the state root, and returns the SOCKET
// PATH — not the directory it lives in.
//
// ⚑ It returns the socket path deliberately, because the two are one letter apart at the
// call site and the daemon rejects the directory with `failed to create unix socket … is a
// directory` (measured). A return value that can only be the socket is a footgun removed.
// The socket directory is per-boot runtime state (0700 — it holds a socket, nothing else);
// the state root is persistent build state (0755).
func EnsureDirs() (socketPath, stateRoot string, err error) {
	if socketPath, err = SocketPath(); err != nil {
		return "", "", err
	}
	if stateRoot, err = StateRoot(); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return "", "", fmt.Errorf("buildkitdrive: socket dir %s: %w", filepath.Dir(socketPath), err)
	}
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return "", "", fmt.Errorf("buildkitdrive: state root %s: %w", stateRoot, err)
	}
	return socketPath, stateRoot, nil
}

// DaemonOptions are the daemon settings the drive resolves and this renderer writes.
type DaemonOptions struct {
	// MaxParallelism bounds how many build steps run concurrently, rendered as
	// `[worker.oci] max-parallelism` — the config-file analogue of buildkitd's own
	// `--oci-max-parallelism` ("limit the number of parallel build steps that can run at
	// the same time"), and therefore the semantic successor of `podman build --jobs N`,
	// which bounded the stages of one build.
	//
	// It is a DAEMON-level setting, not a per-build one, because BuildKit's client has no
	// per-build equivalent. A zero or negative value leaves the key OUT and lets the
	// daemon's own default stand (which is unbounded) — the same meaning as omitting it.
	MaxParallelism int

	// RegistryMirror is the local charly OCI registry the daemon mirrors
	// ghcr.io/opencharly to, so a base image built earlier in the same run resolves
	// locally instead of through the upstream registry. Empty leaves the daemon's own
	// resolution.
	RegistryMirror string

	// ProcessMode is the OCI worker's process mode. Empty selects "sandbox" — the
	// measured shape (CapEff 0, no added capability).
	ProcessMode string

	// Snapshotter pins the worker's snapshotter ("overlayfs" or "native"). Empty leaves
	// the daemon's own "auto" resolution in place; the doubly-nested measurement that
	// required "native" is a VENUE result, not a universal default.
	Snapshotter string

	// NoProcessSandbox runs subprocesses in the main pid namespace (the daemon's
	// `noProcessSandbox`, needed when buildkitd itself runs inside a container).
	NoProcessSandbox bool

	// GCDisabled turns the worker's garbage collector off. The zero value keeps it ON
	// with the package's cache budget below.
	GCDisabled bool

	// GCReservedSpace is the cache floor the GC never reclaims below
	// (`[worker.oci] reservedSpace`). Empty selects DefaultGCReservedSpace.
	GCReservedSpace string

	// GCMaxUsedSpace is the cache ceiling the GC reclaims above
	// (`[worker.oci] maxUsedSpace`). Empty selects DefaultGCMaxUsedSpace.
	GCMaxUsedSpace string

	// GCMinFreeSpace is the free-space target the GC tries to leave
	// (`[worker.oci] minFreeSpace`). Empty selects DefaultGCMinFreeSpace. It is
	// deliberately SMALL: buildkitd's compiled default free target is 373 GB, and on a
	// host with less free space every GC pass prunes the cache to the reserve floor —
	// the measured cause of a repeated build of an unchanged tree re-executing layers.
	GCMinFreeSpace string

	// GCPolicies renders `[[worker.oci.gcpolicy]]` entries (advanced per-record policies).
	// Empty renders none; the scalar budget above still applies.
	GCPolicies []GCPolicy

	// Extra is raw buildkitd.toml appended VERBATIM after the managed sections — the
	// escape hatch for a daemon setting charly does not model. It may not redefine
	// `[worker.oci]` (the managed block is authoritative).
	Extra string
}

// GCPolicy is one `[[worker.oci.gcpolicy]]` entry: a per-record override of the worker's
// cache budget. Empty fields are omitted; `all` applies the policy to every record.
type GCPolicy struct {
	ReservedSpace string
	MaxUsedSpace  string
	MinFreeSpace  string
	KeepDuration  string
	All           bool
	Filters       []string
}

// The default GC budget. buildkitd's own default free-space target (373 GB) exceeds a
// typical developer host's free space, so its GC prunes reclaimable cache down to the
// ~10 GB reserve floor on every pass; a 100 GB floor/ceiling keeps a warm layer cache
// across builds instead. Measured on 2026-11-27: a 1.9 TB disk at 87% used (247 GB free)
// kept ~8-10 GB of cache and re-executed the builder layers (opencharly/charly#1292).
const (
	DefaultGCReservedSpace = "100GB"
	DefaultGCMaxUsedSpace  = "100GB"
	DefaultGCMinFreeSpace  = "10GB"
)

// gcBudget resolves the GC knobs a DaemonOptions carries, applying the package defaults.
func (o DaemonOptions) gcBudget() (reserved, maxUsed, minFree string) {
	reserved, maxUsed, minFree = o.GCReservedSpace, o.GCMaxUsedSpace, o.GCMinFreeSpace
	if reserved == "" {
		reserved = DefaultGCReservedSpace
	}
	if maxUsed == "" {
		maxUsed = DefaultGCMaxUsedSpace
	}
	if minFree == "" {
		minFree = DefaultGCMinFreeSpace
	}
	return reserved, maxUsed, minFree
}

// wantedGC is the ConfigGC these options render, for comparison with a running daemon's file.
func (o DaemonOptions) wantedGC() GCConfig {
	if o.GCDisabled {
		return GCConfig{}
	}
	reserved, maxUsed, minFree := o.gcBudget()
	return GCConfig{Enabled: true, ReservedSpace: reserved, MaxUsedSpace: maxUsed, MinFreeSpace: minFree}
}

// DaemonConfig renders buildkitd.toml for the resolved options.
//
// The measured shape is `process-mode = "sandbox"` with NO added capability (CapEff=0) and
// no `--oci-worker-no-process-sandbox`. No snapshotter is forced by default: the
// `native`-vs-`overlayfs` measurement was taken in a DOUBLY-NESTED user namespace, where
// `native` was required, and on a plain host `overlayfs` is the documented default — so
// forcing either one as a UNIVERSAL default would encode a venue result as a rule. A
// project that genuinely needs one sets it explicitly.
//
// GC IS configured, with a 100 GB budget by default. Leaving it to buildkitd's compiled
// default sets a 373 GB free-space target; on a host with less free space the GC prunes
// reclaimable cache down to the ~10 GB reserve floor on every pass, so a repeated build of
// an unchanged tree re-executes its layers (opencharly/charly#1292). The budget is a
// DAEMON setting like max-parallelism: it takes effect when the daemon starts, and a
// running daemon with a different policy is reported by Ensure.
func DaemonConfig(opts DaemonOptions) string {
	processMode := opts.ProcessMode
	if processMode == "" {
		processMode = "sandbox"
	}
	cfg := "# Rendered by sdk/buildkitdrive. Do not hand-edit: the drive writes it from the\n" +
		"# resolver's roots on every ensure.\n" +
		"[worker.oci]\n" +
		fmt.Sprintf("  process-mode = %q\n", processMode)
	if opts.MaxParallelism > 0 {
		cfg += fmt.Sprintf("  max-parallelism = %d\n", opts.MaxParallelism)
	}
	if opts.Snapshotter != "" {
		cfg += fmt.Sprintf("  snapshotter = %q\n", opts.Snapshotter)
	}
	if opts.NoProcessSandbox {
		cfg += "  noProcessSandbox = true\n"
	}
	if opts.GCDisabled {
		cfg += "  gc = false\n"
	} else {
		reserved, maxUsed, minFree := opts.gcBudget()
		cfg += "  gc = true\n"
		cfg += fmt.Sprintf("  reservedSpace = %q\n", reserved)
		cfg += fmt.Sprintf("  maxUsedSpace = %q\n", maxUsed)
		cfg += fmt.Sprintf("  minFreeSpace = %q\n", minFree)
	}
	for _, p := range opts.GCPolicies {
		cfg += "\n[[worker.oci.gcpolicy]]\n"
		if p.All {
			cfg += "  all = true\n"
		}
		if p.ReservedSpace != "" {
			cfg += fmt.Sprintf("  reservedSpace = %q\n", p.ReservedSpace)
		}
		if p.MaxUsedSpace != "" {
			cfg += fmt.Sprintf("  maxUsedSpace = %q\n", p.MaxUsedSpace)
		}
		if p.MinFreeSpace != "" {
			cfg += fmt.Sprintf("  minFreeSpace = %q\n", p.MinFreeSpace)
		}
		if p.KeepDuration != "" {
			cfg += fmt.Sprintf("  keepDuration = %q\n", p.KeepDuration)
		}
		if len(p.Filters) > 0 {
			quoted := make([]string, len(p.Filters))
			for i, f := range p.Filters {
				quoted[i] = strconv.Quote(f)
			}
			cfg += "  filters = [" + strings.Join(quoted, ", ") + "]\n"
		}
	}
	if opts.RegistryMirror != "" {
		// MEASURED (spike, buildkitd 0.34.0): the mirror key is the registry HOST, not a
		// host+namespace — `[registry."ghcr.io/opencharly"]` is silently ignored and the
		// resolve still goes upstream. A plain-HTTP local mirror also needs `http = true`
		// on its own stanza, or the daemon refuses the mirror rather than falling back.
		cfg += fmt.Sprintf("\n[registry.\"ghcr.io\"]\n  mirrors = [%q]\n", opts.RegistryMirror)
		cfg += fmt.Sprintf("\n[registry.%q]\n  http = true\n", opts.RegistryMirror)
	}
	if extra := strings.TrimSpace(opts.Extra); extra != "" {
		cfg += "\n# operator extra (defaults.buildkit.extra)\n" + extra + "\n"
	}
	return cfg
}

// ConfigMaxParallelism reads the value this file records, so an ensure can tell whether a
// RUNNING daemon was started with the settings now being asked for. It parses the rendered
// form (this kit is the only writer); an absent key reads as 0, exactly as the daemon
// treats it.
func ConfigMaxParallelism(content string) int {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "max-parallelism")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "="))
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// ConfigGC is the GC policy a rendered buildkitd.toml carries, as read back from the file.
// It is the value the daemon would load on its NEXT start; a daemon already running has the
// value it started with, which is why Ensure reports a mismatch instead of rewriting a live
// daemon's file.
type GCConfig struct {
	Enabled       bool
	ReservedSpace string
	MaxUsedSpace  string
	MinFreeSpace  string
}

// ConfigGC reads the GC keys this file records. It parses the rendered form (this kit is the
// only writer), within `[worker.oci]` only; an absent `gc` key reads as disabled (the
// daemon's own default when the key is missing is `false` for the worker's gc field).
func ConfigGC(content string) GCConfig {
	out := GCConfig{}
	inWorker := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inWorker = line == "[worker.oci]"
			continue
		}
		if !inWorker {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "gc":
			out.Enabled = value == "true"
		case "reservedSpace":
			out.ReservedSpace = value
		case "maxUsedSpace":
			out.MaxUsedSpace = value
		case "minFreeSpace":
			out.MinFreeSpace = value
		}
	}
	return out
}

// SameGC reports whether a rendered config's GC policy is the one opts asks for.
func (o DaemonOptions) SameGC(configured GCConfig) bool {
	if o.GCDisabled {
		return !configured.Enabled
	}
	reserved, maxUsed, minFree := o.gcBudget()
	return configured.Enabled && configured.ReservedSpace == reserved &&
		configured.MaxUsedSpace == maxUsed && configured.MinFreeSpace == minFree
}

// Describe renders a ConfigGC for a diagnostic line.
func (c GCConfig) Describe() string {
	if !c.Enabled {
		return "gc disabled"
	}
	return fmt.Sprintf("gc reservedSpace=%s maxUsedSpace=%s minFreeSpace=%s", c.ReservedSpace, c.MaxUsedSpace, c.MinFreeSpace)
}

// GCConfigMismatch reports whether a rendered config's GC policy differs from opts, and the
// diagnostic line Ensure prints for a RUNNING daemon that was started with the old policy.
// It is a PURE function so the running-daemon report is testable without a daemon: the
// policy is fixed at daemon start, and the operator restarts it to apply the new one.
func GCConfigMismatch(rendered string, opts DaemonOptions) (string, bool) {
	configured := ConfigGC(rendered)
	if opts.SameGC(configured) {
		return "", false
	}
	return fmt.Sprintf("buildkitdrive: the running daemon was started with %s; %s was requested. The GC policy is fixed at daemon start — stop the daemon and this drive will start it with the new policy.\n",
		configured.Describe(), opts.wantedGC().Describe()), true
}

// NeedsRestart reports whether a daemon configured with `configured` must be restarted to
// honour `want`.
//
// ⚑ A `want` of 0 or less is NO OPINION, not "unbounded": it never restarts. That arm is
// load-bearing. The value is daemon-level, so two callers with different opinions would
// otherwise restart each other's daemon on every call — a ping-pong that is worse than
// either setting. The drive resolves the authored `0 = auto` into a CONCRETE value before
// this layer (`min(NCPU, cap)`), so a value that expresses a real intent always arrives as
// > 0 and is compared; a caller that does not care passes 0 and leaves the daemon alone.
func NeedsRestart(configured, want int) bool {
	if want <= 0 {
		return false
	}
	return configured != want
}
