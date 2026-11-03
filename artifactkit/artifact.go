// Package artifactkit reads, verifies and materializes a charly PLUGIN ARTIFACT — an OCI
// image layout (an index → one manifest per platform → a metadata config blob plus the
// provider-binary, candy-manifest and `.providers` layers) that carries a precompiled provider
// so a consumer with no plugin source and no Go toolchain can serve it.
//
// It sits on `storekit`: the artifact store IS a named OCI-layout store under the one cache
// root, and a materialized plugin is a VIEW hard-linked out of that store's own blobs — never a
// second copy. DESIGN puts both mechanisms in sdk (D-SDK-1) rather than in `spec`, which holds
// generated code and behaviour methods and no mechanism (D-SPEC-1).
//
// WHAT THIS VERIFIES, and what it deliberately does not: it validates what the artifact
// DECLARES against what the artifact IS — content addresses, the artifact/config media types,
// the three layer roles, the config's platform against the descriptor that pointed at it, an
// absolute libc, and the required runtime-tool declaration. It does NOT negotiate a version:
// DESIGN §8 says there is no version negotiation and that compatibility is the validation of
// every message (D-PROTO-6), so `protocol` and `calver` are advisory diagnostics a caller may
// report and are never a gate. `served_schema_digest` is TRANSITIONAL — the served body is the
// only schema identity verifiable today, until DESIGN §8.1's CUE-module-as-data leg lands.
package artifactkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/opencharly/sdk/storekit"
	"github.com/opencharly/spec/spec"
)

// ArtifactRef names one artifact a store holds.
type ArtifactRef struct {
	// Digest is the per-platform MANIFEST digest (not the index digest: a store is an index,
	// and each entry is one platform's manifest).
	Digest string
	// Tag is the index descriptor's `org.opencontainers.image.ref.name`, when a pull recorded
	// one.
	Tag string
	// Platform is the descriptor's `os/arch[/variant]`.
	Platform string
}

// Verified is the outcome of a successful verification: everything a caller needs to serve the
// binary, and what a log line should say.
type Verified struct {
	// ManifestDigest is the selected per-platform manifest's digest.
	ManifestDigest string
	// Platform is the selected descriptor's platform, and Platforms every platform the store
	// advertises — so a caller that cannot serve the request can name the alternatives.
	Platform  string
	Platforms []string
	// Metadata is the CUE-GENERATED spec.PluginArtifactMetadata. This package decodes into
	// that type rather than a struct of its own: a second hand-written shape would be a second
	// schema, and the two would drift.
	Metadata spec.PluginArtifactMetadata
	// LayerDigests maps a layer ROLE ("binary" | "manifest" | "providers") to its digest.
	LayerDigests map[string]string
}

// View is a materialized plugin: the files a loader reads, all hard-linked to the store's blobs.
type View struct {
	// Name is the provider's leaf name, taken from the metadata's MODULE — the plugin's
	// canonical candy ref leaf, so it is stable across however a project names the candy that
	// declares it (D-PLUG-4's declaration-driven resolution).
	Name string
	// Dir is the view directory inside the store tree, keyed by the manifest digest so two
	// platforms or releases never collide.
	Dir string
	// Binary, Providers and Manifest are the view's paths.
	Binary    string
	Providers string
	Manifest  string
	// Verified is the verification performed before anything was written.
	Verified *Verified
	// AllHardlinked is the measured result of "these files ARE the store's bytes".
	AllHardlinked bool
}

// index / manifest are the two JSON documents a layout stores. Only the fields read here are
// decoded: a consumer must never error on an unknown annotation or media type (the image
// spec's own rule), so nothing below is a strict schema.
type index struct {
	ArtifactType string `json:"artifactType"`
	Manifests    []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
		Platform  *struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant"`
		} `json:"platform"`
		Annotations map[string]string `json:"annotations"`
	} `json:"manifests"`
}

type manifest struct {
	ArtifactType string `json:"artifactType"`
	Config       struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

// ReadIndex parses a store's index.json.
func ReadIndex(store *storekit.Store) (index, error) {
	raw, err := os.ReadFile(filepath.Join(store.Dir(), "index.json"))
	if err != nil {
		return index{}, fmt.Errorf("read store index in %s: %w", store.Dir(), err)
	}
	var idx index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return index{}, fmt.Errorf("parse store index: %w", err)
	}
	return idx, nil
}

