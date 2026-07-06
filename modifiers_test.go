package sdk

import (
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestOpModifierZeroMatchesFields asserts the two lookup layers of the generic
// OpModifierZero. (a) INPUT-MAP layer: every per-verb modifier name a plugin's
// requiredModifiers map references lives in the desugared plugin input map since
// the schema-compaction cutover — the checker reports it absent on an empty
// input and present once set (strings, numbers, bools). (b) SHARED-FIELD layer:
// the genuinely shared #Op fields a method contract may still name (target,
// caps) resolve by yaml-tag reflection — a tag drift fails HERE rather than
// silently making a required-modifier check always-pass at runtime.
func TestOpModifierZeroMatchesFields(t *testing.T) {
	// Per-verb input keys (the union across the live-verb plugins' contracts).
	inputNames := []string{
		"tab", "url", "expression", "selector", "method", "artifact", "text",
		"key", "combo", "x", "y", "x2", "y2", "direction", "command",
		"action", "dest", "path", "tool", "uri",
	}
	for _, name := range inputNames {
		var zero spec.Op
		if !OpModifierZero(&zero, name) {
			t.Errorf("OpModifierZero(zeroOp, %q) = false, want true", name)
		}
		empty := spec.Op{PluginInput: map[string]any{}}
		if !OpModifierZero(&empty, name) {
			t.Errorf("OpModifierZero(emptyInput, %q) = false, want true", name)
		}
		zeroVal := spec.Op{PluginInput: map[string]any{name: ""}}
		if !OpModifierZero(&zeroVal, name) {
			t.Errorf("OpModifierZero(zero-valued input %q) = false, want true", name)
		}
		set := spec.Op{PluginInput: map[string]any{name: "v"}}
		if OpModifierZero(&set, name) {
			t.Errorf("OpModifierZero(input with %q set) = true, want false", name)
		}
	}
	// Numeric + bool input values (JSON round-trip shapes).
	for _, v := range []any{7, int64(7), float64(7), true} {
		op := spec.Op{PluginInput: map[string]any{"x": v}}
		if OpModifierZero(&op, "x") {
			t.Errorf("OpModifierZero(input x=%v %T) = true, want false", v, v)
		}
	}
	// Shared #Op fields still resolve by yaml tag.
	for _, name := range []string{"target", "caps"} {
		var zero spec.Op
		if !OpModifierZero(&zero, name) {
			t.Errorf("OpModifierZero(zeroOp, %q) = false, want true", name)
		}
		op := spec.Op{}
		if !setFieldByYamlTag(&op, name) {
			t.Errorf("no spec.Op field carries yaml tag %q", name)
			continue
		}
		if OpModifierZero(&op, name) {
			t.Errorf("OpModifierZero(op with %q set) = true, want false", name)
		}
	}
}

func setFieldByYamlTag(op *spec.Op, name string) bool {
	v := reflect.ValueOf(op).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		key := t.Field(i).Tag.Get("yaml")
		for j := 0; j < len(key); j++ {
			if key[j] == ',' {
				key = key[:j]
				break
			}
		}
		if key != name {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString("x")
		case reflect.Int, reflect.Int64:
			f.SetInt(1)
		case reflect.Bool:
			f.SetBool(true)
		default:
			return false
		}
		return true
	}
	return false
}
