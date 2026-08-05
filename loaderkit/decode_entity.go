package loaderkit

// decode_entity.go — the per-entity CUE decode path (K1 unit 1, relocated verbatim from
// charly/cue_normalize.go + charly/cue_loader.go). A single entity node (a candy body, a kind
// entity, an assembled node-form body, a plan-step list) is canonicalized in place by
// NormalizeEntityNode against its Go type, re-marshaled, CUE-ingested, and Decoded into the target
// struct — making CUE the universal decode authority for the data model. The import:/discover:
// graph (which drives composition) is decoded by yaml.v3 and resolved in Go
// (sdk/loaderkit/repo_identity.go), never here. See memory cue-loader-switch-design.
//
// This mechanism is zero-registry-coupled: pure reflect+yaml.v3+CUE, consulting only spec.* types
// for its shorthand expanders. It owns its OWN cue.Context (decodeEntityCueCtx below), separate
// from charly core's process-wide cueSchemaCtx (charly/cue_schema.go) — DecodeEntityViaCUE never
// Unifies its built value against the shared base schema (only BuildFile+Decode), so two
// independent CUE contexts never need to interoperate.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"cuelang.org/go/cue/cuecontext"
	cueyaml "cuelang.org/go/encoding/yaml"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// decodeEntityCueCtx is the CUE context this mechanism uses to ingest+decode entity bodies — a
// SEPARATE instance from charly core's process-wide cueSchemaCtx (never Unified against it).
var decodeEntityCueCtx = cuecontext.New()

// DecodeEntityViaCUE normalizes a single entity node against its Go type, then CUE-ingests +
// Decodes it into out (a pointer). Does not mutate the input node. The node must BE the entity
// value (the candy body / a single kind entity / an assembled node-form body), not a kind-keyed
// wrapper. Used by the kind-keyed / candy / inline / node-form decode paths.
func DecodeEntityViaCUE(node *yaml.Node, t reflect.Type, out any, label string) error {
	clone, err := cloneYAMLNode(node)
	if err != nil {
		return fmt.Errorf("%s: clone: %w", label, err)
	}
	// Normalize shorthand → canonical, then CUE-ingest + Decode into the struct.
	if err := NormalizeEntityNode(clone, t); err != nil {
		return fmt.Errorf("%s: normalize: %w", label, err)
	}
	b, err := yaml.Marshal(clone)
	if err != nil {
		return fmt.Errorf("%s: re-marshal: %w", label, err)
	}
	af, err := cueyaml.Extract(label, b)
	if err != nil {
		return fmt.Errorf("%s: cue yaml ingest: %w", label, err)
	}
	cv := decodeEntityCueCtx.BuildFile(af)
	if cv.Err() != nil {
		return fmt.Errorf("%s: cue build: %w", label, cv.Err())
	}
	if err := cv.Decode(out); err != nil {
		return fmt.Errorf("%s: cue decode: %w", label, err)
	}
	return nil
}