// Artifacts lists what a store holds.
func Artifacts(store *storekit.Store) ([]ArtifactRef, error) {
	idx, err := ReadIndex(store)
	if err != nil {
		return nil, err
	}
	out := make([]ArtifactRef, 0, len(idx.Manifests))
	for _, m := range idx.Manifests {
		out = append(out, ArtifactRef{
			Digest:   m.Digest,
			Tag:      m.Annotations["org.opencontainers.image.ref.name"],
			Platform: platformString(m.Platform),
		})
	}
	return out, nil
}

// HostPlatform is this runtime's platform in the image-spec `platform` vocabulary: GOOS,
// GOARCH, and the micro-architecture level the environment declares (GOAMD64 / GOARM64).
func HostPlatform() string {
	p := runtime.GOOS + "/" + runtime.GOARCH
	switch runtime.GOARCH {
	case "amd64":
		if v := os.Getenv("GOAMD64"); v != "" {
			return p + "/" + v
		}
	case "arm64":
		if v := os.Getenv("GOARM64"); v != "" {
			return p + "/" + v
		}
	}
	return p
}

func platformString(p *struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}) string {
	if p == nil {
		return "unknown"
	}
	if p.Variant == "" {
		return p.OS + "/" + p.Architecture
	}
	return p.OS + "/" + p.Architecture + "/" + p.Variant
}

