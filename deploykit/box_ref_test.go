package deploykit

// box_ref_test.go — table + host-state tests for the generic BoxRef resolver
// (VM-box cutover plan task 4). Parse tests are pure; resolve tests cover the
// arms that need no host state (image: / imported: / full-ref box: passthrough),
// the snapshot-registry lookup under a temp CHARLY_VM_STATE_DIR, and the
// short-name bootc arm (a deterministic stub-swap variant mirroring plugin-vm's
// resolveBootcImageRef unit tests, plus a live variant against local container
// storage that skips when no engine/image is available).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
	"github.com/opencharly/spec/container"
	"github.com/opencharly/spec/spec"
)

func TestParseBoxRef(t *testing.T) {
	cases := []struct {
		name            string
		in              string
		wantForm        string
		wantName        string
		wantSnapshot    string
		wantURL         string
		wantErrContains string
	}{
		{name: "box form", in: "box:fedora-bootc", wantForm: BoxRefFormBox, wantName: "fedora-bootc"},
		{name: "box full ref after prefix", in: "box:quay.io/fedora/fedora-bootc:43", wantForm: BoxRefFormBox, wantName: "quay.io/fedora/fedora-bootc:43"},
		{name: "bare name defaults to box", in: "fedora-bootc", wantForm: BoxRefFormBox, wantName: "fedora-bootc"},
		{name: "bare full OCI ref stays box", in: "ghcr.io/opencharly/fedora-bootc:2026.145.0900", wantForm: BoxRefFormBox, wantName: "ghcr.io/opencharly/fedora-bootc:2026.145.0900"},
		{name: "vm entity + snapshot", in: "vm:web@snap1", wantForm: BoxRefFormVm, wantName: "web", wantSnapshot: "snap1"},
		{name: "vm entity without snapshot", in: "vm:web", wantForm: BoxRefFormVm, wantName: "web"},
		{name: "image url", in: "image:https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img", wantForm: BoxRefFormImage, wantURL: "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"},
		{name: "imported domain", in: "imported:legacy-db", wantForm: BoxRefFormImported, wantName: "legacy-db"},
		{name: "empty", in: "", wantErrContains: "empty"},
		{name: "whitespace", in: "   ", wantErrContains: "empty"},
		{name: "empty box name", in: "box:", wantErrContains: "box: form needs a name"},
		{name: "empty vm entity", in: "vm:", wantErrContains: "vm: form needs an entity"},
		{name: "vm only snapshot", in: "vm:@snap1", wantErrContains: "vm: form needs an entity"},
		{name: "vm trailing at", in: "vm:web@", wantErrContains: "empty snapshot"},
		{name: "empty image url", in: "image:", wantErrContains: "image: form needs a url"},
		{name: "empty imported domain", in: "imported:", wantErrContains: "imported: form needs a domain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBoxRef(tc.in)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("ParseBoxRef(%q) = %+v, want error containing %q", tc.in, got, tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("ParseBoxRef(%q) error = %q, want it to contain %q", tc.in, err, tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBoxRef(%q) unexpected error: %v", tc.in, err)
			}
			if got.Form != tc.wantForm || got.Name != tc.wantName || got.Snapshot != tc.wantSnapshot || got.URL != tc.wantURL {
				t.Errorf("ParseBoxRef(%q) = {Form:%q Name:%q Snapshot:%q URL:%q}, want {Form:%q Name:%q Snapshot:%q URL:%q}",
					tc.in, got.Form, got.Name, got.Snapshot, got.URL, tc.wantForm, tc.wantName, tc.wantSnapshot, tc.wantURL)
			}
		})
	}
}

