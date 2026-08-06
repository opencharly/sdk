package loaderkit

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/opencharly/spec/spec"

	"gopkg.in/yaml.v3"
)

// parse_candy_manifest.go — the candy-MANIFEST parse MECHANISM (K-wave 2 cone R1, A2 unit 2),
// relocated verbatim from charly/layers.go's parseCandyYAML + singleCandyMappingNode +
// rejectLegacyCandyKeys + rejectUnknownCandyTopLevelKeys + looksLikeDistroOrFormatKey.
//
// WHY IT MOVED. It is the parseDoc every candy scan injects — ScanCandyManifest's and
// ScanRemoteCandy's `parseDoc` parameter. While it lived in charly core, ANY caller of those two
// mechanisms had to round-trip to the host for the parse, which is why candy/plugin-build reached
// `buildengine-scan-remote` for every remote repo it scanned. The apparent blocker was the
// bootstrap-critical clause-B buildCandy factory the node-form branch called.
//
// That blocker was DISPROVEN by measurement, not argued away. The node-form branch ran
// parsedNodeToGeneric(pn) -> buildCandy(gn) -> decodeNodeValue(gn) -> genericToParsedNode(gn) ->
// DecodeNodeValue(pn') — a pn -> genericNode -> pn ROUND TRIP whose only net effects are the
// DecodeNodeValue and the name stamp. An RDD spike compared the two over the whole real corpus
// (324 candy manifests across candy/ and box/): 321 node-form manifests plus all 3 error paths,
// BYTE-IDENTICAL, zero diffs. So the mechanism needs the registered DocParser + the registry-derived
// Threaded snapshot + the CUE entity decoder — all of which are already in this package or arrive as
// parameters — and never the B bootstrap root.
//
// buildCandy / candyIsImage THEMSELVES stay in charly core: their genuine clause-B consumers are the
// discovered-candy pre-check and foldCandyKind, a DIFFERENT call path this move does not touch.
//
// The vocabulary the shape guard consults is a PARAMETER (spec.CandyVocab) rather than the pair of
// process-global caches it was in core. A compiled-in plugin shares the host's process, so package
// globals would have appeared to work while silently coupling two modules through mutable state and
// failing open for an out-of-process placement; passing the value keeps the mechanism honest in both
// placements.

