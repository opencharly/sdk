package deploykit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/shellquote"
)

// image_transfer.go — the VENUE-GENERIC verified image delivery path.
//
// Delivering a locally-built image into a venue's own container store is the same
// problem at every venue; only the way the load side is REACHED differs:
//
//	VM guest        ssh <alias> [sudo] podman load          (charly vm cp-box)
//	pod, nested     podman exec -i <ctr> podman --remote …  (charly box load)
//
// Everything else — the idempotency skip, the torn-overlay integrity probe, the
// drop-and-re-stream recovery, the post-load tag — is venue-independent, and used
// to exist only in the VM path. This file is that logic, lifted out and expressed
// against DeployExecutor + an injected load command, so a second venue costs a
// constructor rather than a copy.

// ImageVenue describes a container-image store together with the two channels
// needed to deliver into it and then interrogate it.
type ImageVenue struct {
	// Exec runs probe/tag/remove commands INSIDE the venue.
	Exec DeployExecutor

	// PodmanCmd is the in-venue podman invocation prefix, and is the ONE place a
	// venue's storage scope is decided — "podman" (the venue user's rootless
	// store), "sudo podman" (root's store), or a remote form such as
	// "podman --remote --url unix:///run/user/1000/podman/podman.sock" (a nested
	// store served over its own API socket). Every probe, the tag, and the
	// removal all go through it, so the load and the verification can never
	// disagree about WHICH store they are talking about.
	PodmanCmd string

	// Rootless selects RunUser over RunSystem for the post-load tag, matching the
	// privilege the load itself used.
	Rootless bool

	// NewLoadCmd builds the HOST-side process that reads a `podman save` stream on
	// stdin and lands it in this venue's store. Called once per stream attempt —
	// a *exec.Cmd is single-use, and the recovery path streams twice.
	NewLoadCmd func() *exec.Cmd

	// Label prefixes progress and diagnostic lines ("cp-box", "box load").
	Label string
}

