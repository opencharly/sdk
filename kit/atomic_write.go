package kit

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path atomically: a temp file in the SAME dir
// (same filesystem, so rename is atomic) is written, chmod'd, then renamed over
// path. A concurrent reader sees either the old complete file or the new complete
// file, never a partial write; concurrent writers of identical content converge
// (last rename wins, bytes identical). Relocated from charly core (P8) so the
// build render engine (sdk/deploykit) and charly's staging primitives share it.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed away
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	return nil
}
