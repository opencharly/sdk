package deploykit

// image_resolve_test.go — the W3.7 resolution order, the empty-tag rule, and the
// controls that prove each step is REACHED only when the earlier ones miss.

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type importCall struct {
	source string
	ref    string
}

type fakeImageStore struct {
	refs     []string
	present  map[string]bool
	hasErr   error
	listErr  error
	imports  []importCall
	pulls    []string
	tags     [][2]string
	removed  []string
	hasCalls []string
}

func newFakeImageStore(refs ...string) *fakeImageStore {
	return &fakeImageStore{refs: refs, present: map[string]bool{}}
}

func (s *fakeImageStore) Has(_ context.Context, ref string) (bool, error) {
	s.hasCalls = append(s.hasCalls, ref)
	return s.present[ref], s.hasErr
}
func (s *fakeImageStore) Import(_ context.Context, source, ref string) error {
	s.imports = append(s.imports, importCall{source: source, ref: ref})
	return nil
}
func (s *fakeImageStore) Pull(_ context.Context, ref string) error {
	s.pulls = append(s.pulls, ref)
	return nil
}
func (s *fakeImageStore) Tag(_ context.Context, src, dst string) error {
	s.tags = append(s.tags, [2]string{src, dst})
	return nil
}
func (s *fakeImageStore) Remove(_ context.Context, refs ...string) error {
	s.removed = append(s.removed, refs...)
	return nil
}
func (s *fakeImageStore) List(_ context.Context) ([]string, error) {
	return s.refs, s.listErr
}

type fakeCharlyStore struct {
	refs       []string
	present    map[string]bool
	archives   map[string]string
	hasCalls   []string
	listCalls  int
	archiveErr error
}

func newFakeCharlyStore(refs ...string) *fakeCharlyStore {
	return &fakeCharlyStore{refs: refs, present: map[string]bool{}, archives: map[string]string{}}
}

func (s *fakeCharlyStore) Has(_ context.Context, ref string) (bool, error) {
	s.hasCalls = append(s.hasCalls, ref)
	return s.present[ref], nil
}
func (s *fakeCharlyStore) Archive(_ context.Context, ref string) (string, error) {
	if s.archiveErr != nil {
		return "", s.archiveErr
	}
	if path, ok := s.archives[ref]; ok {
		return path, nil
	}
	return "charly-store/" + strings.ReplaceAll(ref, ":", "-") + ".tar", nil
}
func (s *fakeCharlyStore) List(_ context.Context) ([]string, error) {
	s.listCalls++
	return s.refs, nil
}

type fakeRegistry struct {
	present map[string]bool
	calls   []string
}

func (r *fakeRegistry) Has(_ context.Context, ref string) (bool, error) {
	r.calls = append(r.calls, ref)
	return r.present[ref], nil
}

