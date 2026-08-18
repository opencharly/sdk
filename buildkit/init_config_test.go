package buildkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencharly/spec/spec"
)

// init_config_test.go — coverage for InitConfig's detect/resolve/active dispatch. Each case
// would fail if the schema-driven service routing, the capability-requirement filter, or the
// bootc-flavored systemd-over-supervisord preference regressed.

func TestInitConfigInitNames(t *testing.T) {
	var nilIC *InitConfig
	if got := nilIC.InitNames(); got != nil {
		t.Errorf("nil receiver: InitNames() = %v, want nil", got)
	}

	ic := &InitConfig{Init: map[string]*ResolvedInit{
		"systemd":     {},
		"supervisord": {},
		"openrc":      {},
	}}
	got := ic.InitNames()
	want := []string{"openrc", "supervisord", "systemd"}
	if len(got) != len(want) {
		t.Fatalf("InitNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("InitNames()[%d] = %q, want %q (must be sorted)", i, got[i], want[i])
		}
	}
}

func TestInitConfigDetectCandyInit(t *testing.T) {
	t.Run("nil receiver returns nil", func(t *testing.T) {
		var nilIC *InitConfig
		if got := nilIC.DetectCandyInit(&spec.CandyYAML{}, "/tmp"); got != nil {
			t.Errorf("nil receiver: DetectCandyInit() = %v, want nil", got)
		}
	})

	t.Run("nil candy yaml detects nothing", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {CandyFields: []string{"service"}, ServiceSchema: &spec.InitServiceSchema{SupportsPackaged: true}},
		}}
		if got := ic.DetectCandyInit(nil, "/tmp"); got != nil {
			t.Errorf("DetectCandyInit(nil, ...) = %v, want nil", got)
		}
	})

	t.Run("packaged service entry matches an init whose schema supports packaged", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {
				CandyFields:   []string{"service"},
				ServiceSchema: &spec.InitServiceSchema{SupportsPackaged: true},
			},
			"supervisord": {
				CandyFields:   []string{"service"},
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "tmpl"}, // no SupportsPackaged
			},
		}}
		ly := &spec.CandyYAML{Service: []spec.CandyService{{Name: "web", UsePackaged: "nginx"}}}
		got := ic.DetectCandyInit(ly, "/tmp")
		if len(got) != 1 || got[0] != "systemd" {
			t.Errorf("DetectCandyInit(packaged service) = %v, want [systemd]", got)
		}
	})

	t.Run("exec service entry matches an init with a ServiceTemplate, not the packaged-only one", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {
				CandyFields:   []string{"service"},
				ServiceSchema: &spec.InitServiceSchema{SupportsPackaged: true}, // no ServiceTemplate
			},
			"supervisord": {
				CandyFields:   []string{"service"},
				ServiceSchema: &spec.InitServiceSchema{ServiceTemplate: "tmpl"},
			},
		}}
		ly := &spec.CandyYAML{Service: []spec.CandyService{{Name: "web", Exec: "/usr/bin/web"}}}
		got := ic.DetectCandyInit(ly, "/tmp")
		if len(got) != 1 || got[0] != "supervisord" {
			t.Errorf("DetectCandyInit(exec service) = %v, want [supervisord]", got)
		}
	})

	t.Run("candy_field gate: service participation requires the init to list candy_field: [service]", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {
				// no CandyFields — must NOT auto-detect off ly.Service even though the
				// ServiceSchema would otherwise match.
				ServiceSchema: &spec.InitServiceSchema{SupportsPackaged: true},
			},
		}}
		ly := &spec.CandyYAML{Service: []spec.CandyService{{Name: "web", UsePackaged: "nginx"}}}
		if got := ic.DetectCandyInit(ly, "/tmp"); len(got) != 0 {
			t.Errorf("DetectCandyInit without candy_field gate = %v, want empty", got)
		}
	})

	t.Run("candy_file glob match against the real candy dir", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "myapp.service"), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {CandyFiles: []string{"*.service"}},
		}}
		got := ic.DetectCandyInit(&spec.CandyYAML{}, dir)
		if len(got) != 1 || got[0] != "systemd" {
			t.Errorf("DetectCandyInit(candy_file glob) = %v, want [systemd]", got)
		}
	})

	t.Run("candy_file glob with no matching file detects nothing", func(t *testing.T) {
		dir := t.TempDir()
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {CandyFiles: []string{"*.service"}},
		}}
		if got := ic.DetectCandyInit(&spec.CandyYAML{}, dir); len(got) != 0 {
			t.Errorf("DetectCandyInit(no match) = %v, want empty", got)
		}
	})
}

