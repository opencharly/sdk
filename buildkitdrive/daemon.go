package buildkitdrive

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RootlesskitBinary is the wrapper a rootless daemon must be started through: a bare
// `buildkitd --rootless` refuses to start and names it, and the wrapper is what creates
// the user namespace the daemon's process sandbox needs.
const RootlesskitBinary = "rootlesskit"

// DaemonBinary is the daemon itself, launched as an argument of the wrapper.
const DaemonBinary = "buildkitd"

// ReadinessTimeout bounds how long Ensure waits for a freshly started daemon to answer.
// It is a deadline, not a retry ladder: on expiry Ensure FAILS and says so.
const ReadinessTimeout = 15 * time.Second

// readinessInterval is the gap between readiness probes. The daemon needs to create a
// socket and open its stores, which is tens to hundreds of milliseconds on a warm root and
// seconds on a cold one; the probe is cheap, so the interval is short and the deadline does
// the bounding.
const readinessInterval = 100 * time.Millisecond

// Daemon is a rootless BuildKit daemon the drive can talk to.
type Daemon struct {
	// Socket is the unix socket address (a path, not a URL).
	Socket string
	// StateRoot is the daemon's persistent --root.
	StateRoot string
	// Config is the rendered daemon config path.
	Config string
	// Started is true when THIS Ensure started the daemon, false when it attached to one
	// already listening. The distinction matters to the caller: a daemon it started is one
	// it may leave running (the persistent root makes a later start cheap) but did not
	// inherit.
	Started bool
	// MaxParallelism is the value the drive asked for and this daemon was ensured with.
	MaxParallelism int
	// WorkerLabels is the daemon's own report of its workers, as returned by Workers. It is
	// the evidence that the rendered config took effect (`oci.process-mode:SANDBOX` for the
	// OCI worker), and it is recorded here rather than fetched by a dangling accessor,
	// because the readiness probe already asks exactly this question.
	WorkerLabels string
}

// Addr is the socket as a buildctl address.
func (d *Daemon) Addr() string { return "unix://" + d.Socket }

