package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/vmshared"
)

// CandyCopySource returns the build-context path a candy's files are COPYed from:
// the staged _candy dir for a remote candy, the default candy/<name>/ for a local
// candy at the default location, else the relative SourceDir. Relocated (P8).
func (g *Generator) CandyCopySource(candyRef string) string {
	layer := g.Candies[candyRef]
	if layer.GetRemote() {
		return ".build/_candy/" + CandyStageDirName(layer)
	}
	// If SourceDir matches the default candy/<candyRef>/ location, preserve the
	// legacy path format (cheap, avoids filepath.Rel calls on the hot path).
	defaultDir := filepath.Join(g.Dir, kit.DefaultCandyDir, candyRef)
	if layer.GetSourceDir() == "" || layer.GetSourceDir() == defaultDir {
		return kit.DefaultCandyDir + "/" + candyRef
	}
	// A LOCAL candy whose SourceDir is not the default candy/<ref>/ — remote candies returned at
	// the top of this function, so they never reach here. The case this serves is a candy
	// anchored somewhere else in the project: an inline candy takes its declaring charly.yml's
	// directory as SourceDir, and a `discover:` entry can point at a path other than candy/.
	// Resolve it relative to the build root. (Not a `directory:` redirect — that field was
	// removed and is now a hard load error, charly/layers.go.)
	rel, err := filepath.Rel(g.Dir, layer.GetSourceDir())
	if err != nil || strings.HasPrefix(rel, "..") {
		return kit.DefaultCandyDir + "/" + candyRef
	}
	return rel
}

// EmitScratchStages emits a `FROM scratch AS <candy>` + `COPY <src>/ /` per candy,
// staging each candy's files in its own scratch layer, plus the PROJECT-ROOT copy
// sources the candy's plan names (rootCopySources). Relocated (P8).
func (g *Generator) EmitScratchStages(b *strings.Builder, candyOrder []string) {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		stageName := layer.GetName() // use short name for stage alias
		fmt.Fprintf(b, "FROM scratch AS %s\n", stageName)
		fmt.Fprintf(b, "COPY %s/ /\n", g.CandyCopySource(candyName))
		for _, src := range g.rootCopySources(candyName, layer) {
			fmt.Fprintf(b, "COPY %s/ %s/\n", src, src)
		}
		b.WriteString("\n")
	}
}

// rootCopySources returns the plan `copy:` sources a candy resolves at the BUILD ROOT
// rather than inside its own staged directory, as build-context-relative paths.
//
// WHY THIS EXISTS (opencharly/sdk#444). A `copy:` step's source resolves inside the
// candy's stage, which for a local candy is its own directory — `candy/<name>/`. A
// CARRIER candy (one that owns `skill:` nodes and installs their prose with
// `copy: .agents/skills`) names the Agent-Skills tree, which lives at the REPO ROOT
// (opencharly/opencharly#557), never in the candy directory. Without this the stage
// has no `/.agents/skills`, so the final stage's
//
//	COPY --from=<candy> .agents/skills <dest>
//
// fails the whole build with `failed to calculate checksum … "/.agents/skills": not
// found` — and it takes every downstream image with it, because the carrier is
// composed into a base image (`ubuntu` carries `ubuntu-skills`).
//
// The base is MEASURED, not declared by a new field: a source that exists at the build
// root and NOT inside the candy's own directory is root-relative. A candy-relative
// source — the overwhelming majority — keeps exactly today's behaviour, so no existing
// candy changes meaning.
//
// REMOTE candies are skipped: their files live in the materialized `_candy/<name>.<ver>`
// dir, whose own repo root is not present at generation time. Staging the BUILD root's
// tree into a remote candy's stage would substitute the wrong repository.
func (g *Generator) rootCopySources(candyName string, layer CandyModel) []string {
	if layer.GetRemote() {
		return nil
	}
	candyDir := layer.GetSourceDir()
	if candyDir == "" {
		candyDir = filepath.Join(g.Dir, kit.DefaultCandyDir, candyName)
	}
	return rootCopySources(layer.RunOps(), g.Dir, candyDir)
}

// rootCopySources is the pure core of (*Generator).rootCopySources: ops → the sources
// that exist under projectDir but not under candyDir. Order follows the plan, and each
// source is emitted once. Absolute sources and ones escaping the project are ignored —
// neither can be a build-context path.
func rootCopySources(ops []vmshared.Op, projectDir, candyDir string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, op := range ops {
		src := op.Copy
		if src == "" || seen[src] || filepath.IsAbs(src) {
			continue
		}
		seen[src] = true
		if strings.HasPrefix(filepath.Clean(src), "..") {
			continue
		}
		if pathExists(filepath.Join(candyDir, src)) {
			continue // candy-relative: already inside the staged directory
		}
		if !pathExists(filepath.Join(projectDir, src)) {
			continue // neither here nor at the root: nothing to stage (fails loudly at build)
		}
		out = append(out, filepath.ToSlash(src))
	}
	return out
}

// pathExists reports whether p is an existing file or directory.
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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
