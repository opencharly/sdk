package packagekit

// arch_optdepends.go — nFPM does not emit Arch `optdepend` lines (verified in
// nFPM's arch/arch.go — it writes depend/replaces/conflict/provides but not
// optdepend). PKGINFO supports them, so this post-processes the .pkg.tar.zst to
// inject `optdepend = <pkg>: <desc>` lines into .PKGINFO (deterministic,
// unit-tested). Uses archive/tar + github.com/klauspost/compress/zstd — the same
// libs nFPM uses.

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/klauspost/compress/zstd"
)

// InjectOptDepends rewrites a .pkg.tar.zst in place, injecting the optdepends
// into its .PKGINFO. A no-op when optDepends is empty.
func InjectOptDepends(path string, optDepends map[string]string) error {
	if len(optDepends) == 0 {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	zr, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("zstd open %s: %w", path, err)
	}
	defer zr.Close()

	var out bytes.Buffer
	zw, err := zstd.NewWriter(&out)
	if err != nil {
		return fmt.Errorf("zstd writer: %w", err)
	}
	tw := tar.NewWriter(zw)
	tr := tar.NewReader(zr)
	injected := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read %s: %w", path, err)
		}
		if hdr.Name == ".PKGINFO" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("read .PKGINFO: %w", err)
			}
			data = injectOptDependsLines(data, optDepends)
			hdr.Size = int64(len(data))
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("tar write .PKGINFO: %w", err)
			}
			if _, err := tw.Write(data); err != nil {
				return fmt.Errorf("tar write .PKGINFO data: %w", err)
			}
			injected = true
			continue
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("tar write %s: %w", hdr.Name, err)
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return fmt.Errorf("tar copy %s: %w", hdr.Name, err)
		}
	}
	if !injected {
		return fmt.Errorf("%s: no .PKGINFO found", path)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("tar close: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("zstd close: %w", err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// injectOptDependsLines appends `optdepend = <pkg>: <desc>` lines to a .PKGINFO
// body, sorted for determinism.
func injectOptDependsLines(data []byte, optDepends map[string]string) []byte {
	keys := make([]string, 0, len(optDepends))
	for k := range optDepends {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteByte('\n')
	}
	for _, k := range keys {
		fmt.Fprintf(&b, "optdepend = %s: %s\n", k, optDepends[k])
	}
	return b.Bytes()
}
