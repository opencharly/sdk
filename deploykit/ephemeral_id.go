package deploykit

// ephemeral_id.go — the genuinely pure helpers from charly/ephemeral_lifecycle.go (P13-KERNEL
// fold-in): id generation, naming-pattern rendering, and systemd unit-name sanitization have no
// registry or loader coupling at all. The REST of that file's functions — the read/write
// EphemeralRuntime persistence (deploykit.LoadBundleConfig is portable, but the WRITE half needs
// the registry-coupled marshalDeployNode callback that resugars plan steps from the plugin-primaries
// registry — the same node-form marshal the deploy-state writes require),
// registerTransientTimer/cancelTransientTimer (systemd-run / systemctl self-exec), and
// teardownChildrenRec's nested `charly bundle del` self-exec — are genuinely host-only leaves,
// registered FINAL/K5 credential/loader-family inventory (the SAME pattern the former
// layer_secrets.go's ensureCandySecret documented — now sdk/deploykit/secret_candy_resolve.go's
// EnsureCandySecret, #55 coneB-br2 relocated the resolver plugin-side and DELETED layer_secrets.go),
// not moved here.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"text/template"
)

// RenderNamingPattern fills in {{.Source}} and {{.UUID6}} variables.
func RenderNamingPattern(pattern, source, id string) (string, error) {
	t, err := template.New("naming").Parse(pattern)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, struct {
		Source string
		UUID6  string
	}{Source: source, UUID6: id})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// NewEphemeralID returns six characters of cryptographically-strong
// random hex. Six characters is 24 bits of entropy — enough to make
// concurrent collisions vanishingly rare for a per-deploy lifecycle.
func NewEphemeralID() (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// SanitizeUnitName makes a string safe for systemd unit naming
// (replaces / and . with -).
func SanitizeUnitName(s string) string {
	r := strings.ReplaceAll(s, "/", "-")
	r = strings.ReplaceAll(r, ".", "-")
	return r
}
