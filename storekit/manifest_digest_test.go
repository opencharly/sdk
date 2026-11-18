package storekit

// manifest_digest_test.go — the owner of D-STORE-10's manifest address.
//
// The registry view cannot answer `manifests/<tag>` without it, and the two failure modes must stay
// distinct: an ABSENT key is `found=false, err=nil` (the caller answers 404), while an unreadable
// index is an error (the caller answers 500). A test that only asserts "no error" would pass on both.

import "testing"

func TestManifestDigestSeparatesAbsentFromUnreadable(t *testing.T) {
	s := OpenStoreDir(t.TempDir())

	payload, err := s.PutBlob([]byte("payload bytes"))
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if _, err := s.PutEntry(Entry{Key: "v1.0.0", MediaType: "application/vnd.oci.image.layer.v1.tar", Payload: payload}); err != nil {
		t.Fatalf("PutEntry: %v", err)
	}

	digest, found, err := s.ManifestDigest("v1.0.0")
	if err != nil {
		t.Fatalf("ManifestDigest(existing): unexpected error: %v", err)
	}
	if !found || digest == "" {
		t.Fatalf("ManifestDigest(existing) = %q, found=%v: the entry this store just indexed has no manifest address", digest, found)
	}
	if !s.HasBlob(digest) {
		t.Fatalf("ManifestDigest returned %q, which is not a blob in the store — a distribution client would 404 on the address the index gave it", digest)
	}

	// ABSENT is not an error, and it is not an empty success either: the bool carries the difference.
	missing, found, err := s.ManifestDigest("no-such-tag")
	if err != nil {
		t.Fatalf("ManifestDigest(absent) returned an error; an absent key is a 404, not a 500: %v", err)
	}
	if found || missing != "" {
		t.Fatalf("ManifestDigest(absent) = %q, found=%v; want \"\", false", missing, found)
	}
}
