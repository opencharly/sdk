package kit

// local_image.go — RE-EXPORT shim. The local-image ref-resolution family was RELOCATED to the
// spec/container fabric slice (#55 coneB build-render cone, Class A — github.com/opencharly/spec/
// container/local_image_coneb.go). The bodies (LooksLikeFullRef, LocalImageInfo, ListLocalImages,
// ParseLocalImagesJSON, ResolveLocalImageRef, ResolveNewestLocalCalVer, ResolveShellImageRef,
// ExtractCalVerTag, + the internal helpers splitShortTaggedImage/refHasExactTag/resolverCandidate/
// compareCalVerKey/sameRepoAcross/shortNameMatchesRef) now live there; this file re-exports each
// so the ~6 plugin consumers (candy/plugin-clean, candy/plugin-vm, candy/plugin-box,
// candy/plugin-build, candy/plugin-deploy-pod, candy/plugin-kube) and charly core's remaining
// callers keep their kit.X call sites unchanged. New consumers reference spec/container directly.
//
// Testability note: the package-level vars (ListLocalImages, LocalImageExists, InspectLabels) are
// re-exports of the container vars — override container.ListLocalImages / container.LocalImageExists
// / container.InspectLabels (the vars the relocated callees READ) to stub the resolution + label
// decode in tests; overriding the kit re-export vars no longer affects the container bodies.

import (
	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/spec"
)

// ErrImageNotLocal is the canonical image-not-in-local-storage sentinel (spec/spec/image_errors.go).
var ErrImageNotLocal = spec.ErrImageNotLocal

// LooksLikeFullRef returns true if the image ref contains a registry segment
// (a "/" before any ":") — e.g. "ghcr.io/org/name:tag" — so it can be pulled
// without charly.yml resolution.
var LooksLikeFullRef = container.LooksLikeFullRef

// LocalImageInfo describes an image present in the engine's local storage.
type LocalImageInfo = container.LocalImageInfo

// ListLocalImages returns all images in the engine's local storage.
var ListLocalImages = container.ListLocalImages

// ParseLocalImagesJSON parses `{podman,docker} images --format json` output
// into ONE LocalImageInfo per distinct image ID.
var ParseLocalImagesJSON = container.ParseLocalImagesJSON

// ResolveLocalImageRef resolves a user-supplied image reference against the
// engine's local storage — never reads charly.yml.
var ResolveLocalImageRef = container.ResolveLocalImageRef

// ExtractCalVerTag returns the CalVer portion of a ref's tag, or "" if the tag
// is not a recognisable CalVer (`YYYY.DDD.HHMM`).
var ExtractCalVerTag = container.ExtractCalVerTag

// ResolveNewestLocalCalVer is the canonical "find the newest local image for
// this short name" helper.
var ResolveNewestLocalCalVer = container.ResolveNewestLocalCalVer

// ResolveShellImageRef builds the full image reference from registry, name, and tag.
var ResolveShellImageRef = container.ResolveShellImageRef

// InspectImageLabels reads a local image's OCI labels via engine inspect. RELOCATED to
// spec/container (coneA); re-exported here so every existing kit.InspectImageLabels call site
// is untouched.
var InspectImageLabels = container.InspectImageLabels
