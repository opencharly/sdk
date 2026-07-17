package sdk

import (
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// kong_reflect.go — KongSubcommands derives a ProvidedCapability.Subcommands catalog directly
// from an existing Kong-tagged grammar struct (F-CLI-NEST), so a compiled-in command-class plugin
// that already owns a real Kong struct for its internal parse (e.g. candy/plugin-check's CheckCmd,
// parsed a second time inside the plugin via RunInProcCLI) declares its catalog by REFLECTING over
// that SAME struct instead of hand-duplicating a parallel name list (R3) — the struct's own `cmd`/
// `name`/`help`/`hidden` tags stay the single source of truth.

// KongSubcommands walks a Kong-tagged struct (or pointer to one) ONE level deep and returns its
// named `cmd:""` children as a CLISubcommand catalog: Name from the field's `name:` tag when
// present, otherwise the SAME kebab-cased field name Kong itself computes as the default command
// name (RDD-spiked: Kong's `cmd:"<value>"` tag VALUE is never read as a name — a `cmd` tag is a
// pure presence marker — so a value-carrying `cmd:"foo"` with no separate `name:` tag does NOT mean
// the command is named "foo"; replicating the fallback here keeps a declared subcommand's NAME
// byte-identical to what Kong actually dispatches). Help comes from the field's `help:` tag. A
// field tagged `hidden:""` is skipped — machinery subcommands stay invisible to `--help` and MCP
// exactly as in the plugin's own internal grammar. A field with no `cmd` tag key at all (kong
// requires the key to be PRESENT, regardless of value, to mark a subcommand) is skipped too.
func KongSubcommands(v any) []CLISubcommand {
	rv := reflect.Indirect(reflect.ValueOf(v))
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	var out []CLISubcommand
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if _, ok := f.Tag.Lookup("cmd"); !ok {
			continue
		}
		if _, hidden := f.Tag.Lookup("hidden"); hidden {
			continue
		}
		name := f.Tag.Get("name")
		if name == "" {
			name = kongDefaultFieldName(f.Name)
		}
		out = append(out, CLISubcommand{Name: name, Help: f.Tag.Get("help")})
	}
	return out
}

// kongDefaultFieldName reproduces Kong's OWN default flag/command namer
// (strings.ToLower(dashedString(s)), kong.go + build.go in github.com/alecthomas/kong) so a field
// with no explicit `name:` tag gets the IDENTICAL name Kong would assign it — never an
// independently-invented convention that could drift from what actually dispatches.
func kongDefaultFieldName(s string) string { return strings.ToLower(strings.Join(kongCamelCase(s), "-")) }

// kongCamelCase is a straight reimplementation of Kong's vendored camelCase splitter (itself
// github.com/fatih/camelcase, MIT) — Kong's unexported function cannot be imported directly, and
// this small, stable, well-known algorithm is cheaper to mirror exactly than to add a dependency
// for. Splits "ListAgent" -> ["List","Agent"], "PDFLoader" -> ["PDF","Loader"], etc.
func kongCamelCase(src string) (entries []string) {
	if !utf8.ValidString(src) {
		return []string{src}
	}
	var runes [][]rune
	lastClass := 0
	for _, r := range src {
		var class int
		switch {
		case unicode.IsLower(r):
			class = 1
		case unicode.IsUpper(r):
			class = 2
		case unicode.IsDigit(r):
			class = 3
		default:
			class = 4
		}
		if class == lastClass {
			runes[len(runes)-1] = append(runes[len(runes)-1], r)
		} else {
			runes = append(runes, []rune{r})
		}
		lastClass = class
	}
	for i := 0; i < len(runes)-1; i++ {
		if unicode.IsUpper(runes[i][0]) && unicode.IsLower(runes[i+1][0]) {
			runes[i+1] = append([]rune{runes[i][len(runes[i])-1]}, runes[i+1]...)
			runes[i] = runes[i][:len(runes[i])-1]
		}
	}
	for _, s := range runes {
		if len(s) > 0 {
			entries = append(entries, string(s))
		}
	}
	return entries
}
