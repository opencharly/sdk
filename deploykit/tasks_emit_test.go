package deploykit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/spec"
)

// testResolvedBox returns a ResolvedBox suitable for feeding the task
// emitters. Uses a hand-built fedora/rpm DistroDef (the real embedded
// build-vocabulary cache_mount for rpm, from charly/charly.yml) with
// UID/GID 1000 — no charly.yml load needed, this package is sdk-only.
func testResolvedBox() *buildkit.ResolvedBox {
	return &buildkit.ResolvedBox{
		Name:         "test-img",
		User:         "user",
		UID:          1000,
		GID:          1000,
		Home:         "/home/user",
		Pkg:          "rpm",
		BuildFormats: []string{"rpm"},
		Tags:         []string{"all", "rpm"},
		DistroDef: &spec.ResolvedDistro{
			Format: map[string]*spec.Format{
				"rpm": {
					CacheMount: []spec.CacheMount{
						{Dst: "/var/cache/libdnf5", Sharing: "locked"},
					},
				},
			},
		},
	}
}

// --- Variable substitution ---

func TestTaskSubstAutoExports(t *testing.T) {
	img := testResolvedBox()
	cases := []struct {
		in, want string
	}{
		{"${USER}", "user"},
		{"${UID}", "1000"},
		{"${GID}", "1000"},
		{"${HOME}", "/home/user"},
		{"hello ${USER}!", "hello user!"},
		{"${UNKNOWN}", "${UNKNOWN}"}, // left alone
		{"${USER}/${HOME}", "user//home/user"},
	}
	for _, c := range cases {
		got := TaskSubstAutoExports(c.in, img)
		if got != c.want {
			t.Errorf("TaskSubstAutoExports(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTaskSubstPath_TildeExpansion(t *testing.T) {
	img := testResolvedBox()
	got := TaskSubstPath("~/.local/bin", img)
	if got != "/home/user/.local/bin" {
		t.Errorf("tilde expansion: got %q", got)
	}
}

func TestTaskUnresolvedRefs(t *testing.T) {
	known := TaskKnownNames(map[string]string{"MY_VAR": "x"})
	refs := TaskUnresolvedRefs("${MY_VAR}/${USER}/${MISSING}/${NOPE}", known)
	if len(refs) != 2 {
		t.Fatalf("expected 2 unresolved, got %d: %v", len(refs), refs)
	}
	// Order preserved, duplicates deduped
	if refs[0] != "MISSING" || refs[1] != "NOPE" {
		t.Errorf("unresolved = %v, want [MISSING NOPE]", refs)
	}
}

// --- User resolution ---

func TestResolveUserSpec(t *testing.T) {
	img := testResolvedBox()
	cases := []struct {
		in, wantDirective, wantChown string
	}{
		{"", "0", ""},
		{"root", "0", ""},
		{"0", "0", ""},
		{"${USER}", "1000", "1000:1000"},
		{"1000:1000", "1000:1000", "1000:1000"},
		{"500", "500", "500:500"},
		{"postgres", "postgres", "postgres:postgres"},
	}
	for _, c := range cases {
		gotDir, gotCh := ResolveUserSpec(c.in, img)
		if gotDir != c.wantDirective || gotCh != c.wantChown {
			t.Errorf("ResolveUserSpec(%q) = (%q, %q), want (%q, %q)",
				c.in, gotDir, gotCh, c.wantDirective, c.wantChown)
		}
	}
}

// TestTaskRunsAsRoot pins the stage-user decision: RunAs empty inherits the
// image build user (root only when the image builds as root); an explicit
// RunAs wins. This is the ownership pivot for every cache mount — getting it
// wrong root-ifies a non-root stage's cache (the curl-23 build failure).
func TestTaskRunsAsRoot(t *testing.T) {
	userImg := &buildkit.ResolvedBox{User: "user", UID: 1000, GID: 1000, Home: "/home/user"}
	rootImg := &buildkit.ResolvedBox{User: "root", UID: 0, GID: 0, Home: "/root"}
	cases := []struct {
		name  string
		runAs string
		img   *buildkit.ResolvedBox
		want  bool
	}{
		{"empty RunAs on a non-root image inherits the stage's non-root user", "", userImg, false},
		{"empty RunAs on a root image is root", "", rootImg, true},
		{"explicit root RunAs on a non-root image is root", "root", userImg, true},
		{"explicit 0 RunAs on a non-root image is root", "0", userImg, true},
		{"${USER} RunAs on a non-root image is non-root", "${USER}", userImg, false},
	}
	for _, c := range cases {
		if got := taskRunsAsRoot(c.runAs, c.img); got != c.want {
			t.Errorf("%s: taskRunsAsRoot(%q, uid=%d) = %v, want %v", c.name, c.runAs, c.img.UID, got, c.want)
		}
	}
}

// --- Inline content staging ---

func TestStageInlineContent_Idempotent(t *testing.T) {
	dir := t.TempDir()
	buildDir := filepath.Join(dir, ".build", "img")
	ctx := ".build/img"

	rel1, err := StageInlineContent(buildDir, ctx, "lyr", "hello\n")
	if err != nil {
		t.Fatalf("stage 1: %v", err)
	}
	rel2, err := StageInlineContent(buildDir, ctx, "lyr", "hello\n")
	if err != nil {
		t.Fatalf("stage 2: %v", err)
	}
	if rel1 != rel2 {
		t.Errorf("non-idempotent path: %q vs %q", rel1, rel2)
	}
	if !strings.HasPrefix(rel1, ".build/img/_inline/lyr/") {
		t.Errorf("bad rel path: %q", rel1)
	}
	// Different content → different hash
	rel3, err := StageInlineContent(buildDir, ctx, "lyr", "different\n")
	if err != nil {
		t.Fatalf("stage 3: %v", err)
	}
	if rel3 == rel1 {
		t.Error("different content should produce different hash path")
	}
	// File actually exists
	abs := filepath.Join(dir, rel1)
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("reading staged file: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("staged content mismatch: %q", data)
	}
}

// --- Emitters ---

func TestEmitMkdirBatch_Coalesces(t *testing.T) {
	var b strings.Builder
	tasks := []spec.Op{
		{Mkdir: "/a", RunAs: "root"},
		{Mkdir: "/b", RunAs: "root"},
		{Mkdir: "/c", RunAs: "root"},
	}
	EmitMkdirBatch(&b, tasks, testResolvedBox())
	out := b.String()
	if !strings.Contains(out, "RUN mkdir -p /a /b /c") {
		t.Errorf("expected coalesced mkdir, got:\n%s", out)
	}
	// Only one RUN line
	if strings.Count(out, "RUN") != 1 {
		t.Errorf("expected 1 RUN, got %d\n%s", strings.Count(out, "RUN"), out)
	}
}

func TestEmitMkdirBatch_PerModeChmod(t *testing.T) {
	var b strings.Builder
	tasks := []spec.Op{
		{Mkdir: "/a", Mode: "0700"},
		{Mkdir: "/b"}, // default — no chmod
		{Mkdir: "/c", Mode: "0700"},
	}
	EmitMkdirBatch(&b, tasks, testResolvedBox())
	out := b.String()
	if !strings.Contains(out, "mkdir -p /a /b /c") {
		t.Errorf("mkdir missing paths:\n%s", out)
	}
	if !strings.Contains(out, "chmod 0700 /a /c") {
		t.Errorf("chmod should group by mode:\n%s", out)
	}
}

func TestEmitCopy_WithChown(t *testing.T) {
	var b strings.Builder
	EmitCopy(&b,
		spec.Op{Copy: "wrapper", To: "/home/user/.local/bin/wrapper", Mode: "0755", RunAs: "${USER}"},
		"my-layer", testResolvedBox(),
	)
	out := b.String()
	if !strings.Contains(out, "--from=my-layer") {
		t.Errorf("missing layer stage reference:\n%s", out)
	}
	if !strings.Contains(out, "--chmod=0755") {
		t.Errorf("missing chmod:\n%s", out)
	}
	if !strings.Contains(out, "--chown=1000:1000") {
		t.Errorf("missing chown for ${USER} (should resolve to numeric UID:GID):\n%s", out)
	}
	if !strings.Contains(out, "wrapper /home/user/.local/bin/wrapper") {
		t.Errorf("missing src/dest:\n%s", out)
	}
}

func TestEmitCopy_RootNoChown(t *testing.T) {
	var b strings.Builder
	EmitCopy(&b,
		spec.Op{Copy: "traefik.yml", To: "/etc/traefik/traefik.yml", Mode: "0644", RunAs: "root"},
		"traefik", testResolvedBox(),
	)
	out := b.String()
	if strings.Contains(out, "--chown") {
		t.Errorf("root should not emit --chown:\n%s", out)
	}
}

func TestEmitWrite_UsesStagedPath(t *testing.T) {
	var b strings.Builder
	EmitWrite(&b,
		spec.Op{Write: "/etc/foo.conf", Content: "body", Mode: "0644", RunAs: "root"},
		".build/img/_inline/lyr/abc123",
		testResolvedBox(),
	)
	out := b.String()
	if !strings.Contains(out, "COPY --chmod=0644 .build/img/_inline/lyr/abc123 /etc/foo.conf") {
		t.Errorf("write should COPY from staged path:\n%s", out)
	}
	// root: no chown
	if strings.Contains(out, "--chown") {
		t.Errorf("root write should not emit --chown:\n%s", out)
	}
}

func TestEmitLinkBatch(t *testing.T) {
	var b strings.Builder
	tasks := []spec.Op{
		{Link: "/usr/local/bin/node", Target: "/usr/bin/node-24"},
		{Link: "/usr/local/bin/npm", Target: "/usr/bin/npm-24"},
	}
	EmitLinkBatch(&b, tasks, testResolvedBox())
	out := b.String()
	if !strings.Contains(out, "ln -sf /usr/bin/node-24 /usr/local/bin/node") {
		t.Errorf("missing first link:\n%s", out)
	}
	if !strings.Contains(out, "ln -sf /usr/bin/npm-24 /usr/local/bin/npm") {
		t.Errorf("missing second link:\n%s", out)
	}
	if strings.Count(out, "RUN") != 1 {
		t.Errorf("links should coalesce to one RUN:\n%s", out)
	}
}

func TestEmitSetcapBatch_StripAndSet(t *testing.T) {
	var b strings.Builder
	tasks := []spec.Op{
		{Setcap: "/usr/bin/sway"}, // strip
		{Setcap: "/usr/bin/newuidmap", Caps: "cap_setuid=ep"},
	}
	EmitSetcapBatch(&b, tasks, testResolvedBox())
	out := b.String()
	if !strings.Contains(out, "setcap -r /usr/bin/sway") {
		t.Errorf("strip should use -r:\n%s", out)
	}
	if !strings.Contains(out, "setcap cap_setuid=ep /usr/bin/newuidmap") {
		t.Errorf("set should include caps:\n%s", out)
	}
	if strings.Count(out, "RUN") != 1 {
		t.Errorf("setcap should coalesce to one RUN:\n%s", out)
	}
}

func TestEmitDownload_TarGz(t *testing.T) {
	var b strings.Builder
	err := EmitDownload(&b,
		spec.Op{
			Download:       "https://example.com/app.tar.gz",
			Extract:        "tar.gz",
			To:             "/usr/local/bin",
			ExtractInclude: []string{"app"},
		},
		testResolvedBox(),
	)
	if err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	out := b.String()
	// Now extracts from the content-addressed cache file ($__c), not a stream.
	if !strings.Contains(out, `tar -xzf "$__c" -C /usr/local/bin app`) {
		t.Errorf("missing tar -xzf from cache file with include filter:\n%s", out)
	}
	if !strings.Contains(out, "BUILD_ARCH=$(uname -m)") {
		t.Errorf("should set BUILD_ARCH from uname:\n%s", out)
	}
	// The file must actually be CACHED: content-addressed path under the mount,
	// fetched only when absent, atomically renamed from .part on success.
	if !strings.Contains(out, "/tmp/downloads/$(printf %s") || !strings.Contains(out, "sha256sum") {
		t.Errorf("download must be content-addressed in /tmp/downloads:\n%s", out)
	}
	if !strings.Contains(out, `[ -s "$__c" ] ||`) {
		t.Errorf("download must skip re-fetch when the cached file already exists:\n%s", out)
	}
	if !strings.Contains(out, `-o "$__c.part"`) || !strings.Contains(out, `mv -f "$__c.part" "$__c"`) {
		t.Errorf("download must be integrity-safe (.part + atomic rename):\n%s", out)
	}
	// testResolvedBox() is a non-root (UID 1000) stage with no explicit RunAs,
	// so the downloads cache follows the stage user (taskRunsAsRoot) and gets
	// the uid/gid-owned mount, not the shared root-only form.
	if !strings.Contains(out, "--mount=type=cache,id=charly-tmp-downloads-uid1000,dst=/tmp/downloads,uid=1000,gid=1000") {
		t.Errorf("non-root stage should declare an uid/gid-owned downloads cache mount:\n%s", out)
	}
}

func TestEmitDownload_Sh(t *testing.T) {
	var b strings.Builder
	err := EmitDownload(&b,
		spec.Op{Download: "https://sh.install", Extract: "sh", Env: map[string]string{"UV_INSTALL_DIR": "/usr/local/bin"}},
		testResolvedBox(),
	)
	if err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "UV_INSTALL_DIR=") {
		t.Errorf("should include env var assignment:\n%s", out)
	}
	if !strings.Contains(out, "/usr/local/bin") {
		t.Errorf("should include env value:\n%s", out)
	}
	// The install script is now cached then run from the cache file: the env
	// vars precede `sh "$__c"` so the SCRIPT sees them.
	if !strings.Contains(out, `sh "$__c"`) {
		t.Errorf("should run the cached install script:\n%s", out)
	}
	if !strings.Contains(out, "sha256sum") || !strings.Contains(out, "/tmp/downloads") {
		t.Errorf("install script should also be content-addressed in the cache:\n%s", out)
	}
	idxSh := strings.LastIndex(out, `sh "$__c"`)
	idxEnv := strings.LastIndex(out, "UV_INSTALL_DIR=")
	if idxEnv > idxSh {
		t.Errorf("env vars should appear BEFORE `sh \"$__c\"` so the script sees them:\n%s", out)
	}
}

func TestEmitDownload_CacheModifier(t *testing.T) {
	// A download task can declare extra `cache:` mounts (e.g. a build cache),
	// owned per the task user. Root task → shared mount; user task → owned.
	var b strings.Builder
	if err := EmitDownload(&b,
		spec.Op{Download: "https://x/app.zip", Extract: "zip", To: "/opt/app", RunAs: "root",
			Cache: []string{"/var/cache/app-build"}},
		testResolvedBox()); err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "--mount=type=cache,id=charly-var-cache-app-build,dst=/var/cache/app-build,sharing=locked") {
		t.Errorf("root download task should get a SHARED cache mount for cache:\n%s", out)
	}
	if !strings.Contains(out, `unzip -o "$__c" -d /opt/app`) {
		t.Errorf("zip should extract from the cache file:\n%s", out)
	}
}

// TestEmitDownload_DownloadsCacheOwnership: the shared downloads cache follows
// the stage user — non-root stages get an uid/gid-owned cache (so one
// root-stage download can never poison every non-root stage's downloads), root
// stages keep the shared form.
func TestEmitDownload_DownloadsCacheOwnership(t *testing.T) {
	op := spec.Op{Download: "https://example.com/x.tar.gz", Extract: "none", To: "/usr/local/bin/x"}

	var nonRoot strings.Builder
	img := &buildkit.ResolvedBox{User: "user", UID: 1000, GID: 1000, Home: "/home/user"}
	if err := EmitDownload(&nonRoot, op, img); err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	if out := nonRoot.String(); !strings.Contains(out, "--mount=type=cache,id=charly-tmp-downloads-uid1000,dst=/tmp/downloads,uid=1000,gid=1000") {
		t.Errorf("non-root stage must get an uid/gid-owned downloads cache mount:\n%s", out)
	}

	var root strings.Builder
	img.User = "root"
	img.UID = 0
	img.GID = 0
	if err := EmitDownload(&root, op, img); err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	out := root.String()
	if !strings.Contains(out, "--mount=type=cache,id=charly-tmp-downloads,dst=/tmp/downloads,sharing=locked") {
		t.Errorf("root stage must keep the shared downloads cache mount:\n%s", out)
	}
	if strings.Contains(out, "charly-tmp-downloads-uid") {
		t.Errorf("root stage must not get a uid-scoped cache id:\n%s", out)
	}
}

func TestTaskCacheMounts_OwnershipByUser(t *testing.T) {
	img := testResolvedBox() // UID/GID 1000 in the test fixture
	// root task → shared (sharing=locked), no uid in id
	root := TaskCacheMounts(spec.Op{RunAs: "root", Cache: []string{"/var/cache/x"}}, img)
	if len(root) != 1 || !strings.Contains(root[0], "sharing=locked") || strings.Contains(root[0], "uid=") {
		t.Errorf("root cache mount should be shared (no uid): %v", root)
	}
	// user task → owned (uid/gid), id carries -uid<N>
	user := TaskCacheMounts(spec.Op{RunAs: "${USER}", Cache: []string{"/var/cache/x"}}, img)
	if len(user) != 1 || !strings.Contains(user[0], "uid=") || !strings.Contains(user[0], "-uid") {
		t.Errorf("non-root cache mount should be uid-owned: %v", user)
	}
	// no cache: → no mounts
	if got := TaskCacheMounts(spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "x"}}, img); got != nil {
		t.Errorf("no cache: should yield nil, got %v", got)
	}
}

