package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/spec/spec"
)

// TestEmitInitAssemblyEnablesOnlyResolvedInit pins the rule that a distro-shipped
// use_packaged: unit is enabled by the image's RESOLVED init system alone, never by
// every ACTIVE init.
//
// activeInits is a SET — a candy chain routinely contributes fragments for several
// inits at once. Rendering the shared unit list through each of them emitted one
// enable command PER init into a single Containerfile, so a supervisord container
// that happened to carry both an openrc and a systemd fragment received
// `rc-update add` AND `systemctl enable` for the same units, and the build died
// with `rc-update: command not found` on a base that has neither.
//
// This test FAILS against the unnarrowed generator: the supervisord case sees both
// foreign enable commands.
func TestEmitInitAssemblyEnablesOnlyResolvedInit(t *testing.T) {
	openrc := &spec.ResolvedInit{
		SystemEnableTemplate: "RUN{{range $i, $unit := .Units}}{{if $i}} && {{else}} {{end}}rc-update add {{$unit}} default{{end}}\n",
	}
	systemd := &spec.ResolvedInit{
		SystemEnableTemplate: "RUN{{range $i, $unit := .Units}}{{if $i}} && {{else}} {{end}}systemctl enable {{$unit}}{{end}}\n",
	}
	// supervisord declares NO system_enable_template — the correct emission for a
	// supervisord container is no enable command at all.
	supervisord := &spec.ResolvedInit{}
	// Both foreign inits also carry a post-assembly step, so the test below can prove the
	// enable guard did not swallow the rest of the loop body.
	openrc.PostAssemblyTemplate = "RUN openrc-post-assembly\n"
	systemd.PostAssemblyTemplate = "RUN bootc container lint\n"

	activeInits := map[string]*spec.ResolvedInit{
		"supervisord": supervisord,
		"openrc":      openrc,
		"systemd":     systemd,
	}

	g := &Generator{
		Candies: map[string]CandyModel{
			"virt": NewSpecCandyModel(spec.CandyModel{
				Name: "virt",
				Service: []spec.ServiceEntry{
					{Name: "virtqemud", UsePackaged: "virtqemud.socket", Scope: "system"},
				},
			}, spec.CandyView{}),
		},
	}

	for _, tc := range []struct {
		resolved string
		want     []string
		reject   []string
	}{
		// The agentteams-worker case: a supervisord container on an Arch base.
		{resolved: "supervisord", reject: []string{"rc-update", "systemctl enable"}},
		{resolved: "systemd", want: []string{"systemctl enable virtqemud.socket"}, reject: []string{"rc-update"}},
		{resolved: "openrc", want: []string{"rc-update add virtqemud.socket"}, reject: []string{"systemctl enable"}},
	} {
		t.Run(tc.resolved, func(t *testing.T) {
			img := &buildkit.ResolvedBox{InitSystem: tc.resolved}
			var b strings.Builder
			if err := g.EmitInitAssembly(&b, img, []string{"virt"}, activeInits, map[string]bool{}); err != nil {
				t.Fatalf("EmitInitAssembly() error = %v", err)
			}
			got := b.String()
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("resolved init %q: missing %q in:\n%s", tc.resolved, w, got)
				}
			}
			for _, r := range tc.reject {
				if strings.Contains(got, r) {
					t.Errorf("resolved init %q: emitted foreign init's enable command %q in:\n%s", tc.resolved, r, got)
				}
			}
		})
	}
}

// TestEmitInitAssemblyKeepsPostAssemblyPerInit pins the half the enable guard must NOT
// change: assembly and post-assembly stay per-ACTIVE-init.
//
// This exists because the guard was first written as `continue`, which would also have
// skipped the post_assembly_template emitted below it — and the entire deploykit suite
// stayed green with that bug in place, because nothing else in the package references
// PostAssemblyTemplate. Reading the code confirmed the wrap; nothing FAILED on the
// short-circuit. This test does.
func TestEmitInitAssemblyKeepsPostAssemblyPerInit(t *testing.T) {
	openrc := &spec.ResolvedInit{
		SystemEnableTemplate: "RUN rc-update add x default\n",
		PostAssemblyTemplate: "RUN openrc-post-assembly\n",
	}
	systemd := &spec.ResolvedInit{
		SystemEnableTemplate: "RUN systemctl enable x\n",
		PostAssemblyTemplate: "RUN bootc container lint\n",
	}
	activeInits := map[string]*spec.ResolvedInit{
		"supervisord": {},
		"openrc":      openrc,
		"systemd":     systemd,
	}

	g := &Generator{
		Candies: map[string]CandyModel{
			"virt": NewSpecCandyModel(spec.CandyModel{
				Name: "virt",
				Service: []spec.ServiceEntry{
					{Name: "virtqemud", UsePackaged: "virtqemud.socket", Scope: "system"},
				},
			}, spec.CandyView{}),
		},
	}

	// Resolved init is supervisord, so NEITHER foreign init may contribute an enable
	// command — but BOTH must still contribute their post-assembly step.
	img := &buildkit.ResolvedBox{InitSystem: "supervisord"}
	var b strings.Builder
	if err := g.EmitInitAssembly(&b, img, []string{"virt"}, activeInits, map[string]bool{}); err != nil {
		t.Fatalf("EmitInitAssembly() error = %v", err)
	}
	got := b.String()

	for _, want := range []string{"openrc-post-assembly", "bootc container lint"} {
		if !strings.Contains(got, want) {
			t.Errorf("post-assembly for a non-resolved active init was dropped: missing %q in:\n%s", want, got)
		}
	}
	for _, reject := range []string{"rc-update", "systemctl enable"} {
		if strings.Contains(got, reject) {
			t.Errorf("enable command %q leaked from a non-resolved init in:\n%s", reject, got)
		}
	}
}