func variantOf(platform string) string {
	parts := strings.Split(platform, "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// SelectArtifact picks the descriptor answering (selector, want). The selector is what makes a
// multi-artifact store unambiguous — a manifest digest names one artifact exactly, a tag names
// the release a caller asked for, and "" is legal only when the store holds exactly one, since
// otherwise "the first platform match" could serve a different plugin's bytes.
func SelectArtifact(idx index, selector, want string) (int, []string, error) {
	if want == "" {
		want = HostPlatform()
	}
	if selector == "" && len(idx.Manifests) != 1 {
		return 0, nil, fmt.Errorf("store holds %d artifacts — name one by digest or tag", len(idx.Manifests))
	}
	platforms := make([]string, 0, len(idx.Manifests))
	for _, m := range idx.Manifests {
		platforms = append(platforms, platformString(m.Platform))
	}
	candidates := make([]int, 0, len(idx.Manifests))
	for i, m := range idx.Manifests {
		if selector != "" && m.Digest != selector && m.Annotations["org.opencontainers.image.ref.name"] != selector {
			continue
		}
		candidates = append(candidates, i)
	}
	// An empty candidate set is reachable ONLY through a selector that matched nothing: the guard
	// above guarantees exactly one manifest when selector == "" and the loop above appends it
	// unconditionally, so the `selector == ""` arm that used to sit here could never execute. It is
	// deleted rather than left as a branch no test can reach.
	if len(candidates) == 0 {
		return 0, platforms, fmt.Errorf("store holds no artifact %q (carries %d)", selector, len(idx.Manifests))
	}
	matches := make([]int, 0, len(candidates))
	for _, i := range candidates {
		if descriptorMatches(idx.Manifests[i].Platform, want) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return 0, platforms, fmt.Errorf("artifact carries no %s manifest (carries: %s)", want, strings.Join(platforms, ", "))
	case 1:
		return matches[0], platforms, nil
	}
	// Several os/arch matches: prefer an EXACT variant match, then the baseline entry, then
	// the image spec's first-match rule.
	if v := variantOf(want); v != "" {
		for _, i := range matches {
			if idx.Manifests[i].Platform != nil && idx.Manifests[i].Platform.Variant == v {
				return i, platforms, nil
			}
		}
	}
	for _, i := range matches {
		if idx.Manifests[i].Platform != nil && idx.Manifests[i].Platform.Variant == "" {
			return i, platforms, nil
		}
	}
	return matches[0], platforms, nil
}

// descriptorMatches applies the conservative variant rule: an unqualified request answers a
// BASELINE entry or one matching this host's declared level, and an entry built for a HIGHER
// level than the host is never chosen silently — that is a SIGILL, strictly worse than a
// refusal. An explicit request accepts its own variant and any baseline entry.
func descriptorMatches(p *struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}, want string) bool {
	if p == nil {
		return false
	}
	parts := strings.Split(want, "/")
	if len(parts) < 2 || p.OS != parts[0] || p.Architecture != parts[1] {
		return false
	}
	if len(parts) == 2 {
		host := variantOf(HostPlatform())
		return p.Variant == "" || p.Variant == host
	}
	return p.Variant == parts[2] || p.Variant == ""
}

// Verify selects an artifact from the store and checks every declared fact a consumer can
// check BEFORE serving. venueLibc is the caller's venue knowledge (a native host cannot read
// its own libc from Go's build info, so a foreign-distro probe supplies it); "" skips the
// comparison and leaves the declared fact to be REPORTED, never silently ignored.
func Verify(store *storekit.Store, selector, want, venueLibc string) (*Verified, error) {
	idx, err := ReadIndex(store)
	if err != nil {
		return nil, err
	}
	sel, platforms, err := SelectArtifact(idx, selector, want)
	if err != nil {
		return nil, err
	}
	if idx.ArtifactType != spec.ArtifactTypePlugin {
		return nil, fmt.Errorf("index artifactType %q is not %q", idx.ArtifactType, spec.ArtifactTypePlugin)
	}
	descr := idx.Manifests[sel]
	rawManifest, err := store.ReadBlob(descr.Digest)
	if err != nil {
		return nil, err
	}
	var mf manifest
	if err := json.Unmarshal(rawManifest, &mf); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", descr.Digest, err)
	}
	if mf.ArtifactType != spec.ArtifactTypePlugin {
		return nil, fmt.Errorf("manifest %s artifactType %q is not %q", descr.Digest, mf.ArtifactType, spec.ArtifactTypePlugin)
	}
	if mf.Config.MediaType != spec.MediaTypePluginConfig {
		return nil, fmt.Errorf("manifest %s config media type %q is not %q", descr.Digest, mf.Config.MediaType, spec.MediaTypePluginConfig)
	}
	roles := map[string]string{
		spec.MediaTypePluginBinary:    spec.PluginArtifactLayerBinary,
		spec.MediaTypePluginManifest:  spec.PluginArtifactLayerManifest,
		spec.MediaTypePluginProviders: spec.PluginArtifactLayerProviders,
	}
	layerDigests := map[string]string{}
	for _, l := range mf.Layers {
		role, ok := roles[l.MediaType]
		if !ok {
			return nil, fmt.Errorf("manifest %s carries an unexpected layer role %q", descr.Digest, l.MediaType)
		}
		if _, dup := layerDigests[role]; dup {
			return nil, fmt.Errorf("manifest %s carries the %s layer twice", descr.Digest, role)
		}
		layerDigests[role] = l.Digest
	}
	// The loop variable is named `role`, not `want`: `want` is the PLATFORM parameter of this function,
	// and shadowing it inside the verification path is how a comparison silently reads the wrong value.
	for _, role := range []string{spec.PluginArtifactLayerBinary, spec.PluginArtifactLayerManifest, spec.PluginArtifactLayerProviders} {
		if _, ok := layerDigests[role]; !ok {
			return nil, fmt.Errorf("manifest %s carries no %s layer (carries: %s)", descr.Digest, role, layerRoles(layerDigests))
		}
	}
	rawConfig, err := store.ReadBlob(mf.Config.Digest)
	if err != nil {
		return nil, err
	}
	for role, dgst := range layerDigests {
		if _, err := store.ReadBlob(dgst); err != nil {
			return nil, fmt.Errorf("%s layer: %w", role, err)
		}
	}
	var md spec.PluginArtifactMetadata
	if err := json.Unmarshal(rawConfig, &md); err != nil {
		return nil, fmt.Errorf("parse artifact metadata: %w", err)
	}
	if err := verifyMetadata(&md, descr.Platform, venueLibc); err != nil {
		return nil, err
	}
	for role, dgst := range md.Layers {
		if got, ok := layerDigests[role]; !ok {
			return nil, fmt.Errorf("metadata names a %s layer the manifest does not carry", role)
		} else if got != dgst {
			return nil, fmt.Errorf("metadata %s layer digest %s disagrees with the manifest's %s", role, dgst, got)
		}
	}
	return &Verified{
		ManifestDigest: descr.Digest,
		Platform:       platformString(descr.Platform),
		Platforms:      platforms,
		Metadata:       md,
		LayerDigests:   layerDigests,
	}, nil
}

