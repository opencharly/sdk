package deploykit

// sidecar_resolve.go — the pure host-adapter + quadlet-sweep helpers for pod
// sidecars (K4: relocated from charly/sidecar.go, the genuinely pure quarter
// of that file with no project-loader or reverse-channel dependency).
// resolveSidecarsViaPlugin and embeddedSidecarBodies stay in charly-core:
// the former needs an active Executor (hostInvoke), the latter reads the
// binary-embedded default config through the unified loader.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencharly/sdk/spec"
)

// ResolvedSidecarFromSpec adapts one plugin-resolved spec.ResolvedSidecar into
// the host's ResolvedSidecar (the quadlet-gen shape).
func ResolvedSidecarFromSpec(s spec.ResolvedSidecar) ResolvedSidecar {
	rs := ResolvedSidecar{Name: s.Name, Image: s.Image, Env: s.Env}
	if s.Security != nil {
		rs.Security = *s.Security
	}
	for _, v := range s.Volume {
		rs.Volume = append(rs.Volume, VolumeMount(v))
	}
	for _, sec := range s.Secret {
		rs.Secret = append(rs.Secret, CollectedSecret{
			Name:       sec.Name,
			Env:        sec.Env,
			HostEnv:    sec.HostEnv,
			SecretName: sec.SecretName,
		})
	}
	return rs
}

// SidecarTemplatesOf returns the project-root sidecar templates carried by a
// deploy config (nil-safe), as OPAQUE bodies. These extend/override the
// embedded set inside the sidecar plugin's OpResolve.
func SidecarTemplatesOf(dc *BundleConfig) map[string]json.RawMessage {
	if dc == nil {
		return nil
	}
	return dc.Sidecar
}

// HasTailscaleSidecar reports whether a name-keyed sidecar map (opaque
// bodies) attaches the tailscale sidecar — a pure key-existence check.
func HasTailscaleSidecar(sidecars map[string]json.RawMessage) bool {
	_, ok := sidecars["tailscale"]
	return ok
}

// FindPodSidecarQuadlets returns the .container quadlets in qdir that belong
// to the pod podName, identified by the load-bearing `Pod=<podName>.pod`
// directive inside the quadlet's [Container] section. Filename-prefix
// matching is NOT used because it collides with sibling instances of the
// same image (e.g. charly-versa-ecovoyage.container is an instance of versa,
// NOT a sidecar of pod charly-versa.pod). Only true pod members carry the
// Pod= directive — sibling instances and standalone container deploys do
// not. mainContainerFile (typically the main pod container's quadlet
// filename) is excluded from the returned list because its lifecycle is
// owned by the caller's main systemctl disable, not by the sidecar sweep.
func FindPodSidecarQuadlets(qdir, podName, mainContainerFile string) ([]string, error) {
	expected := fmt.Sprintf("Pod=%s.pod", podName)
	entries, err := os.ReadDir(qdir)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".container") {
			continue
		}
		if name == mainContainerFile {
			continue
		}
		content, rErr := os.ReadFile(filepath.Join(qdir, name))
		if rErr != nil {
			continue
		}
		for line := range strings.SplitSeq(string(content), "\n") {
			if strings.TrimSpace(line) == expected {
				matches = append(matches, name)
				break
			}
		}
	}
	sort.Strings(matches)
	return matches, nil
}
