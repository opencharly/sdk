package deploykit

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// The unified builder mechanism (opencharly/sdk#608): every build-time manager — aur, npm, pixi,
// cargo, mise — goes through ONE trigger (NeedsBuilder), ONE fail-loud pre-pass (requireBuilders),
// ONE install contract and ONE apply compile. Each test below runs its rule against every builder.

// allBuilders is the table every rule runs over: each builder word with the detect file that
// selects it (aur has none — it is selected only by a manager declaration).
var allBuilders = []struct {
	word       string
	detectFile string
	inline     bool
}{
	{"aur", "", false},
	{"npm", "package.json", false},
	{"pixi", "pixi.toml", false},
	{"cargo", "Cargo.toml", true},
	{"mise", "mise.toml", false},
}

// vocabulary is a builder vocabulary defining every builder. It is authored data only (detect
// files, inline): what each builder installs is the plugin's (kit), never the authored node.
func vocabulary() *buildkit.BuilderConfig {
	cfg := &buildkit.BuilderConfig{Builder: map[string]*buildkit.BuilderDef{}}
	for _, b := range allBuilders {
		def := &buildkit.BuilderDef{Inline: b.inline}
		if b.detectFile != "" {
			def.DetectFiles = []string{b.detectFile}
		}
		cfg.Builder[b.word] = def
	}
	return cfg
}

// preresolved is the deploy pre-pass result for one candy×builder as the builder plugin reports it
// (its collect leg's package output — kit.BuilderPackageOutput), which plugin-fleet stashes on the
// host context for the pure compile.
func preresolved(candy, word string) HostContext {
	return HostContext{BuilderContext: map[string]BuilderPreresolved{
		BuilderCtxKey(candy, word): {Package: kit.BuilderPackageOutput(word, candy)},
	}}
}

// image returns a resolved image of a distro: arch serves pac with a local_pkg contract, fedora
// serves rpm.
func image(distro string) *buildkit.ResolvedBox {
	img := &buildkit.ResolvedBox{BuilderConfig: vocabulary()}
	img.Name = distro + "-box"
	img.Home = "/home/user"
	img.UID, img.GID = 1000, 1000
	img.Distro = []string{distro}
	format := map[string]string{"arch": "pac", "fedora": "rpm"}[distro]
	img.Pkg = format
	lp := &spec.LocalPkg{InstallTemplate: "pkg-install {{.StageDir}}/{{.Glob}}\n", Probe: "true"}
	img.DistroDef = &spec.ResolvedDistro{Format: map[string]*spec.Format{format: {LocalPkg: lp}}}
	return img
}

// declaring returns a candy that declares packages for `manager` under the distro tag `tag`.
func declaring(t *testing.T, name, tag, manager string) CandyModel {
	t.Helper()
	return NewSpecCandyModel(spec.CandyModel{Name: name, TagSections: map[string]spec.TagPkgConfig{
		tag: {Managers: map[string]spec.PackageSection{manager: {Packages: []string{name + "-pkg"}}}},
	}}, spec.CandyView{Name: name})
}

// carrying returns a candy whose source dir carries `file`.
func carrying(t *testing.T, name, file string) CandyModel {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, file), []byte("[package]\nname = \""+name+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewSpecCandyModel(spec.CandyModel{Name: name, SourceDir: dir}, spec.CandyView{Name: name})
}

// A builder is needed by its detect file OR by a manager declaration the image's tag chain
// selects — the same rule for every builder, and a declaration under another distro's tag does
// not reach the image (the scope OS packages already have; sdk#610 was the regression of
// ignoring it).
func TestNeedsBuilderIsOneRuleForEveryBuilder(t *testing.T) {
	arch, fedora := image("arch"), image("fedora")
	for _, b := range allBuilders {
		def := arch.BuilderConfig.Builder[b.word]
		decl := declaring(t, "c-"+b.word, "arch", b.word)
		if !NeedsBuilder(decl, arch, b.word, def) {
			t.Errorf("%s: an arch-tagged declaration does not trigger the builder on an arch image", b.word)
		}
		if NeedsBuilder(decl, fedora, b.word, def) {
			t.Errorf("%s: an arch-tagged declaration triggers the builder on a fedora image", b.word)
		}
		if b.detectFile != "" && !NeedsBuilder(carrying(t, "f-"+b.word, b.detectFile), fedora, b.word, def) {
			t.Errorf("%s: its detect file %s does not trigger it", b.word, b.detectFile)
		}
		plain := NewSpecCandyModel(spec.CandyModel{Name: "plain"}, spec.CandyView{Name: "plain"})
		if NeedsBuilder(plain, arch, b.word, def) {
			t.Errorf("%s: a candy with neither a detect file nor a declaration triggers it", b.word)
		}
	}
}

