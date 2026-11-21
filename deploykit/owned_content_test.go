package deploykit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// owned_content_test.go — the build kit's half of ops.OpContent (opencharly/sdk#598), across the render
// contract's split: render-prep ASKS the content kinds (compileOwnedContent, with the live *Config), the
// envelope CARRIES the files (ProjectResolvedBox -> NewSpecResolvedBox), the render STAGES them
// (EmitOwnedContent, with Config nil). The kind's own rule (which nodes a box owns, where a body lives)
// is the kind plugin's and is tested there; here a recording fake stands in for the kind.

// contentFixture is a project whose repo root holds two candies under candy/<name>/ — `owner`, which the
// box composes, and `other`, which it does not — plus one file a kind will contribute.
type contentFixture struct {
	g    *Generator
	img  *ResolvedBox
	repo string
	body string // absolute path of the file the fake kind contributes
}

func newContentFixture(t *testing.T) *contentFixture {
	t.Helper()
	repo := t.TempDir()
	body := filepath.Join(repo, ".agents", "skills", "s", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(body, []byte("---\nname: s\ndescription: d — non-ASCII on purpose\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	candy := func(name string) CandyModel {
		return NewSpecCandyModel(spec.CandyModel{Name: name, SourceDir: filepath.Join(repo, "candy", name)}, spec.CandyView{Name: name})
	}
	g := &Generator{
		Dir:      repo,
		BuildDir: filepath.Join(repo, ".build"),
		Candies:  map[string]CandyModel{"owner": candy("owner"), "other": candy("other")},
		Config: &spec.Config{
			Box: spec.BoxMap{"app": spec.EncodeBox(spec.BoxConfig{Candy: []string{"owner"}})},
			PluginKinds: map[string]map[string]json.RawMessage{
				"skill": {"s": json.RawMessage(`{"owner":"owner"}`)},
			},
		},
	}
	img := &ResolvedBox{ResolvedBox: spec.ResolvedBox{Name: "app", UID: 1000, GID: 1000, Home: "/home/user"}}
	return &contentFixture{g: g, img: img, repo: repo, body: body}
}

// recordingKind answers with one file and one label, and records every request it got.
func (f *contentFixture) recordingKind(got *[]spec.OwnedContentRequest, asked *[]string) OwnedContentSource {
	return OwnedContentSource{
		Kinds: []string{"skill"},
		Ask: func(kind string, req spec.OwnedContentRequest) (*spec.OwnedContentReply, error) {
			*asked = append(*asked, kind)
			*got = append(*got, req)
			return &spec.OwnedContentReply{
				Files:  []spec.ContentFile{{Source: f.body, Path: ".agents/skills/s/SKILL.md"}},
				Labels: map[string]spec.RawBody{spec.LabelSkill: spec.RawBody(`[{"family":"f","name":"s","content":"é — prose"}]`)},
			}, nil
		},
	}
}

// TestOwnedContent_TheRequestIsTheBoxs: the kind is asked with the box's image user, ONLY the composed
// candies (with the absolute root of the repo that authors each — here the project root, because they
// sit under candy/<name>/), the names that are images, and its own nodes.
func TestOwnedContent_TheRequestIsTheBoxs(t *testing.T) {
	f := newContentFixture(t)
	var got []spec.OwnedContentRequest
	var asked []string
	f.g.Content = f.recordingKind(&got, &asked)
	if _, err := f.g.compileOwnedContent("app", f.img); err != nil {
		t.Fatalf("compileOwnedContent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the kind was asked %d times, want once", len(got))
	}
	req := got[0]
	if req.Box != "app" || req.Home != "/home/user" || req.UID != 1000 || req.GID != 1000 {
		t.Fatalf("request box/user = %q %q %d:%d, want app /home/user 1000:1000", req.Box, req.Home, req.UID, req.GID)
	}
	if len(req.Candies) != 1 || req.Candies[0].Name != "owner" || req.Candies[0].RepoRoot != f.repo {
		t.Fatalf("request candies = %+v, want only the composed `owner` with repo root %s", req.Candies, f.repo)
	}
	if strings.Join(req.Images, ",") != "app" {
		t.Fatalf("request images = %q, want the scope's images", req.Images)
	}
	if string(req.Nodes["s"]) != `{"owner":"owner"}` {
		t.Fatalf("request nodes = %v, want the kind's raw node verbatim", req.Nodes)
	}
}

// TestOwnedContent_AKindWithNoNodesIsNotAsked: a declared kind with no node in the box's scope has
// nothing to contribute and is not invoked; no declared kind means nothing is asked at all.
func TestOwnedContent_AKindWithNoNodesIsNotAsked(t *testing.T) {
	f := newContentFixture(t)
	var got []spec.OwnedContentRequest
	var asked []string
	f.g.Content = f.recordingKind(&got, &asked)
	f.g.Content.Kinds = []string{"hook"} // declared, but the project has no hook node
	if c, err := f.g.compileOwnedContent("app", f.img); err != nil || len(c.files) != 0 || len(asked) != 0 {
		t.Fatalf("a kind with no nodes: asked %v, files %v, err %v — want not asked", asked, c.files, err)
	}
	f.g.Content.Kinds = nil
	if c, err := f.g.compileOwnedContent("app", f.img); err != nil || len(c.files) != 0 || len(asked) != 0 {
		t.Fatalf("no content kinds: asked %v, files %v, err %v — want nothing", asked, c.files, err)
	}
}

// TestOwnedContent_NamespacedBoxAsksWithItsOwningNamespace: a box reached as `arch.app` is asked about
// the IMPORTED namespace's nodes and images — the root's were empty for `arch.arch` before the fix.
func TestOwnedContent_NamespacedBoxAsksWithItsOwningNamespace(t *testing.T) {
	f := newContentFixture(t)
	sub := f.g.Config
	f.g.Config = &spec.Config{Namespaces: map[string]*spec.Config{"arch": sub}}
	var got []spec.OwnedContentRequest
	var asked []string
	f.g.Content = f.recordingKind(&got, &asked)
	if _, err := f.g.compileOwnedContent("arch.app", f.img); err != nil {
		t.Fatalf("compileOwnedContent: %v", err)
	}
	if len(got) != 1 || len(got[0].Nodes) != 1 || len(got[0].Candies) != 1 {
		t.Fatalf("namespaced box request = %+v, want the namespace's node and composed candy", got)
	}
}

// TestOwnedContent_DiagnosticsAndConflictsFailTheBuild: a kind's diagnostics are fatal (an image must not
// silently lack content its nodes declare), a label two kinds claim is fatal, and declared kinds with no
// way to ask them are fatal rather than an image built without them.
func TestOwnedContent_DiagnosticsAndConflictsFailTheBuild(t *testing.T) {
	f := newContentFixture(t)
	f.g.Content = OwnedContentSource{Kinds: []string{"skill"}, Ask: func(string, spec.OwnedContentRequest) (*spec.OwnedContentReply, error) {
		return &spec.OwnedContentReply{Diagnostics: []string{`skill "s": no body`}}, nil
	}}
	if _, err := f.g.compileOwnedContent("app", f.img); err == nil || !strings.Contains(err.Error(), `skill "s": no body`) {
		t.Fatalf("diagnostics: err = %v, want the kind's diagnostic", err)
	}

	f.g.Config.PluginKinds["hook"] = map[string]json.RawMessage{"h": json.RawMessage(`{}`)}
	f.g.Content = OwnedContentSource{Kinds: []string{"skill", "hook"}, Ask: func(string, spec.OwnedContentRequest) (*spec.OwnedContentReply, error) {
		return &spec.OwnedContentReply{Labels: map[string]spec.RawBody{"ai.opencharly.x": spec.RawBody(`1`)}}, nil
	}}
	if _, err := f.g.compileOwnedContent("app", f.img); err == nil || !strings.Contains(err.Error(), "more than one content kind") {
		t.Fatalf("label conflict: err = %v, want the conflict named", err)
	}

	f.g.Content = OwnedContentSource{Kinds: []string{"skill"}}
	if _, err := f.g.compileOwnedContent("app", f.img); err == nil {
		t.Fatal("declared content kinds with no Ask produced no error")
	}
}

// TestOwnedContent_RenderInstallsTheReplyFromTheEnvelope runs the whole build path the way plugin-build
// does: render-prep asks the kind, the box crosses the envelope, and a render with NO Config stages the
// files verbatim and emits ONE COPY owned by the image user; the label is written by the JSON writer so
// non-ASCII prose survives.
func TestOwnedContent_RenderInstallsTheReplyFromTheEnvelope(t *testing.T) {
	f := newContentFixture(t)
	var got []spec.OwnedContentRequest
	var asked []string
	f.g.Content = f.recordingKind(&got, &asked)
	content, err := f.g.compileOwnedContent("app", f.img)
	if err != nil {
		t.Fatalf("compileOwnedContent: %v", err)
	}
	f.img.OwnedContent = content.files
	back := NewSpecResolvedBox(ProjectResolvedBox(f.img), nil, nil)
	if len(back.OwnedContent) != 1 || back.OwnedContent[0] != content.files[0] {
		t.Fatalf("after the envelope round trip OwnedContent = %+v, want %+v", back.OwnedContent, content.files)
	}

	render := &Generator{Dir: f.g.Dir, BuildDir: f.g.BuildDir}
	var b strings.Builder
	if err := render.EmitOwnedContent(&b, "app", back); err != nil {
		t.Fatalf("EmitOwnedContent: %v", err)
	}
	want := "COPY --chown=1000:1000 .build/_content/app/ /home/user/\n"
	if out := b.String(); strings.Count(out, "COPY ") != 1 || !strings.Contains(out, want) {
		t.Fatalf("emitted:\n%s\nwant exactly one %q", out, want)
	}
	staged := filepath.Join(f.g.BuildDir, contentStageDir, "app", ".agents/skills/s/SKILL.md")
	data, err := os.ReadFile(staged)
	src, _ := os.ReadFile(f.body)
	if err != nil || string(data) != string(src) {
		t.Fatalf("staged file = %q (%v), want the source verbatim", data, err)
	}
	if fi, err := os.Stat(staged); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("staged mode = %v (%v), want the default 0644 (the source is 0600)", fi.Mode().Perm(), err)
	}

	var lb strings.Builder
	(&Generator{}).WriteLabels(&lb, &spec.BakedLabelSet{Version: "v", Box: "app", Contributed: content.labels}, "app")
	if !strings.Contains(lb.String(), "LABEL "+spec.LabelSkill+"='") || !strings.Contains(lb.String(), "é — prose") {
		t.Fatalf("labels:\n%s\nwant the contributed skill label in JSON form with its prose intact", lb.String())
	}
}

// TestOwnedContent_NoContentEmitsNothing: a box with no owned content gets no COPY and no stage.
func TestOwnedContent_NoContentEmitsNothing(t *testing.T) {
	f := newContentFixture(t)
	var b strings.Builder
	if err := f.g.EmitOwnedContent(&b, "app", f.img); err != nil || b.Len() != 0 {
		t.Fatalf("emitted %q (%v) for a box with no owned content", b.String(), err)
	}
	if _, err := os.Stat(filepath.Join(f.g.BuildDir, contentStageDir)); !os.IsNotExist(err) {
		t.Fatalf("a stage was created for a box with no owned content (stat err %v)", err)
	}
}

// TestOwnedContentRequestFor_ProjectScope: with no box the request is the project's — every candy and
// every image — which is what `charly box validate` asks a kind to diagnose.
func TestOwnedContentRequestFor_ProjectScope(t *testing.T) {
	f := newContentFixture(t)
	req, err := OwnedContentRequestFor(f.g.Config, f.g.Candies, "skill", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Box != "" || req.Home != "" || len(req.Candies) != 2 || req.Candies[0].Name != "other" || req.Candies[1].Name != "owner" {
		t.Fatalf("project-scope request = %+v, want no box, no user, every candy sorted", req)
	}
}