// TestResolveBoxRef_PureArms covers every arm that resolves without host state:
// image: and imported: pass their payload straight through, a full box: ref
// (contains "/") passes through without touching the engine or container
// storage, and the vm: form without a snapshot errors before any registry read.
func TestResolveBoxRef_PureArms(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name            string
		in              string
		wantKind        string
		wantDisk        string
		wantErrContains string
	}{
		{name: "image form passes the url through", in: "image:https://cloud-images.example.invalid/noble-server-cloudimg-amd64.img", wantKind: BoxSourceCloudImage, wantDisk: "https://cloud-images.example.invalid/noble-server-cloudimg-amd64.img"},
		{name: "imported form passes the domain through", in: "imported:legacy-db", wantKind: BoxSourceImported, wantDisk: "legacy-db"},
		{name: "full box ref passes through prefixed", in: "box:quay.io/fedora/fedora-bootc:43", wantKind: BoxSourceBootc, wantDisk: "quay.io/fedora/fedora-bootc:43"},
		{name: "full box ref passes through bare", in: "ghcr.io/opencharly/fedora-bootc:2026.145.0900", wantKind: BoxSourceBootc, wantDisk: "ghcr.io/opencharly/fedora-bootc:2026.145.0900"},
		{name: "vm form without snapshot errors", in: "vm:web", wantErrContains: "requires a snapshot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBoxRef(ctx, nil, tc.in)
			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("ResolveBoxRef(%q) = %+v, want error containing %q", tc.in, got, tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("ResolveBoxRef(%q) error = %q, want it to contain %q", tc.in, err, tc.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveBoxRef(%q) unexpected error: %v", tc.in, err)
			}
			if got.SourceKind != tc.wantKind || got.DiskSource != tc.wantDisk {
				t.Errorf("ResolveBoxRef(%q) = {SourceKind:%q DiskSource:%q}, want {SourceKind:%q DiskSource:%q}", tc.in, got.SourceKind, got.DiskSource, tc.wantKind, tc.wantDisk)
			}
			if got.Metadata != nil {
				t.Errorf("ResolveBoxRef(%q): Metadata = %+v, want nil (metadata read is a later cutover)", tc.in, got.Metadata)
			}
		})
	}
}

// TestResolveBoxRef_BoxShortNameCalVer is the deterministic short-name arm test:
// it stubs the local-image listing + existence check (the same var swap
// plugin-vm's vm_bootc_install_test.go uses) so a short kind:image name elects
// its newest local CalVer tag — never ":latest" — without touching container
// storage.
func TestResolveBoxRef_BoxShortNameCalVer(t *testing.T) {
	origList := container.ListLocalImages
	origExists := container.LocalImageExists
	container.ListLocalImages = func(engine string) ([]container.LocalImageInfo, error) {
		return []container.LocalImageInfo{{
			Names:  []string{"ghcr.io/opencharly/fedora-bootc:2026.145.0900"},
			Labels: map[string]string{spec.LabelBox: "fedora-bootc", spec.LabelVersion: "2026.145.0900"},
		}}, nil
	}
	container.LocalImageExists = func(engine, ref string) bool { return true }
	t.Cleanup(func() {
		container.ListLocalImages = origList
		container.LocalImageExists = origExists
	})

	got, err := ResolveBoxRef(context.Background(), nil, "fedora-bootc")
	if err != nil {
		t.Fatalf("ResolveBoxRef(bare short name) unexpected error: %v", err)
	}
	if got.SourceKind != BoxSourceBootc {
		t.Errorf("SourceKind = %q, want %q", got.SourceKind, BoxSourceBootc)
	}
	if got.DiskSource != "ghcr.io/opencharly/fedora-bootc:2026.145.0900" {
		t.Errorf("DiskSource = %q, want the newest local CalVer ref", got.DiskSource)
	}
	if strings.Contains(got.DiskSource, ":latest") {
		t.Errorf("DiskSource = %q — charly is CalVer-only, never :latest", got.DiskSource)
	}
	if got.Metadata != nil {
		t.Errorf("Metadata = %+v, want nil", got.Metadata)
	}
}