// cloneYAMLNode deep-copies a node by marshal+reparse (no cycles in raw input).
func cloneYAMLNode(node *yaml.Node) (*yaml.Node, error) {
	b, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	var out yaml.Node
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// shorthandExpander rewrites a shorthand node into its canonical shape in place.
type shorthandExpander func(node *yaml.Node) error

// cueShorthandExpanders maps a canonical Go type to the expander that
// canonicalizes its shorthand wire forms. Keyed by the value type (not pointer).
var cueShorthandExpanders = map[reflect.Type]shorthandExpander{
	reflect.TypeOf(spec.PackageItem{}):       expandPackageItemNode,
	reflect.TypeOf(spec.PortSpec{}):          expandPortSpecNode,
	reflect.TypeOf(spec.PreemptibleConfig{}): expandPreemptibleNode,
	reflect.TypeOf(spec.TunnelYAML{}):        expandTunnelNode,
}

var jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// implementsJSONUnmarshaler reports whether t or *t has an UnmarshalJSON method.
func implementsJSONUnmarshaler(t reflect.Type) bool {
	return t.Implements(jsonUnmarshalerType) || reflect.PointerTo(t).Implements(jsonUnmarshalerType)
}

// NormalizeEntityNode canonicalizes a single entity's YAML node against the Go
// type t (the authored struct, e.g. CandyYAML). Mutates node in place.
func NormalizeEntityNode(node *yaml.Node, t reflect.Type) error {
	return normalizeNode(node, t)
}

func normalizeNode(node *yaml.Node, t reflect.Type) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		return normalizeNode(node.Content[0], t)
	}
	// Resolve aliases to their anchor target for type-walking purposes.
	if node.Kind == yaml.AliasNode {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// Opaque self-decoding types (Matcher/MatcherList/PortScope/LibvirtGraphicsListeners):
	// cue.Value.Decode runs their UnmarshalJSON, so leave the shorthand untouched and do not recurse.
	if implementsJSONUnmarshaler(t) {
		return nil
	}

	// Shorthand expander (turns scalar/seq/dynamic-key shapes into the struct form).
	if exp, ok := cueShorthandExpanders[t]; ok {
		if err := exp(node); err != nil {
			return err
		}
	}

	// Generic scalar→string coercion: a non-string scalar bound for a Go string
	// field must carry the !!str tag so CUE ingests it as a string.
	if t.Kind() == reflect.String && node.Kind == yaml.ScalarNode {
		forceStringTag(node)
		return nil
	}

	switch t.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return nil // shape mismatch — CUE validation reports it precisely
		}
		flat := flattenYamlFields(t)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			ft, ok := flat[key]
			if !ok {
				continue // unknown key — CUE closedness reports it
			}
			if err := normalizeNode(node.Content[i+1], ft); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return nil
		}
		for _, el := range node.Content {
			if err := normalizeNode(el, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if err := normalizeNode(node.Content[i+1], t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// flattenYamlFields maps every yaml key authorable on a struct (following
// inline/anonymous embeds, e.g. Step embeds Op `yaml:",inline"`) to its Go
// field type. A bare field name with no yaml tag maps under its lowercased name
// (yaml.v3's own default). Cached per type.
var flatYamlFieldsCache = map[reflect.Type]map[string]reflect.Type{}

func flattenYamlFields(t reflect.Type) map[string]reflect.Type {
	if m, ok := flatYamlFieldsCache[t]; ok {
		return m
	}
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Wire key: the yaml tag. The CUE-generated spec types carry DOUBLED
		// yaml+json tags (cue:gen -mode=retag doubles every json tag into a
		// matching yaml tag), so every generated snake_case field is reachable
		// by its yaml tag — the former json fallback is redundant (R3). The only
		// hand union type with json-only fields, Matcher{Op,Value}, is reached but
		// inert (its wire form is a scalar/operator-map, never op:/value: keys).
		tag := f.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		inline := strings.Contains(opts, "inline")
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if (f.Anonymous || inline) && ft.Kind() == reflect.Struct {
			for k, v := range flattenYamlFields(ft) {
				if _, exists := out[k]; !exists {
					out[k] = v
				}
			}
			continue
		}
		if name == "-" {
			continue
		}
		if name == "" {
			out[strings.ToLower(f.Name)] = f.Type
			continue
		}
		out[name] = f.Type
	}
	flatYamlFieldsCache[t] = out
	return out
}

// forceStringTag retags a scalar node as !!str so CUE ingests it as a string
// (mirrors yaml.v3's implicit int/bool→string coercion into a string field).
func forceStringTag(node *yaml.Node) {
	if node.Kind != yaml.ScalarNode {
		return
	}
	if node.Tag == "" || node.Tag == "!!int" || node.Tag == "!!bool" || node.Tag == "!!float" {
		node.Tag = "!!str"
		node.Style = yaml.DoubleQuotedStyle
	}
}

// --- expanders (port the canonical mapping from the deleted UnmarshalYAML) ---

// expandPackageItemNode: a bare scalar `nginx` → `{name: nginx}` (PackageItem).
func expandPackageItemNode(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return nil // already mapping form
	}
	nameVal := cloneScalar(node)
	*node = *mappingNodes("name", nameVal)
	return nil
}

// expandPortSpecNode: `8080` → {port: 8080, protocol: http};
// `"tcp:5900"` → {port: 5900, protocol: tcp}; `"8080"` → {port: 8080, protocol: http}.
func expandPortSpecNode(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return nil
	}
	s := node.Value
	port := s
	proto := "http"
	if _, err := strconv.Atoi(s); err != nil {
		if before, after, ok := strings.Cut(s, ":"); ok {
			proto = before
			port = after
		}
	}
	portNode := spec.ScalarNode(port)
	portNode.Tag = "!!int"
	*node = *mappingNodes(
		"port", portNode,
		"protocol", spec.ScalarNode(proto),
	)
	return nil
}

// expandPreemptibleNode: the token-list shorthand `[a, b]` → {holds: [a, b]}
// (PreemptibleConfig); a mapping is already canonical.
func expandPreemptibleNode(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	seq := *node // copy before overwrite (avoid node-in-itself cycle)
	*node = *mappingNodes("holds", &seq)
	return nil
}

// expandTunnelNode: provider scalar shorthand → {provider, default scope}.
// `tailscale` → {provider: tailscale, private: all}; `cloudflare` →
// {provider: cloudflare, public: all}; any other scalar → {provider: <scalar>}.
// (PortScope's all-ports wire form is the scalar "all".)
func expandTunnelNode(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return nil
	}
	kv := []any{"provider", spec.ScalarNode(node.Value)}
	switch node.Value {
	case "tailscale":
		kv = append(kv, "private", spec.ScalarNode("all"))
	case "cloudflare":
		kv = append(kv, "public", spec.ScalarNode("all"))
	}
	*node = *mappingNodes(kv...)
	return nil
}

// --- yaml.Node construction helpers ---

func cloneScalar(n *yaml.Node) *yaml.Node {
	c := *n
	return &c
}

func mappingNodes(kv ...any) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(kv); i += 2 {
		k := kv[i].(string)
		var vn *yaml.Node
		switch v := kv[i+1].(type) {
		case string:
			vn = spec.ScalarNode(v)
		case *yaml.Node:
			vn = v
		}
		m.Content = append(m.Content, spec.ScalarNode(k), vn)
	}
	return m
}
