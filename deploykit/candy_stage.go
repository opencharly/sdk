package deploykit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// CandyCopySource returns the build-context path a candy's files are COPYed from — the ONE
// mapping from a candy's RESOLUTION ORIGIN to its COPY source:
//
//   - an @github ref or a candy declared in an imported namespace is STAGED, copied into
//     .build/_candy/<name>.<version>/ by the build's host-fs prep;
//   - a project-local candy is referenced by its project-relative directory (the
//     conventional candy/<name>/ at the default location).
//
// The origin is decided from WHERE the candy lives — its resolved SourceDir relative to the
// build root, plus the resolver's Remote marker — never from its name. Deriving the source
// from the name alone emitted candy/<name>/ for a candy that lives outside the project (an
// imported namespace's checkout), a directory the importing project cannot have
// (opencharly/.github#263). Relocated (P8).
func (g *Generator) CandyCopySource(candyRef string) string {
	return g.candyCopySource(g.Candies[candyRef], candyRef)
}

// candyCopySource is the origin→COPY-source mapping for a candy already in hand. A caller
// that holds the layer (a builder stage context) calls this instead of re-deriving the candy's
// map key, so there is ONE derivation. name is the fallback directory name, used only for a
// candy carrying no SourceDir (a project-local candy at the conventional candy/<name>/); the
// origin itself is read from the layer.
func (g *Generator) candyCopySource(layer CandyModel, name string) string {
	if name == "" {
		name = layer.GetName()
	}
	sourceDir := g.candySourceDir(layer, name)
	staged, rel := g.candyOrigin(layer, sourceDir)
	if staged {
		return ".build/_candy/" + CandyStageDirName(layer)
	}
	// The conventional project-local candy/<name>/ keeps the legacy path form.
	if sourceDir == filepath.Join(g.Dir, kit.DefaultCandyDir, name) {
		return kit.DefaultCandyDir + "/" + name
	}
	// A LOCAL candy anchored elsewhere IN the project: an inline candy takes its declaring
	// charly.yml's directory as SourceDir, and a `discover:` entry can point at a path other
	// than candy/. candyOrigin already proved this directory is inside the build root, so rel
	// IS the COPY source — there is no second outside-root guard to duplicate its verdict.
	// (Not a `directory:` redirect — that field is a hard load error.)
	return rel
}

// candySourceDir returns the directory a resolved candy's files actually live in: its
// SourceDir, or the conventional project-local candy/<name>/ when the model carries none.
func (g *Generator) candySourceDir(layer CandyModel, name string) string {
	if sourceDir := layer.GetSourceDir(); sourceDir != "" {
		return sourceDir
	}
	return filepath.Join(g.Dir, kit.DefaultCandyDir, name)
}

// candyOrigin is the ONE origin predicate behind the COPY-source mapping: staged reports
// whether a candy's files must be copied into the build context
// (.build/_candy/<name>.<version>/) rather than referenced by a project-relative COPY
// source, and when staged is false, rel is that project-relative directory.
//
// staged is true for a candy the resolver marked Remote (an @github ref) and for ANY candy
// whose resolved directory lies OUTSIDE the build root — an imported namespace's candy is
// fetched into the namespace's own checkout, so no project-relative path can name it. The
// outside-root test is exact (`..` or a `../` prefix, never the loose `..` string prefix a
// sibling directory named `..<x>` would match); callers map this verdict to a path and never
// re-implement it.
func (g *Generator) candyOrigin(layer CandyModel, sourceDir string) (staged bool, rel string) {
	if layer.GetRemote() {
		return true, ""
	}
	rel, err := filepath.Rel(g.Dir, sourceDir)
	if err != nil {
		return true, ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true, ""
	}
	return false, rel
}

// EmitScratchStages emits a `FROM scratch AS <candy>` + `COPY <src>/ /` per candy,
// staging each candy's files in its own scratch layer. A `copy:` resolves inside that
// stage, never at the build root: content that lives at a repo root (a skill body) is a
// kind's owned content and is installed by EmitOwnedContent instead. Relocated (P8).
func (g *Generator) EmitScratchStages(b *strings.Builder, candyOrder []string) {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		stageName := layer.GetName() // use short name for stage alias
		fmt.Fprintf(b, "FROM scratch AS %s\n", stageName)
		fmt.Fprintf(b, "COPY %s/ /\n", g.CandyCopySource(candyName))
		b.WriteString("\n")
	}
}

// EmitExtractStages emits a `FROM <source> AS <candy>-extract-<i>` stage per
// candy extract directive (the source images files are extracted from). (P8).
func (g *Generator) EmitExtractStages(b *strings.Builder, candyOrder []string) {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if !layer.HasExtract() {
			continue
		}
		for i, ext := range layer.Extract() {
			stageName := fmt.Sprintf("%s-extract-%d", candyName, i)
			fmt.Fprintf(b, "FROM %s AS %s\n\n", ext.Source, stageName)
		}
	}
}

// EmitExtractedFiles emits the COPY --from=<extract-stage> directives that pull
// extracted files into the image (chowned to the image user). (P8).
func (g *Generator) EmitExtractedFiles(b *strings.Builder, img *buildkit.ResolvedBox, candyOrder []string) {
	hasExtract := false
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if !layer.HasExtract() {
			continue
		}
		if !hasExtract {
			b.WriteString("# Copy extracted files from Docker images\n")
			b.WriteString("USER root\n")
			hasExtract = true
		}
		for i, ext := range layer.Extract() {
			stageName := fmt.Sprintf("%s-extract-%d", candyName, i)
			fmt.Fprintf(b, "COPY --from=%s --chown=%d:%d %s %s\n",
				stageName, img.UID, img.GID, ext.Path, ext.Dest)
		}
	}
	if hasExtract {
		b.WriteString("\n")
	}
}

// WriteDataStaging emits COPY --from=<candy-scratch-stage> directives that stage a
// candy's data/ tree under /data/<volume>/[dest] for deploy-time provisioning into
// bind-backed volumes. Uses the short stage alias (GetName) — a remote candy's map
// key is not a valid build-stage ref. Relocated from charly (P8); byte-identical.
func (g *Generator) WriteDataStaging(b *strings.Builder, candyOrder []string, img *buildkit.ResolvedBox) {
	hasData := false
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if !layer.HasData() {
			continue
		}
		if !hasData {
			b.WriteString("# Data staging (for deploy-time provisioning into bind-backed volumes)\n")
			hasData = true
		}
		for _, d := range layer.Data() {
			srcPath := "/" + d.Src
			if !strings.HasSuffix(srcPath, "/") {
				srcPath += "/"
			}
			dstPath := "/data/" + d.Volume + "/"
			if d.Dest != "" {
				dstPath += d.Dest
				if !strings.HasSuffix(dstPath, "/") {
					dstPath += "/"
				}
			}
			fmt.Fprintf(b, "COPY --from=%s --chown=%d:%d %s %s\n",
				layer.GetName(), img.UID, img.GID, srcPath, dstPath)
		}
	}
	if hasData {
		b.WriteString("\n")
	}
}
