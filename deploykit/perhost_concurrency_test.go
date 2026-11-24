package deploykit

// perhost_concurrency_test.go — ONE per-host config, FOUR writers, every section survives.
//
// ~/.config/charly/charly.yml is a shared document: the deploy overlay (`deploykit`), the install
// ledger and the host snapshot (`kit`), and the git-ref cache (`spec/refs`) all write into it, from
// charly processes that run concurrently (`charly config`, `charly deploy add`, `charly status`,
// a bed roster). On 2026-11-24 the copies of "lock, read, set my key, rename" disagreed badly enough
// to destroy real data on a host: one cleared a section by deleting the whole file, one wrote a
// stale in-memory value over a value another process had just changed, and the ledger's own writers
// read the ledger OUTSIDE the lock and replaced the whole section with that stale snapshot — so two
// concurrent ledger writes each dropped the other's record.
//
// This test drives the four real writers at once and asserts that every one of them left its mark:
// a lost update anywhere shows up as a missing deploy entry, a missing ledger record, a missing
// `cache:`/`system:` section, and fails the test. It fails on the pre-perhost writers, whose
// read-outside-the-lock ledger mutation loses `candyN` records outright.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/refs"
	"gopkg.in/yaml.v3"
)

// concMarshalNode is a minimal faithful marshalNode: the node-form body `<disc>: <fields>`.
func concMarshalNode(_ string, node *DeployNode) (*yaml.Node, error) {
	body, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	content := &yaml.Node{Kind: yaml.MappingNode}
	if len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		inner := &yaml.Node{Kind: yaml.MappingNode}
		inner.Content = doc.Content[0].Content
		content.Content = append(content.Content, kit.ScalarNode("pod"), inner)
	}
	return content, nil
}

func TestConcurrentPerHostWritersPreserveEverySection(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "charly.yml")
	t.Setenv(kit.DeployConfigEnv, dest)
	// The fail-safe re-read degrades to (nil, nil) with no host registered — this test is about the
	// file mechanics, not about the loader seam.
	prev := DeployStateHost
	DeployStateHost = nil
	t.Cleanup(func() { DeployStateHost = prev })

	ctx := context.Background()
	paths := &kit.LedgerPaths{ConfigFile: dest, LockFile: dest + ".lock"}
	const rounds = 8

	const ledgerWriters = 8
	const ledgerWaves = 3
	var (
		wg      sync.WaitGroup
		errs    = make(chan error, 16)
		arrived = make(chan int, ledgerWriters*ledgerWaves)
		gates   = make([]chan struct{}, ledgerWaves)
	)
	for r := range gates {
		gates[r] = make(chan struct{})
	}
	go func() {
		for r := 0; r < ledgerWaves; r++ {
			for i := 0; i < ledgerWriters; i++ {
				<-arrived
			}
			close(gates[r])
		}
	}()
	report := func(name string, err error) {
		if err != nil {
			errs <- fmt.Errorf("%s: %w", name, err)
		}
	}

	// 1. The deploy overlay — through the ONE locked cycle, its save being SaveDeployConfig.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var mu sync.Mutex
		own := map[string]DeployNode{}
		read := func() (*DeployConfig, error) {
			mu.Lock()
			defer mu.Unlock()
			cp := make(map[string]DeployNode, len(own))
			for k, v := range own {
				cp[k] = v
			}
			return &DeployConfig{Deploy: cp}, nil
		}
		save := func(dc *DeployConfig) error { return SaveDeployConfig(dc, concMarshalNode, nil, ctx) }
		for i := 0; i < rounds; i++ {
			_, err := MutateDeployConfig(read, save, func(dc *DeployConfig) (bool, error) {
				name := fmt.Sprintf("dep%d", i)
				dc.Deploy[name] = DeployNode{Image: name}
				mu.Lock()
				own[name] = dc.Deploy[name]
				mu.Unlock()
				return true, nil
			}, ctx)
			if err != nil {
				report("deploy cycle", err)
				return
			}
		}
	}()

	// 2. The install ledger — the candy refcount writer `charly config` uses, driven by SEVERAL
	//    concurrent installs. They are released together (a barrier per wave) so their reads of the
	//    ledger land in the same window: with a read-outside-the-lock writer each one replaces the
	//    `candies` map with its own single-entry snapshot and drops its peers' records.
	for w := 0; w < ledgerWriters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for round := 0; round < ledgerWaves; round++ {
				arrived <- w
				<-gates[round]
				name := fmt.Sprintf("candy%d-%d", w, round)
				if err := kit.AddCandyDeployment(paths, name, "dep0", nil); err != nil {
					report("ledger AddCandyDeployment", err)
					return
				}
			}
		}(w)
	}

	// 3. The git-ref cache — `charly cache bypass`, which persists into `cache:`.
	wg.Add(1)
	go func() {
		defer wg.Done()
		g := refs.NewGitClient(dest)
		for i := 0; i < rounds; i++ {
			if err := g.SetBypass(i%2 == 0); err != nil {
				report("cache SetBypass", err)
				return
			}
		}
	}()

	// 4. The host snapshot — `charly status`'s PopulateSystemInfo.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2; i++ {
			if err := kit.PopulateSystemInfo(); err != nil {
				report("system PopulateSystemInfo", err)
				return
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent writer failed: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading %s: %v", dest, err)
	}
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the shared file no longer parses:\n%s\n%v", data, err)
	}

	// Every deploy entry the cycle wrote must still be there: the ledger/cache/system writers
	// touch their OWN keys and must never remove a deploy key.
	for i := 0; i < rounds; i++ {
		name := fmt.Sprintf("dep%d", i)
		if _, ok := doc[name]; !ok {
			t.Errorf("deploy entry %q was lost by a concurrent writer:\n%s", name, data)
		}
	}
	// Every ledger record must still be there: the point of read-inside-the-lock.
	var ledger struct {
		Candies map[string]yaml.Node `yaml:"candies"`
	}
	if n, ok := doc["ledger"]; ok {
		_ = n.Decode(&ledger)
	}
	for w := 0; w < ledgerWriters; w++ {
		for round := 0; round < ledgerWaves; round++ {
			name := fmt.Sprintf("candy%d-%d", w, round)
			if _, ok := ledger.Candies[name]; !ok {
				t.Errorf("ledger record %q was lost by a concurrent writer:\n%s", name, data)
			}
		}
	}
	// And the other two sections are present, never cleared.
	if _, ok := doc["cache"]; !ok {
		t.Errorf("a concurrent writer cleared the cache section:\n%s", data)
	}
	if _, ok := doc["system"]; !ok {
		t.Errorf("a concurrent writer cleared the system section:\n%s", data)
	}
}
