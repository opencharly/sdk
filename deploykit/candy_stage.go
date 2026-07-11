package deploykit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// CandyMapKey returns the candy's map key: the full @github ref for a remote candy
// (RepoPath/SubPathPrefix+Name), else the bare Name. Relocated from charly (P8).
func CandyMapKey(layer CandyModel) string {
	if layer.GetRemote() {
		return layer.GetRepoPath() + "/" + layer.GetSubPathPrefix() + layer.GetName()
	}
	return layer.GetName()
}

// CandyStageDirName returns the content-addressed staging dir name for a remote
// candy's copied tree (name.version). Relocated from charly (P8); byte-identical.
func CandyStageDirName(layer CandyModel) string {
	if layer.GetVersion() == "" {
		return layer.GetName() // defensive; remote candies are mandatorily versioned
	}
	return layer.GetName() + "." + layer.GetVersion()
}

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
	// `directory:` override — resolve SourceDir relative to the build root.
	rel, err := filepath.Rel(g.Dir, layer.GetSourceDir())
	if err != nil || strings.HasPrefix(rel, "..") {
		return kit.DefaultCandyDir + "/" + candyRef
	}
	return rel
}

// EmitScratchStages emits a `FROM scratch AS <candy>` + `COPY <src>/ /` per candy,
// staging each candy's files in its own scratch layer. Relocated (P8).
func (g *Generator) EmitScratchStages(b *strings.Builder, candyOrder []string) {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		stageName := layer.GetName() // use short name for stage alias
		fmt.Fprintf(b, "FROM scratch AS %s\n", stageName)
		fmt.Fprintf(b, "COPY %s/ /\n\n", g.CandyCopySource(candyName))
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
