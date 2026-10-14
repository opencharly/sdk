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
}

// DaemonConfig renders buildkitd.toml for the resolved options.
//
// The ONE worker configuration this drive needs is the OCI worker's process mode: the
// measured shape is `process-mode = "sandbox"` with NO added capability (CapEff=0) and
// no `--oci-worker-no-process-sandbox`. Nothing else is set here, and in particular no
// snapshotter is forced: the `native`-vs-`overlayfs` measurement was taken in a
// DOUBLY-NESTED user namespace (where `native` was required and `overlayfs` produced an
// empty container rootfs) and on a plain host `overlayfs` is the documented default and
// was not tested — so forcing either one here would encode a venue-specific result as a
// universal rule. GC is likewise left alone: it is expressible only in this file and is
// UNPROVEN, so it is not invented.
func DaemonConfig(opts DaemonOptions) string {
	cfg := "# Rendered by sdk/buildkitdrive. Do not hand-edit: the drive writes it from the\n" +
		"# resolver's roots on every ensure.\n" +
		"[worker.oci]\n" +
		"  process-mode = \"sandbox\"\n"
	if opts.MaxParallelism > 0 {
		cfg += fmt.Sprintf("  max-parallelism = %d\n", opts.MaxParallelism)
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
