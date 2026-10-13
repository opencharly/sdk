package kit

import (
	"fmt"
	"sync"
	"testing"
)

// TestWriteVmSshStanzaConcurrent — the read-modify-write race regression: N concurrent
// writers must NOT lose each other's stanzas (measured: the 16-lane check-run stalls
// where the lost ssh alias hangs that lane's executor dial). The guarantee now comes
// from loading the fragment UNDER the file lock, not from a fresh-load re-merge.
func TestWriteVmSshStanzaConcurrent(t *testing.T) {
	home := t.TempDir()
	const writers = 32
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			alias := fmt.Sprintf("charly-vm-%d", i)
			if err := WriteVmSshStanza(home, VmSshStanza{
				Alias:        alias,
				Hostname:     "127.0.0.1",
				Port:         2200 + i,
				User:         "u",
				IdentityFile: "/k",
			}); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	aliases, err := ListVmSshAliases(home)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(aliases) != writers {
		t.Fatalf("lost stanzas under concurrency: got %d, want %d", len(aliases), writers)
	}
}

// TestRemoveVmSshStanzaKeepsOthers — the removal-persistence regression. Removing one
// alias must DROP it while KEEPING every sibling. The bug: saveStanzas re-merged against
// a fresh disk load, so a deletion was resurrected whenever any other stanza remained
// (only the single-stanza `len==0 → os.Remove` arm hid it). `charly vm destroy` reported
// success yet left the alias behind — measured live on a 20-stanza fragment.
func TestRemoveVmSshStanzaKeepsOthers(t *testing.T) {
	home := t.TempDir()
	write := func(alias string, port int) {
		t.Helper()
		if err := WriteVmSshStanza(home, VmSshStanza{
			Alias:        alias,
			Hostname:     "127.0.0.1",
			Port:         port,
			User:         "ubuntu",
			IdentityFile: "/k",
		}); err != nil {
			t.Fatalf("write %s: %v", alias, err)
		}
	}
	write("charly-a", 2201)
	write("charly-b", 2202)
	write("charly-c", 2203)

	remaining, err := RemoveVmSshStanza(home, "charly-b")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining = %d, want 2", remaining)
	}

	aliases, err := ListVmSshAliases(home)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]bool{}
	for _, a := range aliases {
		got[a] = true
	}
	if got["charly-b"] {
		t.Fatalf("removed alias charly-b still present: %v", aliases)
	}
	if !got["charly-a"] || !got["charly-c"] {
		t.Fatalf("sibling aliases lost: %v", aliases)
	}
}