// ParseCandyManifest reads and unmarshals a candy manifest file. Strict schema:
//   - Empty / comment-only file → zero-value spec.Candy.
//   - Single top-level `candy:` key → decode its body as the candy body (canonical form).
//   - `candy:` + other top-level keys → error (ambiguous shape).
//   - Multi-document stream → error (the candy manifest is not a bundle file).
//   - Flat form (no `candy:` wrapper) → error with migration hint.
//
// t is the registry-derived kind-recognition snapshot the node-form parse needs; vocab is the build
// vocabulary the misplaced-section shape guard consults (a zero value fails the guard OPEN — no
// false positives — exactly as an unregistered vocabulary did in core).
func ParseCandyManifest(path string, t spec.Threaded, vocab spec.CandyVocab) (*spec.CandyYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Empty / comment-only guard.
	if strings.TrimSpace(string(data)) == "" {
		return &spec.CandyYAML{}, nil
	}

	// Parse the stream down to its single top-level mapping node (or nil for an
	// all-comment/null file → zero-value candy body).
	inner, err := SingleCandyMappingNode(path, data)
	if err != nil {
		return nil, err
	}
	if inner == nil {
		return &spec.CandyYAML{}, nil
	}

	// Unified node-form: one or more name-first nodes `<name>: {candy: …, <children>}`.
	// (The `candy` discriminator is NESTED under the node name, so the kind-keyed branch below —
	// which looks for a TOP-LEVEL `candy:` key — won't match.) A STACKED manifest carries a candy
	// node PLUS sibling kind entities (`skill:`/`hook:`/`marketplace:` — the plugins→candies
	// migration): find the candy node among them and return ITS body — the discovered-candy scan
	// enumerates candies; the sibling entities are resolved by the full load path (ParseDoc).
	if len(inner.Content) >= 2 {
		if _, pp, perr := ParseDoc(inner, t); perr == nil {
			for i := range pp.Nodes {
				if pp.Nodes[i].Disc != "candy" {
					continue
				}
				// Decode-ONLY at load (fast, runs on every invocation): the full closed-schema CUE
				// validation (CalVer/enum/unknown-key checks) runs at `charly box validate`, not here.
				var c spec.CandyYAML
				if derr := DecodeNodeValue(pp.Nodes[i], &c); derr != nil {
					return nil, fmt.Errorf("%s: %w", path, derr)
				}
				// Name is the node KEY in node-form (the migration moves a legacy body `name:` up to
				// the key), so stamp it — the decoded body carries no `name:`.
				c.Name = pp.Nodes[i].Name
				return &c, nil
			}
		}
	}

	// Collect top-level keys.
	var keys []string
	candyIdx := -1
	for i := 0; i < len(inner.Content); i += 2 {
		k := inner.Content[i].Value
		keys = append(keys, k)
		if k == "candy" {
			candyIdx = i + 1
		}
	}

	if candyIdx >= 0 {
		// Canonical kind-keyed form — `candy:` must be the only top-level key.
		if len(keys) != 1 {
			var other []string
			for _, k := range keys {
				if k != "candy" {
					other = append(other, k)
				}
			}
			return nil, fmt.Errorf("%s: ambiguous — `candy:` wrapper present AND other top-level keys %v (pick one form)", path, other)
		}
		// 2026-05 Calamares cutover: hard-fail on legacy field shapes.
		// Every legacy form has a one-shot remediation via `charly migrate`.
		body := inner.Content[candyIdx]
		if body != nil && body.Kind == yaml.MappingNode {
			if err := rejectLegacyCandyKeys(path, body, vocab); err != nil {
				return nil, err
			}
			// Load-time top-level typo-detection (CUE-decode is lenient and would silently drop a
			// plural/singular typo; full closed-schema validation is `charly box validate`'s job).
			if err := rejectUnknownCandyTopLevelKeys(path, body); err != nil {
				return nil, err
			}
		}
		var ly spec.CandyYAML
		if err := DecodeEntityViaCUE(body, reflect.TypeOf(spec.CandyYAML{}), &ly, path); err != nil {
			return nil, err
		}
		return &ly, nil
	}

	// Neither node-form nor the `candy:` kind-keyed form — an unrecognized manifest.
	return nil, fmt.Errorf("%s: unrecognized candy manifest shape — expected node-form `<name>: {candy: …}` (or the `candy:` kind-keyed form)", path)
}

// kindWord reports whether w is one of the reserved authoring KIND keywords (the #Node
// discriminators). The set is CUE-derived (spec.KindWords) and is EMPTY today — every authoring kind
// is plugin-served — but the guard is preserved verbatim from the pre-move charly/layers.go rather
// than folded away, because collapsing it would silently change the shape-classification rule if a
// built-in arm ever returned.
func kindWord(w string) bool {
	for _, k := range spec.KindWords {
		if k == w {
			return true
		}
	}
	return false
}

// SingleCandyMappingNode parses a candy manifest's bytes as a YAML multi-document stream and returns
// the single top-level mapping node (DocumentNode unwrapped). It returns (nil, nil) when the stream
// holds no non-empty document (an all-comment / null file → zero-value candy body), and errors on a
// multi-document stream or a non-mapping top level.
func SingleCandyMappingNode(path string, data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	var docs []yaml.Node
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// Skip empty (null-valued) docs.
		if node.Kind == 0 || (node.Kind == yaml.DocumentNode && (len(node.Content) == 0 || (len(node.Content) == 1 && node.Content[0].Tag == "!!null"))) {
			continue
		}
		docs = append(docs, node)
	}
	if len(docs) == 0 {
		return nil, nil
	}
	if len(docs) > 1 {
		return nil, fmt.Errorf("%s: the candy manifest is not a multi-document stream; bundle files belong in the unified charly.yml", path)
	}
	// Unwrap the DocumentNode wrapper.
	inner := &docs[0]
	if inner.Kind == yaml.DocumentNode && len(inner.Content) > 0 {
		inner = inner.Content[0]
	}
	if inner.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: top level must be a mapping (got kind=%v)", path, inner.Kind)
	}
	return inner, nil
}