// Ensure returns a daemon listening on the resolver's socket, starting one if none is.
//
// The shape is measured, not invented: a rootless daemon is started as
//
//	rootlesskit --state-dir=<state> --net=host buildkitd --rootless \
//	    --root <state root> --addr unix://<socket> --config <buildkitd.toml>
//
// and needs NO added capability (the measured worker reports `oci.process-mode:SANDBOX`
// with CapEff 0), so no `--oci-worker-no-process-sandbox` and no privilege escalation is
// involved. The wrapper does not daemonize — it stays in the foreground of the process the
// drive starts — which is why Ensure releases the child rather than waiting on it: the
// daemon must outlive the call for the builds that follow.
func Ensure(ctx context.Context, r Runner, opts DaemonOptions) (*Daemon, error) {
	if r == nil {
		return nil, fmt.Errorf("buildkitdrive: Ensure: no runner")
	}
	sock, stateRoot, err := EnsureDirs()
	if err != nil {
		return nil, err
	}
	cfg, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	d := &Daemon{Socket: sock, StateRoot: stateRoot, Config: cfg, MaxParallelism: opts.MaxParallelism}
	if d.ready(ctx, r) {
		// A daemon is already listening, so its settings are FIXED for its lifetime: the
		// config file is read back and compared, and a differing value is reported as a
		// named mismatch rather than silently ignored or force-applied.
		//
		// ⚑ WHY NO RESTART, measured rather than preferred: the daemon is a detached
		// `rootlesskit` tree that RE-EXECS itself, so there is no single pid to record, probe
		// or signal — a liveness probe on the recorded parent reports a LIVE daemon as dead
		// (observed), and every restart variant tried here failed on that identity problem in
		// one form or another (a stale rootlesskit state-dir lock, a socket file left behind
		// by a killed daemon, an unowned daemon answering for the one we thought we replaced).
		// Applying a changed daemon-level setting reliably therefore needs a SUPERVISOR (a
		// user unit), which is a design decision this package does not take on its own.
		configured := 0
		if raw, rerr := os.ReadFile(cfg); rerr == nil {
			configured = ConfigMaxParallelism(string(raw))
		}
		if NeedsRestart(configured, opts.MaxParallelism) {
			return nil, fmt.Errorf("buildkitdrive: the running daemon was configured with max-parallelism=%d but %d was requested; the value is set at daemon start, so stop the daemon (%s) and this drive will start it with the new value", configured, opts.MaxParallelism, d.Socket)
		}
		return d, nil
	}
	// No daemon is listening. A caller WITH an opinion writes the config; a caller WITHOUT
	// one adopts whatever is on disk (the drive's value, or the daemon's own defaults) and
	// never overwrites it — that is what keeps two callers from fighting over the file.
	if opts.MaxParallelism > 0 {
		if err := writeIfChanged(cfg, DaemonConfig(opts)); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(cfg); err != nil {
		if err := os.WriteFile(cfg, []byte(DaemonConfig(DaemonOptions{})), 0o644); err != nil {
			return nil, fmt.Errorf("buildkitdrive: write %s: %w", cfg, err)
		}
	}
	args := DaemonArgs(sock, stateRoot, cfg)
	// ⚑ The daemon OUTLIVES the caller, so it must NOT inherit the caller's streams. A
	// detached child holding the parent's stderr keeps the parent's I/O open, and a
	// short-lived caller then cannot finish: measured as a package-level gate failure —
	// "Test I/O incomplete 1m0s after exiting / exec: WaitDelay expired before I/O
	// complete" — even though every assertion in the test had PASSED. Its output belongs
	// in a log beside its state root, which is also where a reader looks for the daemon's
	// own startup diagnostics.
	logPath := filepath.Join(stateRoot, LogName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("buildkitdrive: daemon log %s: %w", logPath, err)
	}
	defer func() { _ = logFile.Close() }() // the child keeps its own descriptor
	cmd := exec.CommandContext(ctx, RootlesskitBinary, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("buildkitdrive: start %s: %w", RootlesskitBinary, err)
	}
	// Release rather than Wait: the daemon outlives this call by design, and releasing
	// hands its reaping to init instead of leaving a zombie behind.
	if err := cmd.Process.Release(); err != nil {
		return nil, fmt.Errorf("buildkitdrive: release %s: %w", RootlesskitBinary, err)
	}
	d.Started = true
	deadline := time.Now().Add(ReadinessTimeout)
	for {
		if d.ready(ctx, r) {
			return d, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("buildkitdrive: the daemon did not answer on %s within %s (state root %s); %s", sock, ReadinessTimeout, stateRoot, logTail(logPath, 6))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(readinessInterval):
		}
	}
}

// DaemonArgs renders the daemon's spawn argv.
//
// It is a PURE function on purpose: the address it carries must be the SOCKET PATH, and a
// unit test can assert that without a daemon, an engine or a user namespace. The measured
// shape is `rootlesskit --state-dir=<state> --net=host buildkitd --rootless --root <root>
// --addr unix://<socket> --config <toml>`, which needs no added capability
// (`oci.process-mode:SANDBOX`, CapEff 0) and no `--oci-worker-no-process-sandbox`.
func DaemonArgs(socketPath, stateRoot, configPath string) []string {
	return []string{
		"--state-dir=" + filepath.Join(filepath.Dir(socketPath), "rootlesskit-state"),
		"--net=host",
		DaemonBinary, "--rootless",
		"--root", stateRoot,
		"--addr", "unix://" + socketPath,
		"--config", configPath,
	}
}

// logTail returns the last n lines of the daemon log, so a start failure carries its REASON
// rather than only the fact that nothing answered.
func logTail(path string, n int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("daemon log %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return fmt.Sprintf("daemon log %s: (empty)", path)
	}
	return "daemon log: " + strings.Join(lines, " | ")
}

// ready reports whether a daemon answers on the socket, and records what it reports.
//
// It asks through Workers so the kit has exactly ONE place that knows how to interrogate a
// daemon about its workers; the answer lands on the Daemon as evidence rather than being
// discarded.
func (d *Daemon) ready(ctx context.Context, r Runner) bool {
	if _, err := os.Stat(d.Socket); err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, readinessInterval*5)
	defer cancel()
	out, err := Workers(probeCtx, r, d)
	if err != nil {
		return false
	}
	d.WorkerLabels = out
	return true
}

// writeIfChanged writes content to path only when it differs, so an ensure that changes
// nothing does not touch the daemon's config mtime.
func writeIfChanged(path, content string) error {
	if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("buildkitdrive: write %s: %w", path, err)
	}
	return nil
}

// Workers returns the daemon's worker labels, which is the evidence that a worker is
// configured the way this drive expects (`oci.process-mode:SANDBOX` for the OCI worker).
func Workers(ctx context.Context, r Runner, d *Daemon) (string, error) {
	out, err := r.Run(ctx, BuildctlBinary, "--addr", d.Addr(), "debug", "workers")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