// TestResolveBoxRef_BoxShortName proves a short box: name resolves against REAL
// local container storage through kit.ResolveLocalImageRef (label-preferred:
// a box built with `charly box build` carries ai.opencharly.box). Skips when
// the resolved engine's CLI is absent or local storage holds no images.
func TestResolveBoxRef_BoxShortName(t *testing.T) {
	engine := resolveEngine()
	if _, err := exec.LookPath(engine); err != nil {
		t.Skipf("engine %q not installed — skipping live short-name resolution", engine)
	}
	infos, err := kit.ListLocalImages(engine)
	if err != nil {
		t.Skipf("listing local %s images: %v", engine, err)
	}
	short := ""
	for _, info := range infos {
		if b := info.Labels[spec.LabelBox]; b != "" {
			short = b
			break
		}
		if short == "" && len(info.Names) > 0 {
			n := info.Names[0]
			if i := strings.LastIndex(n, "/"); i >= 0 {
				n = n[i+1:]
			}
			if i := strings.LastIndex(n, ":"); i > 0 {
				n = n[:i]
			}
			short = n
		}
	}
	if short == "" {
		t.Skip("no local images to resolve a short name against")
	}

	got, err := ResolveBoxRef(context.Background(), nil, short)
	if err != nil {
		t.Fatalf("ResolveBoxRef(%q) unexpected error: %v", short, err)
	}
	if got.SourceKind != BoxSourceBootc {
		t.Errorf("SourceKind = %q, want %q", got.SourceKind, BoxSourceBootc)
	}
	if got.DiskSource == short || !strings.Contains(got.DiskSource, "/") {
		t.Errorf("DiskSource = %q, want a resolved full ref (not the raw short name %q)", got.DiskSource, short)
	}
	if got.Metadata != nil {
		t.Errorf("Metadata = %+v, want nil", got.Metadata)
	}
}

// TestResolveBoxRef_VmSnapshot proves the vm:@snapshot arm resolves the
// snapshot's external disk path from the snapshot registry. It points
// CHARLY_VM_STATE_DIR at a temp dir and writes a minimal registry (the layout
// vmshared's LookupSnapshot reads: <state>/charly-<vm>/snapshots/registry.json)
// so no real host state is touched.
func TestResolveBoxRef_VmSnapshot(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(vmshared.VmStateDirEnv, stateRoot)

	entity := "src-vm"
	disk := filepath.Join(stateRoot, "golden-disk.qcow2")
	if err := os.WriteFile(disk, nil, 0o644); err != nil {
		t.Fatalf("writing snapshot disk file: %v", err)
	}
	reg := vmshared.SnapshotRegistry{Version: 1, Snapshots: map[string]*vmshared.SnapshotEntry{
		"golden":        {Name: "golden", Mode: "external", DiskPath: disk},
		"internal-only": {Name: "internal-only", Mode: "internal"},
	}}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("marshaling registry: %v", err)
	}
	regDir := filepath.Join(stateRoot, "charly-"+entity, "snapshots")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatalf("creating registry dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, "registry.json"), data, 0o644); err != nil {
		t.Fatalf("writing registry: %v", err)
	}

	t.Run("external snapshot resolves to its disk path", func(t *testing.T) {
		got, err := ResolveBoxRef(context.Background(), nil, "vm:"+entity+"@golden")
		if err != nil {
			t.Fatalf("ResolveBoxRef(vm:...@golden) unexpected error: %v", err)
		}
		if got.SourceKind != BoxSourceClone {
			t.Errorf("SourceKind = %q, want %q", got.SourceKind, BoxSourceClone)
		}
		if got.DiskSource != disk {
			t.Errorf("DiskSource = %q, want the snapshot's external disk %q", got.DiskSource, disk)
		}
		if got.Metadata != nil {
			t.Errorf("Metadata = %+v, want nil", got.Metadata)
		}
	})

	t.Run("unknown snapshot errors", func(t *testing.T) {
		_, err := ResolveBoxRef(context.Background(), nil, "vm:"+entity+"@nope")
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("error = %v, want it to report the snapshot does not exist", err)
		}
	})

	t.Run("internal snapshot without disk path is refused", func(t *testing.T) {
		_, err := ResolveBoxRef(context.Background(), nil, "vm:"+entity+"@internal-only")
		if err == nil || !strings.Contains(err.Error(), "no external disk path") {
			t.Fatalf("error = %v, want it to refuse an internal snapshot", err)
		}
	})
}