func TestImageResolverOrder(t *testing.T) {
	ctx := context.Background()
	const ref = "ghcr.io/opencharly/box:2026.296.1200"

	t.Run("target store hit wins and consults nothing else", func(t *testing.T) {
		target := newFakeImageStore()
		target.present[ref] = true
		charly := newFakeCharlyStore()
		charly.present[ref] = true
		registry := &fakeRegistry{present: map[string]bool{ref: true}}
		built := 0
		resolver := &ImageResolver{
			Charly: charly, Registry: registry,
			Build: func(context.Context, string) error { built++; return nil },
		}
		got, err := resolver.ResolveImage(ctx, ref, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Origin != OriginTarget || got.Ref != ref {
			t.Fatalf("got %+v, want origin %s ref %s", got, OriginTarget, ref)
		}
		if len(charly.hasCalls) != 0 || len(registry.calls) != 0 || built != 0 {
			t.Fatalf("a target-store hit consulted a later layer: charly=%v registry=%v built=%d",
				charly.hasCalls, registry.calls, built)
		}
	})

	t.Run("charly store hit imports and never probes the registry", func(t *testing.T) {
		target := newFakeImageStore()
		charly := newFakeCharlyStore()
		charly.present[ref] = true
		charly.archives[ref] = "/charly/box.tar"
		registry := &fakeRegistry{present: map[string]bool{ref: true}}
		built := 0
		resolver := &ImageResolver{Charly: charly, Registry: registry,
			Build: func(context.Context, string) error { built++; return nil }}
		got, err := resolver.ResolveImage(ctx, ref, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Origin != OriginImported {
			t.Fatalf("origin = %s, want %s", got.Origin, OriginImported)
		}
		if len(target.imports) != 1 || target.imports[0] != (importCall{"/charly/box.tar", ref}) {
			t.Fatalf("imports = %+v, want the charly archive imported as %s", target.imports, ref)
		}
		if len(target.pulls) != 0 || len(registry.calls) != 0 || built != 0 {
			t.Fatalf("an import consulted a later layer: pulls=%v registry=%v built=%d",
				target.pulls, registry.calls, built)
		}
	})

	t.Run("registry hit pulls when both stores miss", func(t *testing.T) {
		target := newFakeImageStore()
		charly := newFakeCharlyStore()
		registry := &fakeRegistry{present: map[string]bool{ref: true}}
		built := 0
		resolver := &ImageResolver{Charly: charly, Registry: registry,
			Build: func(context.Context, string) error { built++; return nil }}
		got, err := resolver.ResolveImage(ctx, ref, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Origin != OriginPulled {
			t.Fatalf("origin = %s, want %s", got.Origin, OriginPulled)
		}
		if len(target.pulls) != 1 || target.pulls[0] != ref {
			t.Fatalf("pulls = %v, want [%s]", target.pulls, ref)
		}
		if built != 0 {
			t.Fatalf("a registry-pull consulted build:ensure (%d calls)", built)
		}
	})

	t.Run("all miss then build:ensure re-enters at import", func(t *testing.T) {
		target := newFakeImageStore()
		charly := newFakeCharlyStore()
		registry := &fakeRegistry{present: map[string]bool{}}
		built := 0
		resolver := &ImageResolver{Charly: charly, Registry: registry,
			Build: func(_ context.Context, builtRef string) error {
				built++
				if builtRef != ref {
					return fmt.Errorf("built %q, want %q", builtRef, ref)
				}
				charly.present[ref] = true
				charly.archives[ref] = "/charly/built.tar"
				return nil
			}}
		got, err := resolver.ResolveImage(ctx, ref, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Origin != OriginBuilt {
			t.Fatalf("origin = %s, want %s", got.Origin, OriginBuilt)
		}
		if built != 1 {
			t.Fatalf("build:ensure calls = %d, want 1", built)
		}
		if len(target.imports) != 1 || target.imports[0].source != "/charly/built.tar" {
			t.Fatalf("imports = %+v, want the freshly built archive", target.imports)
		}
	})

	t.Run("nothing resolves and no build hook", func(t *testing.T) {
		resolver := &ImageResolver{Charly: newFakeCharlyStore(), Registry: &fakeRegistry{present: map[string]bool{}}}
		if _, err := resolver.ResolveImage(ctx, ref, newFakeImageStore()); err == nil {
			t.Fatal("ResolveImage succeeded with no source")
		}
	})

	t.Run("build without a charly store is an error, not a false success", func(t *testing.T) {
		resolver := &ImageResolver{Build: func(context.Context, string) error { return nil }}
		_, err := resolver.ResolveImage(ctx, ref, newFakeImageStore())
		if err == nil || !strings.Contains(err.Error(), "no charly image store") {
			t.Fatalf("error = %v, want the missing charly store named", err)
		}
	})
}

func TestImageResolverEmptyTagNewestCalVer(t *testing.T) {
	ctx := context.Background()
	const repo = "ghcr.io/opencharly/box"
	const newest = "2026.200.0900"

	t.Run("newest across both stores", func(t *testing.T) {
		target := newFakeImageStore(repo + ":2026.100.1200")
		target.present[repo+":"+newest] = true
		charly := newFakeCharlyStore(
			repo+":2026.199.2359",
			repo+":"+newest,
			repo+":latest",
		)
		resolver := &ImageResolver{Charly: charly}
		got, err := resolver.ResolveImage(ctx, repo, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Ref != repo+":"+newest {
			t.Fatalf("ref = %q, want %q", got.Ref, repo+":"+newest)
		}
		if got.Origin != OriginTarget {
			t.Fatalf("origin = %s, want %s", got.Origin, OriginTarget)
		}
	})

	t.Run("a v-prefixed CalVer tag is addressable in its own spelling", func(t *testing.T) {
		const vTag = "v2026.201.0100"
		target := newFakeImageStore()
		target.present[repo+":"+vTag] = true
		charly := newFakeCharlyStore(repo + ":" + vTag)
		resolver := &ImageResolver{Charly: charly}
		got, err := resolver.ResolveImage(ctx, repo, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Ref != repo+":"+vTag {
			t.Fatalf("ref = %q, want the store's own spelling %q", got.Ref, repo+":"+vTag)
		}
	})

	t.Run("no CalVer tag is an error naming the repository", func(t *testing.T) {
		target := newFakeImageStore(repo + ":latest")
		charly := newFakeCharlyStore("ghcr.io/opencharly/other:2026.100.1200")
		resolver := &ImageResolver{Charly: charly}
		_, err := resolver.ResolveImage(ctx, repo, target)
		if err == nil || !strings.Contains(err.Error(), "no CalVer tag") || !strings.Contains(err.Error(), repo) {
			t.Fatalf("error = %v, want the no-CalVer-tag failure naming %s", err, repo)
		}
	})

	t.Run("a digest-pinned reference is not expanded", func(t *testing.T) {
		digestRef := repo + "@sha256:" + strings.Repeat("a", 64)
		target := newFakeImageStore()
		target.present[digestRef] = true
		resolver := &ImageResolver{Charly: newFakeCharlyStore(repo + ":2026.100.1200")}
		got, err := resolver.ResolveImage(ctx, digestRef, target)
		if err != nil {
			t.Fatalf("ResolveImage: %v", err)
		}
		if got.Ref != digestRef {
			t.Fatalf("ref = %q, want the digest unchanged", got.Ref)
		}
	})
}

func TestSplitImageRef(t *testing.T) {
	cases := []struct{ ref, repo, tag string }{
		{"ghcr.io/opencharly/box:2026.296.1200", "ghcr.io/opencharly/box", "2026.296.1200"},
		{"ghcr.io/opencharly/box", "ghcr.io/opencharly/box", ""},
		{"localhost:5000/box:1", "localhost:5000/box", "1"},
		{"localhost:5000/box", "localhost:5000/box", ""},
		{"ghcr.io/opencharly/box@sha256:abc", "ghcr.io/opencharly/box", ""},
		{"box:", "box", ""},
	}
	for _, tc := range cases {
		repo, tag := splitImageRef(tc.ref)
		if repo != tc.repo || tag != tc.tag {
			t.Fatalf("splitImageRef(%q) = (%q,%q), want (%q,%q)", tc.ref, repo, tag, tc.repo, tc.tag)
		}
	}
}