func TestInitConfigResolveInitSystem(t *testing.T) {
	t.Run("nil receiver returns zero values", func(t *testing.T) {
		var nilIC *InitConfig
		name, def := nilIC.ResolveInitSystem(nil, nil, "")
		if name != "" || def != nil {
			t.Errorf("nil receiver: ResolveInitSystem() = (%q, %v), want (\"\", nil)", name, def)
		}
	})

	t.Run("explicit override selects among triggered inits", func(t *testing.T) {
		// An override SELECTS among the inits candies already trigger; it cannot
		// INTRODUCE one. Resolution must always name a key of ActiveInit, because
		// EmitInitAssembly enables system units through the resolved init only
		// (`if initName == img.InitSystem`) — a resolved name that is not active
		// matches nothing and silently enables no units at all.
		want := &ResolvedInit{Model: "explicit-def"}
		ic := &InitConfig{Init: map[string]*ResolvedInit{"systemd": want}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{hasInit: map[string]bool{"systemd": true}},
		}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "systemd")
		if name != "systemd" || def != want {
			t.Errorf("ResolveInitSystem(explicit) = (%q, %v), want (systemd, %v)", name, def, want)
		}
	})

	t.Run("explicit override naming an untriggered init falls through", func(t *testing.T) {
		// The contract narrowing this pins: previously an override consulted
		// ic.Init directly, so an operator could force an init NO candy triggers.
		// It now consults the triggered-and-capability-satisfied candidate set, so
		// an untriggered override falls through to auto-detect — the same treatment
		// an override naming an unknown init has always received (subtest below).
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd":     {},
			"supervisord": {},
		}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{hasInit: map[string]bool{"supervisord": true}},
		}
		name, _ := ic.ResolveInitSystem(layers, []string{"a"}, "systemd")
		if name != "supervisord" {
			t.Errorf("ResolveInitSystem(untriggered explicit) = %q, want supervisord (auto-detect)", name)
		}
	})

	t.Run("explicit override naming an unknown init falls through to auto-detect", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd": {},
		}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{hasInit: map[string]bool{"systemd": true}},
		}
		name, _ := ic.ResolveInitSystem(layers, []string{"a"}, "does-not-exist")
		if name != "systemd" {
			t.Errorf("ResolveInitSystem(unknown explicit) name = %q, want systemd (auto-detect fallback)", name)
		}
	})

	t.Run("bootc-flavored composition (preserve_user) prefers systemd over supervisord", func(t *testing.T) {
		systemdDef := &ResolvedInit{Model: "systemd-def"}
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd":     systemdDef,
			"supervisord": {Model: "supervisord-def"},
		}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{
				hasInit: map[string]bool{"systemd": true, "supervisord": true},
				caps:    &spec.CandyCapability{PreserveUser: true},
			},
		}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "")
		if name != "systemd" || def != systemdDef {
			t.Errorf("ResolveInitSystem(preserve_user) = (%q, %v), want systemd", name, def)
		}
	})

	t.Run("container composition (no preserve_user) prefers supervisord over other candidates", func(t *testing.T) {
		supervisordDef := &ResolvedInit{Model: "supervisord-def"}
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd":     {Model: "systemd-def"},
			"supervisord": supervisordDef,
		}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{hasInit: map[string]bool{"systemd": true, "supervisord": true}},
		}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "")
		if name != "supervisord" || def != supervisordDef {
			t.Errorf("ResolveInitSystem(container) = (%q, %v), want supervisord", name, def)
		}
	})

	t.Run("relay ports trigger any init system with a relay_template", func(t *testing.T) {
		relayDef := &ResolvedInit{RelayTemplate: "relay.tmpl"}
		ic := &InitConfig{Init: map[string]*ResolvedInit{"relay-init": relayDef}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{relayPorts: []int{8080}},
		}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "")
		if name != "relay-init" || def != relayDef {
			t.Errorf("ResolveInitSystem(relay ports) = (%q, %v), want relay-init", name, def)
		}
	})

	t.Run("capability requirement not met filters the init system out entirely", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"needs-gpu": {RequiresCapability: []string{"gpu"}},
		}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{hasInit: map[string]bool{"needs-gpu": true}}, // no gpu capability provided
		}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "")
		if name != "" || def != nil {
			t.Errorf("ResolveInitSystem(unmet capability) = (%q, %v), want (\"\", nil)", name, def)
		}
	})

	t.Run("capability requirement met keeps the init system in the candidate set", func(t *testing.T) {
		// AggregateCandyCapabilities only marks a capability "provided" via the fixed
		// booleans (preserve_user/needs_root_after_init/data_only) or an oci_labels/
		// init_system: key — there's no generic "declare arbitrary capability name"
		// field, so this exercises the real provider: a candy with PreserveUser=true
		// satisfies an init system whose RequiresCapability names "preserve_user".
		presUser := &ResolvedInit{RequiresCapability: []string{"preserve_user"}}
		ic := &InitConfig{Init: map[string]*ResolvedInit{"needs-preserve-user": presUser}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{
				hasInit: map[string]bool{"needs-preserve-user": true},
				caps:    &spec.CandyCapability{PreserveUser: true},
			},
		}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "")
		if name != "needs-preserve-user" || def != presUser {
			t.Errorf("ResolveInitSystem(met capability) = (%q, %v), want needs-preserve-user", name, def)
		}
	})

	t.Run("no init system detected returns zero values", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{"systemd": {}}}
		layers := map[string]spec.CandyReader{"a": &fakeCandyReader{}}
		name, def := ic.ResolveInitSystem(layers, []string{"a"}, "")
		if name != "" || def != nil {
			t.Errorf("ResolveInitSystem(nothing detected) = (%q, %v), want (\"\", nil)", name, def)
		}
	})
}

