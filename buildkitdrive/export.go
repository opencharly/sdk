package buildkitdrive

// export.go — the ONE lifecycle rule for a staged OCI export archive.
//
// The export is a full copy of the image, written into the builder's state root
// (`StageDir`/`ExportFile`), and it has exactly one consumer: the bridge that loads it into
// the run engine. Keeping one per build on a persistent root is storage nobody reclaims, and
// a FAILED build leaves either a partial or a 0-byte archive under the same naming — so it is
// removed on EVERY exit path, never only on success. Callers install the cleanup the moment
// the destination is known:
//
//	dest := buildkitdrive.ExportFile(stage, ref)
//	defer func() { err = errors.Join(err, buildkitdrive.RemoveExport(dest)) }()
//
// A missing archive is not an error: a build can fail before the exporter ever opens it, and a
// caller that never created one must not turn that into a second failure.

import (
	"fmt"
	"os"
)

// RemoveExport removes a staged OCI export archive, whatever the build's outcome was.
//
// It is deliberately a plain removal rather than a "cleanup func" so a caller can JOIN its
// error onto the build's own: the archive's removal must never REPLACE the reason a build
// failed (measured: an ENOSPC during the export write is exactly the case where the build's
// error and a removal error can both be present).
//
// An empty destination is a no-op: there is nothing to remove and nothing to report.
func RemoveExport(dest string) error {
	if dest == "" {
		return nil
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing the export %s: %w", dest, err)
	}
	return nil
}
