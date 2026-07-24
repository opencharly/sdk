package kit

// `charly --host <alias|user@host[:port]> <verb>` — re-exec charly on a
// remote machine over SSH. Shells out to the system `ssh` binary so
// ~/.ssh/config, agent forwarding, and ControlMaster all Just Work.
//
// This is the DI-shell relocation (S6, the same shape as
// deploykit/compile_construct_step.go's buildGenericOpStep — K5-A item 1's
// ctx/exec-threaded successor to the retired core-init()-set
// deploykit.CompileActOp func var): ReexecOverSSH's body is 100%
// stdlib+sdk/kit — zero core-only calls — so it lives here as a plain exported
// function. The ONE thing that MUST stay in charly's own main.go is the ~15-line
// "should I reexec" decision (shouldReexecForHost) — it must run before Kong
// dispatches to anything, mirroring the already-accepted precedent of
// plugin_command_prescan.go's pre-parse hooks; that is not a new capability
// class, it is the same decide-before-dispatch shape core already owns.
// main.go resolves the two charly-core-only inputs (the active controller binary
// path via its own activeCharlyBinary(), and this binary's CharlyVersion()) and
// passes them in — everything else (alias resolution, argv rewriting, the ssh
// invocation itself) lives here.
//
// wantTTY (whether stdin is a terminal, deciding ssh's `-tt` pty-allocation flag)
// is likewise threaded in as a plain bool rather than computed here via
// golang.org/x/term: `sdk/kit` is imported by nearly every out-of-tree plugin
// candy, so a NEW kit dependency ripples into every one of their go.sum files
// (confirmed live: ~38 candy modules import sdk/kit without also importing
// sdk/deploykit, which already carries x/term for its own unrelated terminal
// probes) — a blast radius wildly disproportionate to one flag decision. The
// caller (main.go, which already links x/term) computes it.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ReexecOverSSH rewrites os.Args by stripping --host and the client-
// local path flags (--dir/-C, --repo), resolves the remote charly
// endpoint (the venue's own PATH charly when it is at least as new as
// the local controller; otherwise a version-gated replica of the local
// binary delivered by EnsureCharlyInDeployVenue), then invokes
// `ssh <resolved-target> <endpoint> <rest of argv>`. Stdin/stdout/stderr
// are piped straight through. The returned exit code is whatever `ssh`
// exits with (which propagates the remote `charly` exit code). The
// happy path prints nothing — a diagnostic appears only when the local
// binary is actually replicated or the bootstrap fails.
//
// host/identityFile/options are the caller's --host/--host-identity-file/
// --host-option flag values; controllerBin is the caller's OWN resolved
// active binary path (charly-core's activeCharlyBinary()); version is the
// caller's OWN CalVer identity (charly-core's CharlyVersion()); wantTTY is
// whether stdin is a terminal (charly-core's term.IsTerminal(stdin)) — all
// charly-core-only concerns the caller resolves and threads in, so this
// function stays pure stdlib+sdk/kit.
func ReexecOverSSH(host, identityFile string, options []string, controllerBin, version string, wantTTY bool) int {
	target, err := resolveHostAlias(host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "charly: --host %q: %v\n", host, err)
		return 2
	}
	remoteArgv := buildRemoteArgv(os.Args[1:])
	destination, portText, err := splitSSHTarget(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "charly: --host %q: %v\n", host, err)
		return 2
	}
	user, hostname := "", destination
	if at := strings.LastIndex(destination, "@"); at >= 0 {
		user, hostname = destination[:at], destination[at+1:]
	}
	port := 0
	if portText != "" {
		port, _ = strconv.Atoi(portText) // splitSSHTarget already validated it.
	}
	extra := make([]string, 0, 2+len(options)*2)
	if identityFile != "" {
		extra = append(extra, "-i", identityFile)
	}
	for _, option := range options {
		extra = append(extra, "-o", option)
	}
	executor := &SSHExecutor{User: user, Host: hostname, Port: port, Args: extra}
	remoteBin, err := EnsureCharlyInDeployVenue(context.Background(), executor, controllerBin, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "charly: --host %q: bootstrap Charly endpoint: %v\n", host, err)
		return 1
	}
	if remoteBin != "charly" {
		// The venue's PATH charly was absent or older, so the remote command
		// runs a replica of THIS local binary — a version skew worth one line.
		fmt.Fprintf(os.Stderr, "charly: --host %q: venue charly absent/older; running replicated controller binary at %s\n", host, remoteBin)
	}
	sshArgs, err := sshCmdArgsWithEndpoint(target, remoteBin, identityFile, options, remoteArgv, wantTTY)
	if err != nil {
		fmt.Fprintf(os.Stderr, "charly: --host %q: %v\n", host, err)
		return 2
	}
	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "charly: ssh %s: %v\n", target, err)
		return 1
	}
	return 0
}

