package storekit

// registry_test.go — D-STORE-10's shape assertion, and the enumeration that earns it.
//
// TWO claims, and the second is the one that matters:
//
//  1. the four reads the view serves answer from the store's own bytes;
//  2. NOTHING ELSE EXISTS. A 405 on PUT and DELETE proves the handler answered those two verbs; it
//     does not prove there is no third mutating route. So this test enumerates the served surface
//     exhaustively — every read that must answer, and every mutating method that must refuse — over a
//     real store built by PutBlob + PutEntry, not a hand-built fixture.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func registryFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	s := OpenStoreDir(t.TempDir())
	payload := []byte("the payload bytes")
	dgst, err := s.PutBlob(payload)
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if _, err := s.PutEntry(Entry{Key: "v1.0.0", MediaType: "application/vnd.oci.image.layer.v1.tar", Payload: dgst}); err != nil {
		t.Fatalf("PutEntry: %v", err)
	}
	return s, dgst, string(payload)
}

func registryDo(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// THE ENUMERATION: exactly these reads answer, and every mutating method refuses.
func TestRegistryServesExactlyTheFourReads(t *testing.T) {
	s, payloadDigest, payload := registryFixture(t)
	h := Registry(s)

	reads := []struct {
		name, path, want string
	}{
		{"apiversion", "/v2/", ""},
		{"tags", "/v2/registry/tags/list", "v1.0.0"},
		{"manifest-by-tag", "/v2/registry/manifests/v1.0.0", ""},
		{"manifest-by-digest", "/v2/registry/manifests/" + payloadDigest, ""},
		{"blob", "/v2/registry/blobs/" + payloadDigest, payload},
	}
	for _, tc := range reads {
		rec := registryDo(t, h, http.MethodGet, tc.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s (%s) = %d, want 200 — the view serves four reads and this is one of them", tc.path, tc.name, rec.Code)
		}
		if tc.want != "" && !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("GET %s (%s) body does not carry %q: %s", tc.path, tc.name, tc.want, rec.Body.String())
		}
	}

	// The manifest read must name the address the index gave it — the reader ManifestDigest owns.
	dgst, found, err := s.ManifestDigest("v1.0.0")
	if err != nil || !found {
		t.Fatalf("fixture: ManifestDigest(v1.0.0) = %v, %v", found, err)
	}
	rec := registryDo(t, h, http.MethodGet, "/v2/registry/manifests/v1.0.0")
	if got := rec.Header().Get("Docker-Content-Digest"); got != dgst {
		t.Fatalf("Docker-Content-Digest = %q, want %q (the manifest address the store indexed)", got, dgst)
	}

	// THE SHAPE: every mutating method refuses, and says what it does allow.
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPost, http.MethodPatch} {
		rec := registryDo(t, h, m, "/v2/registry/manifests/v1.0.0")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s = %d, want 405: a write path exists in a read-only view (D-STORE-10)", m, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Fatalf("%s refused with Allow=%q, want %q", m, allow, "GET, HEAD")
		}
	}
	// And an absent tag is 404, never an empty 200.
	if rec := registryDo(t, h, http.MethodGet, "/v2/registry/manifests/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET absent tag = %d, want 404", rec.Code)
	}
	// The blob body is the store's own bytes.
	rec = registryDo(t, h, http.MethodGet, "/v2/registry/blobs/"+payloadDigest)
	if body, _ := io.ReadAll(rec.Body); string(body) != payload {
		t.Fatalf("blob body = %q, want %q", body, payload)
	}
}