func TestEmitDownload_UnknownExtract(t *testing.T) {
	var b strings.Builder
	err := EmitDownload(&b, spec.Op{Download: "http://x", Extract: "rar"}, testResolvedBox())
	if err == nil {
		t.Fatal("expected error for unknown extract")
	}
}

func TestEmitCmd_RootCacheMounts(t *testing.T) {
	var b strings.Builder
	EmitCmd(&b,
		spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "echo hello"}, RunAs: "root"},
		"my-layer", testResolvedBox(), true,
	)
	out := b.String()
	if !strings.Contains(out, "--mount=type=bind,from=my-layer") {
		t.Errorf("should bind-mount layer stage at /ctx:\n%s", out)
	}
	if !strings.Contains(out, "libdnf5") {
		t.Errorf("root cmd should include distro format cache:\n%s", out)
	}
	if !strings.Contains(out, "set -e") {
		t.Errorf("should include set -e:\n%s", out)
	}
	if !strings.Contains(out, "BUILD_ARCH=$(uname -m)") {
		t.Errorf("should set BUILD_ARCH inside shell:\n%s", out)
	}
}

func TestEmitCmd_UserNpmCache(t *testing.T) {
	var b strings.Builder
	EmitCmd(&b,
		spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "xdg-settings default-browser foo"}, RunAs: "${USER}"},
		"my-layer", testResolvedBox(), false,
	)
	out := b.String()
	if strings.Contains(out, "libdnf5") {
		t.Errorf("non-root cmd should NOT include distro cache:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/npm-cache") {
		t.Errorf("non-root cmd should include npm cache:\n%s", out)
	}
	if !strings.Contains(out, "uid=1000,gid=1000") {
		t.Errorf("npm cache should be UID/GID owned:\n%s", out)
	}
}

// --- emitVarsEnv ---

func TestEmitVarsEnv_AlwaysEmitsArch(t *testing.T) {
	var b strings.Builder
	EmitVarsEnv(&b, nil)
	out := b.String()
	if !strings.Contains(out, "ARG TARGETARCH") {
		t.Errorf("expected ARG TARGETARCH:\n%s", out)
	}
	if !strings.Contains(out, "ENV ARCH=${TARGETARCH}") {
		t.Errorf("expected ENV ARCH=${TARGETARCH}:\n%s", out)
	}
}

func TestEmitVarsEnv_SortedKeys(t *testing.T) {
	var b strings.Builder
	EmitVarsEnv(&b, map[string]string{"ZETA": "z", "ALPHA": "a", "MIDDLE": "m"})
	out := b.String()
	idxA := strings.Index(out, "ENV ALPHA")
	idxM := strings.Index(out, "ENV MIDDLE")
	idxZ := strings.Index(out, "ENV ZETA")
	if idxA >= idxM || idxM >= idxZ {
		t.Errorf("vars should be emitted in sorted order:\n%s", out)
	}
}
