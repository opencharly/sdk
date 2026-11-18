package deploykit

// image_resolve.go — W3.7's ONE resolution order for a reference, whatever the
// engine:
//
//  1. present in the target store                     → use it
//  2. present in the charly image store               → import into the target
//  3. present in the registry                         → pull into the target
//  4. otherwise `build:ensure` (W3.6)                 → then step 2
//
// An empty tag resolves to the newest CalVer across the target store and the
// charly store.
//
// The charly image store is an INTERFACE here, never a second implementation of
// its OCI layout. That layout — the `oci-layout` marker, one `index.json`,
// `blobs/<alg>/<digest>`, the atomic index rewrite under a cross-process lock, and
// the `io.containerd.image.name` naming annotation — lives in `sdk/storekit` as the
// named `charly` store, under the DATA root. A layout reader/writer written here
// would be the R2 duplication the store move (opencharly/sdk#460) existed to
// remove, so the production CharlyImageSource below is the store's own answer with
// no layout knowledge of its own.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/opencharly/sdk/storekit"
)

// ImageOrigin names which step of the resolution order answered.
type ImageOrigin string

const (
	// OriginTarget: the target store already held the reference.
	OriginTarget ImageOrigin = "target"
	// OriginImported: imported from the charly image store.
	OriginImported ImageOrigin = "imported"
	// OriginPulled: pulled from the registry.
	OriginPulled ImageOrigin = "pulled"
	// OriginBuilt: built by `build:ensure`, then imported from the charly store.
	OriginBuilt ImageOrigin = "built"
)

// ResolvedImage is the resolver's answer: the full reference and where it came
// from. The reference is fully tagged even when the caller passed a bare
// repository.
type ResolvedImage struct {
	Ref    string
	Origin ImageOrigin
}

// CharlyImageSource is the engine-neutral charly image store as the resolver
// needs it. `Archive` materialises one stored reference as an OCI archive
// carrying the `io.containerd.image.name` annotation, which is what
// ImageStore.Import expects.
type CharlyImageSource interface {
	// Has reports whether the store holds ref.
	Has(ctx context.Context, ref string) (bool, error)
	// Archive materialises ref as an OCI archive carrying the
	// `io.containerd.image.name` annotation, and returns its path. THE PATH
	// BELONGS TO THE CALLER: an archive is as large as the image, so the caller
	// removes it once the import that needed it is done.
	Archive(ctx context.Context, ref string) (string, error)
	// List returns every reference the store holds.
	List(ctx context.Context) ([]string, error)
}

// RegistryProbe reports whether a reference exists in its registry. A nil probe
// means the caller has no registry channel, so step 3 is not attempted; the
// caller states that by passing nil rather than the resolver assuming a
// registry it cannot reach.
type RegistryProbe interface {
	Has(ctx context.Context, ref string) (bool, error)
}

// BuildEnsure builds one reference into the charly image store (W3.6's
// `build:ensure`). It is a hook so the resolver stays free of the build drive.
type BuildEnsure func(ctx context.Context, ref string) error

// ImageResolver owns the resolution order for one caller. Every dependency is
// explicit; nothing is inferred from a name or a local default.
type ImageResolver struct {
	// Charly is the engine-neutral store built images land in. Required for
	// steps 2 and 4; nil means "this caller has no charly store", and a
	// resolution that would need it says so instead of pretending.
	Charly CharlyImageSource
	// Registry is the remote presence probe. Nil skips step 3.
	Registry RegistryProbe
	// Build is the `build:ensure` hook. Nil skips step 4.
	Build BuildEnsure
}

