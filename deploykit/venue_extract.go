package deploykit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/shellquote"
)

// venue_extract.go — the VENUE-AGNOSTIC ExtractStep execution path: the machine-venue
// (target:local / target:vm) analog of the Containerfile extract stages
// (EmitExtractStages / EmitExtractedFiles). A candy's `extract:` entries are build-time
// Containerfile operations (FROM <source> AS <candy>-extract-<i> + COPY --from=...), so a
// machine venue — where the built image filesystem is never used and the install plan
// executes over the venue's own filesystem — could not receive extracted content. This
// replicates the COPY --from semantics on the HOST (where podman + the source images
// live) and ships the content into the venue over the DeployExecutor, mirroring the
// BuilderStep host-engine pattern (RunVenueBuilderStep).
//
// The COPY semantics are replicated exactly:
//
//   - Path is a DIRECTORY → its CONTENTS are extracted into Dest (Dest is a directory).
//   - Path is a FILE → the file lands at Dest (Dest is the file path; a trailing-slash
//     Dest means "into this directory", keeping the source basename).
//
// Ownership is NOT handled here — the candy's own plan steps chown the extracted paths
// (the agentteams-higress candy chowns /etc/certs /etc/istio /var/lib/istio
// /var/log/proxy to uid 1000), mirroring the build where COPY --chown is explicit per
// entry. The extract itself runs as root (ScopeSystem — the dests are system paths).
//
// resolveImage/ensureImage are the injected image-resolve/ensure closures (the SAME
// shape RunVenueBuilderStep takes) — the caller's ONE genuine core dependency (a project
// Config + dir to resolve a short/namespace-qualified source ref and fall back to a local
// `charly box build`). The podman create/cp/rm + tar run through charly's own machinery
// (kit.EngineBinary + exec.CommandContext), never ad-hoc operator podman.

