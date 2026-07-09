package kit

import (
	"fmt"
	"os"
	"os/exec"
)

// LocalImageExists checks whether an image reference exists in the given engine's local store.
// Package-level var for testability (same pattern as DetectGPU in gpu.go).
var LocalImageExists = defaultLocalImageExists

func defaultLocalImageExists(engine, imageRef string) bool {
	binary := EngineBinary(engine)
	switch engine {
	case "podman":
		cmd := exec.Command(binary, "image", "exists", imageRef)
		return cmd.Run() == nil
	default:
		// Docker has no "image exists" subcommand; use "image inspect"
		cmd := exec.Command(binary, "image", "inspect", imageRef)
		cmd.Stdout = nil
		cmd.Stderr = nil
		return cmd.Run() == nil
	}
}

// TransferImage pipes an image from one engine to another via save | load.
func TransferImage(srcEngine, dstEngine, imageRef string) error {
	srcBinary := EngineBinary(srcEngine)
	dstBinary := EngineBinary(dstEngine)

	fmt.Fprintf(os.Stderr, "Transferring %s from %s to %s\n", imageRef, srcEngine, dstEngine)

	save := exec.Command(srcBinary, "save", imageRef)
	load := exec.Command(dstBinary, "load")

	pipe, err := save.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %w", err)
	}
	load.Stdin = pipe
	load.Stderr = os.Stderr

	if err := load.Start(); err != nil {
		return fmt.Errorf("starting %s load: %w", dstBinary, err)
	}
	if err := save.Run(); err != nil {
		return fmt.Errorf("%s save failed: %w", srcBinary, err)
	}
	if err := load.Wait(); err != nil {
		return fmt.Errorf("%s load failed: %w", dstBinary, err)
	}

	fmt.Fprintf(os.Stderr, "Transferred %s to %s\n", imageRef, dstEngine)
	return nil
}

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