// candyKnownFields lists non-format top-level keys in the candy manifest. Unknown keys are routed to
// FormatSections (if matching an embedded distro format) or TagSections (otherwise).
//
// `directory`, `info` deleted in the 2026-05 Calamares cutover (0 YAML files used either;
// `description:` carries the metadata `info:` previously held). `depends` renamed to `requires`.
// Calamares-shaped `packages` + `distros` added as the unified package surface; per-format
// `rpm:`/`deb:`/`pac:`/`aur:` and per-distro tag sections (debian:13: etc.) collapse into them via
// `charly migrate`.
var candyKnownFields = map[string]bool{
	"description": true, "version": true, "status": true,
	"name": true, "from": true,
	"candy": true, "require": true, "engine": true, "env": true,
	"path_append": true, "port": true, "route": true, "service": true,
	"volume": true, "alias": true, "extract": true, "security": true,
	"libvirt": true, "hook": true,
	"port_relay": true, "secret": true, "data": true,
	"env_provide": true, "env_require": true, "env_accept": true,
	"secret_accept": true, "secret_require": true,
	"mcp_provide": true, "mcp_require": true, "mcp_accept": true,
	"var": true, "plan": true,
	"plugin":     true,
	"artifact":   true,
	"capability": true, "requires_capability": true,
	"package": true, "distro": true,
	"apk":      true,
	"shell":    true,
	"localpkg": true, "reboot": true,
	"bake_plugin": true,
}

// rejectUnknownCandyTopLevelKeys hard-errors on an unknown top-level candy key (a plural/singular
// typo). This is the load-time typo-detection the deleted CandyYAML.UnmarshalYAML used to do —
// CUE-decode is lenient and would silently drop the key. Comprehensive closed-schema validation is
// `charly box validate`.
func rejectUnknownCandyTopLevelKeys(path string, body *yaml.Node) error {
	if body == nil || body.Kind != yaml.MappingNode {
		return nil
	}
	var unknown []string
	for i := 0; i+1 < len(body.Content); i += 2 {
		key := body.Content[i].Value
		if candyKnownFields[key] {
			continue
		}
		unknown = append(unknown, key)
	}
	if len(unknown) > 0 {
		return fmt.Errorf("%s: candy has unknown top-level key(s) %v — almost always a plural/singular typo: use the SINGULAR form (task: not tasks:, var: not vars:, candy: not layers:, env_provide: not env_provides:); a package format (rpm:/deb:/pac:/aur:) nests under the `distro:` map, never at the candy root", path, unknown)
	}
	return nil
}

// rejectLegacyCandyKeys is the candy-manifest shape guard: a removed field name
// (`depends`/`directory`/`info`) or a misplaced package-format / per-distro section at the candy root
// produces a clear error describing the current schema. Runs before standard YAML decoding so the
// user sees a precise message, not a generic "field not found". The format/distro vocabulary it
// recognizes is the DYNAMIC build vocabulary sourced from the embedded build vocabulary — no
// hardcoded format/distro list, so a newly-added format or distro is caught automatically.
func rejectLegacyCandyKeys(path string, body *yaml.Node, vocab spec.CandyVocab) error {
	for i := 0; i+1 < len(body.Content); i += 2 {
		key := body.Content[i].Value
		switch key {
		case "depends":
			return fmt.Errorf("%s: candy manifest uses the removed `depends:` field — rename it to `require:`", path)
		case "directory":
			return fmt.Errorf("%s: candy manifest uses the removed `directory:` field — the candy directory is implicit", path)
		case "info":
			return fmt.Errorf("%s: candy manifest uses the removed `info:` field — use `description:`", path)
		}
		// A package-format family key (pac:/deb:/rpm:/aur:) or a per-distro tag section
		// (`debian:`, `debian:13:`, `debian,ubuntu:`) at the candy ROOT belongs UNDER the `distro:`
		// map. Both vocabularies come from the embedded build vocabulary.
		if vocab.LooksLikeDistroOrFormatKey(key) {
			return fmt.Errorf("%s: candy manifest places `%s:` at the top level — package-format and per-distro sections nest under the `distro:` map (e.g. `distro:\n  %s:\n    package: [...]`)", path, key, key)
		}
	}
	return nil
}