func TestInitConfigActiveInit(t *testing.T) {
	t.Run("nil receiver returns nil", func(t *testing.T) {
		var nilIC *InitConfig
		if got := nilIC.ActiveInit(nil, nil); got != nil {
			t.Errorf("nil receiver: ActiveInit() = %v, want nil", got)
		}
	})

	t.Run("multiple candies can activate multiple, DISTINCT init systems (bootc-flavored: systemd + supervisord)", func(t *testing.T) {
		systemdDef := &ResolvedInit{Model: "systemd-def"}
		supervisordDef := &ResolvedInit{Model: "supervisord-def"}
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"systemd":     systemdDef,
			"supervisord": supervisordDef,
		}}
		layers := map[string]spec.CandyReader{
			"sysd-candy": &fakeCandyReader{hasInit: map[string]bool{"systemd": true}},
			"sup-candy":  &fakeCandyReader{hasInit: map[string]bool{"supervisord": true}},
		}
		got := ic.ActiveInit(layers, []string{"sysd-candy", "sup-candy"})
		if len(got) != 2 || got["systemd"] != systemdDef || got["supervisord"] != supervisordDef {
			t.Errorf("ActiveInit() = %+v, want both systemd and supervisord active", got)
		}
	})

	t.Run("capability-gated init system is excluded from the active set when unmet", func(t *testing.T) {
		ic := &InitConfig{Init: map[string]*ResolvedInit{
			"needs-gpu": {RequiresCapability: []string{"gpu"}},
		}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{hasInit: map[string]bool{"needs-gpu": true}},
		}
		got := ic.ActiveInit(layers, []string{"a"})
		if len(got) != 0 {
			t.Errorf("ActiveInit(unmet capability) = %+v, want empty", got)
		}
	})

	t.Run("relay ports activate every init system carrying a relay_template", func(t *testing.T) {
		relayDef := &ResolvedInit{RelayTemplate: "relay.tmpl"}
		ic := &InitConfig{Init: map[string]*ResolvedInit{"relay-init": relayDef}}
		layers := map[string]spec.CandyReader{
			"a": &fakeCandyReader{relayPorts: []int{9000}},
		}
		got := ic.ActiveInit(layers, []string{"a"})
		if len(got) != 1 || got["relay-init"] != relayDef {
			t.Errorf("ActiveInit(relay ports) = %+v, want relay-init active", got)
		}
	})
}
