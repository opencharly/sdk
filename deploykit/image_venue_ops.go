package deploykit

// image_venue_ops.go — the venue-op seam that lets `ImageVenue` address a store that
// is NOT podman. The podman-shaped probes in image_transfer.go (`… image exists`,
// `… run --rm`, `… rmi -f`, `… tag`) have no crictl/ctr equivalent — a k8s NODE's
// store is containerd, reached with `ctr -n k8s.io images …`, and `ctr` has no
// `run` (so no torn-overlay probe). Rather than fork the verified-transfer logic
// per store kind (R3), the four store operations move behind this interface; the
// podman implementation below is byte-for-byte today's behaviour, so existing
// venues (charly vm cp-box, charly box load) are unchanged.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/opencharly/spec/shellquote"
)

// venueOps is the store-kind seam: the four operations the verified transfer needs,
// expressed against the venue's own engine verbs. One implementation per store kind
// (podman today; containerd/crictl for a k8s node).
type venueOps interface {
	// HasImage reports whether the store holds the ref by name.
	HasImage(ctx context.Context, ref string) bool
	// IsCorrupt reports whether an existing image is unusable — the torn-overlay
	// signature — or false when the store has no way to probe for it.
	IsCorrupt(ctx context.Context, ref string) bool
	// Remove best-effort removes the refs so a re-load re-extracts clean layers.
	Remove(ctx context.Context, refs ...string)
	// Tag applies a stable ref to an already-loaded one.
	Tag(ctx context.Context, ref, as string, opts EmitOpts) error
	// Describe names the store for a dry-run line.
	Describe() string
}

// podmanOps is the DEFAULT venue op set — the in-venue podman store (a VM guest's or
// a pod's, local or served over `--remote --url`). Every command goes through the one
// prefix so load, probe, tag and removal can never address a different store.
type podmanOps struct {
	exec      DeployExecutor
	podmanCmd string
	rootless  bool
}

func (o podmanOps) HasImage(ctx context.Context, ref string) bool {
	_, _, code, err := o.exec.RunCapture(ctx, o.podmanCmd+" image exists "+shellquote.ShellQuote(ref))
	return err == nil && code == 0
}

// IsCorrupt mounts the image's rootfs via a throwaway `podman run … /usr/bin/true`
// (no GPU, no entrypoint); a torn layer fails container setup with a
// `…/storage/overlay/<hash>: no such file` error. ONLY that storage signature counts:
// any other failure (probe binary absent, exotic entrypoint) means the overlay mounted
// fine, so the image is treated as intact — an integrity check, not an entrypoint test.
func (o podmanOps) IsCorrupt(ctx context.Context, ref string) bool {
	stdout, stderr, code, err := o.exec.RunCapture(ctx,
		o.podmanCmd+" run --rm --entrypoint /usr/bin/true "+shellquote.ShellQuote(ref))
	if err == nil && code == 0 {
		return false
	}
	return strings.Contains(stdout+stderr, "storage/overlay")
}

func (o podmanOps) Remove(ctx context.Context, refs ...string) {
	for _, r := range refs {
		if r == "" {
			continue
		}
		_, _, _, _ = o.exec.RunCapture(ctx, o.podmanCmd+" rmi -f "+shellquote.ShellQuote(r))
	}
}

func (o podmanOps) Tag(ctx context.Context, ref, as string, opts EmitOpts) error {
	tag := o.podmanCmd + " tag " + shellquote.ShellQuote(ref) + " " + shellquote.ShellQuote(as)
	if o.rootless {
		return o.exec.RunUser(ctx, tag, opts)
	}
	return o.exec.RunSystem(ctx, tag, opts)
}

func (o podmanOps) Describe() string { return o.podmanCmd }

// ctrOps addresses a k8s NODE's containerd store through the CRI CLI. `ctr -n k8s.io`
// is the namespace kubelet uses, so an image imported here is visible to the cluster
// without a registry pull. There is no `run` in ctr, so IsCorrupt is a no-op (false):
// the node venue relies on ctr's import being atomic; the podman torn-overlay recovery
// is not applicable and is documented as such rather than faked.
type ctrOps struct {
	exec   DeployExecutor
	ctrCmd string // e.g. "ctr -n k8s.io"
}

func (o ctrOps) HasImage(ctx context.Context, ref string) bool {
	// `ctr images ls -q` prints one ref per line; match on the exact ref.
	stdout, _, code, err := o.exec.RunCapture(ctx, o.ctrCmd+" images ls -q")
	if err != nil || code != 0 {
		return false
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == ref {
			return true
		}
	}
	return false
}

// IsCorrupt is intentionally false: containerd's `ctr images import` is atomic, and
// there is no `run` to probe a torn layer. Returning a real probe here would require a
// container runtime the node CLI does not expose.
func (o ctrOps) IsCorrupt(_ context.Context, _ string) bool { return false }

func (o ctrOps) Remove(ctx context.Context, refs ...string) {
	for _, r := range refs {
		if r == "" {
			continue
		}
		_, _, _, _ = o.exec.RunCapture(ctx, o.ctrCmd+" images rm "+shellquote.ShellQuote(r))
	}
}

func (o ctrOps) Tag(ctx context.Context, ref, as string, _ EmitOpts) error {
	// ctr tag runs on the node (rootless is not a concept for the node store); through
	// the venue executor, which reaches the node.
	_, stderr, code, err := o.exec.RunCapture(ctx,
		o.ctrCmd+" images tag "+shellquote.ShellQuote(ref)+" "+shellquote.ShellQuote(as))
	if err != nil {
		return fmt.Errorf("ctr images tag %s %s: %w", ref, as, err)
	}
	if code != 0 {
		return fmt.Errorf("ctr images tag %s %s: exit %d: %s", ref, as, code, strings.TrimSpace(stderr))
	}
	return nil
}

func (o ctrOps) Describe() string { return o.ctrCmd }

// NewNodeVenue builds an ImageVenue whose store is a k8s NODE's containerd, reached
// through exec (a DeployExecutor that runs on the node — e.g. a host-engine exec into a
// kind node container, or a kubectl-exec into a node's debug pod). newLoadCmd must
// stream a `podman save` archive on ITS STDIN into `ctr -n k8s.io images import -`.
// This is the generalised form of plugin-kube's kindLoadImage host→node path, lifted so
// a second node kind costs a constructor (R3), not a copy.
func NewNodeVenue(exec DeployExecutor, ctrCmd string, newLoadCmd func() *exec.Cmd, label string) ImageVenue {
	if ctrCmd == "" {
		ctrCmd = "ctr -n k8s.io"
	}
	return ImageVenue{
		Exec:       exec,
		NewLoadCmd: newLoadCmd,
		Label:      label,
		ops:        ctrOps{exec: exec, ctrCmd: ctrCmd},
	}
}
