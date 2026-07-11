package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
)

// GenerateTraefikRoutes writes the image's traefik dynamic-config (routers +
// services) to .build/<box>/traefik-routes.yml, egress-gated before write.
// Relocated from charly (P8); byte-identical.
func (g *Generator) GenerateTraefikRoutes(boxName string, candyOrder []string, _ *buildkit.ResolvedBox) error {
	var b strings.Builder

	b.WriteString("# .build/" + boxName + "/traefik-routes.yml (generated -- do not edit)\n")
	b.WriteString("http:\n")
	b.WriteString("  routers:\n")

	type routeEntry struct {
		name string
		cfg  *RouteConfig
	}
	var routes []routeEntry
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if !layer.HasRoute() {
			continue
		}
		route, err := layer.Route()
		if err != nil || route == nil {
			continue
		}
		routes = append(routes, routeEntry{name: candyName, cfg: route})
	}

	for _, r := range routes {
		host := r.cfg.Host
		fmt.Fprintf(&b, "    %s:\n", r.name)
		fmt.Fprintf(&b, "      rule: \"Host(`%s`)\"\n", host)
		fmt.Fprintf(&b, "      service: %s\n", r.name)
		b.WriteString("      entryPoints:\n")
		b.WriteString("        - websecure\n")
		b.WriteString("      tls:\n")
		b.WriteString("        certResolver: letsencrypt\n")
	}

	b.WriteString("  services:\n")
	for _, r := range routes {
		fmt.Fprintf(&b, "    %s:\n", r.name)
		b.WriteString("      loadBalancer:\n")
		b.WriteString("        servers:\n")
		fmt.Fprintf(&b, "          - url: \"http://127.0.0.1:%s\"\n", r.cfg.Port)
	}

	imageDir := filepath.Join(g.BuildDir, boxName)
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return err
	}

	routesYAML := []byte(b.String())
	if err := g.ValidateEgress("traefik_routes", filepath.Join(boxName, "traefik-routes.yml"), routesYAML); err != nil {
		return err
	}
	return kit.AtomicWriteFile(filepath.Join(imageDir, "traefik-routes.yml"), routesYAML, 0644)
}

// EmitTraefikRouteStage emits the traefik-routes scratch stage when the image has
// route candies AND traefik, generating the routes file. Returns (hasRoutes,
// hasTraefik). Relocated from charly (P8); byte-identical.
func (g *Generator) EmitTraefikRouteStage(b *strings.Builder, boxName string, img *buildkit.ResolvedBox, candyOrder []string) (hasRoutes, hasTraefik bool, err error) {
	for _, candyName := range candyOrder {
		layer := g.Candies[candyName]
		if layer.HasRoute() {
			hasRoutes = true
		}
		if layer.GetName() == "traefik" {
			hasTraefik = true
		}
	}

	if hasRoutes && hasTraefik {
		if rerr := g.GenerateTraefikRoutes(boxName, candyOrder, img); rerr != nil {
			return hasRoutes, hasTraefik, rerr
		}
		b.WriteString("FROM scratch AS traefik-routes\n")
		fmt.Fprintf(b, "COPY .build/%s/traefik-routes.yml /routes.yml\n\n", boxName)
	}
	return hasRoutes, hasTraefik, nil
}