// resolveHostAlias looks up the `hosts.<alias>` setting if the input
// doesn't already look like an ssh target (user@host or host[:port]).
// Returns the raw string when no alias match exists — matches the
// behavior of `git remote add` / `kubectl --context`.
func resolveHostAlias(h string) (string, error) {
	if h == "" {
		return "", fmt.Errorf("empty host")
	}
	// Looks like an ssh target already? (contains @ or a dot)
	if strings.ContainsAny(h, "@.") {
		return h, nil
	}
	// Try alias lookup.
	cfg, err := LoadRuntimeConfig()
	if err != nil {
		// Fall back to raw — let ssh resolve via ~/.ssh/config.
		return h, nil
	}
	if v, ok := cfg.HostAliases[h]; ok && v != "" {
		return v, nil
	}
	// Not a configured alias — pass through and let ssh try its own
	// host resolution (~/.ssh/config Host entries, DNS, etc.).
	return h, nil
}

// buildRemoteArgv strips client-only flags from argv before shipping
// it to the remote host.
//
// Stripped:
//   - --host X  /  --host=X
//   - --dir / -C X  /  --dir=X
//   - --repo X / --repo=X
//
// Everything else is passed through verbatim.
func buildRemoteArgv(argv []string) []string {
	out := make([]string, 0, len(argv))
	skipNext := false
	for i := range argv {
		a := argv[i]
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--host" || a == "--host-identity-file" || a == "--host-option" || a == "--dir" || a == "-C" || a == "--repo" {
			skipNext = true
			continue
		}
		if strings.HasPrefix(a, "--host=") ||
			strings.HasPrefix(a, "--host-identity-file=") ||
			strings.HasPrefix(a, "--host-option=") ||
			strings.HasPrefix(a, "--dir=") ||
			strings.HasPrefix(a, "-C=") ||
			strings.HasPrefix(a, "--repo=") {
			continue
		}
		_ = i
		out = append(out, a)
	}
	return out
}

// sshCmdArgs builds the full argv for the `ssh` process:
//
//	ssh [-tt] <target> charly <remoteArgv...>
//
// -tt allocates a pseudo-TTY when stdin is a TTY, so interactive
// programs (prompts, pagers) work; piped stdin gets plain mode. Used by tests
// only (production always goes through sshCmdArgsWithEndpoint), so wantTTY is
// hardcoded false — matching what a non-TTY `go test` process would compute.
func sshCmdArgs(target string, remoteArgv []string) ([]string, error) {
	return sshCmdArgsWithEndpoint(target, "charly", "", nil, remoteArgv, false)
}

func sshCmdArgsWithEndpoint(target, remoteBinary, identityFile string, options, remoteArgv []string, wantTTY bool) ([]string, error) {
	destination, port, err := splitSSHTarget(target)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, 8+len(options)*2)
	if wantTTY {
		args = append(args, "-tt")
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	if identityFile != "" {
		args = append(args, "-i", identityFile)
	}
	for _, option := range options {
		args = append(args, "-o", option)
	}
	remote := ShellQuote(remoteBinary)
	for _, arg := range remoteArgv {
		remote += " " + ShellQuote(arg)
	}
	args = append(args, destination, remote)
	return args, nil
}

// splitSSHTarget converts the documented user@host[:port] form into OpenSSH's
// argv representation. Ports are data, never embedded in the destination
// token; IPv6 literals use the standard bracketed host:port form.
func splitSSHTarget(target string) (destination, port string, err error) {
	if target == "" {
		return "", "", errors.New("empty SSH target")
	}
	user := ""
	hostPort := target
	if at := strings.LastIndex(target, "@"); at >= 0 {
		if at == 0 || at == len(target)-1 {
			return "", "", fmt.Errorf("invalid SSH target %q", target)
		}
		user, hostPort = target[:at], target[at+1:]
	}
	host := hostPort
	switch {
	case strings.HasPrefix(hostPort, "["):
		switch {
		case strings.Contains(hostPort, "]:"):
			host, port, err = net.SplitHostPort(hostPort)
			if err != nil {
				return "", "", fmt.Errorf("invalid SSH target %q: %w", target, err)
			}
		case strings.HasSuffix(hostPort, "]"):
			host = strings.TrimSuffix(strings.TrimPrefix(hostPort, "["), "]")
		default:
			return "", "", fmt.Errorf("invalid SSH target %q", target)
		}
	case strings.Count(hostPort, ":") == 1:
		host, port, err = net.SplitHostPort(hostPort)
		if err != nil {
			return "", "", fmt.Errorf("invalid SSH target %q: %w", target, err)
		}
	}
	if host == "" {
		return "", "", fmt.Errorf("invalid SSH target %q: empty host", target)
	}
	if port != "" {
		value, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || value == 0 {
			return "", "", fmt.Errorf("invalid SSH target %q: port must be between 1 and 65535", target)
		}
	}
	destination = host
	if strings.Contains(host, ":") {
		destination = "[" + host + "]"
	}
	if user != "" {
		destination = user + "@" + destination
	}
	return destination, port, nil
}