// Every way a needed builder can be unable to run is a hard error naming the candy and the
// builder — for every builder: no definition, no plugin, no route to a builder box.
func TestRequireBuildersRefusesEveryUnrunnableBuilder(t *testing.T) {
	for _, b := range allBuilders {
		candy := declaring(t, "c-"+b.word, "arch", b.word)
		cases := map[string]func(g *Generator, img *buildkit.ResolvedBox){
			"no definition": func(_ *Generator, img *buildkit.ResolvedBox) { delete(img.BuilderConfig.Builder, b.word) },
			"no vocabulary": func(_ *Generator, img *buildkit.ResolvedBox) { img.BuilderConfig = nil },
			"no plugin":     func(g *Generator, _ *buildkit.ResolvedBox) { delete(g.ExternalizedBuilders, b.word) },
			"no route":      func(g *Generator, _ *buildkit.ResolvedBox) { g.Boxes["arch-box"].Builder = nil },
		}
		for name, mutate := range cases {
			if b.inline && name == "no route" {
				continue // an inline builder runs inside the candy's own stage, so it has no route
			}
			img := image("arch")
			g := generatorFor(img, candy)
			mutate(g, img)
			var sb strings.Builder
			err := g.EmitBuilderStages(&sb, "arch-box", img, []string{candy.GetName()})
			if err == nil {
				t.Errorf("%s / %s: a needed builder that cannot run did not fail the build", b.word, name)
				continue
			}
			if !strings.Contains(err.Error(), candy.GetName()) || !strings.Contains(err.Error(), b.word) {
				t.Errorf("%s / %s: error %q does not name the candy and the builder", b.word, name, err)
			}
		}
	}
}

// generatorFor is a generator whose every builder is externalized and routed to a builder box,
// with a resolve seam that renders through the real kit (so a stage really is produced).
func generatorFor(img *buildkit.ResolvedBox, candies ...CandyModel) *Generator {
	g := &Generator{
		Candies:              map[string]CandyModel{},
		Boxes:                map[string]*buildkit.ResolvedBox{"arch-box": img, "fedora-box": img, "builder-box": {}},
		ExternalizedBuilders: map[string]bool{},
	}
	g.Boxes["builder-box"].FullTag = "localhost/builder:1"
	img.Builder = spec.BuilderMap{}
	for _, b := range allBuilders {
		g.ExternalizedBuilders[b.word] = true
		img.Builder[b.word] = "builder-box"
	}
	for _, c := range candies {
		g.Candies[c.GetName()] = c
	}
	// The plugin's reply: a stage, its payload copy, and — the plugin's own declaration — the
	// package output (aur), exactly what kit.BuilderResolve returns.
	g.ResolveDetectionBuilderStage = func(word string, in spec.BuilderResolveInput, _ *spec.ResolvedBox) (spec.BuilderResolveReply, error) {
		return spec.BuilderResolveReply{
			Stage:         "FROM " + in.BuilderRef + " AS " + in.StageName,
			CopyArtifacts: []string{"COPY --from=" + in.StageName + " payload /"},
			Package:       kit.BuilderPackageOutput(word, in.Candy),
		}, nil
	}
	return g
}

