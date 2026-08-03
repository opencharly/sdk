package sdk

// validate.go — the SchemaValidator (kept in the sdk root — a plugin-side unit not
// relocated by the spec leg) + re-exports of ValidateGenerated / DecodeGeneratedJSON
// (relocated to github.com/opencharly/spec/climodel, #55 import-purity). The CUE
// validation primitives (ValidateCUEValue / ValidateCUEInput) are the R3-shared
// single source in spec/climodel; SchemaValidator.Validate / ValidateJSON delegate
// to them so the sdk root holds NO duplicate validation path.

import (
	"fmt"
	"io/fs"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/opencharly/spec/climodel"
	"github.com/opencharly/spec/schemaconcat"
)

// SchemaValidator validates plugin-owned generated values against the same
// embedded CUE source that the plugin publishes through Describe.
type SchemaValidator struct {
	ctx   *cue.Context
	value cue.Value
}

// NewSchemaValidator compiles one self-contained embedded plugin schema.
func NewSchemaValidator(schemaFS fs.FS, dir string) (*SchemaValidator, error) {
	body, _, err := schemaconcat.ConcatSchema(schemaFS, dir, nil)
	if err != nil {
		return nil, fmt.Errorf("concatenate CUE schema: %w", err)
	}
	ctx := cuecontext.New()
	value := ctx.CompileString(body)
	if err := value.Err(); err != nil {
		return nil, fmt.Errorf("compile CUE schema: %w", err)
	}
	return &SchemaValidator{ctx: ctx, value: value}, nil
}

// Validate checks a value against a named definition in the compiled schema.
func (v *SchemaValidator) Validate(definition string, value any) error {
	return climodel.ValidateCUEValue(v.ctx, v.value, definition, value)
}

// ValidateJSON validates the original JSON bytes without first decoding JSON
// numbers through float64-backed map[string]any values.
func (v *SchemaValidator) ValidateJSON(definition string, payload []byte) error {
	input := v.ctx.CompileBytes(payload)
	if err := input.Err(); err != nil {
		return fmt.Errorf("decode JSON for %s: %w", definition, err)
	}
	return climodel.ValidateCUEInput(v.value, definition, input)
}

// ValidateGenerated validates a generated SDK value against its authoritative
// CUE definition. Command plugins use the same embedded schema as core, so
// moving command ownership never creates a hand-maintained validation copy.
// Relocated to spec/climodel; re-exported here so candy call sites compile UNCHANGED.
var ValidateGenerated = climodel.ValidateGenerated

// DecodeGeneratedJSON strictly decodes one persisted or received JSON value
// into its generated Go type, then validates that typed value against the
// authoritative CUE definition. Typed decoding is required for fields such as
// []byte, whose standard JSON representation is base64 text but whose CUE value
// is bytes. Unknown fields and trailing JSON values are rejected before CUE
// validation so decoding cannot silently discard persisted input. Relocated to
// spec/climodel; re-exported here so candy call sites compile UNCHANGED.
var DecodeGeneratedJSON = climodel.DecodeGeneratedJSON
