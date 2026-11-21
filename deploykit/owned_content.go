package deploykit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// owned_content.go — the build kit's half of a KIND's owned content (DESIGN §1.1 M1, Content -> IR;
// ops.OpContent; opencharly/sdk#598).
//
// A node of a plugin kind can be owned by a candy (a `skill:` names its owner), and what the node
// contributes to an image is that candy's content. The KIND that defines the node is the one place
// that knows what that is (D-PLUG-1: a plugin owns its words end to end), so this kit never reads a
// kind's nodes. It only:
//
//   - builds the request one box offers (OwnedContentRequestFor) — the image user, the composed
//     candies with the roots of the repositories that author them, the names that are images, and the
//     kind's raw nodes from the box's OWNING scope (an imported namespace's own document for
//     `arch.arch`);
//   - asks every kind in the envelope's content_kinds through OwnedContentSource (render-prep, where
//     the live *Config is), keeping the files as the box's render cache (img.OwnedContent) and merging
//     the labels into the baked labels;
//   - stages those files and installs them with ONE `COPY --chown` onto the image user's home
//     (EmitOwnedContent), reading nothing but the box — the plugin-build render runs with Config nil.
//
// The first content kind is `kind:skill` (opencharly/plugin-harness-kind): a skill a composed candy
// owns is installed at ~/.agents/skills/<name>/ and labelled ai.opencharly.skill. That used to be a
// carrier candy's `copy: .agents/skills` step, which could never reach its own repo root
// (opencharly/sdk#444) and failed every remote import (distro-arch#73).
//
// THE STAGE. Each file is copied out of its host source (a remote candy's repo@version cache sits
// OUTSIDE the build context) into `<buildDir>/_content/<box>/`, laid out as the image user's home
// holds it. The COPY carries the top directories it creates, so they are owned by the image user
// rather than by root. The stage is built in a temp dir and swapped in atomically
// (kit.InstallDirAtomic), the `_candy` stage's discipline, so two concurrent builds of one box never
// COPY a half-written tree; cleanStaleBuildDirs never prunes it (it skips `_`-prefixed dirs).

// contentStageDir is the build-dir subdirectory holding the per-box content stages.
const contentStageDir = "_content"

// OwnedContentSource is how render-prep reaches the content kinds: the kind words that declare
// `contents` (the envelope's content_kinds — a registry fact the host projects, never a word this
// kit names) and the call that asks one of them (InvokeProvider kind:<word> ops.OpContent, wired by
// the plugin that drives the build).
type OwnedContentSource struct {
	Kinds []string
	Ask   func(kind string, req spec.OwnedContentRequest) (*spec.OwnedContentReply, error)
}

// ownedContent is what every content kind contributed to one box: the files to install and the
// labels to bake.
type ownedContent struct {
	files  []spec.ContentFile
	labels map[string]spec.RawBody
}

// scopeConfig returns the config that OWNS boxName — an imported namespace's own projection for a
// namespace-qualified box — which is where the box's chain is walked (BoxCandyChain) and so where its
// kind nodes are authored. Reading the root's nodes for an imported box gave it none: measured,
// `arch.arch` built from pod-kde-cstream composed `arch-skills` but carried no skill.
func scopeConfig(cfg *spec.Config, boxName string) *spec.Config {
	if owner, _, _, ok := BoxOwner(cfg, boxName); ok && owner != nil {
		return owner
	}
	return cfg
}

