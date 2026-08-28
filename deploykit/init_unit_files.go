package deploykit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/opencharly/spec/spec"
)

// initUnitFiles returns the files in a candy that THIS init claims, globbed from the
// init's own `candy_file:` patterns.
//
// This is what makes `candy_file:` live. The candy scan cannot do it:
// `loaderkit.ScanCandyManifest` is init-agnostic by construction — it has no init
// definition in scope — so it collected a hardcoded `*.service` glob into
// `CandyModel.ServiceFiles`, and `def.CandyFiles` was consulted for init DETECTION
// only. Two consequences, both silent:
//
//   - OpenRC's declared `candy_file: ['*.initd']` had never matched a file, because
//     no `.initd` was ever collected for it to claim.
//   - An init could not widen its file set at all: adding `*.socket` to the
//     vocabulary would have been a no-op, since the scan still only found `*.service`.
//
// Globbing at the point of USE fixes both, because that is the only place where the
// candy directory and the resolved init are in scope together.
//
// An init that declares no `candy_file:` keeps the historical `*.service` glob, so
// every existing init definition renders exactly as before.
func initUnitFiles(def *spec.ResolvedInit, layer CandyModel) []string {
	if def == nil || layer == nil {
		return nil
	}
	patterns := def.CandyFiles
	if len(patterns) == 0 {
		patterns = []string{"*.service"}
	}
	dir := layer.GetSourceDir()
	if dir == "" {
		// No anchor for a live glob (a candy materialized without a source tree):
		// fall back to whatever the scan collected rather than silently claiming none.
		return layer.ServiceFiles()
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, p))
		if err != nil {
			// A malformed pattern is an authoring error in the init vocabulary, not a
			// reason to drop the other patterns' matches.
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	// Deterministic order: the fragment copy lines and the staged files must not
	// reorder between builds.
	sort.Strings(out)
	return out
}

// unsatisfiedInitDepends reports an init whose `depends_candy:` names a candy the
// project cannot see, so nothing was injected and the box will build an image whose
// declared init has no binary.
//
// It is a warning rather than a hard error because the injector runs at every
// box-resolution chokepoint, including on partially-resolved configs where a later
// pass still supplies the candy; failing here would break those. The point is that the
// cause is stated at the moment it is known instead of resurfacing as an opaque
// "executable file not found in $PATH" at container start.
// Deduped per (box, init, candy): InjectInitDependsCandy runs at EVERY box-resolution
// chokepoint, so an undeduped report fires once per pass and reads like three separate
// problems. The dedupe lives HERE, at the call path, not inside the reporting hook —
// "report at most once" is the caller's policy, while the hook only decides HOW to
// report, which is what lets a test swap the hook and still observe the policy.
var unsatisfiedInitDependsSeen sync.Map

func reportUnsatisfiedInitDepends(boxName, initName, dependsCandy string) {
	if _, dup := unsatisfiedInitDependsSeen.LoadOrStore(
		boxName+"\x00"+initName+"\x00"+dependsCandy, struct{}{}); dup {
		return
	}
	unsatisfiedInitDepends(boxName, initName, dependsCandy)
}

var unsatisfiedInitDepends = func(boxName, initName, dependsCandy string) {
	fmt.Fprintf(os.Stderr,
		"warning: box %q resolves init %q, which depends on the %q candy, but no candy of "+
			"that name is in this project's scanned set — nothing was injected, so the image "+
			"will declare init %q with no %s binary and fail at start. Reference the candy "+
			"directly in the box's candy: list, or pull it in via a composed candy's require:.\n",
		boxName, initName, dependsCandy, initName, dependsCandy)
}
