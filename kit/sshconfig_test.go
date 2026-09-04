package kit

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// TestSaveStanzasConcurrent — the read-modify-write race regression: N concurrent
// saveStanzas writers must NOT lose each other's stanzas (measured: the 16-lane
// check-run stalls where the lost ssh alias hangs that lane's executor dial).
func TestSaveStanzasConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frag")
	const writers = 32
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			alias := fmt.Sprintf("charly-vm-%d", i)
			st := map[string]string{alias: "Host " + alias + "\n    Hostname 127.0.0.1"}
			if err := saveStanzas(path, st); err != nil {
				t.Errorf("save %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	stanzas := loadStanzas(path)
	if len(stanzas) != writers {
		t.Fatalf("lost stanzas under concurrency: got %d, want %d", len(stanzas), writers)
	}
}

