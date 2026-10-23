package kit

import "github.com/opencharly/spec/spec"

// init_runtime.go — the container-venue init RUNTIME argv, in ONE place.
//
// A container engine can host a real init as PID 1 (systemd does, under
// podman/runc on cgroup v2) only when it is TOLD so: systemd mode gives it the
// cgroup subtree, the /run tmpfs and the SIGRTMIN+3 stop signal. The declaration
// lives on the init definition and is carried to the deploy on the image's
// `ai.opencharly.init_def` label (spec.CapabilityInitDef), so no consumer has to
// switch on an init-system NAME — and both argv builders (the detached service
// launch and plugin-deploy-pod's direct-mode twin) and the quadlet renderer read
// the same declaration.
//
// The engine's own spelling of its systemd flag is DATA (`systemd_mode_arg` on
// the engine capability table), never a literal: podman takes
// `--systemd=always`, containerd/nerdctl take their own form, and an engine with
// no such mode (docker) yields no flag at all rather than an invented one.

// InitContainerRuntimeArgs returns the engine argv that puts a container into the
// init system's declared container-runtime mode. Nil when the init declares none
// (every machine-venue init, and every image that predates the label).
func InitContainerRuntimeArgs(engine string, initDef *spec.CapabilityInitDef) []string {
	if initDef == nil {
		return nil
	}
	var args []string
	if initDef.SystemdMode {
		if c, ok := EngineCapabilityFor(engine); ok && c.SystemdModeArg != "" {
			args = append(args, c.SystemdModeArg)
		}
	}
	// SIGRTMIN+3 is systemd's `systemctl halt`. The signal name is carried as
	// authored data; the flag spelling (`--stop-signal`) is the engine-neutral
	// podman/docker/nerdctl form.
	if initDef.StopSignal != "" {
		args = append(args, "--stop-signal="+initDef.StopSignal)
	}
	return args
}

// InitRequiresRoot reports whether the image's init declares that the CONTAINER
// must run as root. The DEPLOY overlay consults it before restoring a USER; the
// BUILD reads the same field directly on its own generated type
// (spec.ResolvedInit.RequiresRoot in deploykit/generate.go), so this helper takes
// the deploy-side type (spec.CapabilityInitDef) and is not a second predicate —
// both surfaces read one declared field.
func InitRequiresRoot(initDef *spec.CapabilityInitDef) bool {
	return initDef != nil && initDef.RequiresRoot
}
