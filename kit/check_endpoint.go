package kit

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/opencharly/sdk/spec"
)

// check_endpoint.go — the KIND-BLIND host-endpoint resolver the `charly check` verb dispatch
// serves back to an out-of-process verb over CheckContextService (cc.ResolveEndpoint). Relocated
// from charly/check_venue.go (the core resolveCheckEndpoint's venue.Kind switch + its
// containerPublishedAddr / sshForwardEndpoint bodies): endpoint resolution dispatches on the
// GENERIC transport word of a spec.VenueDescriptor (shell / ssh / container — the same discriminator
// kit.DescriptorFromExecutor produces), never on a concrete deploy-kind, so the ONE mechanism serves
// core AND plugin-check (R3). The host retains only the venue→descriptor PROJECTION (a stamped
// CheckVenue.Descriptor) + the tunnel-cleanup OWNERSHIP (the caller registers ep.Close and drains it
// after the nested Invoke — the ssh -L forward must outlive the plugin's dial).

// CheckEndpoint is a host-reachable TCP address for a port that lives inside the venue. The caller
// MUST Close() it when done (a no-op for container/shell venues; tears down the ssh -L forward for an
// ssh/vm venue).
type CheckEndpoint struct {
	Addr    string
	cleanup func()
}

// Close tears down any ssh -L forward the endpoint opened (no-op otherwise).
func (e *CheckEndpoint) Close() {
	if e != nil && e.cleanup != nil {
		e.cleanup()
	}
}

// EndpointForVenue returns a host-reachable "ip:port" for the given in-venue TCP port, dispatched by
// the descriptor's generic transport word:
//
//	"container" → the host published port via `<engine> port` (host-networked → 127.0.0.1:port)
//	"ssh"       → an ssh -L forward into the descriptor's target (vm alias / ssh host)
//	"shell"/""  → 127.0.0.1:port directly (the local host venue)
//
// Byte-equivalent to the former core resolveCheckEndpoint switch (container/host/vm) — the host
// projects its CheckVenue onto this descriptor once at construction; the resolution here is
// kind-blind.
func EndpointForVenue(desc spec.VenueDescriptor, port int) (*CheckEndpoint, error) {
	switch desc.Kind {
	case "container":
		addr, err := containerPublishedAddr(desc.Engine, desc.ContainerName, port)
		if err != nil {
			return nil, err
		}
		return &CheckEndpoint{Addr: addr}, nil
	case "ssh":
		return sshForwardEndpoint(desc, port)
	case "shell", "":
		return &CheckEndpoint{Addr: fmt.Sprintf("127.0.0.1:%d", port)}, nil
	}
	return nil, fmt.Errorf("cannot resolve a port endpoint for venue transport %q", desc.Kind)
}

// containerPublishedAddr returns the host "ip:port" that maps to <port> inside a running container
// via `<engine> port`, normalizing 0.0.0.0 / [::] to 127.0.0.1. Host-networked containers (no
// mappings) fall back to 127.0.0.1:port. Shared by cdp / vnc / mcp (replaces their per-verb copies — R3).
func containerPublishedAddr(engine, containerName string, port int) (string, error) {
	out, err := exec.Command(engine, "port", containerName, strconv.Itoa(port)).Output()
	if err != nil {
		if IsHostNetworked(engine, containerName) {
			return fmt.Sprintf("127.0.0.1:%d", port), nil
		}
		return "", fmt.Errorf("no port mapping found for %d in %s", port, containerName)
	}
	return ParsePublishedPort(string(out), port)
}

// sshForwardEndpoint opens a `ssh -NT -L 127.0.0.1:<rand>:127.0.0.1:<port>` forward into the
// descriptor's ssh target using the same credential-free system-ssh path as SSHExecutor (ssh-config /
// managed alias supply the user/port/key). A bounded readiness probe waits for the local listener — a
// readiness probe, not a blind sleep (R4).
func sshForwardEndpoint(desc spec.VenueDescriptor, port int) (*CheckEndpoint, error) {
	// Reserve a free local port, then release it for ssh to bind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("reserving local port: %w", err)
	}
	localPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)

	timeout := desc.ConnectTimeout
	if timeout <= 0 {
		timeout = 10
	}
	args := []string{
		"-N", "-T",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "LogLevel=ERROR",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeout),
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, port),
	}
	if desc.Port > 0 {
		args = append(args, "-p", strconv.Itoa(desc.Port))
	}
	args = append(args, desc.Args...)
	dest := desc.Host
	if desc.User != "" {
		dest = desc.User + "@" + desc.Host
	}
	args = append(args, dest)

	cmd := exec.Command("ssh", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ssh -L forward to %s: %w", dest, err)
	}
	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}

	// Readiness probe (WaitCapped): the ssh -L listener binds right after authentication. CALLER cap =
	// ConnectTimeout+5s (preserved); the 300ms dial is the per-attempt probe. FATAL fast-fail if ssh
	// has exited (auth/forward failure) — note cmd.ProcessState is only populated after Wait
	// (cleanup), so this remains best-effort, as before.
	cfg := ReadinessProvider().WaitCapped(fmt.Sprintf("ssh-forward %s", dest), spec.PollLocal, time.Duration(timeout+5)*time.Second)
	perr := spec.PollUntil(context.Background(), cfg, func(context.Context) (bool, float64, error) {
		if c, derr := net.DialTimeout("tcp", localAddr, 300*time.Millisecond); derr == nil {
			_ = c.Close()
			return true, 0, nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return false, 0, spec.ErrPollFatal // ssh died (auth/forward failure)
		}
		return false, 0, nil
	})
	if perr == nil {
		return &CheckEndpoint{Addr: localAddr, cleanup: cleanup}, nil
	}
	cleanup()
	return nil, fmt.Errorf("ssh -L forward to %s:%d did not become ready: %w", dest, port, perr)
}