// A builder's PACKAGE output is installed through the image distro's local_pkg contract when the
// distro serves its format — from a per-candy directory, so two candies' packages never mix — and
// its payload is NOT also copied. On a distro that does not serve the format, the payload is what
// installs, exactly like every other builder's.
func TestBuilderPackageInstallIsReadFromData(t *testing.T) {
	for _, tc := range []struct {
		distro          string
		wantLocalPkg    bool
		wantPayloadCopy bool
	}{{"arch", true, false}, {"fedora", false, true}} {
		img := image(tc.distro)
		a, c := declaring(t, "alpha", tc.distro, "aur"), declaring(t, "beta", tc.distro, "aur")
		g := generatorFor(img, a, c)
		var stages, arts strings.Builder
		if err := g.EmitBuilderStages(&stages, tc.distro+"-box", img, []string{"alpha", "beta"}); err != nil {
			t.Fatalf("%s: stages: %v", tc.distro, err)
		}
		if err := g.EmitBuilderArtifacts(&arts, img, []string{"alpha", "beta"}); err != nil {
			t.Fatalf("%s: artifacts: %v", tc.distro, err)
		}
		if got := strings.Contains(arts.String(), "payload /"); got != tc.wantPayloadCopy {
			t.Errorf("%s: payload copied = %v, want %v:\n%s", tc.distro, got, tc.wantPayloadCopy, arts.String())
		}
		var steps strings.Builder
		if err := g.emitBuilderPackageInstalls(&steps, a, img); err != nil {
			t.Fatalf("%s: package install: %v", tc.distro, err)
		}
		got := steps.String()
		if tc.wantLocalPkg {
			if !strings.Contains(got, "pkg-install /tmp/aur-pkgs/alpha/*.pkg.tar.zst") || strings.Contains(got, "/tmp/aur-pkgs/beta") {
				t.Errorf("%s: the candy's packages are not installed from its own directory through local_pkg:\n%s", tc.distro, got)
			}
		} else if got != "" {
			t.Errorf("%s: a distro without the pac local_pkg contract got a package install:\n%s", tc.distro, got)
		}
	}
}

// A needed builder with no resolved stage is an error, never a silently absent payload.
func TestEmitBuilderArtifactsRefusesAMissingStage(t *testing.T) {
	img := image("fedora")
	candy := carrying(t, "web", "package.json")
	g := generatorFor(img, candy)
	g.detectionBuilderReplies = nil
	var b strings.Builder
	err := g.EmitBuilderArtifacts(&b, img, []string{"web"})
	if err == nil || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("a needed builder with no resolved stage must fail naming candy and builder, got %v", err)
	}
}

// Apply mode compiles builders with the SAME trigger, and a builder whose plugin declared a package
// output (the pre-pass) carries its per-candy package directory, its system scope and the venue's
// local_pkg contract; a venue that cannot install it is a hard error, never a step that installs
// nothing.
func TestCompileBuilderStepsUsesTheSameRule(t *testing.T) {
	for _, b := range allBuilders {
		arch := image("arch")
		steps, err := compileBuilderSteps(declaring(t, "c-"+b.word, "arch", b.word), arch, preresolved("c-"+b.word, b.word))
		if err != nil || len(steps) != 1 || steps[0].(*BuilderStep).Builder != b.word {
			t.Errorf("%s: arch declaration compiled to %+v, %v; want one %s step", b.word, steps, err, b.word)
			continue
		}
		fedoraSteps, err := compileBuilderSteps(declaring(t, "c-"+b.word, "arch", b.word), image("fedora"), HostContext{})
		if err != nil || len(fedoraSteps) != 0 {
			t.Errorf("%s: an arch-tagged declaration compiled on fedora to %+v, %v; want nothing", b.word, fedoraSteps, err)
		}
	}
	arch := image("arch")
	steps, _ := compileBuilderSteps(declaring(t, "kde-shell", "arch", "aur"), arch, preresolved("kde-shell", "aur"))
	st := steps[0].(*BuilderStep)
	if len(st.Artifacts) != 1 || st.Artifacts[0].ContainerPath != "/tmp/aur-pkgs/kde-shell/" || st.LocalPkg == nil || st.Scope() != spec.ScopeSystem {
		t.Errorf("aur apply step = artifacts %+v, local_pkg %v, scope %v; want the candy's own package dir, the venue contract, system scope", st.Artifacts, st.LocalPkg, st.Scope())
	}
	// On a distro without the pac local_pkg contract the step carries no LocalPkg: an image build
	// copies the payload, and a HOST venue refuses the step rather than installing nothing.
	fedoraSteps, err := compileBuilderSteps(declaring(t, "kde-shell", "fedora", "aur"), image("fedora"), preresolved("kde-shell", "aur"))
	if err != nil || len(fedoraSteps) != 1 || fedoraSteps[0].(*BuilderStep).LocalPkg != nil {
		t.Fatalf("aur on fedora compiled to %+v, %v; want one step with no LocalPkg", fedoraSteps, err)
	}
	if err := RunVenueBuilderStep(context.Background(), nil, "/home/u", nil, nil, fedoraSteps[0].(*BuilderStep), EmitOpts{}); err == nil || !strings.Contains(err.Error(), "local_pkg") {
		t.Errorf("a host venue applied aur packages it cannot install: %v", err)
	}
}

