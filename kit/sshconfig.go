package kit

// sshconfig.go — the managed ssh-config fragment for VM aliases (moved from charly core so the
// externalized candy/plugin-deploy-vm, which runs co-located on the host, publishes the stanza
// itself). `charly vm create` / the vm deploy lifecycle writes one Host stanza per VM into a managed
// file at ~/.config/charly/ssh_config, fenced by the shared `# opencharly:begin`/`# opencharly:end`
// markers (profile.go). The user's ~/.ssh/config gains a single managed `Include` line pointing at
// the fragment. After this, `ssh charly-<vmname>` works from any terminal, and an SSHExecutor built
// with just `Host: "charly-"+vmName` lets ssh-config supply User/Port/Key. Idempotent.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/opencharly/spec/spec"
)

// VmSshStanza captures the fields to render one ssh-config Host stanza for a VM.
type VmSshStanza struct {
	Alias          string // ssh-config Host alias (e.g. "charly-arch-vm"); unique within the fragment
	Hostname       string // IP/DNS ssh connects to ("127.0.0.1" for user-mode networking)
	Port           int    // host-side port forwarded to the guest's :22
	User           string // guest account ssh logs in as
	IdentityFile   string // absolute private-key path
	KnownHostsFile string // absolute per-VM known_hosts path
}

// SshFragmentPath returns ~/.config/charly/ssh_config for the user's home dir.
func SshFragmentPath(home string) string {
	return filepath.Join(home, ".config", "charly", "ssh_config")
}

// SshConfigPath returns ~/.ssh/config for the user's home dir.
func SshConfigPath(home string) string {
	return filepath.Join(home, ".ssh", "config")
}

// EnsureSshConfigInclude inserts the managed `Include ~/.config/charly/ssh_config` directive at the
// TOP of the user's ~/.ssh/config (creating it if needed) — the Include MUST be outside any Host
// block (ssh_config(5) lexical scoping), so it is PREPENDED. Idempotent.
func EnsureSshConfigInclude(home string) error {
	cfgPath := SshConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("Include %s", SshFragmentPath(home))
	var existing string
	if data, err := os.ReadFile(cfgPath); err == nil {
		existing = string(data)
	}
	updated := ReplaceOrPrependManagedBlock(existing, body, "")
	return os.WriteFile(cfgPath, []byte(updated), 0o600)
}

// RemoveSshConfigInclude removes the managed Include line from ~/.ssh/config. Idempotent.
func RemoveSshConfigInclude(home string) error {
	cfgPath := SshConfigPath(home)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stripped := StripManagedBlock(string(data), "")
	if strings.TrimSpace(stripped) == "" {
		return os.Remove(cfgPath)
	}
	if stripped == string(data) {
		return nil
	}
	return os.WriteFile(cfgPath, []byte(stripped), 0o600)
}

// WriteVmSshStanza adds (or replaces) a Host stanza in the managed fragment. Idempotent.
func WriteVmSshStanza(home string, s VmSshStanza) error {
	if s.Alias == "" {
		return fmt.Errorf("ssh stanza: empty alias")
	}
	if s.Hostname == "" || s.IdentityFile == "" {
		return fmt.Errorf("ssh stanza %q: hostname and identity_file required", s.Alias)
	}
	frag := SshFragmentPath(home)
	if err := os.MkdirAll(filepath.Dir(frag), 0o700); err != nil {
		return err
	}
	release, stanzas, err := lockStanzas(frag)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	stanzas[s.Alias] = renderStanza(s)
	return writeStanzasLocked(frag, stanzas)
}

// RemoveVmSshStanza drops the named alias and returns the remaining count (0 → the caller also
// removes the Include). Idempotent.
func RemoveVmSshStanza(home string, alias string) (remaining int, err error) {
	frag := SshFragmentPath(home)
	if _, err := os.Stat(frag); os.IsNotExist(err) {
		return 0, nil
	}
	release, stanzas, err := lockStanzas(frag)
	if err != nil {
		return 0, err
	}
	defer func() { _ = release() }()
	if _, ok := stanzas[alias]; !ok {
		return len(stanzas), nil
	}
	delete(stanzas, alias)
	if len(stanzas) == 0 {
		_ = os.Remove(frag)
		return 0, nil
	}
	return len(stanzas), writeStanzasLocked(frag, stanzas)
}

