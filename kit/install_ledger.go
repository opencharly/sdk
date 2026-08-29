package kit

// install_ledger.go — persistent record of host deploys.
//
// Every `charly fleet add host …` writes structured records to the ledger so a
// later `charly fleet del host …` can reverse the exact operations. The ledger
// lives in the `ledger:` section of the PER-HOST charly.yml
// (~/.config/charly/charly.yml) — the single home for local system state
// (deployments under `deploy:`, install records under `ledger:`, local system
// info under `system:`, cache status under `cache:`). The former per-deploy JSON
// files under ~/.config/opencharly/installed/ (deploys/<id>.json +
// layers/<candy>.json) are DELETED by this cutover — one file, one schema, one
// validation path.
//
// Refcounting lives in the candy records: `deployed_by` is the set of deploy IDs
// that include this candy. Uninstalling one deploy decrements the set; only when
// it becomes empty does the candy's steps actually reverse.
//
// This file implements I/O (read/write/lock) and ledger-shape types. The actual
// reverse-execution logic lives in deploy_host_helpers.go.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// LedgerPaths describes where the ledger lives. The ledger is the `ledger:`
// section of the per-host charly.yml. Extracted so tests can redirect to a temp
// file.
//
// The legacy per-deploy JSON-file layout (~/.config/opencharly/installed/) is
// DELETED by this cutover. The consumers (plugin-fleet, plugin-substrate) migrate
// to ConfigFile in the same wave (their PRs land with the new sdk pin); a stale
// consumer fails to COMPILE against this shape — a loud failure, never a silent
// wrong answer.
type LedgerPaths struct {
	ConfigFile string // the per-host charly.yml path
	LockFile   string // the advisory lock path
}

// DefaultLedgerPaths returns the canonical paths anchored at the per-host
// charly.yml (~/.config/charly/charly.yml, honoring the CHARLY_DEPLOY_CONFIG
// override).
func DefaultLedgerPaths() (*LedgerPaths, error) {
	path, err := spec.DefaultDeployConfigPath()
	if err != nil {
		return nil, fmt.Errorf("DefaultLedgerPaths: %w", err)
	}
	return &LedgerPaths{
		ConfigFile: path,
		LockFile:   path + ".lock",
	}, nil
}

// Ensure creates the config directory if missing.
func (p *LedgerPaths) Ensure() error {
	return os.MkdirAll(filepath.Dir(p.ConfigFile), 0o755)
}

// ---------------------------------------------------------------------------
// Flock — serialize concurrent charly fleet sessions.
// ---------------------------------------------------------------------------

// LedgerLock is an acquired advisory lock on the ledger. Call Release() when
// done. Panic-safe via defer.
type LedgerLock struct {
	release func() error
}

// AcquireLedgerLock takes a blocking exclusive flock on the ledger lock file via
// the shared AcquireFileLock primitive (filelock.go). Blocks until the lock is
// available.
func AcquireLedgerLock(paths *LedgerPaths) (*LedgerLock, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	release, err := AcquireFileLock(paths.LockFile, true)
	if err != nil {
		return nil, fmt.Errorf("ledger lock: %w", err)
	}
	return &LedgerLock{release: release}, nil
}

// Release releases the flock and closes the file.
func (l *LedgerLock) Release() error {
	if l == nil || l.release == nil {
		return nil
	}
	err := l.release()
	l.release = nil
	return err
}

// ---------------------------------------------------------------------------
// Ledger records
// ---------------------------------------------------------------------------

// SPIKE (value-type relocation, #55 cluster 4): DeployRecord/CandyRecord/
// StepRecord relocated to spec (spec/spec/ledger_records.go) — every field
// already resolved to a spec.* type and none carried methods, so they moved
// verbatim. Zero-churn aliases.
type (
	DeployRecord = spec.DeployRecord
	CandyRecord  = spec.CandyRecord
	StepRecord   = spec.StepRecord
)