// ResolveImage resolves ref against target in the W3.7 order. A bare
// repository (no tag) first resolves its tag to the newest CalVer in the target
// and charly stores.
func (r *ImageResolver) ResolveImage(ctx context.Context, ref string, target ImageStore) (ResolvedImage, error) {
	if target == nil {
		return ResolvedImage{}, fmt.Errorf("resolve %q: nil target store", ref)
	}
	full, err := r.resolveTag(ctx, ref, target)
	if err != nil {
		return ResolvedImage{}, err
	}
	ref = full

	// 1. Already in the target store.
	present, err := target.Has(ctx, ref)
	if err != nil {
		return ResolvedImage{}, fmt.Errorf("resolve %q: %w", ref, err)
	}
	if present {
		return ResolvedImage{Ref: ref, Origin: OriginTarget}, nil
	}

	// 2. In the charly image store → import into the target.
	if r.Charly != nil {
		imported, err := r.importFromCharly(ctx, ref, target)
		if err != nil {
			return ResolvedImage{}, err
		}
		if imported {
			return ResolvedImage{Ref: ref, Origin: OriginImported}, nil
		}
	}

	// 3. In the registry → pull into the target.
	if r.Registry != nil {
		inRegistry, err := r.Registry.Has(ctx, ref)
		if err != nil {
			return ResolvedImage{}, fmt.Errorf("resolve %q: probe the registry: %w", ref, err)
		}
		if inRegistry {
			if err := target.Pull(ctx, ref); err != nil {
				return ResolvedImage{}, fmt.Errorf("resolve %q: pull into the target store: %w", ref, err)
			}
			return ResolvedImage{Ref: ref, Origin: OriginPulled}, nil
		}
	}

	// 4. Build it, then re-enter at step 2. The charly store is required BEFORE
	// the build runs: a build that cannot be materialised must not have side
	// effects.
	if r.Build != nil {
		if r.Charly == nil {
			return ResolvedImage{}, fmt.Errorf(
				"resolve %q: build:ensure is configured but no charly image store can materialise the result", ref)
		}
		if err := r.Build(ctx, ref); err != nil {
			return ResolvedImage{}, fmt.Errorf("resolve %q: build:ensure: %w", ref, err)
		}
		imported, err := r.importFromCharly(ctx, ref, target)
		if err != nil {
			return ResolvedImage{}, err
		}
		if !imported {
			return ResolvedImage{}, fmt.Errorf(
				"resolve %q: build:ensure reported success, but the charly image store does not hold it", ref)
		}
		return ResolvedImage{Ref: ref, Origin: OriginBuilt}, nil
	}

	return ResolvedImage{}, fmt.Errorf(
		"resolve %q: absent from the target store, the charly image store and the registry, and no build:ensure hook is configured", ref)
}

// importFromCharly answers step 2. It reports false when the charly store does
// not hold ref — that is a miss, not an error — and imports + verifies when it
// does.
func (r *ImageResolver) importFromCharly(ctx context.Context, ref string, target ImageStore) (bool, error) {
	held, err := r.Charly.Has(ctx, ref)
	if err != nil {
		return false, fmt.Errorf("resolve %q: probe the charly image store: %w", ref, err)
	}
	if !held {
		return false, nil
	}
	archive, err := r.Charly.Archive(ctx, ref)
	if err != nil {
		return false, fmt.Errorf("resolve %q: materialise it from the charly image store: %w", ref, err)
	}
	// The archive is the CALLER's from here on (see the interface's Archive
	// contract) and it is as large as the image, so it goes whatever the import
	// does — a successful resolve must not leave a copy of the image behind.
	defer func() { _ = os.Remove(archive) }()
	if err := target.Import(ctx, archive, ref); err != nil {
		return false, fmt.Errorf("resolve %q: import from the charly image store: %w", ref, err)
	}
	return true, nil
}

// resolveTag expands a bare repository to its newest CalVer tag across the
// target and charly stores. A reference that already carries a tag (or a
// digest) is returned unchanged.
func (r *ImageResolver) resolveTag(ctx context.Context, ref string, target ImageStore) (string, error) {
	if strings.ContainsRune(ref, '@') {
		// A digest-pinned reference is already exact; there is no tag to expand.
		return ref, nil
	}
	repo, tag := splitImageRef(ref)
	if tag != "" {
		return ref, nil
	}
	candidates, err := target.List(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve the newest tag for %q: list the target store: %w", repo, err)
	}
	if r.Charly != nil {
		charlyRefs, err := r.Charly.List(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve the newest tag for %q: list the charly image store: %w", repo, err)
		}
		candidates = append(candidates, charlyRefs...)
	}

	newestKey, newestTag := "", ""
	for _, candidate := range candidates {
		candidateRepo, candidateTag := splitImageRef(candidate)
		if candidateRepo != repo {
			continue
		}
		key, ok := calverKey(candidateTag)
		if !ok {
			continue
		}
		if key > newestKey {
			newestKey, newestTag = key, candidateTag
		}
	}
	if newestKey == "" {
		return "", fmt.Errorf(
			"resolve %q: no CalVer tag for the repository in the target store or the charly image store", repo)
	}
	// The EXACT tag from the store is returned: a store may spell it with a
	// leading `v`, and the addressable reference must be the one it holds.
	return repo + ":" + newestTag, nil
}

