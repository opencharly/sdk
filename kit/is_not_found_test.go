package kit

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

// TestIsNotFound pins the ONE classifier every GetFile caller shares. It is deliberately
// explicit about what it does NOT match: over-matching would let a caller treat an unreadable
// substrate ledger as an absent one and overwrite it.
func TestIsNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"os.ErrNotExist bare", os.ErrNotExist, true},
		{"os.ErrNotExist wrapped", fmt.Errorf("read ledger: %w", os.ErrNotExist), true},
		{"*os.PathError from a local read", &os.PathError{Op: "open", Path: "/x", Err: os.ErrNotExist}, true},
		{
			"ssh cat missing file (the real first-deploy error)",
			errors.New("ssh cat ~/.config/charly/charly.yml: exit status 1 " +
				"(stderr: cat: /home/arch/.config/charly/charly.yml: No such file or directory)"),
			true,
		},
		{"bare no such file", errors.New("no such file"), true},
		{"ls cannot access", errors.New("ls: cannot access '/etc/x': No such file or directory"), true},
		{"tool reports not found", errors.New("kubectl: config not found"), true},

		{"permission denied", errors.New("ssh cat ~/.config/charly/charly.yml: exit status 1 " +
			"(stderr: cat: /home/arch/.config/charly/charly.yml: Permission denied)"), false},
		{"broken channel", errors.New("ssh: connect to host arch port 22: Connection refused"), false},
		{"corrupt substrate yaml", errors.New("ledger: substrate charly.yml is not a mapping"), false},
		{"unrelated error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNotFound(tc.err); got != tc.want {
				t.Fatalf("IsNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
