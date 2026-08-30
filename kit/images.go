package kit

// images.go — generic container-image teardown helper for co-located deploy plugins (a plugin runs ON
// the host, so it can drive the host container engine directly). The externalized pod deploy plugin
// drops its synthesized overlay images itself here, instead of forwarding to a hidden core command.

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// runEngineCommand runs an engine command bounded by ctx, killing the whole
// process group on timeout (a shell wrapper's children must not survive and
// hold the output pipe open).
func runEngineCommand(ctx context.Context, engineBin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, engineBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	out, err := cmd.Output()
	return string(out), err
}

// engineCommandTimeout bounds every engine command RemoveImagesByReference runs.
// Under heavy concurrent load the container engine can stall (a saturated podman
// daemon), and an unbounded exec would hang the calling bed's cleanup step in
// futex_wait forever (the recurring fleet-del/remove stall). A package var (not a
// const) so a test can shorten it.
var engineCommandTimeout = 2 * time.Minute

// RemoveImagesByReference best-effort removes every local image whose repository BASENAME exactly
// equals `reference` (e.g. "<deploy>-overlay") via `<engineBin> images … | rmi`. Silent on error
// (image cleanup is best-effort). engineBin is the resolved engine binary (the host resolves
// "podman"/"docker"/"auto" and passes the concrete binary — kit does no detection).
//
// The `--filter reference=<reference>` glob is NOT trusted alone: podman lists EVERY repo:tag of any
// matched image ID, so a base image or a cross-deploy image that shares content (same image ID as the
// overlay) leaks into the output. rmi'ing those blindly would destroy the base + unrelated deploys'
// images. So the emitted repo is re-checked in Go and only the EXACT `<reference>` repo is removed.
func RemoveImagesByReference(engineBin, reference string) {
	if engineBin == "" {
		engineBin = "podman"
	}
	// Bound the engine commands with a timeout: under heavy concurrent load the
	// container engine can stall (a saturated podman daemon), and an unbounded
	// exec would hang the calling bed's cleanup step in futex_wait forever (the
	// recurring fleet-del/remove stall). Fail fast instead — image cleanup is
	// best-effort, so a timed-out list/rmi is a skipped cleanup, never a hang.
	ctx, cancel := context.WithTimeout(context.Background(), engineCommandTimeout)
	defer cancel()
	out, err := runEngineCommand(ctx, engineBin, "images",
		"--filter", "reference="+reference, "--format", "{{.Repository}} {{.Tag}}")
	if err != nil {
		return
	}
	for _, ref := range exactRepoRefs(string(out), reference) {
		_, _ = runEngineCommand(ctx, engineBin, "rmi", ref)
	}
}

// exactRepoRefs parses `<repository> <tag>` lines and returns the "<repository>:<tag>" refs whose
// repository BASENAME (the last path segment) exactly equals reference. This filters out the base and
// cross-deploy images that podman's reference filter leaks via shared image IDs, so only the deploy's
// own `<reference>` overlay is dropped. Split out as a pure function so the (destructive) selection is
// unit-testable without a live engine.
func exactRepoRefs(imagesOutput, reference string) []string {
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(imagesOutput), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		repo, tag := fields[0], fields[1]
		base := repo
		if i := strings.LastIndex(repo, "/"); i >= 0 {
			base = repo[i+1:]
		}
		if base == reference {
			refs = append(refs, repo+":"+tag)
		}
	}
	return refs
}