// OwnedContentRequestFor builds the request one box offers a content kind. With boxName == "" it is
// the PROJECT-scope request `charly box validate` sends: every candy of the project and every image,
// so the kind diagnoses its nodes whether or not any box composes them.
func OwnedContentRequestFor(cfg *spec.Config, layers map[string]CandyModel, kind, boxName string, img *ResolvedBox) (spec.OwnedContentRequest, error) {
	scope := cfg
	var names []string
	if boxName != "" {
		scope = scopeConfig(cfg, boxName)
		chain, err := BoxCandyChain(cfg, layers, boxName)
		if err != nil {
			return spec.OwnedContentRequest{}, fmt.Errorf("box %q: resolving the composed candies: %w", boxName, err)
		}
		names = chain
	} else {
		for name := range layers {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	req := spec.OwnedContentRequest{Box: boxName, Nodes: scope.PluginKinds[kind]}
	if img != nil {
		req.Home, req.UID, req.GID = img.Home, img.UID, img.GID
	}
	for _, name := range names {
		c := spec.OwnedContentCandy{Name: name}
		if root, ok := candyRepoRoot(layers[name]); ok {
			abs, err := filepath.Abs(root)
			if err != nil {
				return spec.OwnedContentRequest{}, err
			}
			c.RepoRoot = abs
		}
		req.Candies = append(req.Candies, c)
	}
	for name := range scope.Box {
		req.Images = append(req.Images, name)
	}
	sort.Strings(req.Images)
	return req, nil
}

// compileOwnedContent asks every content kind what its nodes contribute to boxName. A kind with no
// node in the box's scope is not asked. A reply's diagnostics fail the build — a skill whose body is
// missing must not become an image that silently lacks it — and so does a label two kinds both claim.
func (g *Generator) compileOwnedContent(boxName string, img *ResolvedBox) (ownedContent, error) {
	var out ownedContent
	if len(g.Content.Kinds) == 0 {
		return out, nil
	}
	if g.Content.Ask == nil {
		return out, fmt.Errorf("box %q: %d content kind(s) are declared (%s) but the render has no way to ask them",
			boxName, len(g.Content.Kinds), strings.Join(g.Content.Kinds, ", "))
	}
	kinds := append([]string(nil), g.Content.Kinds...)
	sort.Strings(kinds)
	scope := scopeConfig(g.Config, boxName)
	var problems []string
	for _, kind := range kinds {
		if len(scope.PluginKinds[kind]) == 0 {
			continue
		}
		req, err := OwnedContentRequestFor(g.Config, g.Candies, kind, boxName, img)
		if err != nil {
			return out, err
		}
		reply, err := g.Content.Ask(kind, req)
		if err != nil {
			return out, fmt.Errorf("box %q: kind %q content: %w", boxName, kind, err)
		}
		if reply == nil {
			continue
		}
		for _, d := range reply.Diagnostics {
			problems = append(problems, fmt.Sprintf("kind %q: %s", kind, d))
		}
		out.files = append(out.files, reply.Files...)
		for key, value := range reply.Labels {
			if out.labels == nil {
				out.labels = map[string]spec.RawBody{}
			}
			if _, dup := out.labels[key]; dup {
				return out, fmt.Errorf("box %q: label %q is contributed by more than one content kind", boxName, key)
			}
			out.labels[key] = value
		}
	}
	if len(problems) > 0 {
		return out, fmt.Errorf("box %q: %s", boxName, strings.Join(problems, "; "))
	}
	return out, nil
}

// EmitOwnedContent stages the box's owned content (img.OwnedContent, filled by render-prep) and emits
// the ONE `COPY --chown` that installs it under the image user's home. It emits and stages nothing
// when the box has no owned content.
func (g *Generator) EmitOwnedContent(b *strings.Builder, boxName string, img *ResolvedBox) error {
	if len(img.OwnedContent) == 0 {
		return nil
	}
	if img.Home == "" {
		return fmt.Errorf("box %q has %d owned-content file(s) but resolved no home directory to install them under", boxName, len(img.OwnedContent))
	}
	root := filepath.Join(g.BuildDir, contentStageDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, "."+filepath.Base(boxName)+".tmp.*")
	if err != nil {
		return err
	}
	if err := stageContentFiles(img.OwnedContent, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("box %q: %w", boxName, err)
	}
	stage := filepath.Join(root, boxName)
	if err := kit.InstallDirAtomic(tmp, stage); err != nil {
		return fmt.Errorf("installing the owned-content stage for box %q: %w", boxName, err)
	}
	rel, err := contextRelative(g.Dir, stage)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "# The owned content of this image's candies (%d file(s): skill bodies and the like), installed for the image user\n", len(img.OwnedContent))
	fmt.Fprintf(b, "COPY --chown=%d:%d %s/ %s/\n\n", img.UID, img.GID, filepath.ToSlash(rel), img.Home)
	return nil
}

// stageContentFiles copies each file verbatim to its home-relative path under the stage and gives it
// its mode (0644 when the reply names none). The stage itself gets the ordinary directory mode, since
// MkdirTemp creates 0700. A path's confinement under the home is a SCHEMA rule (#ContentFile.path in
// spec's CUE), so it is not restated here (D-SCH-1).
func stageContentFiles(files []spec.ContentFile, stage string) error {
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	var errs []error
	for _, f := range files {
		dst := filepath.Join(stage, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if out, err := exec.Command("cp", "--preserve=timestamps", f.Source, dst).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("staging %s: %s: %w", f.Source, strings.TrimSpace(string(out)), err))
			continue
		}
		mode := os.FileMode(0o644)
		if f.Mode != 0 {
			mode = os.FileMode(f.Mode)
		}
		if err := os.Chmod(dst, mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// contextRelative names `path` relative to the build context `dir`, the form a COPY source takes.
// A stage outside the context could never be COPYed, so that is an error rather than a `../` path.
func contextRelative(dir, path string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("the owned-content stage %s is outside the build context %s", absPath, absDir)
	}
	return rel, nil
}
