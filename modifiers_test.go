package sdk

import (
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

// TestOpModifierZeroMatchesFields is the differential-equivalence gate that authorizes
// replacing every per-plugin modifierZero func with the generic reflection-based
// OpModifierZero. It asserts, for EVERY modifier name the live-verb plugins reference in
// their requiredModifiers maps, that (a) the name maps to a real spec.Op field via its yaml
// tag — so OpModifierZero can ever see it — and (b) OpModifierZero reports the field absent on
// a zero Op and present once the field is set. A name with no matching yaml tag (a typo, or a
// modifier whose field tag drifted) fails HERE rather than silently making a required-modifier
// check always-pass at runtime.
func TestOpModifierZeroMatchesFields(t *testing.T) {
	// The union of every modifier name across plugin-{cdp,vnc,wl,dbus,mcp,record,spice,adb,
	// appium,kube}'s requiredModifiers maps + modifierZero cases.
	names := []string{
		"tab", "url", "expression", "selector", "method", "artifact", "text",
		"key", "combo", "x", "y", "x2", "y2", "direction", "target", "command",
		"action", "dest", "path", "tool", "uri",
	}
	for _, name := range names {
		var zero spec.Op
		if !OpModifierZero(&zero, name) {
			t.Errorf("OpModifierZero(zeroOp, %q) = false, want true", name)
		}
		op := spec.Op{}
		if !setFieldByYamlTag(&op, name) {
			t.Errorf("no spec.Op field carries yaml tag %q — OpModifierZero can never detect modifier %q", name, name)
			continue
		}
		if OpModifierZero(&op, name) {
			t.Errorf("OpModifierZero(op with %q set, %q) = true, want false", name, name)
		}
	}
}

// setFieldByYamlTag sets the spec.Op field carrying yaml tag `name` to a non-zero value,
// returning false if no field carries that tag. Mirrors OpModifierZero's field lookup so the
// two agree on the tag→field mapping.
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