// verifyMetadata checks the declared facts a consumer CAN check. Anything not checkable is
// reported from the returned metadata by the caller — never silently dropped.
func verifyMetadata(md *spec.PluginArtifactMetadata, descrPlatform *struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}, venueLibc string) error {
	if md.Module == "" {
		return fmt.Errorf("artifact metadata declares no module")
	}
	if md.Served_schema_digest == "" {
		return fmt.Errorf("artifact metadata declares no served_schema_digest (transitional, but it is the only schema identity verifiable today)")
	}
	if md.Runtime_requires == nil {
		return fmt.Errorf("artifact metadata declares no runtime_requires — an absent list is indistinguishable from an unaudited one (declare [] to claim none)")
	}
	if descrPlatform != nil {
		if md.Platform.Os != descrPlatform.OS || md.Platform.Architecture != descrPlatform.Architecture {
			return fmt.Errorf("artifact metadata platform %s/%s disagrees with the index descriptor's %s/%s",
				md.Platform.Os, md.Platform.Architecture, descrPlatform.OS, descrPlatform.Architecture)
		}
		if md.Platform.Variant != descrPlatform.Variant {
			return fmt.Errorf("artifact metadata variant %q disagrees with the index descriptor's %q", md.Platform.Variant, descrPlatform.Variant)
		}
	}
	switch md.Platform.Libc {
	case "glibc", "musl":
	default:
		return fmt.Errorf("artifact metadata declares libc %q — want glibc or musl (image-spec's platform object cannot express libc, which is why the artifact must)", md.Platform.Libc)
	}
	if venueLibc != "" && venueLibc != md.Platform.Libc {
		return fmt.Errorf("artifact links %s but this venue is %s: the binary cannot run here", md.Platform.Libc, venueLibc)
	}
	if len(md.Providers) == 0 {
		return fmt.Errorf("artifact metadata declares no providers")
	}
	return nil
}

// Materialize verifies an artifact and creates its runtime VIEW as hardlinks to the store's own
// blobs. Nothing lands until verification has passed: a partially materialized plugin would be
// found by a LATER run and served without the check that rejected it.
func Materialize(store *storekit.Store, selector, want, venueLibc string) (*View, error) {
	v, err := Verify(store, selector, want, venueLibc)
	if err != nil {
		return nil, err
	}
	leaf := filepath.Base(v.Metadata.Module)
	if leaf == "" || leaf == "." || leaf == string(filepath.Separator) {
		return nil, fmt.Errorf("artifact module %q yields no usable leaf name", v.Metadata.Module)
	}
	viewDir := filepath.Join(store.Dir(), "views", leaf+"@"+strings.TrimPrefix(v.ManifestDigest, "sha256:"))
	out := &View{
		Name:      leaf,
		Dir:       viewDir,
		Binary:    filepath.Join(viewDir, leaf),
		Providers: filepath.Join(viewDir, leaf+".providers"),
		Manifest:  filepath.Join(viewDir, "charly.yml"),
		Verified:  v,
	}
	for path, role := range map[string]string{
		out.Binary:    spec.PluginArtifactLayerBinary,
		out.Providers: spec.PluginArtifactLayerProviders,
		out.Manifest:  spec.PluginArtifactLayerManifest,
	} {
		if err := store.LinkBlob(v.LayerDigests[role], path); err != nil {
			return nil, fmt.Errorf("%s view: %w", role, err)
		}
	}
	// Mode is not part of a digest, so the binary's executable bit is set on the SHARED blob:
	// one instance of the bytes, still exec-able. The other two stay read-only.
	if err := os.Chmod(out.Binary, 0o755); err != nil {
		return nil, err
	}
	out.AllHardlinked = true
	for path, role := range map[string]string{
		out.Binary:    spec.PluginArtifactLayerBinary,
		out.Providers: spec.PluginArtifactLayerProviders,
		out.Manifest:  spec.PluginArtifactLayerManifest,
	} {
		blobPath, err := store.BlobPath(v.LayerDigests[role])
		if err != nil {
			return nil, err
		}
		same, err := storekit.SameFile(blobPath, path)
		if err != nil {
			return nil, err
		}
		if !same {
			out.AllHardlinked = false
		}
	}
	return out, nil
}

func layerRoles(m map[string]string) string {
	out := make([]string, 0, len(m))
	for role := range m {
		out = append(out, role)
	}
	return strings.Join(out, ", ")
}
