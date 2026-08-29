package kit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// system_info_test.go — the `system:` section populator for the per-host
// charly.yml. The populator must (1) write the host identity into the `system:`
// section, (2) preserve every other key (deploy:, provides:, cache:, ledger:),
// and (3) stamp the HEAD schema version on a fresh file.

func TestWriteSystemInfoPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "charly.yml")
	// Pre-existing per-host config with deploy + cache sections.
	existing := "version: 2026.240.1943\nweb-local:\n    pod:\n        image: web\ncache:\n    git:\n        latest_tags:\n            https://github.com/opencharly/example:\n                value: v1\n                resolved: 2026-08-29T10:00:00Z\n"
	if err := os.WriteFile(cfg, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	info := spec.SystemInfo{
		Hostname:      "testhost",
		DistroID:      "fedora",
		DistroVersion: "43",
		Kernel:        "6.8.0",
		Arch:          "amd64",
		UpdatedAt:     "2026-08-29T10:00:00Z",
	}
	if err := writeSystemInfo(cfg, info); err != nil {
		t.Fatalf("writeSystemInfo: %v", err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version  string `yaml:"version"`
		WebLocal *struct {
			Pod *struct {
				Image string `yaml:"image"`
			} `yaml:"pod"`
		} `yaml:"web-local"`
		Cache *struct {
			Git *struct {
				LatestTags map[string]struct {
					Value string `yaml:"value"`
				} `yaml:"latest_tags"`
			} `yaml:"git"`
		} `yaml:"cache"`
		System *struct {
			Hostname string `yaml:"hostname"`
			DistroID string `yaml:"distro_id"`
		} `yaml:"system"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
	if doc.Version != "2026.240.1943" {
		t.Fatalf("version lost: %q", doc.Version)
	}
	if doc.WebLocal == nil || doc.WebLocal.Pod == nil || doc.WebLocal.Pod.Image != "web" {
		t.Fatal("deploy node lost or corrupted")
	}
	if doc.Cache == nil || doc.Cache.Git == nil {
		t.Fatal("cache section lost")
	}
	if _, ok := doc.Cache.Git.LatestTags["https://github.com/opencharly/example"]; !ok {
		t.Fatal("cache entry lost")
	}
	if doc.System == nil || doc.System.Hostname != "testhost" || doc.System.DistroID != "fedora" {
		t.Fatalf("system section wrong: %+v", doc.System)
	}
}

func TestWriteSystemInfoFreshFileGetsVersionStamp(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "charly.yml")
	info := spec.SystemInfo{Hostname: "testhost", UpdatedAt: "2026-08-29T10:00:00Z"}
	if err := writeSystemInfo(cfg, info); err != nil {
		t.Fatalf("writeSystemInfo: %v", err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version string `yaml:"version"`
		System  *struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"system"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
	if doc.Version != spec.SchemaVersion {
		t.Fatalf("fresh file version = %q, want %q", doc.Version, spec.SchemaVersion)
	}
	if doc.System == nil || doc.System.Hostname != "testhost" {
		t.Fatalf("system section wrong: %+v", doc.System)
	}
}