// splitImageRef splits repo[:tag]. A tag is a colon AFTER the last slash, so a
// registry port (`localhost:5000/repo`) is not mistaken for one. A digest ref
// yields no tag.
func splitImageRef(ref string) (repo, tag string) {
	if cut := strings.IndexByte(ref, '@'); cut >= 0 {
		return ref[:cut], ""
	}
	slash := strings.LastIndexByte(ref, '/')
	colon := strings.LastIndexByte(ref, ':')
	if colon > slash {
		if colon == len(ref)-1 {
			// A trailing colon is an empty tag, not part of the repository.
			return ref[:colon], ""
		}
		return ref[:colon], ref[colon+1:]
	}
	return ref, ""
}

// CalVer tag shape: YYYY.DDD.HHMM, every component fixed-width and zero-padded,
// so a plain string comparison orders them chronologically. The image tag is the
// git tag WITHOUT the leading `v` (W5.4); a `v`-prefixed tag is still accepted
// and normalised, because that is the same instant in the other spelling.
const (
	calverTagLen   = 13 // 4 + 1 + 3 + 1 + 4
	calverYearDot  = 4
	calverDayDot   = 8
	calverNumStart = '0'
	calverNumEnd   = '9'
)

// charlyImageSource is the PRODUCTION CharlyImageSource: the charly image store as
// `sdk/storekit` owns it. Every method is the store's own answer — the adapter adds
// nothing but the context discipline — because a second reading of `index.json`
// here would be exactly the duplication this move removed.
type charlyImageSource struct {
	store *storekit.ImageStore
}

var _ CharlyImageSource = (*charlyImageSource)(nil)

// NewCharlyImageSource opens the charly image store as the resolver's source:
// `CHARLY_IMAGE_STORE_DIR` when set (a disposable target's relocated store), else
// `images/` under the DATA root that `storekit.Dir(storekit.RootData)` resolves. A
// caller with no charly store passes nil to ImageResolver instead of a source that
// would answer for a store nothing wrote.
func NewCharlyImageSource() (CharlyImageSource, error) {
	store, err := storekit.OpenImageStore()
	if err != nil {
		return nil, err
	}
	return &charlyImageSource{store: store}, nil
}

// Has reports whether the store holds ref.
func (s *charlyImageSource) Has(ctx context.Context, ref string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, ok, err := s.store.Lookup(ref)
	if err != nil {
		return false, fmt.Errorf("look up %q in the charly image store: %w", ref, err)
	}
	return ok, nil
}

// Archive materialises ref as an OCI archive. The materialisation is one pass over
// local, immutable, content-addressed blobs, so it observes cancellation only before
// it starts: it cannot honour a cancel that arrives mid-blob.
func (s *charlyImageSource) Archive(ctx context.Context, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.store.Archive(ref)
}

// List returns every reference the store holds, sorted, one per image.
func (s *charlyImageSource) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.store.List()
}

// calverKey returns the comparison key for a CalVer tag. Anything else (a
// `latest`, a digest, a branch word) is not a CalVer tag.
func calverKey(tag string) (string, bool) {
	normalised := strings.TrimPrefix(tag, "v")
	if len(normalised) != calverTagLen {
		return "", false
	}
	for index, r := range normalised {
		switch index {
		case calverYearDot, calverDayDot:
			if r != '.' {
				return "", false
			}
		default:
			if r < calverNumStart || r > calverNumEnd {
				return "", false
			}
		}
	}
	return normalised, true
}