// Plugin connection asks for exactly the builders the render will need — the same rule.
func TestDetectExternalizedBuildersMatchesNeedsBuilder(t *testing.T) {
	img := image("fedora")
	candies := map[string]CandyModel{
		"web":   carrying(t, "web", "package.json"),
		"shell": declaring(t, "shell", "arch", "aur"),
	}
	got := DetectExternalizedBuilders([]string{"web", "shell"}, candies, img)
	if !slices.Equal(got, []string{"npm"}) {
		t.Errorf("needed builders = %v, want [npm] (the arch-tagged aur declaration does not reach fedora)", got)
	}
}

// The external_builder road obeys the same rule: a candy whose external builder resolved a stage
// that copies nothing — or resolved no stage at all — fails, never ships without its build.
func TestEmitExternalBuilderArtifactsRefusesAnEmptyPayload(t *testing.T) {
	candy := NewSpecCandyModel(spec.CandyModel{Name: "ext", ExternalBuilder: "zig"}, spec.CandyView{Name: "ext"})
	g := &Generator{Candies: map[string]CandyModel{"ext": candy}}
	var b strings.Builder
	if err := g.EmitExternalBuilderArtifacts(&b, []string{"ext"}); err == nil || !strings.Contains(err.Error(), "ext") {
		t.Errorf("an unresolved external builder did not fail naming the candy: %v", err)
	}
	g.externalBuilderReplies = map[string]spec.BuilderResolveReply{"ext": {Stage: "FROM x AS y"}}
	if err := g.EmitExternalBuilderArtifacts(&b, []string{"ext"}); err == nil || !strings.Contains(err.Error(), "no copy artifacts") {
		t.Errorf("an external builder with an empty payload did not fail: %v", err)
	}
	g.externalBuilderReplies["ext"] = spec.BuilderResolveReply{Stage: "FROM x AS y", CopyArtifacts: []string{"COPY --from=y /out /usr/local"}}
	if err := g.EmitExternalBuilderArtifacts(&b, []string{"ext"}); err != nil || !strings.Contains(b.String(), "COPY --from=y /out /usr/local") {
		t.Errorf("a resolved external builder did not copy its payload: %v\n%s", err, b.String())
	}
}

// The collect input carries the candy's detected manifest, so each builder derives its teardown
// from what the candy actually declares.
func TestBuilderCollectInputCarriesTheDetectedManifest(t *testing.T) {
	img := image("arch")
	candy := carrying(t, "rg", "Cargo.toml")
	in, err := BuilderCollectInputFor(candy, img, "cargo", img.BuilderConfig.Builder["cargo"])
	if err != nil {
		t.Fatal(err)
	}
	if in.Manifest != "Cargo.toml" || !strings.Contains(in.ManifestContent, `name = "rg"`) || in.Home != "/home/user" {
		t.Errorf("collect input = %+v, want the candy's Cargo.toml and the image home", in)
	}
	decl := declaring(t, "shell", "arch", "aur")
	in, err = BuilderCollectInputFor(decl, img, "aur", img.BuilderConfig.Builder["aur"])
	if err != nil || !slices.Equal(in.Packages, []string{"shell-pkg"}) {
		t.Errorf("aur collect input = %+v, %v; want the image's declared packages", in, err)
	}
}

// The builder images a build needs are exactly the ones its stages name: one per routed builder a
// candy needs on the image, deduplicated — what the pod overlay ensures before rendering them.
func TestNeededBuilderImagesMatchTheStages(t *testing.T) {
	img := image("fedora")
	g := generatorFor(img, carrying(t, "web", "package.json"), carrying(t, "py", "pixi.toml"), declaring(t, "shell", "arch", "aur"))
	got := g.NeededBuilderImages("fedora-box", img, []string{"web", "py", "shell"})
	if !slices.Equal(got, []string{"localhost/builder:1"}) {
		t.Errorf("needed builder images = %v, want the one builder box npm and pixi route to (the arch-tagged aur declaration does not reach fedora)", got)
	}
	if got := g.NeededBuilderImages("fedora-box", img, []string{"shell"}); len(got) != 0 {
		t.Errorf("a candy that needs no builder on this image asked for %v", got)
	}
}
