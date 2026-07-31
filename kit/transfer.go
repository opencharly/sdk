package kit

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/opencharly/spec/container"
)

// LocalImageExists checks whether an image reference exists in the given engine's local store.
// Package-level var for testability (same pattern as DetectGPU in gpu.go). RELOCATED to the
// spec/container fabric slice (#55 coneB build-render cone, Class A — co-located with the
// ResolveLocalImageRef family that reads it); re-exported here so every existing direct
// kit.LocalImageExists call site (candy/plugin-build, candy/plugin-box, candy/plugin-deploy-pod,
// candy/plugin-kube, charly core's host_build_pod_config_seams + ensure_image) is unchanged.
// Override container.LocalImageExists (the var container.ResolveLocalImageRef reads) to stub the
// resolution path in tests; this kit re-export var is a value-copy that no longer affects the
// relocated body.
var LocalImageExists = container.LocalImageExists

// TransferImage pipes an image from one engine to another via save | load. RELOCATED to
// the spec/container fabric slice (#55 coneC — charly/ off sdk/kit, co-located with the
// EngineBinary + LocalImageExists family this transfer path complements); re-exported here
// so every existing direct kit.TransferImage call site (candy/plugin-build, candy/plugin-box,
// candy/plugin-deploy-pod, …) is unchanged. A package-level var (not a func) so tests can
// override it the same way as LocalImageExists.
var TransferImage = container.TransferImage

// SudoLocalImageExists checks whether an image reference exists in the rootful
// (sudo podman) local store. Mirrors LocalImageExists but always queries the
// root user's storage namespace, regardless of the caller's BuildEngine. The
// rootless and rootful podman storage roots are isolated, so an image built by
// the user's `podman build` is invisible to `sudo podman` until transferred.
//
// Package-level var for testability (same pattern as LocalImageExists).
var SudoLocalImageExists = defaultSudoLocalImageExists

func defaultSudoLocalImageExists(imageRef string) bool {
	cmd := exec.Command("sudo", "-n", "podman", "image", "exists", imageRef)
	return cmd.Run() == nil
}

// TransferToRootful pipes an image from rootless podman storage into rootful
// (sudo podman) storage via `podman save | sudo podman load`. Idempotent —
// returns nil immediately when the image already exists in rootful storage.
//
// Used by RunPrivileged when engine.rootful=sudo because rootless and rootful
// podman maintain separate container-storage trees (~/.local/share/containers
// vs /var/lib/containers). Without this transfer, sudo podman run against a
// locally-built image falls back to a registry pull (which 403s for
// build-only images that were never pushed).
//
// Surfaced 2026-05 by the cachyos / cachyos-pacstrap-builder pair — the
// first time the bootstrap-builder framework was exercised end-to-end on a
// host with rootless build + sudo run.
func TransferToRootful(imageRef string) error {
	if SudoLocalImageExists(imageRef) {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Transferring %s into rootful podman storage (rootless build → sudo run)\n", imageRef)

	save := exec.Command("podman", "save", imageRef)
	load := exec.Command("sudo", "-n", "podman", "load")

	pipe, err := save.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %w", err)
	}
	load.Stdin = pipe
	load.Stderr = os.Stderr
	save.Stderr = os.Stderr

	if err := load.Start(); err != nil {
		return fmt.Errorf("starting sudo podman load: %w", err)
	}
	if err := save.Run(); err != nil {
		return fmt.Errorf("podman save %s: %w", imageRef, err)
	}
	if err := load.Wait(); err != nil {
		return fmt.Errorf("sudo podman load: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Transferred %s into rootful storage\n", imageRef)
	return nil
}