// TransferImageToVenue streams a host image into a venue's container store by
// piping `<hostEngine> save <ref>` straight into that venue's load command — NO
// intermediate tarball on either side. (A file-based copy fails for a multi-GB
// image when the destination's /tmp is a size-limited tmpfs, which is the common
// case for both a VM guest and a container.)
//
// VERIFIED transfer: `podman load` can exit 0 on a TRUNCATED stream and register
// an image whose overlay layers are incomplete — a `podman run` then fails with
// `faccessat …/storage/overlay/<hash>: no such file`. So the transfer is never
// trusted on the load exit code alone:
//   - The idempotency skip fires only when the venue already holds the target ref
//     AND that image is verified intact (a name-only check would wrongly skip a
//     present-but-torn image — the case a disposable bed hits when a rebuild
//     reuses persistent storage, so a prior, possibly partial, image survives).
//   - After a fresh load the image is probed; on the overlay-corruption signature
//     it is dropped and re-streamed ONCE; a second failure is a hard error,
//     surfaced rather than silently shipped as a broken image.
func TransferImageToVenue(ctx context.Context, v ImageVenue, hostEngine, ref, as string, opts EmitOpts) error {
	if v.Exec == nil {
		return fmt.Errorf("%s: nil executor", v.Label)
	}
	if v.NewLoadCmd == nil {
		return fmt.Errorf("%s: nil load command builder", v.Label)
	}
	if hostEngine == "" {
		hostEngine = "podman"
	}
	probeRef := ref
	if as != "" {
		probeRef = as
	}

	// Verified idempotency.
	if VenueHasImage(ctx, v, probeRef) {
		if !VenueImageCorrupt(ctx, v, probeRef) {
			fmt.Fprintf(os.Stderr, "%s: venue already has %s (verified intact) — skipping transfer\n", v.Label, probeRef)
			return nil
		}
		fmt.Fprintf(os.Stderr, "%s: venue %s is present but corrupt (torn overlay) — re-loading\n", v.Label, probeRef)
		removeVenueImages(ctx, v, probeRef, ref)
	}

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] %s save %s | %s load\n", hostEngine, ref, v.PodmanCmd)
		return nil
	}

	if err := streamAndTag(ctx, v, hostEngine, ref, as, opts); err != nil {
		return err
	}
	if VenueImageCorrupt(ctx, v, probeRef) {
		fmt.Fprintf(os.Stderr, "%s: load produced a corrupt %s — re-streaming once\n", v.Label, probeRef)
		removeVenueImages(ctx, v, probeRef, ref)
		if err := streamAndTag(ctx, v, hostEngine, ref, as, opts); err != nil {
			return err
		}
		if VenueImageCorrupt(ctx, v, probeRef) {
			return fmt.Errorf("%s: %s is still corrupt in venue storage after a clean re-load — transfer unreliable", v.Label, probeRef)
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %s is now in venue storage (verified intact)\n", v.Label, probeRef)
	return nil
}

// VenueHasImage reports whether the venue's store holds the image by name.
func VenueHasImage(ctx context.Context, v ImageVenue, ref string) bool {
	_, _, code, err := v.Exec.RunCapture(ctx, v.PodmanCmd+" image exists "+shellquote.ShellQuote(ref))
	return err == nil && code == 0
}

// VenueImageCorrupt reports whether an existing image is unusable because its
// overlay storage is torn (a lower layer's diff dir is missing). It mounts the
// image's rootfs via a throwaway `podman run … /usr/bin/true` (no GPU, no
// entrypoint) — a torn layer fails container setup with a
// `…/storage/overlay/<hash>: no such file` error. ONLY that storage signature
// counts as corruption: any other failure (the probe binary is absent, an exotic
// entrypoint) means the overlay mounted fine, so the image is treated as intact —
// this is an integrity check, not an entrypoint test.
func VenueImageCorrupt(ctx context.Context, v ImageVenue, ref string) bool {
	stdout, stderr, code, err := v.Exec.RunCapture(ctx,
		v.PodmanCmd+" run --rm --entrypoint /usr/bin/true "+shellquote.ShellQuote(ref))
	if err == nil && code == 0 {
		return false
	}
	return strings.Contains(stdout+stderr, "storage/overlay")
}

// removeVenueImages best-effort removes the given refs (and their now-unused
// layers) so a subsequent load re-extracts clean overlay dirs. `podman rmi -f` on
// EVERY ref pointing at the torn image ID is required — dropping only one tag
// leaves the broken layers in storage, and a re-load that shares those layer
// digests would skip extraction and inherit the corruption.
func removeVenueImages(ctx context.Context, v ImageVenue, refs ...string) {
	for _, r := range refs {
		if r == "" {
			continue
		}
		_, _, _, _ = v.Exec.RunCapture(ctx, v.PodmanCmd+" rmi -f "+shellquote.ShellQuote(r))
	}
}

// streamAndTag streams `hostEngine save <ref>` into the venue's store, then (when
// `as` is set) tags the loaded ref under that stable name. The tag runs through
// the SAME PodmanCmd the load targeted, so it can never land in a different store
// than the image did.
func streamAndTag(ctx context.Context, v ImageVenue, hostEngine, ref, as string, opts EmitOpts) error {
	fmt.Fprintf(os.Stderr, "%s: streaming %s into venue storage (save | load)...\n", v.Label, ref)
	if err := container.StreamLoad(
		exec.CommandContext(ctx, hostEngine, "save", ref),
		v.NewLoadCmd(),
	); err != nil {
		return fmt.Errorf("%s: %w", v.Label, err)
	}
	if as == "" {
		return nil
	}
	tag := v.PodmanCmd + " tag " + shellquote.ShellQuote(ref) + " " + shellquote.ShellQuote(as)
	var tagErr error
	if v.Rootless {
		tagErr = v.Exec.RunUser(ctx, tag, opts)
	} else {
		tagErr = v.Exec.RunSystem(ctx, tag, opts)
	}
	if tagErr != nil {
		return fmt.Errorf("%s: venue podman tag %s -> %s: %w", v.Label, ref, as, tagErr)
	}
	return nil
}