// RunVenueExtractStep materializes one ExtractStep onto the venue the executor addresses.
func RunVenueExtractStep(ctx context.Context, dexec DeployExecutor, resolveImage func(string) (string, error), ensureImage func(context.Context, string) error, s *ExtractStep, opts EmitOpts) error {
	if s.Source == "" || s.Path == "" || s.Dest == "" {
		return fmt.Errorf("extract step (candy=%s): source/path/dest are all required (got %q %q %q)", s.CandyName, s.Source, s.Path, s.Dest)
	}
	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] extract %s:%s -> %s (candy=%s)\n", s.Source, s.Path, s.Dest, s.CandyName)
		return nil
	}

	// Resolve a short/namespace-qualified source ref to its concrete podman storage key
	// (side-effect-free), then ensure the image is present (pull / local-build fallback).
	source := s.Source
	if resolveImage != nil {
		if resolved, rerr := resolveImage(source); rerr == nil && resolved != "" {
			source = resolved
		}
	}
	if ensureImage != nil {
		if err := ensureImage(ctx, s.Source); err != nil {
			return fmt.Errorf("extract %s (candy=%s): ensure source image: %w", s.Source, s.CandyName, err)
		}
	}

	// Host staging dir for the podman cp output + the transfer tarball. The MkdirTemp
	// suffix doubles as the unique scope for the container name AND the venue tarball
	// name, so two concurrent deploys of the same candy to the same venue never
	// interleave (the second PutFile overwriting the first's tarball before its extract).
	stageHost, err := os.MkdirTemp("", "charly-extract-")
	if err != nil {
		return fmt.Errorf("extract staging mkdir: %w", err)
	}
	proc.RegisterTempCleanup(stageHost)
	defer func() { _ = os.RemoveAll(stageHost); proc.UnregisterTempCleanup(stageHost) }()
	scope := filepath.Base(stageHost)

	// Create a throwaway container from the source image (no run — we only need a
	// filesystem view to cp from), cp the path out, and remove the container. This is
	// the same podman create + cp + rm path the data-provisioning mechanism uses
	// (provisionFromScratchImageToHost) — charly's own machinery.
	binary := kit.EngineBinary("auto")
	containerName := "charly-extract-" + scope
	if err := runEngineCmd(ctx, binary, "create", "--name", containerName, source); err != nil {
		return fmt.Errorf("extract %s (candy=%s): podman create: %w", s.Source, s.CandyName, err)
	}
	cpErr := runEngineCmd(ctx, binary, "cp", containerName+":"+s.Path, stageHost+"/")
	rmErr := runEngineCmd(ctx, binary, "rm", containerName)
	if cpErr != nil {
		return fmt.Errorf("extract %s:%s (candy=%s): podman cp: %w", s.Source, s.Path, s.CandyName, cpErr)
	}
	if rmErr != nil {
		return fmt.Errorf("extract %s (candy=%s): podman rm: %w", s.Source, s.CandyName, rmErr)
	}

	// Determine file-vs-dir from the copied path (podman cp places it at
	// <staging>/<basename(path)>), then tar to replicate the COPY semantics.
	copied := filepath.Join(stageHost, filepath.Base(s.Path))
	info, err := os.Lstat(copied)
	if err != nil {
		return fmt.Errorf("extract %s:%s (candy=%s): stat copied path: %w", s.Source, s.Path, s.CandyName, err)
	}

	tarball := filepath.Join(stageHost, "extract.tar.gz")
	var tarArgs []string
	var venueExtractDir string
	if info.IsDir() {
		// Directory path → COPY copies the CONTENTS into Dest (Dest is a directory).
		tarArgs = []string{"-C", copied, "-czf", tarball, "."}
		venueExtractDir = s.Dest
	} else {
		// File path → COPY lands the file at Dest (Dest is the file path; a trailing
		// slash means "into this directory", keeping the source basename). Rename the
		// copied file to basename(Dest) so the tarball root name matches the destination.
		// The trailing-slash test must be EXPLICIT: filepath.Base strips trailing slashes,
		// so Base("/usr/local/bin/") is "bin" — without the HasSuffix check a trailing-slash
		// Dest would rename the file to "bin" instead of keeping the source basename.
		name := filepath.Base(s.Dest)
		if strings.HasSuffix(s.Dest, "/") || name == "" || name == "." || name == string(filepath.Separator) {
			name = filepath.Base(s.Path)
		}
		if name != filepath.Base(s.Path) {
			if err := os.Rename(copied, filepath.Join(stageHost, name)); err != nil {
				return fmt.Errorf("extract %s:%s (candy=%s): rename to dest basename: %w", s.Source, s.Path, s.CandyName, err)
			}
		}
		tarArgs = []string{"-C", stageHost, "-czf", tarball, name}
		venueExtractDir = filepath.Dir(s.Dest)
	}
	if err := tarCreate(ctx, tarArgs...); err != nil {
		return fmt.Errorf("extract %s:%s (candy=%s): tar: %w", s.Source, s.Path, s.CandyName, err)
	}

	// Ship the tarball into the venue and extract it in place (as root — the dests are
	// system paths). The tarball name carries the unique scope so concurrent deploys of
	// the same candy to the same venue cannot interleave.
	venueTar := "/tmp/charly-extract-" + s.CandyName + "-" + scope + ".tar.gz"
	if err := dexec.PutFile(ctx, tarball, venueTar, 0o644, true, opts); err != nil {
		return fmt.Errorf("extract %s:%s (candy=%s): transfer: %w", s.Source, s.Path, s.CandyName, err)
	}
	extractScript := fmt.Sprintf("set -e\nmkdir -p %s\ntar -C %s -xzf %s\n",
		shellquote.ShellQuote(venueExtractDir), shellquote.ShellQuote(venueExtractDir), shellquote.ShellQuote(venueTar))
	if err := dexec.RunSystem(ctx, extractScript, opts); err != nil {
		return fmt.Errorf("extract %s:%s (candy=%s): extract on venue: %w", s.Source, s.Path, s.CandyName, err)
	}
	// Remove the tarball AS ROOT: PutFile placed it via `sudo install`, so it is
	// root-owned, and /tmp is sticky (1777) — the venue user can't remove a root-owned
	// file there. Mirrors the builder tarball cleanup in RunVenueHomeArtifactBuilder.
	if err := dexec.RunSystem(ctx, fmt.Sprintf("rm -f %s\n", shellquote.ShellQuote(venueTar)), opts); err != nil {
		return fmt.Errorf("extract %s:%s (candy=%s): remove tarball on venue: %w", s.Source, s.Path, s.CandyName, err)
	}
	return nil
}

// runEngineCmd runs one container-engine command (create/cp/rm) with stderr inherited,
// so podman's own diagnostics reach the operator. A package-level var so tests can
// substitute a recorder that materializes the copied path without spawning a real engine.
var runEngineCmd = func(ctx context.Context, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// tarCreate runs one host tar command (the COPY-semantics tarball). A package-level var so
// tests can record the tarball root name (the file-vs-dir + trailing-slash decisions land in
// the tar args) without re-implementing tar.
var tarCreate = func(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "tar", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
