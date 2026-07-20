package sdk

import (
	"reflect"
	"testing"
)

// kongReflectFixture mirrors the exact tag shapes candy/plugin-check's CheckCmd uses: an explicit
// `name:` tag, a `cmd:"<value>"` shorthand whose value happens to match the kebab-cased field name,
// a `cmd:"<value>"` shorthand whose value does NOT match the kebab-cased field name (the ListAgent
// case, RDD-spiked live against real kong.New — see kong_reflect.go), a hidden field, and a
// non-cmd field that must be skipped entirely.
type kongReflectFixture struct {
	Box       struct{} `cmd:"" name:"box" help:"box help"`
	LastTag   struct{} `cmd:"last-tag" help:"last-tag help"`
	ListAgent struct{} `cmd:"list-ai" help:"list agents"`
	RunLocal  struct{} `cmd:"run-local" hidden:"" help:"hidden"`
	NotACmd   struct{} `help:"not a subcommand"`
}

func TestKongSubcommands(t *testing.T) {
	got := KongSubcommands(&kongReflectFixture{})
	want := []CLISubcommand{
		{Name: "box", Help: "box help"},
		{Name: "last-tag", Help: "last-tag help"},
		// Kong ignores the `cmd:"list-ai"` VALUE for naming — only the field name is kebab-cased —
		// so the real dispatched (and here, derived) name is "list-agent", not "list-ai".
		{Name: "list-agent", Help: "list agents"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KongSubcommands() = %#v, want %#v", got, want)
	}
}

func TestKongSubcommands_NonStruct(t *testing.T) {
	if got := KongSubcommands("not a struct"); got != nil {
		t.Fatalf("KongSubcommands(non-struct) = %#v, want nil", got)
	}
}
