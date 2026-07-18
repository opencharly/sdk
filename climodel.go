package sdk

// Command-model reflection is implemented once in the SDK so every command
// plugin and the host emit the same CUE-generated spec.CLIModel contract.

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/opencharly/sdk/spec"
)

// BuildCLIModel reflects a Kong command tree into the generated #CLIModel.
// Prefix is a dotted command path prepended to every leaf (for example
// "agent" when a command plugin reflects only its owned subtree).
func BuildCLIModel(root any, name, version, prefix string, options ...kong.Option) (*spec.CLIModel, error) {
	opts := append([]kong.Option{kong.Name(name), kong.UsageOnError()}, options...)
	k, err := kong.New(root, opts...)
	if err != nil {
		return nil, fmt.Errorf("building %s command model: %w", name, err)
	}
	model := &spec.CLIModel{Name: name, Version: version}
	for _, leaf := range k.Model.Leaves(true) {
		value := KongLeafToCLILeaf(leaf)
		if prefix != "" {
			value.Path = prefix + "." + value.Path
		}
		model.Leaves = append(model.Leaves, value)
	}
	return model, nil
}

// KongLeafToCLILeaf converts one Kong leaf into the generated wire shape.
func KongLeafToCLILeaf(leaf *kong.Node) spec.CLILeaf {
	out := spec.CLILeaf{Path: strings.ReplaceAll(leaf.Path(), " ", "."), Help: strings.TrimSpace(leaf.Help)}
	if strings.HasPrefix(out.Path, "__") {
		out.Hidden = true
	}
	for _, pos := range leaf.Positional {
		out.Positionals = append(out.Positionals, kongValueToCLIArg(pos, "", pos.Required))
	}
	seen := map[string]bool{}
	for _, pos := range leaf.Positional {
		seen[sanitizeCLIPropName(pos.Name)] = true
	}
	for _, group := range leaf.AllFlags(true) {
		for _, flag := range group {
			if flag.Hidden || flag.Name == "help" || flag.Name == "help-all" {
				continue
			}
			prop := sanitizeCLIPropName(flag.Name)
			if seen[prop] {
				continue
			}
			seen[prop] = true
			arg := kongValueToCLIArg(flag.Value, flag.Name, flag.Required)
			arg.IsBool = flag.IsBool()
			arg.Negated = flag.Negated
			out.Flags = append(out.Flags, arg)
		}
	}
	return out
}

func kongValueToCLIArg(value *kong.Value, flagName string, required bool) spec.CLIArg {
	arg := spec.CLIArg{Prop: sanitizeCLIPropName(value.Name), Name: flagName, Help: value.Help, Required: required}
	if value.Enum != "" {
		for _, enum := range value.EnumSlice() {
			arg.Enum = append(arg.Enum, fmt.Sprint(enum))
		}
	}
	if value.IsSlice() {
		arg.Type, arg.IsSlice, arg.ElemType = "array", true, cliJSONType(value.Target.Type().Elem().Kind())
		return arg
	}
	if value.IsMap() {
		arg.Type, arg.IsMap = "object", true
		return arg
	}
	arg.Type = cliJSONType(value.Target.Kind())
	if value.HasDefault && value.Default != "" {
		arg.HasDefault, arg.Default = true, value.Default
	}
	return arg
}

func sanitizeCLIPropName(value string) string {
	return strings.ReplaceAll(strings.ToLower(value), "-", "_")
}

func cliJSONType(kind reflect.Kind) string {
	switch kind {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}
