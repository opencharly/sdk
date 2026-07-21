package deploykit

// deploy_host_helpers.go — package-level deploy helpers shared by the SURVIVING deploy
// paths (the external local/vm/pod substrates, all routed through externalDeployTarget)
// after the in-proc local deploy target externalized into candy/plugin-deploy-local.
// Relocated from charly/deploy_host_helpers.go + charly/deploy_add_shared.go (P13-KERNEL,
// the 4/5 sdk lift): these bodies have no charly-core (registry/loader) dependency once
// RemoveEnvdFile is threaded as an injected seam (below) — everything else is kit/spec/
// buildkit types and pure logic.

import (
	"fmt"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
)

// RenderHostPackageCommand renders the host-venue package-install command for a
// SystemPackagesStep from the format's phase.install.host cell in the embedded vocabulary —
// the SAME PhaseTemplate + NewInstallContext + RenderTemplate path deploykit.OCITarget uses for the
// container venue (R3). No hardcoded dnf/apt/pacman dispatch: the format selects the
// template; the command is config-driven.
//
// Returns ("", nil) when the step is not an install-phase step, has no packages, or the
// format declares no host cell — all "nothing to run", not errors. A missing DistroConfig /
// format definition IS an error (the deploy can't honor a package step it can't render).
func RenderHostPackageCommand(distroCfg *buildkit.DistroConfig, s *SystemPackagesStep) (string, error) {
	if s.Phase != spec.PhaseInstall || len(s.Packages) == 0 {
		return "", nil
	}
	if distroCfg == nil {
		return "", fmt.Errorf("no distro config for format %q host install", s.Format)
	}
	formatDef := distroCfg.FindFormat(s.Format)
	if formatDef == nil {
		return "", fmt.Errorf("no format %q in distro config", s.Format)
	}
	tmpl := buildkit.FormatPhaseTemplate(formatDef, spec.PhaseInstall, spec.VenueHostNative)
	if tmpl == "" {
		return "", nil // no host cell for this format → skip
	}
	ctx := buildkit.NewInstallContext(s.RawInstallContext, formatDef.CacheMount)
	cmd, err := buildkit.RenderTemplate(s.Format+"-host-install", tmpl, ctx)
	if err != nil {
		return "", fmt.Errorf("rendering %s host install template: %w", s.Format, err)
	}
	return strings.TrimSpace(cmd), nil
}

// HostReverseExec is the ReverseExecutor adapter combining a teardown's gate flags with a
// per-call DryRun + ReverseRunner. Used by the host-teardown path (externalDeployTarget.Del
// for the local/external substrate).
type HostReverseExec struct {
	DryRun          bool
	KeepRepoChanges bool
	KeepServices    bool
	Runner          kit.ReverseRunner
}

func (e *HostReverseExec) ReverseDryRun() bool              { return e.DryRun }
func (e *HostReverseExec) ReverseKeepRepoChanges() bool     { return e.KeepRepoChanges }
func (e *HostReverseExec) ReverseKeepServices() bool        { return e.KeepServices }
func (e *HostReverseExec) ReverseRunner() kit.ReverseRunner { return e.Runner }

// RemoveEnvdFile is an injected seam: charly core's env.d file removal
// (shell_profile.go, a pure filesystem helper) is the ONE non-portable leaf
// TeardownHostDeploy calls — set by charly core's init(). A nil func (a caller that never
// wires it) is a no-op, matching the original "_ = RemoveEnvdFile(...)" best-effort
// semantics (the error was always discarded).
var RemoveEnvdFile = func(hostHome, candyName string) error { return nil }

// TeardownHostDeploy reverses a single host/external deploy record: for each candy whose
// refcount drops to zero it replays the recorded ReverseOps, removes the env.d file, and
// deletes the candy record; then deletes the deploy record. Only RECORDED ops are
// replayed (record-and-replay). Shared by externalDeployTarget.Del (the local/external
// host-venue teardown).
func TeardownHostDeploy(paths *kit.LedgerPaths, rec *kit.DeployRecord, hostHome string, re kit.ReverseExecutor) error {
	for _, layer := range rec.Candy {
		candyRec, shouldRemove, err := kit.RemoveCandyDeployment(paths, layer, rec.DeployID)
		if err != nil {
			return err
		}
		if !shouldRemove {
			continue
		}
		kit.RunReverseOps(candyRec.ReverseOps, re)
		_ = RemoveEnvdFile(hostHome, layer)
		if err := kit.DeleteCandyRecord(paths, layer); err != nil {
			return err
		}
	}
	return kit.DeleteDeployRecord(paths, rec.DeployID)
}

// BuildArtifactEnv composes the env used for candy-artifact path
// substitution: the resolved secret env first, then the deploy node's
// own env: lines overlaid (last-wins). nil node contributes nothing.
//
// Shared by the local deploy target.Add / the vm deploy's Add path — both feed it
// to RetrieveCandyArtifacts so rewrite rules like ${K3S_KUBECONFIG_SERVER}
// resolve to the declared value rather than a literal placeholder. The
// node is the dispatch-merged BundleNode (never re-read from disk).
func BuildArtifactEnv(secretEnv map[string]string, node *spec.BundleNode) map[string]string {
	env := make(map[string]string, len(secretEnv))
	for k, v := range secretEnv {
		env[k] = v
	}
	if node != nil {
		for _, line := range node.Env {
			if idx := strings.Index(line, "="); idx > 0 {
				env[line[:idx]] = line[idx+1:]
			}
		}
	}
	return env
}

// CandyArtifactRegisters returns the DISTINCT `register:` hints declared across every
// candy's artifact list — name-blind (it reads each artifact's own declaration, never a
// candy name). The declaration-reading half of the k3s-server artifact-declaration-driven
// dispatch fix (P13-KERNEL); the WORD-KEYED HANDLER TABLE (function pointers into
// charly-core-only bodies like K3sPostProvision) stays host-side as the thin dispatch
// anchor — charly/deploy_add_shared.go's artifactRegisterHandlers, never here.
func CandyArtifactRegisters(layers []spec.CandyReader) map[string]bool {
	out := map[string]bool{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		for _, a := range layer.Artifact() {
			if a.Register != "" {
				out[a.Register] = true
			}
		}
	}
	return out
}
