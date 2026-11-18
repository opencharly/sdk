package storekit

// registry.go — D-STORE-10's READ-ONLY serving view over the store.
//
// DESIGN.md:2491: "The store is served, read-only, as an image registry to the deployments that pull
// images ... The registry is a view of store entries (D-STORE-3), never a second copy, and nothing it
// serves is fetched again from a public registry."
//
// So this file adds no storage, no index and no cache of its own: every byte it answers comes from the
// store's own reader, and every tag is an entry the store already indexed. Deleting the view deletes
// nothing the store needs.
//
// READ-ONLY IS THE SHAPE, not a policy flag. The view serves exactly four reads:
//
//	GET  /v2/                              the API version check
//	GET  /v2/<store>/tags/list             the store's entries, as OCI tags
//	GET  /v2/<store>/manifests/<tag|digest> an entry manifest, by name or by address
//	GET  /v2/<store>/blobs/<digest>         a blob, by digest
//
// and every other method is answered 405. There is no push and no delete anywhere in the file, so a
// mutation cannot be reached by a caller; the 405 is what the test asserts, and the four reads above
// are what the same test enumerates, because "no write path" is proven by the enumeration of what
// exists, not by a grep for the word DELETE.
//
// WHAT A TAG IS. `<name>` is the store; an entry's KEY is exposed as a tag when it is a legal
// distribution tag (OCI's `[A-Za-z0-9_][A-Za-z0-9._-]{0,127}`). A key that is not tag-legal is still served
// BY DIGEST — nothing is renamed and nothing is hashed to invent a tag, so a tag that does not exist in
// the store is a 404 rather than an alias for some entry.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// ociManifestMediaType is what a store entry's manifest IS: entries are OCI image manifests.
const ociManifestMediaType = "application/vnd.oci.image.manifest.v1+json"

// distributionTag is OCI's tag grammar; a key outside it is served by digest only.
var distributionTag = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// Registry answers the read-only subset of the OCI distribution API over ONE store.
func Registry(s *Store) http.Handler { return &registryView{store: s} }

type registryView struct{ store *Store }

func (v *registryView) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "this registry view is read-only (D-STORE-10)", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "v2" || path == "v2/" {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	rest, ok := strings.CutPrefix(path, "v2/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	// `<store>/<tail>`: the store name is the FIRST segment, so it is stripped before the verb is
	// read. Cutting the verb first turned `<store>` into the verb and every route into a 404.
	name, tail, ok := strings.Cut(rest, "/")
	if !ok || name == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(tail, "tags/list") {
		v.tags(w, name)
		return
	}
	verb, ref, ok := strings.Cut(tail, "/")
	if !ok || ref == "" {
		http.NotFound(w, r)
		return
	}
	switch verb {
	case "manifests":
		v.manifest(w, ref)
	case "blobs":
		v.blob(w, ref)
	default:
		http.NotFound(w, r)
	}
}

// tags answers `<store>/tags/list` from the store's OWN index. A store with no entries is an empty
// list, never a 404: the repository exists and holds nothing.
func (v *registryView) tags(w http.ResponseWriter, storeName string) {
	entries, err := v.store.Entries()
	if err != nil {
		http.Error(w, "reading the store index: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tags := make([]string, 0, len(entries))
	for _, e := range entries {
		if distributionTag.MatchString(e.Key) {
			tags = append(tags, e.Key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": storeName, "tags": tags})
}

// manifest answers `manifests/<tag|digest>`. A tag resolves through ManifestDigest — the entry's
// manifest address — and a digest is served directly if the store holds it.
func (v *registryView) manifest(w http.ResponseWriter, ref string) {
	digest, found, err := v.store.ManifestDigest(ref)
	if err != nil {
		http.Error(w, "reading the store index: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !found && strings.HasPrefix(ref, "sha256:") && v.store.HasBlob(ref) {
		digest, found = ref, true
	}
	if !found {
		http.NotFound(w, nil)
		return
	}
	data, err := v.store.ReadBlob(digest)
	if err != nil {
		http.Error(w, "reading the manifest blob: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ociManifestMediaType)
	w.Header().Set("Docker-Content-Digest", digest)
	_, _ = w.Write(data)
}

// blob answers `blobs/<digest>` straight from the store's blob reader.
func (v *registryView) blob(w http.ResponseWriter, digest string) {
	if !v.store.HasBlob(digest) {
		http.NotFound(w, nil)
		return
	}
	data, err := v.store.ReadBlob(digest)
	if err != nil {
		http.Error(w, "reading the blob: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
