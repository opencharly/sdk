package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/opencharly/sdk/schema"
	"github.com/opencharly/sdk/schemaconcat"
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
	return validateCUEValue(v.ctx, v.value, definition, value)
}

// ValidateJSON validates the original JSON bytes without first decoding JSON
// numbers through float64-backed map[string]any values.
func (v *SchemaValidator) ValidateJSON(definition string, payload []byte) error {
	input := v.ctx.CompileBytes(payload)
	if err := input.Err(); err != nil {
		return fmt.Errorf("decode JSON for %s: %w", definition, err)
	}
	return validateCUEInput(v.value, definition, input)
}

var generatedSchema struct {
	sync.Once
	ctx   *cue.Context
	value cue.Value
	err   error
}

// ValidateGenerated validates a generated SDK value against its authoritative
// CUE definition. Command plugins use the same embedded schema as core, so
// moving command ownership never creates a hand-maintained validation copy.
func ValidateGenerated(definition string, value any) error {
	if err := loadGeneratedSchema(); err != nil {
		return err
	}
	return validateCUEValue(generatedSchema.ctx, generatedSchema.value, definition, value)
}

// DecodeGeneratedJSON strictly decodes one persisted or received JSON value
// into its generated Go type, then validates that typed value against the
// authoritative CUE definition. Typed decoding is required for fields such as
// []byte, whose standard JSON representation is base64 text but whose CUE value
// is bytes. Unknown fields and trailing JSON values are rejected before CUE
// validation so decoding cannot silently discard persisted input.
func DecodeGeneratedJSON(definition string, payload []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode JSON for %s: %w", definition, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode JSON for %s: trailing JSON value", definition)
		}
		return fmt.Errorf("decode JSON for %s: trailing data: %w", definition, err)
	}
	return ValidateGenerated(definition, dst)
}

func loadGeneratedSchema() error {
	generatedSchema.Do(func() {
		generatedSchema.ctx = cuecontext.New()
		body, _, err := schemaconcat.ConcatSchema(schema.FS, ".", nil)
		if err != nil {
			generatedSchema.err = err
			return
		}
		generatedSchema.value = generatedSchema.ctx.CompileString(body)
		generatedSchema.err = generatedSchema.value.Err()
	})
	if generatedSchema.err != nil {
		return fmt.Errorf("compile SDK CUE schema: %w", generatedSchema.err)
	}
	return nil
}

func validateCUEValue(ctx *cue.Context, schemaValue cue.Value, definition string, value any) error {
	input := ctx.Encode(value)
	if input.Err() != nil {
		return input.Err()
	}
	return validateCUEInput(schemaValue, definition, input)
}

func validateCUEInput(schemaValue cue.Value, definition string, input cue.Value) error {
	def := schemaValue.LookupPath(cue.ParsePath(definition))
	if !def.Exists() {
		return fmt.Errorf("CUE definition %s does not exist", definition)
	}
	if err := input.Unify(def).Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("%s: %w", definition, err)
	}
	return nil
}
