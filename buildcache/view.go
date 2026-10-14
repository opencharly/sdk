package buildcache

// view.go — publishing and asserting the VIEW a loader execs.
//
// The view is a HARDLINK of the payload blob, and storekit.LinkBlob deliberately has NO
// cross-device fallback: a link failure is a hard error naming why, because a silent copy fallback
// is exactly how the operator's "bytes exist once per digest" property would stop being true
// without anyone noticing. The view therefore lives UNDER the store tree, where it is
// same-filesystem by construction, and callers can assert the sharing with SameFile rather than
// trust it.

import (
	"fmt"

	"github.com/opencharly/sdk/storekit"
)

// linkView publishes viewPath as a hardlink of the store's payload blob. LinkBlob removes any
// existing destination itself, so re-publishing over a stale view is safe and atomic at the
// filesystem level: a reader (the LocalTransport.Connect that spawns the binary) sees either the
// complete old view or the complete new one.
//
// The blob's mode is NOT set here. A view is a hardlink, so the mode must be applied once to the
// shared inode by the caller (Build does `os.Chmod(viewPath, 0o755)`); mode is not part of a
// digest, which is why it is the consumer's concern and why the store exposes no chmod call.
func linkView(store *storekit.Store, payloadDigest, viewPath string) error {
	if err := store.LinkBlob(payloadDigest, viewPath); err != nil {
		return fmt.Errorf("link payload %s: %w", payloadDigest, err)
	}
	return nil
}

// SharedView reports whether viewPath and the store's payload blob are the SAME file (the same
// inode), i.e. that the bytes exist once. Callers and tests ASSERT the sharing with this rather
// than assume it — the operator's "cached once" property is proven by measuring the inode, never
// by the absence of an error.
func SharedView(store *storekit.Store, payloadDigest, viewPath string) (bool, error) {
	blobPath, err := store.BlobPath(payloadDigest)
	if err != nil {
		return false, fmt.Errorf("resolve blob path for %s: %w", payloadDigest, err)
	}
	return storekit.SameFile(viewPath, blobPath)
}
