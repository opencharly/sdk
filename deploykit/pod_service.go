package deploykit

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/opencharly/sdk/kit"
)

// pod_service.go — the already-configured-pod service start/stop/restart (K4: relocated from
// charly/start.go). Homed in deploykit (not kit) because they need ResolveBoxEngineForDeploy, a
// deploykit-only mechanism (kit cannot import deploykit — see box_engine.go).
//
// CURRENT STATE (corrected 2026-07-20): as of this commit, charly main STILL carries the
// original bare stopPodService/startPodService in charly/start.go, called directly by the
// resource arbiter (preempt.go) — a bare, reversible service stop/restore that leaves the
// holder's disk/container intact — and by StartCmd/StopCmd's own bodies. The companion
// DEPLOY-wave charly PR deletes those bare originals and repoints preempt.go (+ every other
// caller) to deploykit.StopPodService/StartPodService/RestartPodService directly (K3
// ZERO-ALIASES — no alias file), tracked there, not yet true on main today.

// StopPodService stops a running pod deployment — the quadlet service when
// one exists (always via systemctl, so podman-stop + Restart=always can't
// create a restart loop), else the container directly via the resolved engine
// with a fallback to the other engine. It performs NO tunnel/unmount side
// effects — callers layer those on. Used by `charly stop` and the resource
// arbiter's preemption path, which wants a bare, reversible service stop that
// leaves the holder's disk/container intact for restart.
func StopPodService(boxName, instance string) error {
	quadletActive, _ := kit.QuadletExistsInstance(boxName, instance)
	if quadletActive {
		svc := kit.ServiceNameInstance(boxName, instance)
		cmd := exec.Command("systemctl", "--user", "stop", svc)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("stopping %s: %w", svc, err)
		}
		fmt.Fprintf(os.Stderr, "Stopped %s\n", svc)
		return nil
	}

	rt, err := kit.ResolveRuntime()
	if err != nil {
		return err
	}
	runEngine := ResolveBoxEngineForDeploy(boxName, instance, rt.RunEngine)
	engine := kit.EngineBinary(runEngine)
	name := kit.ContainerNameInstance(boxName, instance)

	cmd := exec.Command(engine, "stop", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: try the other engine if the container wasn't found
		otherEngine := "docker"
		if runEngine == "docker" {
			otherEngine = "podman"
		}
		otherBinary := kit.EngineBinary(otherEngine)
		fallbackCmd := exec.Command(otherBinary, "stop", name)
		if _, fallbackErr := fallbackCmd.CombinedOutput(); fallbackErr == nil {
			fmt.Fprintf(os.Stderr, "Stopped %s (via %s)\n", name, otherEngine)
			return nil
		}
		return fmt.Errorf("%s stop failed: %w\n%s", engine, err, strings.TrimSpace(string(output)))
	}

	fmt.Fprintf(os.Stderr, "Stopped %s\n", name)
	return nil
}

// StartPodService starts an already-configured pod deployment — the quadlet
// service when one exists, else the existing stopped container via the
// resolved engine. Used by the resource arbiter to restore a preempted holder:
// the deployment's quadlet/container already exists (the holder was running
// before preemption), so this is a plain service/container start, not a full
// `charly start` re-config.
func StartPodService(boxName, instance string) error {
	quadletActive, _ := kit.QuadletExistsInstance(boxName, instance)
	if quadletActive {
		svc := kit.ServiceNameInstance(boxName, instance)
		cmd := exec.Command("systemctl", "--user", "start", svc)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("starting %s: %w", svc, err)
		}
		fmt.Fprintf(os.Stderr, "Started %s\n", svc)
		return nil
	}

	rt, err := kit.ResolveRuntime()
	if err != nil {
		return err
	}
	runEngine := ResolveBoxEngineForDeploy(boxName, instance, rt.RunEngine)
	engine := kit.EngineBinary(runEngine)
	name := kit.ContainerNameInstance(boxName, instance)

	cmd := exec.Command(engine, "start", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s start failed: %w\n%s", engine, err, strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(os.Stderr, "Started %s\n", name)
	return nil
}

// RestartPodService restarts a service container. In quadlet mode it issues a single `systemctl
// --user restart`, which is atomic from systemd's perspective — ExecStopPost (e.g. tailscale
// serve --off) runs before ExecStartPost (tailscale serve), and the unit ends in either active or
// failed, never the silent stopped state a manual stop+start sequence can produce when start
// fails. Direct mode delegates to the resolved engine's restart. Used by `charly restart`.
func RestartPodService(boxName, instance string) error {
	rt, err := kit.ResolveRuntime()
	if err != nil {
		return err
	}

	quadletActive, _ := kit.QuadletExistsInstance(boxName, instance)
	if quadletActive {
		svc := kit.ServiceNameInstance(boxName, instance)
		cmd := exec.Command("systemctl", "--user", "restart", svc)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("restarting %s: %w", svc, err)
		}
		fmt.Fprintf(os.Stderr, "Restarted %s\n", svc)
		return nil
	}

	// Direct mode: delegate to engine restart.
	runEngine := ResolveBoxEngineForDeploy(boxName, instance, rt.RunEngine)
	engine := kit.EngineBinary(runEngine)
	name := kit.ContainerNameInstance(boxName, instance)

	cmd := exec.Command(engine, "restart", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s restart %s failed: %w\n%s", engine, name, err, strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(os.Stderr, "Restarted %s\n", name)
	return nil
}
