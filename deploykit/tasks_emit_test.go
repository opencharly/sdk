package deploykit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// testResolvedBox returns a ResolvedBox suitable for feeding the task
// emitters. Uses a hand-built fedora/rpm DistroDef (the real embedded
// build-vocabulary cache_mount for rpm, from charly/charly.yml) with
// UID/GID 1000 — no charly.yml load needed, this package is sdk-only.
func testResolvedBox() *buildkit.ResolvedBox {
	return &buildkit.ResolvedBox{ResolvedBox: spec.ResolvedBox{Name: "test-img", User: "user", UID: 1000, GID: 1000, Home: "/home/user", Pkg: "rpm", BuildFormats: []string{"rpm"}, Tags: []string{"all", "rpm"}}, DistroDef: &spec.ResolvedDistro{
		Format: map[string]*spec.Format{
			"rpm": {
				CacheMount: []spec.CacheMount{
					{Dst: "/var/cache/libdnf5", Sharing: "locked"},
				},
			},
		},
	}}
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
	userImg := &buildkit.ResolvedBox{ResolvedBox: spec.ResolvedBox{User: "user", UID: 1000, GID: 1000, Home: "/home/user"}}
	rootImg := &buildkit.ResolvedBox{ResolvedBox: spec.ResolvedBox{User: "root", UID: 0, GID: 0, Home: "/root"}}
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
	// The dest dir is created first (mkdir -p), so a download to a
	// not-yet-existing directory (e.g. /opt/agentteams) works — the tar -C
	// target must exist before tar runs. Matches `copy:`'s auto-create.
	if !strings.Contains(out, `mkdir -p /usr/local/bin && tar -xzf "$__c"`) {
		t.Errorf("download must mkdir -p the extract dest before tar:\n%s", out)
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
	img := &buildkit.ResolvedBox{ResolvedBox: spec.ResolvedBox{User: "user", UID: 1000, GID: 1000, Home: "/home/user"}}
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

// TestEmitDownload_NoneCreatesParentDir: extract:none's dest is a FILE (e.g.
// /usr/local/bin/uv), so the download creates the parent dir (dirname), not the
// dest itself — a download to a not-yet-existing directory tree works.
func TestEmitDownload_NoneCreatesParentDir(t *testing.T) {
	var b strings.Builder
	err := EmitDownload(&b,
		spec.Op{Download: "https://example.com/uv", Extract: "none", To: "/usr/local/bin/uv"},
		testResolvedBox(),
	)
	if err != nil {
		t.Fatalf("EmitDownload: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, `mkdir -p $(dirname /usr/local/bin/uv) && cp -f "$__c" /usr/local/bin/uv`) {
		t.Errorf("extract:none must mkdir -p the parent dir before cp:\n%s", out)
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

// --- EmitTasks orchestrator (relocated from charly/tasks_test.go, #55 cone-render Unit A:
// these exercise deploykit.Generator.EmitTasks directly — the render engine lives here, so the
// coverage lives here too; charly's toDeploykit() wrapper was production-dead and deleted). ---

func TestEmitTasks_UserCoalescing(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Mkdir: "/a", RunAs: "root"},
		{Mkdir: "/b", RunAs: "root"},
		{Mkdir: "/c", RunAs: "root"}, // all root → single USER 0 header, one RUN
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "USER") {
		t.Errorf("no USER directive expected when starting user matches task user:\n%s", out)
	}
	if strings.Count(out, "RUN") != 1 {
		t.Errorf("three mkdirs should coalesce to one RUN:\n%s", out)
	}
}

// A command: task (authored as plugin:command) must emit a RUN through the EmitTasks verb
// switch (the plugin:command special case rehydrates to EmitCmd — no EmitPluginOp seam needed).
func TestEmitTasks_CommandEmitsRun(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Plugin: "command", PluginInput: map[string]any{"command": "echo rpmfusion-enable"}, RunAs: "root"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "RUN") || !strings.Contains(out, "echo rpmfusion-enable") {
		t.Errorf("command task must emit a RUN in the OCI build, got:\n%s", out)
	}
}

func TestEmitTasks_UserSwitches(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Mkdir: "/a", RunAs: "root"},
		{Mkdir: "/b", RunAs: "${USER}"},
		{Mkdir: "/c", RunAs: "${USER}"}, // coalesces with previous
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	if strings.Count(out, "USER ") != 1 {
		t.Errorf("expected 1 USER switch, got %d:\n%s", strings.Count(out, "USER "), out)
	}
	if !strings.Contains(out, "USER 1000") {
		t.Errorf("should switch to USER 1000 (numeric form from ${USER}):\n%s", out)
	}
	if strings.Count(out, "mkdir") != 2 {
		t.Errorf("expected 2 mkdir (across users):\n%s", out)
	}
}

func TestEmitTasks_OrderPreserved(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Mkdir: "/a", RunAs: "root"},
		{Copy: "f", To: "/a/f", RunAs: "root"},
		{Mkdir: "/b", RunAs: "root"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	idx1 := strings.Index(out, "mkdir -p /a")
	idxCopy := strings.Index(out, "COPY")
	idx2 := strings.Index(out, "mkdir -p /b")
	if idx1 < 0 || idxCopy < 0 || idx2 < 0 {
		t.Fatalf("missing directive: mkdir1=%d copy=%d mkdir2=%d\n%s", idx1, idxCopy, idx2, out)
	}
	if idx1 >= idxCopy || idxCopy >= idx2 {
		t.Errorf("order violated: mkdir1=%d copy=%d mkdir2=%d\n%s", idx1, idxCopy, idx2, out)
	}
}

func TestEmitTasks_ParentDirAutoInsert(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Copy: "traefik.yml", To: "/etc/traefik/traefik.yml", RunAs: "root"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	idxMkdir := strings.Index(out, "mkdir -p /etc/traefik")
	idxCopy := strings.Index(out, "COPY")
	if idxMkdir < 0 {
		t.Errorf("expected auto-inserted parent mkdir:\n%s", out)
	}
	if idxCopy < idxMkdir {
		t.Errorf("parent mkdir must precede COPY:\n%s", out)
	}
}

func TestEmitTasks_ParentDirSuppressedWhenDeclared(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Mkdir: "/etc/foo", RunAs: "root"},
		{Copy: "bar", To: "/etc/foo/bar", RunAs: "root"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	if strings.Count(out, "mkdir -p /etc/foo") != 1 {
		t.Errorf("should not auto-insert parent dir already declared by author:\n%s", out)
	}
}

func TestEmitTasks_WriteStagesContent(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	ops := []spec.Op{
		{Write: "/etc/foo.conf", Content: "hello world\n", RunAs: "root"},
	}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	buildDir := filepath.Join(dir, "test-img")
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, buildDir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "COPY --chmod=0644 .build/test-img/_inline/lyr/") {
		t.Errorf("expected COPY from staged inline path:\n%s", out)
	}
	entries, _ := os.ReadDir(filepath.Join(buildDir, "_inline", "lyr"))
	if len(entries) != 1 {
		t.Errorf("expected one staged file, got %d", len(entries))
	}
}

