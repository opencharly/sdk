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
