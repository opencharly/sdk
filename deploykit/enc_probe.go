package deploykit

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

// enc_probe.go — the encrypted-volume STATE-PROBE + plan-building functions relocated from
// charly/enc.go (Cutover B unit 2). These are genuinely portable (no provider-registry or
// credential-store coupling — pure FS probes, exec.Command wrappers, and charly.yml reads
// via the SAME deploykit.LoadBundleConfig every deploy-config consumer already uses), so they
// move here rather than staying behind a HostBuild seam. What does NOT move (charly/enc.go,
// STILL core): encExecViaPlugin (verb:enc InvokeProvider dispatch) and the credential-store
// family (resolveEncPassphrase*/awaitKeyringUnlockViaPlugin — registered FINAL/K5 inventory,
// blocked on an InvokeProvider lazy-connect enabler for verb:credential). candy/plugin-deploy-pod
// had ALREADY hand-duplicated 3 of these (isEncryptedMountedLocal/cipherPopulatedPlainEmptyLocal/
// verifyBindMountsLocal in config_setup_helpers.go) because the originals weren't movable as a
// whole — those duplicates are deleted in the same cutover, now calling this package instead (R3).

// ResolveEncVolumeDir returns the volume directory for an encrypted volume.
// If the volume has an explicit Host path, use it directly.
// Otherwise, use the global default: <storagePath>/charly-<image>-<name>.
func ResolveEncVolumeDir(vol vmshared.DeployVolumeConfig, defaultStoragePath, boxName string) string {
	if vol.Host != "" {
		return kit.ExpandHostHome(vol.Host)
	}
	return filepath.Join(defaultStoragePath, EncryptedVolumeName(boxName, vol.Name))
}

// IsEncryptedInitialized checks if gocryptfs has been initialized (gocryptfs.conf exists).
func IsEncryptedInitialized(cipherDir string) bool {
	_, err := os.Stat(filepath.Join(cipherDir, "gocryptfs.conf"))
	return err == nil
}

// IsEncryptedMounted checks if the plain dir is a FUSE mount by reading /proc/mounts.
// Package-level var for testability.
var IsEncryptedMounted = DefaultIsEncryptedMounted

