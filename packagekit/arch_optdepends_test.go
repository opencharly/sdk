package packagekit

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// writePkgTarZst writes a zstd-compressed tar with a .PKGINFO + one file.
func writePkgTarZst(t *testing.T, path, pkgInfo string) {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	entries := []struct {
		name, body string
	}{
		{".PKGINFO", pkgInfo},
		{"usr/bin/charly", "#!/bin/sh\n"},
	}
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readPkgInfo extracts the .PKGINFO body from a .pkg.tar.zst.
func readPkgInfo(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == ".PKGINFO" {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
	t.Fatal("no .PKGINFO found")
	return ""
}

func TestInjectOptDepends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "charly-2026.225.1200-1-x86_64.pkg.tar.zst")
	writePkgTarZst(t, path, "pkgname = charly\npkgver = 2026.225.1200\n")

	opt := map[string]string{
		"qemu-full": "full-system VM support",
		"libvirt":   "libvirt VM support",
	}
	if err := InjectOptDepends(path, opt); err != nil {
		t.Fatalf("InjectOptDepends: %v", err)
	}
	got := readPkgInfo(t, path)
	for _, want := range []string{
		"pkgname = charly",
		"optdepend = libvirt: libvirt VM support",
		"optdepend = qemu-full: full-system VM support",
	} {
		if !strings.Contains(got, want) {
			t.Errorf(".PKGINFO missing %q:\n%s", want, got)
		}
	}
	// Deterministic order: libvirt before qemu-full.
	if strings.Index(got, "optdepend = libvirt") > strings.Index(got, "optdepend = qemu-full") {
		t.Errorf("optdepends not sorted:\n%s", got)
	}
}

func TestInjectOptDependsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "charly.pkg.tar.zst")
	writePkgTarZst(t, path, "pkgname = charly\n")
	if err := InjectOptDepends(path, nil); err != nil {
		t.Fatalf("InjectOptDepends(nil): %v", err)
	}
	got := readPkgInfo(t, path)
	if strings.Contains(got, "optdepend") {
		t.Errorf("unexpected optdepend injected:\n%s", got)
	}
}