// ---------------------------------------------------------------------------
// I/O — the `ledger:` section of the per-host charly.yml
// ---------------------------------------------------------------------------

// ledgerSchemaVersion is the install-ledger record format version (the
// ledger-candy-keys cutover's CalVer). It is INDEPENDENT of the project schema
// CalVer (LatestSchemaVersion) — a non-ledger schema cutover that bumps the
// project HEAD must NOT invalidate the ledger gate. Every record written
// carries it; the read path rejects a record without it (a pre-cutover record
// whose json:"layer" key would silently unmarshal to an empty Candy).
// The value lives in kit (the importable host-engine shared with out-of-tree
// plugin candies); this is the in-core alias.
const ledgerSchemaVersion = LedgerSchemaVersion

// ledgerDoc is the minimal per-host charly.yml shape this package reads: the
// `ledger:` section. Everything else is preserved as-is on write.
type ledgerDoc struct {
	Ledger *struct {
		Deploys map[string]DeployRecord `yaml:"deploys"`
		Candies map[string]CandyRecord  `yaml:"candies"`
	} `yaml:"ledger"`
}

// readLedger reads the `ledger:` section of the per-host charly.yml (best-effort;
// an absent/corrupt file yields an empty ledger).
func readLedger(paths *LedgerPaths) (map[string]DeployRecord, map[string]CandyRecord) {
	deploys := map[string]DeployRecord{}
	candies := map[string]CandyRecord{}
	data, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		return deploys, candies
	}
	var doc ledgerDoc
	if yaml.Unmarshal(data, &doc) != nil {
		return deploys, candies
	}
	if doc.Ledger != nil {
		if doc.Ledger.Deploys != nil {
			deploys = doc.Ledger.Deploys
		}
		if doc.Ledger.Candies != nil {
			candies = doc.Ledger.Candies
		}
	}
	return deploys, candies
}

// writeLedger persists the `ledger:` section into the per-host charly.yml under
// the advisory lock (best-effort). It reads the CURRENT file, updates only the
// `ledger:` key (preserving every other key — deploy:, provides:, cache:,
// system:, …), and writes back atomically (tempfile + rename).
func writeLedger(paths *LedgerPaths, deploys map[string]DeployRecord, candies map[string]CandyRecord) error {
	unlock, err := AcquireFileLock(paths.LockFile, true)
	if err != nil {
		return err
	}
	defer unlock()

	data, err := os.ReadFile(paths.ConfigFile)
	var doc yaml.Node
	if err == nil {
		if yaml.Unmarshal(data, &doc) != nil {
			return fmt.Errorf("ledger: refusing to clobber a corrupt %s", paths.ConfigFile)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode, Tag: "!!map"},
		}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("ledger: %s is not a mapping", paths.ConfigFile)
	}

	// Ensure the HEAD schema version stamp is present (the per-host file is
	// loaded through the unified loader, which requires it).
	if !hasMappingKey(root, "version") {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "version"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: spec.SchemaVersion},
		)
	}

	// Build the ledger: {deploys, candies} value.
	ledgerVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "deploys"},
		recordMapNode(deploys),
		{Kind: yaml.ScalarNode, Value: "candies"},
		recordMapNode(candies),
	}}
	SetMappingKey(root, "ledger", ledgerVal)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(paths.ConfigFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".charly-ledger-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, paths.ConfigFile)
}

// hasMappingKey reports whether a mapping node has a top-level key with the given
// name.
func hasMappingKey(m *yaml.Node, name string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == name {
			return true
		}
	}
	return false
}

// recordMapNode builds a YAML mapping node from a record map (deploy-id or
// candy-name → record), with deterministic key order.
func recordMapNode[T any](records map[string]T) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		body, err := yaml.Marshal(records[k])
		if err != nil {
			continue
		}
		var val yaml.Node
		if yaml.Unmarshal(body, &val) != nil || len(val.Content) == 0 {
			continue
		}
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			val.Content[0],
		)
	}
	return n
}