func DefaultIsEncryptedMounted(plainDir string) bool {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck

	// Resolve symlinks for comparison
	resolved, err := filepath.EvalSymlinks(plainDir)
	if err != nil {
		resolved = plainDir
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 {
			mountPoint, err := filepath.EvalSymlinks(fields[1])
			if err != nil {
				mountPoint = fields[1]
			}
			if mountPoint == resolved && fields[2] == "fuse.gocryptfs" {
				return true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: /proc/mounts scan error: %v\n", err)
	}
	return false
}

// EncPlanFor host-prelifts the per-volume gocryptfs execution plan for the given
// box/instance, filtered to `volume` when non-empty. It loads the deploy config
// (LoadEncryptedVolume), resolves each volume's cipher/plain dirs
// (ResolveEncVolumeDir), and probes the initialized/mounted state
// (IsEncryptedInitialized/IsEncryptedMounted). scopeDir is the scope-unit
// directory component: DeployStorageDir(box,instance) for mount/unmount/passwd, or the
// bare box name for the ensure path (a pre-existing derivation difference, identical
// for the common empty-instance case, preserved exactly by this cutover). The result
// is the self-contained plan candy/plugin-enc executes over OpExecute.
func EncPlanFor(boxName, instance, volume, scopeDir string) ([]spec.EncVolumePlan, error) {
	mounts, storagePath, err := LoadEncryptedVolume(boxName, instance)
	if err != nil {
		return nil, err
	}
	storageDir := DeployStorageDir(boxName, instance)
	var plan []spec.EncVolumePlan
	for _, m := range mounts {
		if volume != "" && m.Name != volume {
			continue
		}
		volDir := ResolveEncVolumeDir(m, storagePath, storageDir)
		cipherDir := filepath.Join(volDir, "cipher")
		plainDir := filepath.Join(volDir, "plain")
		plan = append(plan, spec.EncVolumePlan{
			Name:        m.Name,
			CipherDir:   cipherDir,
			PlainDir:    plainDir,
			ScopeUnit:   fmt.Sprintf("charly-enc-%s-%s", scopeDir, m.Name),
			Initialized: IsEncryptedInitialized(cipherDir),
			Mounted:     IsEncryptedMounted(plainDir),
		})
	}
	return plan, nil
}

// FuseConfPath is the fuse.conf location; a package var so tests point it elsewhere.
var FuseConfPath = "/etc/fuse.conf"

// FuseAllowOtherEnabled reports whether fuse.conf has an ACTIVE (uncommented, value-less)
// `user_allow_other` line. Every charly encrypted-volume mount uses `gocryptfs -allow_other`
// (so rootless podman keep-id can reach the FUSE plain dir), and fusermount3 REFUSES
// -allow_other unless this option is set. An absent/unreadable file counts as not enabled.
func FuseAllowOtherEnabled() bool {
	data, err := os.ReadFile(FuseConfPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "user_allow_other" {
			return true
		}
	}
	return false
}

// LoadEncryptedVolume loads encrypted volume configs from charly.yml for an image.
// Returns the deploy volume configs with type=encrypted and the encrypted storage path.
func LoadEncryptedVolume(boxName, instance string) ([]vmshared.DeployVolumeConfig, string, error) {
	rt, err := kit.ResolveRuntime()
	if err != nil {
		return nil, "", err
	}

	// Propagate LoadBundleConfig errors instead of swallowing them. A
	// schema error (e.g. the 2026-05-12 require-image cutover rejecting
	// pre-cutover deploy.yml entries) used to silently degrade to "no
	// encrypted volumes", which broke the encMount short-circuit and
	// drove the call into resolveEncPassphraseForMount → systemd-ask-
	// password → indefinite hang waiting for stdin. Surfacing the error
	// turns that hang into a clean error message with a remediation
	// hint pointing at `charly migrate`.
	dc, err := LoadBundleConfig()
	if err != nil {
		return nil, "", fmt.Errorf("loading deploy config for encrypted volumes: %w", err)
	}
	if dc == nil {
		return nil, rt.EncryptedStoragePath, nil
	}

	overlay, ok := dc.Bundle[DeployKey(boxName, instance)]
	if !ok {
		return nil, rt.EncryptedStoragePath, nil
	}

	var encrypted []vmshared.DeployVolumeConfig
	for _, dv := range overlay.Volume {
		if dv.Type == "encrypted" {
			encrypted = append(encrypted, dv)
		}
	}
	return encrypted, rt.EncryptedStoragePath, nil
}

// EncServiceFilename returns the systemd service filename for a legacy crypto companion unit.
// Used only for cleanup of stale enc services from older charly versions.
func EncServiceFilename(boxName string) string {
	return kit.ContainerName(boxName) + "-enc.service"
}

// RemoveEncryptedVolumes deletes the gocryptfs cipher/plain dirs for the deploy's
// encrypted volumes at `charly remove --purge`. removeVolumes handles named podman
// volumes, but an encrypted volume is a filesystem directory under the encrypted
// storage path, NOT a podman volume — so without this a purged disposable enc bed
// leaves an orphaned, credential-less cipher dir behind, and the next deploy fails
// to mount it ("cipher: message authentication failed", the fresh passphrase no
// longer matching the stale master key).
//
// It enumerates the on-disk dirs by the deploy's `charly-<storageDir>-` prefix
// (the same per-deploy prefix removeVolumes filters podman volumes by) rather than
// via LoadEncryptedVolume, so it works even when the deploy config is already gone
// (the orphaned-after-a-crash case is exactly when the dir persists). Each mount
// is unmounted best-effort before removal; a purge never hard-fails on cleanup.
func RemoveEncryptedVolumes(boxName, instance string) {
	rt, err := kit.ResolveRuntime()
	if err != nil {
		return
	}
	base := rt.EncryptedStoragePath
	prefix := "charly-" + DeployStorageDir(boxName, instance) + "-"
	entries, err := os.ReadDir(base)
	if err != nil {
		return // no encrypted storage dir — nothing to purge
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		volName := strings.TrimPrefix(e.Name(), prefix)
		volDir := filepath.Join(base, e.Name())
		plainDir := filepath.Join(volDir, "plain")
		// Unmount the FUSE plain dir first — a mounted gocryptfs cannot be cleanly
		// deleted. fusermount3/fusermount is the OS FUSE-teardown tool (the same
		// mechanism candy/plugin-enc uses internally); the plugin path needs live
		// deploy state, which the orphan case lacks, so unmount directly here.
		if IsEncryptedMounted(plainDir) {
			if fuErr := exec.Command("fusermount3", "-u", plainDir).Run(); fuErr != nil {
				_ = exec.Command("fusermount", "-u", plainDir).Run()
			}
		}
		if rmErr := os.RemoveAll(volDir); rmErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: removing encrypted volume dir %s: %v\n", volDir, rmErr)
		} else {
			fmt.Fprintf(os.Stderr, "Removed encrypted volume %s\n", volName)
		}
	}
}

// EncStatus prints the status of encrypted bind mounts for an image.
func EncStatus(boxName, instance string) error {
	mounts, storagePath, err := LoadEncryptedVolume(boxName, instance)
	if err != nil {
		return err
	}

	if len(mounts) == 0 {
		fmt.Println("No encrypted bind mounts configured")
		return nil
	}

	fmt.Printf("%-20s %-12s %-8s %s\n", "NAME", "INITIALIZED", "MOUNTED", "PATH")
	for _, m := range mounts {
		volDir := ResolveEncVolumeDir(m, storagePath, DeployStorageDir(boxName, instance))
		cipherDir := filepath.Join(volDir, "cipher")
		plainDir := filepath.Join(volDir, "plain")

		initialized := "no"
		if IsEncryptedInitialized(cipherDir) {
			initialized = "yes"
		}
		mounted := "no"
		if IsEncryptedMounted(plainDir) {
			mounted = "yes"
		}
		fmt.Printf("%-20s %-12s %-8s %s\n", m.Name, initialized, mounted, plainDir)
	}
	return nil
}

// HasEncryptedBindMounts returns true if any bind mount is encrypted.
func HasEncryptedBindMounts(mounts []ResolvedBindMount) bool {
	for _, m := range mounts {
		if m.Encrypted {
			return true
		}
	}
	return false
}

// VerifyBindMounts checks that all bind mounts are ready to use:
// - Plain mounts: host directory must exist
// - Encrypted mounts: must be mounted (FUSE)
//
// For encrypted mounts where FUSE is unmounted, an extra discrimination
// fires: if the cipher dir on disk holds real encrypted data (anything
// beyond the gocryptfs metadata files) AND the plain mount target is
// empty, we surface a louder error spelling out the data-loss risk.
// That's the immich-2026-04-incident shape — quadlet without ExecStartPre,
// FUSE never re-mounted after a reboot, container about to bind an empty
// plain/ over a populated cipher tree and start writing plaintext on top.
// The previous generic "not mounted" message was indistinguishable from
// a fresh-setup state where no harm exists yet.
func VerifyBindMounts(mounts []ResolvedBindMount, boxName string) error { //nolint:unparam // boxName names the offending box in the "charly config mount %s" hint text; today's callers are all test literals, but the param is genuinely load-bearing for the error message
	for _, m := range mounts {
		if m.Encrypted {
			if !IsEncryptedMounted(m.HostPath) {
				cipherDir := filepath.Join(filepath.Dir(m.HostPath), "cipher")
				if CipherPopulatedPlainEmpty(cipherDir, m.HostPath) {
					return fmt.Errorf(
						"encrypted volume %q: cipher dir at %s is populated but plain mount at %s is empty — refusing to start (would write plaintext over encrypted data); run 'charly config mount %s' first",
						m.Name, cipherDir, m.HostPath, boxName,
					)
				}
				return fmt.Errorf("encrypted bind mount %q for image %q is not mounted; run 'charly config mount %s' first", m.Name, boxName, boxName)
			}
		} else {
			info, err := os.Stat(m.HostPath)
			if err != nil {
				return fmt.Errorf("bind mount %q: host path %q: %w", m.Name, m.HostPath, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("bind mount %q: host path %q is not a directory", m.Name, m.HostPath)
			}
		}
	}
	return nil
}

// CipherPopulatedPlainEmpty reports whether the gocryptfs cipher directory
// holds user data (anything beyond the gocryptfs.conf + gocryptfs.diriv
// metadata files) AND the plain mount target is empty. The combination
// means FUSE is unmounted on top of a populated vault — letting a
// container start now would silently bind the empty plain/ as a plaintext
// directory and write new data on top of the encrypted tree.
//
// Returns false on stat errors (the surrounding error path will surface
// those — this helper is only a discrimination hint).
func CipherPopulatedPlainEmpty(cipherDir, plainDir string) bool {
	plainEntries, err := os.ReadDir(plainDir)
	if err != nil || len(plainEntries) > 0 {
		return false
	}
	cipherEntries, err := os.ReadDir(cipherDir)
	if err != nil {
		return false
	}
	for _, e := range cipherEntries {
		switch e.Name() {
		case "gocryptfs.conf", "gocryptfs.diriv":
			continue
		}
		return true
	}
	return false
}

// AskPassword prompts for a password using systemd-ask-password.
// id is a unique identifier for kernel keyring caching, prompt is shown to the user.
// Package-level var for testability.
var AskPassword = DefaultAskPassword

func DefaultAskPassword(id, prompt string) (string, error) {
	cmd := exec.Command("systemd-ask-password",
		"--id="+id, "--timeout=0", "--echo=masked", prompt)
	// Ensure tty access for interactive prompt
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("systemd-ask-password: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
