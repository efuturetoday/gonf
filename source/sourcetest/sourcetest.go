// Package sourcetest checks that a gonf.Source keeps the contract gonf
// relies on. A Source of its own runs it in its tests:
//
//	func TestContract(t *testing.T) {
//		sourcetest.Run(t, vault.New(addr), map[string]string{"db.url": "postgres://db"})
//	}
package sourcetest

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/efuturetoday/gonf"
)

// WatchTimeout is how long Run waits for Watch to return once its context
// ends.
const WatchTimeout = 5 * time.Second

// TB is the part of testing.TB that Run uses.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Run checks src against the raw values it is expected to load, keyed as
// Load returns them, such as "db.url" or "%dev.db.url":
//
//   - Name starts with a scheme (gonf.Scheme).
//   - Load returns exactly expected, with no empty keys and valid profile
//     keys; a MappedSource is asked through LoadMapped with the base keys of
//     expected.
//   - A second Load returns the same values, and changing the map of the
//     first does not change the second.
//   - An OptionalSource that loaded values is not missing.
//   - gonf.Load accepts the Source.
//   - A Watcher returns from Watch once its context ends.
func Run(t TB, src gonf.Source, expected map[string]string) {
	t.Helper()
	ctx := context.Background()

	if _, ok := gonf.Scheme(src.Name()); !ok {
		t.Errorf("Name() = %q, want a scheme such as \"file:\"", src.Name())
	}

	first, err := load(ctx, src, expected)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := raws(first); !maps.Equal(got, expected) {
		t.Errorf("Load = %v, want %v", got, expected)
	}
	for key := range first {
		checkKey(t, key)
	}

	clear(first)
	second, err := load(ctx, src, expected)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := raws(second); !maps.Equal(got, expected) {
		t.Errorf("second Load = %v, want %v", got, expected)
	}

	if o, ok := src.(gonf.OptionalSource); ok && len(expected) > 0 && o.IsMissing() {
		t.Errorf("IsMissing() = true after Load returned values")
	}

	if _, isMapped := src.(gonf.MappedSource); !isMapped {
		if _, err := gonf.Load(ctx, gonf.WithSources(src)); err != nil {
			t.Errorf("gonf.Load: %v", err)
		}
	}

	if w, ok := src.(gonf.Watcher); ok {
		checkWatch(t, w)
	}
}

// load calls LoadMapped for a MappedSource, Load otherwise.
func load(ctx context.Context, src gonf.Source, expected map[string]string) (map[string]gonf.Value, error) {
	m, ok := src.(gonf.MappedSource)
	if !ok {
		return src.Load(ctx)
	}
	keys := map[string]struct{}{}
	for key := range expected {
		keys[baseKey(key)] = struct{}{}
	}
	return m.LoadMapped(ctx, slices.Sorted(maps.Keys(keys)))
}

func raws(values map[string]gonf.Value) map[string]string {
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v.Raw
	}
	return out
}

// checkKey reports an empty key and a profile key gonf rejects.
func checkKey(t TB, key string) {
	t.Helper()
	if key == "" {
		t.Errorf("Load returned an empty key")
		return
	}
	if !strings.HasPrefix(key, "%") {
		return
	}
	profile, base, ok := strings.Cut(key[1:], ".")
	if !ok || profile == "" || base == "" || strings.ContainsAny(profile, ", \t\n") {
		t.Errorf("Load returned the invalid profile key %q, want %%profile.key", key)
	}
}

// baseKey returns key without its profile prefix.
func baseKey(key string) string {
	if !strings.HasPrefix(key, "%") {
		return key
	}
	_, base, _ := strings.Cut(key, ".")
	return base
}

func checkWatch(t TB, w gonf.Watcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Watch(ctx, func() {}) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Watch returned %v after its context ended, want nil", err)
		}
	case <-time.After(WatchTimeout):
		t.Errorf("Watch did not return within %v after its context ended", WatchTimeout)
	}
}