// --- EmitTasks plugin-verb DISPATCH via the EmitPluginOp seam (relocated intent from the 4
// charly MIXED tests, #55 cone-render Unit A). The charly tests wired EmitPluginOp to the
// (production-dead) core provider registry; the LIVE dispatch behavior — EmitTasks routes a
// non-command plugin verb to the EmitPluginOp seam and splices the returned fragment (verbatim
// when ActScript=false, EmitCmd-wrapped when ActScript=true) — lives HERE in deploykit.EmitTasks,
// and candy/plugin-build wires the real seam (InvokeProvider(OpEmit)). A STUB seam proves the
// dispatch+splice without any provider registry. The act verbs' fragment CONTENT (package→dnf,
// unix-group→groupadd) is a SEPARATE per-act-plugin coverage concern (flagged follow-up). ---

func TestEmitTasks_PluginVerb_DispatchesToSeam_Verbatim(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	var gotOp string
	g.EmitPluginOp = func(op *spec.Op, _ *spec.ResolvedBox) (string, bool, error) {
		gotOp = op.Plugin
		return "RUN echo stub-fragment", false, nil // ActScript=false → spliced verbatim
	}
	ops := []spec.Op{{Plugin: "unix-group", PluginInput: map[string]any{"group": "docker"}, RunAs: "root"}}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	if gotOp != "unix-group" {
		t.Errorf("a non-command plugin verb must dispatch to the EmitPluginOp seam; got op %q", gotOp)
	}
	if !strings.Contains(b.String(), "RUN echo stub-fragment") {
		t.Errorf("EmitPluginOp's non-act fragment must be spliced verbatim:\n%s", b.String())
	}
}

