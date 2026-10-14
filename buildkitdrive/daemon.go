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
func Ensure(ctx context.Context, r Runner) (*Daemon, error) {
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
	d := &Daemon{Socket: sock, StateRoot: stateRoot, Config: cfg}
	if d.ready(ctx, r) {
		return d, nil
	}
	if err := writeIfChanged(cfg, DaemonConfig()); err != nil {
		return nil, err
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
	defer logFile.Close()
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
			return nil, fmt.Errorf("buildkitdrive: the daemon did not answer on %s within %s (state root %s)", sock, ReadinessTimeout, stateRoot)
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

// ready reports whether a daemon answers on the socket.
func (d *Daemon) ready(ctx context.Context, r Runner) bool {
	if _, err := os.Stat(d.Socket); err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, readinessInterval*5)
	defer cancel()
	_, err := r.Run(probeCtx, BuildctlBinary, "--addr", d.Addr(), "debug", "workers")
	return err == nil
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
