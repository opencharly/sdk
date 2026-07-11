package deploykit

import (
	"slices"

	"github.com/opencharly/sdk/buildkit"
)

// ResolveBaseImage returns the base image ref for img: the external OCI ref when
// external, else the parent image's full CalVer tag. Relocated from charly (P8).
func (g *Generator) ResolveBaseImage(img *buildkit.ResolvedBox) string {
	if img.IsExternalBase {
		return img.Base
	}
	parentImg := g.Boxes[img.Base]
	return parentImg.FullTag
}

// BuilderRefForFormat returns the full tag of the builder image for a format, or
// "" when there is no distinct builder. Relocated from charly (P8).
func (g *Generator) BuilderRefForFormat(boxName, format string) string {
	img := g.Boxes[boxName]
	builder := img.Builder.BuilderFor(format)
	if builder == "" || builder == boxName {
		return ""
	}
	if builderImg, ok := g.Boxes[builder]; ok {
		return builderImg.FullTag
	}
	return ""
}

// GlobalOrderForBox returns the candy order for an image by filtering the global
// order to only the image's needed candies (expanded + transitively resolved).
// Shared candies keep the same cross-image order, maximizing cache reuse. Any
// needed candy missing from the global order is appended in resolution order (a
// safety net that shouldn't trigger). Relocated from charly core (P8); byte-identical.
func (g *Generator) GlobalOrderForBox(imageCandies []string, parentCandies map[string]bool) ([]string, error) {
	needed, err := ResolveCandyOrder(imageCandies, g.Candies, parentCandies)
	if err != nil {
		return nil, err
	}

	neededSet := make(map[string]bool, len(needed))
	for _, l := range needed {
		neededSet[l] = true
	}

	var order []string
	for _, l := range g.GlobalOrder {
		if neededSet[l] {
			order = append(order, l)
		}
	}

	for _, l := range needed {
		if !slices.Contains(order, l) {
			order = append(order, l)
		}
	}

	return order, nil
}