// WriteDeployRecord serializes rec into the `ledger:` section under
// ledger.deploys[rec.DeployID].
func WriteDeployRecord(paths *LedgerPaths, rec *DeployRecord) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	rec.SchemaVersion = ledgerSchemaVersion
	if err := spec.ValidateRecord("deploy_record", paths.ConfigFile, rec); err != nil {
		return err
	}
	deploys, candies := readLedger(paths)
	deploys[rec.DeployID] = *rec
	return writeLedger(paths, deploys, candies)
}

// ReadDeployRecord loads ledger.deploys[id]; returns nil, nil if absent.
func ReadDeployRecord(paths *LedgerPaths, id string) (*DeployRecord, error) {
	deploys, _ := readLedger(paths)
	rec, ok := deploys[id]
	if !ok {
		return nil, nil
	}
	if rec.SchemaVersion == "" {
		return nil, fmt.Errorf("ReadDeployRecord: %s is a pre-cutover install-ledger record (legacy json:\"layer\" keys, no schema_version) — remove the stale record (it regenerates on the next deploy)", id)
	}
	return &rec, nil
}

// ListDeployIDs returns every deploy id in the `ledger:` section, sorted.
//
// This is the enumeration half of the ledger API. Before the relocation, a consumer
// enumerated by listing *.json stems under LedgerPaths.Deploys; that directory no longer
// exists, so without this an out-of-tree consumer cannot discover what is deployed at all —
// it can only ReadDeployRecord an id it already knows. Absent/corrupt file yields an empty
// slice, matching readLedger's best-effort contract (and the old empty-directory case).
func ListDeployIDs(paths *LedgerPaths) []string {
	deploys, _ := readLedger(paths)
	ids := make([]string, 0, len(deploys))
	for id := range deploys {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// ListCandyNames returns every candy name in the `ledger:` section, sorted.
// The candy-side counterpart of ListDeployIDs; see that doc for why it exists.
func ListCandyNames(paths *LedgerPaths) []string {
	_, candies := readLedger(paths)
	names := make([]string, 0, len(candies))
	for name := range candies {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// WriteCandyRecord serializes rec into the `ledger:` section under
// ledger.candies[rec.Candy].
func WriteCandyRecord(paths *LedgerPaths, rec *CandyRecord) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	rec.SchemaVersion = ledgerSchemaVersion
	if err := spec.ValidateRecord("candy_record", paths.ConfigFile, rec); err != nil {
		return err
	}
	deploys, candies := readLedger(paths)
	candies[rec.Candy] = *rec
	return writeLedger(paths, deploys, candies)
}

// ReadCandyRecord loads ledger.candies[layer]; returns nil, nil if absent.
func ReadCandyRecord(paths *LedgerPaths, layer string) (*CandyRecord, error) {
	_, candies := readLedger(paths)
	rec, ok := candies[layer]
	if !ok {
		return nil, nil
	}
	if rec.SchemaVersion == "" {
		return nil, fmt.Errorf("ReadCandyRecord: %s is a pre-cutover install-ledger record (legacy json:\"layer\" keys, no schema_version) — remove the stale record (it regenerates on the next deploy)", layer)
	}
	return &rec, nil
}

// DeleteDeployRecord removes ledger.deploys[id]; silently ignores not-found
// (teardown is idempotent).
func DeleteDeployRecord(paths *LedgerPaths, id string) error {
	deploys, candies := readLedger(paths)
	if _, ok := deploys[id]; !ok {
		return nil
	}
	delete(deploys, id)
	return writeLedger(paths, deploys, candies)
}

// DeleteCandyRecord removes ledger.candies[layer].
func DeleteCandyRecord(paths *LedgerPaths, layer string) error {
	deploys, candies := readLedger(paths)
	if _, ok := candies[layer]; !ok {
		return nil
	}
	delete(candies, layer)
	return writeLedger(paths, deploys, candies)
}

// ---------------------------------------------------------------------------
// Refcount helpers
// ---------------------------------------------------------------------------

// AddCandyDeployment adds deployID to candy.DeployedBy and writes the record.
// Used at install time.
func AddCandyDeployment(paths *LedgerPaths, candyName, deployID string, update func(*CandyRecord)) error {
	rec, err := ReadCandyRecord(paths, candyName)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &CandyRecord{
			Candy:      candyName,
			DeployedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	if !containsString(rec.DeployedBy, deployID) {
		rec.DeployedBy = append(rec.DeployedBy, deployID)
	}
	if update != nil {
		update(rec)
	}
	return WriteCandyRecord(paths, rec)
}

// RemoveCandyDeployment decrements a candy's deployed_by set. Returns
// (recordAfter, shouldFullyRemove, error). When shouldFullyRemove is true, the
// caller should perform the actual file/package/service teardown and then delete
// the candy ledger entry.
func RemoveCandyDeployment(paths *LedgerPaths, candyName, deployID string) (*CandyRecord, bool, error) {
	rec, err := ReadCandyRecord(paths, candyName)
	if err != nil {
		return nil, false, err
	}
	if rec == nil {
		return nil, false, nil // already gone
	}
	out := rec.DeployedBy[:0]
	for _, id := range rec.DeployedBy {
		if id != deployID {
			out = append(out, id)
		}
	}
	rec.DeployedBy = out
	if len(rec.DeployedBy) == 0 {
		return rec, true, nil
	}
	return rec, false, WriteCandyRecord(paths, rec)
}

func containsString(s []string, v string) bool {
	return slices.Contains(s, v)
}

// ---------------------------------------------------------------------------
// Executor-routed ledger I/O for nested deploys.
//
// A nested host-deploy (e.g. arch-vm.arch-host — host-target running INSIDE a VM
// via SSH) must write its ledger on the SUBSTRATE filesystem (guest HOME), not
// on the operator's local FS. The ancestor-executor-chain derivation in
// deploy_add_cmd.go already routes install commands through the correct
// executor; the ledger needs the same treatment.
// ---------------------------------------------------------------------------

// AddCandyDeploymentVia is the executor-routed variant of AddCandyDeployment.
// When exec is nil or a local executor, it falls back to operator-side file I/O
// (today's behaviour). When exec is a non-local DeployExecutor (SSHExecutor /
// NestedExecutor), the ledger I/O goes through exec.GetFile + exec.RunUser so
// the ledger lands on the substrate's filesystem under the substrate's
// ~/.config/charly/charly.yml — matching the install's actual venue
// (arch-vm.arch-host writes in the arch VM guest; sway-pod with nested pods
// writes in the parent pod; etc.).
func AddCandyDeploymentVia(exec spec.DeployExecutor, paths *LedgerPaths, candyName, deployID string, update func(*CandyRecord)) error {
	if exec == nil {
		return AddCandyDeployment(paths, candyName, deployID, update)
	}
	if _, isLocal := exec.(ShellExecutor); isLocal {
		return AddCandyDeployment(paths, candyName, deployID, update)
	}
	ctx := context.Background()
	// Substrate ledger: the `ledger:` section of the substrate's per-host
	// charly.yml. `~` resolves in the substrate shell.
	const remoteFile = "~/.config/charly/charly.yml"
	data, err := exec.GetFile(ctx, remoteFile, false, EmitOpts{})
	// Read-modify-write the substrate's charly.yml: preserve every other key
	// (deploy:, provides:, cache:, system:), update only the ledger: section.
	out, err := mutateRemoteLedger(data, func(deploys map[string]DeployRecord, candies map[string]CandyRecord) (map[string]DeployRecord, map[string]CandyRecord, error) {
		rec, ok := candies[candyName]
		if !ok {
			rec = CandyRecord{
				Candy:      candyName,
				DeployedAt: time.Now().UTC().Format(time.RFC3339),
			}
		}
		if !containsString(rec.DeployedBy, deployID) {
			rec.DeployedBy = append(rec.DeployedBy, deployID)
		}
		if update != nil {
			update(&rec)
		}
		rec.SchemaVersion = ledgerSchemaVersion
		if err := spec.ValidateRecord("candy_record", remoteFile, &rec); err != nil {
			return nil, nil, err
		}
		candies[candyName] = rec
		return deploys, candies, nil
	})
	if err != nil {
		return err
	}
	script := "mkdir -p ~/.config/charly && cat > " + remoteFile + " <<'CHARLY_LEDGER_EOF'\n" +
		string(out) + "\nCHARLY_LEDGER_EOF\n"
	if runErr := exec.RunUser(ctx, script, EmitOpts{}); runErr != nil {
		return fmt.Errorf("AddCandyDeploymentVia: write via executor: %w", runErr)
	}
	return nil
}

// WriteDeployRecordVia is the executor-routed variant of WriteDeployRecord. Same
// semantics as AddCandyDeploymentVia but for deploy records.
func WriteDeployRecordVia(exec spec.DeployExecutor, paths *LedgerPaths, rec *DeployRecord) error {
	if exec == nil {
		return WriteDeployRecord(paths, rec)
	}
	if _, isLocal := exec.(ShellExecutor); isLocal {
		return WriteDeployRecord(paths, rec)
	}
	ctx := context.Background()
	const remoteFile = "~/.config/charly/charly.yml"
	rec.SchemaVersion = ledgerSchemaVersion
	if err := spec.ValidateRecord("deploy_record", remoteFile, rec); err != nil {
		return err
	}
	data, err := exec.GetFile(ctx, remoteFile, false, EmitOpts{})
	out, err := mutateRemoteLedger(data, func(deploys map[string]DeployRecord, candies map[string]CandyRecord) (map[string]DeployRecord, map[string]CandyRecord, error) {
		deploys[rec.DeployID] = *rec
		return deploys, candies, nil
	})
	if err != nil {
		return err
	}
	script := "mkdir -p ~/.config/charly && cat > " + remoteFile + " <<'CHARLY_LEDGER_EOF'\n" +
		string(out) + "\nCHARLY_LEDGER_EOF\n"
	return exec.RunUser(ctx, script, EmitOpts{})
}

// mutateRemoteLedger reads the substrate's charly.yml bytes (may be empty/absent),
// applies the ledger mutation, and returns the updated file bytes with every other
// key preserved.
func mutateRemoteLedger(data []byte, mutate func(map[string]DeployRecord, map[string]CandyRecord) (map[string]DeployRecord, map[string]CandyRecord, error)) ([]byte, error) {
	var doc yaml.Node
	if len(data) > 0 {
		if yaml.Unmarshal(data, &doc) != nil {
			return nil, fmt.Errorf("ledger: refusing to clobber a corrupt substrate charly.yml")
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode, Tag: "!!map"},
		}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("ledger: substrate charly.yml is not a mapping")
	}
	if !hasMappingKey(root, "version") {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "version"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: spec.SchemaVersion},
		)
	}
	// Decode the current ledger section (if any).
	deploys := map[string]DeployRecord{}
	candies := map[string]CandyRecord{}
	if lv := FindMappingValue(root, "ledger"); lv != nil && lv.Kind == yaml.MappingNode {
		if dv := FindMappingValue(lv, "deploys"); dv != nil {
			_ = dv.Decode(&deploys)
		}
		if cv := FindMappingValue(lv, "candies"); cv != nil {
			_ = cv.Decode(&candies)
		}
	}
	deploys, candies, err := mutate(deploys, candies)
	if err != nil {
		return nil, err
	}
	ledgerVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "deploys"},
		recordMapNode(deploys),
		{Kind: yaml.ScalarNode, Value: "candies"},
		recordMapNode(candies),
	}}
	SetMappingKey(root, "ledger", ledgerVal)
	return yaml.Marshal(&doc)
}