func TestEmitTasks_PluginVerb_ActScriptWrappedInRun(t *testing.T) {
	dir := t.TempDir()
	g := NewRenderGenerator()
	g.BuildDir = dir
	g.EmitPluginOp = func(_ *spec.Op, _ *spec.ResolvedBox) (string, bool, error) {
		return "groupadd -f docker", true, nil // ActScript=true → EmitCmd-wrapped into a RUN
	}
	ops := []spec.Op{{Plugin: "unix-group", PluginInput: map[string]any{"group": "docker"}, RunAs: "root"}}
	layer := testCandy("lyr", spec.CandyModel{}, spec.CandyView{})
	var b strings.Builder
	if _, err := g.EmitTasks(&b, layer, testResolvedBox(), ops, dir, ".build/test-img"); err != nil {
		t.Fatalf("EmitTasks: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "RUN") || !strings.Contains(out, "groupadd -f docker") {
		t.Errorf("an ActScript act-shell must be EmitCmd-wrapped into a RUN:\n%s", out)
	}
}

// assertShellParses runs `bash -n` over a fragment the emitters produced. Substring and index
// assertions all pass on a string that cannot be executed — which is exactly how a `;` landing at
// the start of its own line survived review — so every guard test parses what it asserts on.
func assertShellParses(t *testing.T, label, script string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}
	f := filepath.Join(t.TempDir(), "probe.sh")
	if err := os.WriteFile(f, []byte(script+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", f).CombinedOutput()
	if err != nil {
		t.Errorf("%s does not parse as shell: %v\n--- bash -n ---\n%s\n--- script ---\n%s",
			label, err, out, script)
	}
}

// TestEmitDownload_UnlessExists is the discriminating coverage for the `unless_exists` gate:
// delete the wrap from EmitDownload and the first subtest fails on the missing `if [ -e ]`, while
// the second proves the emission is byte-identical to the unguarded form when the field is absent
// — so the test cannot pass by wrapping everything unconditionally.
func TestEmitDownload_UnlessExists(t *testing.T) {
	base := spec.Op{
		Download: "https://example.invalid/tool-1.0.tar.gz",
		Extract:  "tar.gz",
		To:       "/usr",
		Mode:     "0755",
	}

	t.Run("guard present wraps fetch, extract AND chmod", func(t *testing.T) {
		op := base
		op.UnlessExists = "/usr/bin/tool"
		var b strings.Builder
		if err := EmitDownload(&b, op, testResolvedBox()); err != nil {
			t.Fatalf("EmitDownload() error = %v", err)
		}
		got := b.String()
		// The whole RUN body is shell-quoted by the outer `sh -c` wrapper, so assert on the
		// structure that survives that escaping rather than on a literal quoting style.
		for _, want := range []string{
			"if [ -e ",
			"/usr/bin/tool",
			"skipping download:",
			"already present",
			"else {",
			"; fi",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("emission is missing %q:\n%s", want, got)
			}
		}
		// The chmod must be INSIDE the guard — outside it, a skipped step chmods a path it
		// never created and the build fails on the success case.
		guardStart := strings.Index(got, "if [ -e ")
		chmod := strings.Index(got, "chmod")
		fi := strings.LastIndex(got, "; fi")
		if guardStart < 0 || chmod < guardStart || chmod > fi {
			t.Errorf("chmod is not inside the guard (guard@%d chmod@%d fi@%d):\n%s",
				guardStart, chmod, fi, got)
		}
		assertShellParses(t, "the guarded download payload", unquoteSingle(got[guardStart:fi+len("; fi")]))
	})

	t.Run("guard absent emits byte-identically to before", func(t *testing.T) {
		var withField, without strings.Builder
		op := base
		op.UnlessExists = "   " // whitespace-only is not a guard
		if err := EmitDownload(&withField, op, testResolvedBox()); err != nil {
			t.Fatalf("EmitDownload() error = %v", err)
		}
		if err := EmitDownload(&without, base, testResolvedBox()); err != nil {
			t.Fatalf("EmitDownload() error = %v", err)
		}
		if withField.String() != without.String() {
			t.Errorf("a blank unless_exists changed the emission:\n--- with ---\n%s\n--- without ---\n%s",
				withField.String(), without.String())
		}
		if strings.Contains(without.String(), "if [ -e ") {
			t.Errorf("unguarded emission grew a guard:\n%s", without.String())
		}
	})
}

// TestWrapUnlessExists_QuotesBothPositions pins the fix for a defect in the first draft: the guard
// was ShellQuote'd for the `[ -e ]` test and interpolated RAW into the echo. A path containing a
// double quote made the emitted RUN a shell syntax error; one containing $(…) substituted in the
// message while the test used the literal, so the skip line named a different path than the one
// checked.
func TestWrapUnlessExists_QuotesBothPositions(t *testing.T) {
	got := WrapUnlessExists("true", `/opt/a"b$(id)`, "download")
	if strings.Count(got, `'/opt/a"b$(id)'`) != 2 {
		t.Errorf("the guard must appear shell-quoted in BOTH the test and the echo; got:\n%s", got)
	}
	if strings.Contains(got, `"skipping download: /opt/a`) {
		t.Errorf("the guard was interpolated raw into the echo:\n%s", got)
	}
	if WrapUnlessExists("true", "  ", "download") != "true" {
		t.Error("a blank guard must return the command unchanged")
	}
}

// TestEmitCmd_UnlessExists covers the gate on the `run:` verb. The schema documents
// unless_exists as a gate for "an install step", so honouring it in exactly one emitter would
// make it a silent no-op everywhere else — the shape a reader has no way to detect.
//
// It is honoured by the two emitters that produce ONE RUN for ONE op (download, cmd). The batch
// emitters (mkdir/link/setcap) fold MANY ops into a single RUN, so a per-op guard has no
// expressible position there, and copy/write emit a COPY instruction, which no shell test can
// wrap. Those are rejected at validation rather than silently ignored.
func TestEmitCmd_UnlessExists(t *testing.T) {
	img := testResolvedBox()

	var guarded strings.Builder
	EmitCmd(&guarded, spec.Op{
		Command:      "make install\nldconfig\n",
		UnlessExists: "/usr/bin/tool",
	}, "layer0", img, true)

	got := guarded.String()
	for _, want := range []string{"if [ -e ", "/usr/bin/tool", "skipping run:", "; fi"} {
		if !strings.Contains(got, want) {
			t.Errorf("guarded emission is missing %q:\n%s", want, got)
		}
	}
	// Both authored lines must sit INSIDE the guard — a multi-line command is skipped as one
	// unit, not line by line.
	if i, j := strings.Index(got, "if [ -e "), strings.LastIndex(got, "; fi"); i < 0 ||
		strings.Index(got, "make install") < i || strings.Index(got, "ldconfig") > j {
		t.Errorf("the authored command is not fully inside the guard:\n%s", got)
	}
	// The heredoc body is emitted verbatim, so it must PARSE. Substring and ordering checks above
	// are all satisfied by shell that bash refuses to run.
	assertShellParses(t, "the guarded run: heredoc body", heredocBody(got))

	var plain strings.Builder
	EmitCmd(&plain, spec.Op{Command: "make install\nldconfig\n"}, "layer0", img, true)
	if strings.Contains(plain.String(), "if [ -e ") {
		t.Errorf("an unguarded run: step grew a guard:\n%s", plain.String())
	}
}

// heredocBody extracts what EmitCmd wrote between its `<<'OVCMD'` marker and the closing `OVCMD`.
func heredocBody(emitted string) string {
	const open, close = "<<'OVCMD'\n", "\nOVCMD\n"
	i := strings.Index(emitted, open)
	j := strings.LastIndex(emitted, close)
	if i < 0 || j < 0 {
		return emitted
	}
	return emitted[i+len(open) : j]
}

// unquoteSingle undoes the '\” escaping the emitters apply when a payload is passed as a
// single-quoted `sh -c` argument, recovering the script the shell actually receives.
func unquoteSingle(s string) string {
	return strings.ReplaceAll(s, `'\''`, `'`)
}
