package deploykit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencharly/sdk/kit"
)

// StageInlineContent writes a write-task's inline content to
// <buildDir>/_inline/<candy>/<sha256> on disk and returns the build-context-relative
// path suitable for COPY. Writes are idempotent — repeated calls with identical
// content are no-ops via content-addressed filenames. Relocated from charly (P8).
func StageInlineContent(buildDir, contextRelPrefix, candyName, content string) (string, error) {
	sum := sha256.Sum256([]byte(content))
	hexSum := hex.EncodeToString(sum[:])
	relToBuildDir := filepath.ToSlash(filepath.Join("_inline", candyName, hexSum))
	abs := filepath.Join(buildDir, relToBuildDir)
	contextRel := filepath.ToSlash(filepath.Join(contextRelPrefix, relToBuildDir))

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("staging inline content dir: %w", err)
	}
	// Idempotent: skip write if file already exists with identical content.
	if existing, err := os.ReadFile(abs); err == nil && string(existing) == content {
		return contextRel, nil
	}
	if err := kit.AtomicWriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("staging inline content: %w", err)
	}
	return contextRel, nil
}