// ListVmSshAliases returns the alias names in the managed fragment, sorted.
func ListVmSshAliases(home string) ([]string, error) { //nolint:unparam // error kept for API stability
	frag := SshFragmentPath(home)
	if _, err := os.Stat(frag); os.IsNotExist(err) {
		return nil, nil
	}
	stanzas := loadStanzas(frag)
	out := make([]string, 0, len(stanzas))
	for k := range stanzas {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// VmSshAlias returns the canonical alias for a VM deployment name ("charly-" namespaced).
// The pure naming leaf now lives in package spec (a peer of VmDomainIdentity — the always-floor-
// legal wire/vocabulary home, #55 K4); kit re-exports it so the host-coupled ssh-config-fragment
// writers here (renderStanza/VmSshStanza/SshFragmentPath, which STAY in kit — they write
// ~/.config/charly/ssh_config) reference `kit.VmSshAlias` unchanged.
var VmSshAlias = spec.VmSshAlias

func renderStanza(s VmSshStanza) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Host %s\n", s.Alias)
	fmt.Fprintf(&sb, "    Hostname %s\n", s.Hostname)
	if s.Port > 0 {
		fmt.Fprintf(&sb, "    Port %d\n", s.Port)
	}
	if s.User != "" {
		fmt.Fprintf(&sb, "    User %s\n", s.User)
	}
	fmt.Fprintf(&sb, "    IdentityFile %s\n", s.IdentityFile)
	fmt.Fprintf(&sb, "    StrictHostKeyChecking accept-new\n")
	if s.KnownHostsFile != "" {
		fmt.Fprintf(&sb, "    UserKnownHostsFile %s\n", s.KnownHostsFile)
	}
	return sb.String()
}

var stanzaHostRegex = regexp.MustCompile(`(?m)^Host\s+(\S+)`)

func loadStanzas(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	body := ManagedBody(string(data))
	if body == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	matches := stanzaHostRegex.FindAllStringSubmatchIndex(body, -1)
	for i, m := range matches {
		alias := body[m[2]:m[3]]
		start := m[0]
		end := len(body)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out[alias] = body[start:end]
	}
	return out
}

// lockStanzas acquires the fragment's cross-process file lock and loads the current stanza
// set UNDER it, so a caller's read-modify-write is atomic against concurrent writers/removers.
// The returned release MUST be deferred and called AFTER any writeStanzasLocked.
func lockStanzas(path string) (release func() error, stanzas map[string]string, err error) {
	release, err = AcquireFileLock(path+".lock", true)
	if err != nil {
		return nil, nil, err
	}
	return release, loadStanzas(path), nil
}

// writeStanzasLocked writes the EXACT stanza set — deliberately NO re-merge against a fresh
// disk read. The caller already holds the lock AND loaded under it, so the concurrency guarantee
// (measured: the 16-lane check-run stalls where a lost ssh alias hangs that lane's executor dial)
// is preserved by loading under the lock, never by re-merging without it. Re-merging was the bug:
// RemoveVmSshStanza deletes an alias, then a fresh-load upsert re-added it from disk whenever any
// sibling stanza remained — so `charly vm destroy` reported success yet left the alias behind.
// Only the single-stanza `len==0 → os.Remove` arm hid it.
func writeStanzasLocked(path string, stanzas map[string]string) error {
	keys := make([]string, 0, len(stanzas))
	for k := range stanzas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.TrimRight(stanzas[k], "\n"))
		sb.WriteString("\n")
	}
	body := sb.String()
	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	updated := ReplaceOrAppendManagedBlock(existing, strings.TrimRight(body, "\n"), "")
	return os.WriteFile(path, []byte(updated), 0o600)
}
