package buildkit

// externalized_builders.go — the D-FACT of which detection-builder words are served by an EXTERNAL
// out-of-process plugin (no in-proc BuilderProvider). Relocated from charly core (K3 build-engine, U6)
// so BOTH charly core AND candy/plugin-build (running the build-engine RESOLVE plugin-side) read the
// ONE source (R3). charly's package-main `externalizedBuilders` now aliases this value.

// ExternalizedBuilders is THE single source of truth for which builder words are served by an EXTERNAL
// out-of-process plugin. A word here resolves through the registry to a *grpcProvider connected at
// plugin-load time.
var ExternalizedBuilders = map[string]bool{
	"cargo": true,
	"npm":   true,
	"pixi":  true,
	"aur":   true,
}
