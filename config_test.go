package gonf_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

type failingSource struct{ name string }

func (s failingSource) Name() string { return s.name }
func (s failingSource) Ordinal() int { return 100 }
func (s failingSource) Load(context.Context) (map[string]gonf.Value, error) {
	return nil, errors.New("boom")
}

// upperSource is a KeyMapper Source with environment-style keys, so tests
// do not need the process environment.
type upperSource map[string]string

func (s upperSource) Name() string { return "test:upper" }
func (s upperSource) Ordinal() int { return 300 }
func (s upperSource) Load(context.Context) (map[string]gonf.Value, error) {
	out := map[string]gonf.Value{}
	for k, v := range s {
		out[k] = gonf.Value{Raw: v}
	}
	return out, nil
}
func (s upperSource) MapKey(key string) string {
	return strings.ToUpper(strings.NewReplacer(".", "_", "%", "_").Replace(key))
}

func TestWithProfiles(t *testing.T) {
	t.Parallel()
	mem := func(name string, ordinal int, values map[string]string) gonf.Source {
		return memory.New(values, source.WithName(name), source.WithOrdinal(ordinal))
	}
	tests := []struct {
		name            string
		sources         []gonf.Source
		profiles        []string
		expectedRaw     string
		expectedProfile string
	}{
		{
			name:        "no profile uses base",
			sources:     []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			expectedRaw: "base",
		},
		{
			name:            "active profile beats base in same source",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			profiles:        []string{"dev"},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name:        "inactive profile is ignored",
			sources:     []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			profiles:    []string{"prod"},
			expectedRaw: "base",
		},
		{
			name:            "last profile wins",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"%dev.a": "dev", "%local.a": "local"})},
			profiles:        []string{"dev", "local"},
			expectedRaw:     "local",
			expectedProfile: "local",
		},
		{
			name:            "duplicate profile counts at its last position",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"%dev.a": "dev", "%local.a": "local"})},
			profiles:        []string{"dev", "local", "dev"},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name: "profile beats later registered base at equal ordinal",
			sources: []gonf.Source{
				mem("test:profile", 100, map[string]string{"%dev.a": "dev"}),
				mem("test:base", 100, map[string]string{"a": "base"}),
			},
			profiles:        []string{"dev"},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name: "higher ordinal base beats profile",
			sources: []gonf.Source{
				mem("test:profile", 100, map[string]string{"%dev.a": "dev"}),
				mem("test:high", 200, map[string]string{"a": "high"}),
			},
			profiles:    []string{"dev"},
			expectedRaw: "high",
		},
		{
			name:            "names are trimmed and empty names ignored",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			profiles:        []string{" dev ", ""},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name:            "mapped profile key",
			sources:         []gonf.Source{upperSource{"A": "env-base", "_DEV_A": "env-dev"}},
			profiles:        []string{"dev"},
			expectedRaw:     "env-dev",
			expectedProfile: "dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := gonf.Load(context.Background(), gonf.WithSources(tt.sources...), gonf.WithProfiles(tt.profiles...))
			if err != nil {
				t.Fatal(err)
			}
			v, ok := cfg.Lookup("a")
			if !ok {
				t.Fatal("Lookup(a): not found")
			}
			if v.Raw != tt.expectedRaw || v.Origin.Profile != tt.expectedProfile {
				t.Errorf("Lookup(a) = %q (profile %q), want %q (profile %q)",
					v.Raw, v.Origin.Profile, tt.expectedRaw, tt.expectedProfile)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(
		memory.New(map[string]string{"a": "high"}, source.WithName("test:high"), source.WithOrdinal(200)),
		memory.Defaults(map[string]string{"a": "default", "b": "only-default"}),
	))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		key             string
		expectedRaw     string
		expectedSource  string
		expectedOrdinal int
	}{
		{name: "higher ordinal wins", key: "a", expectedRaw: "high", expectedSource: "test:high", expectedOrdinal: 200},
		{name: "single source", key: "b", expectedRaw: "only-default", expectedSource: "memory:defaults", expectedOrdinal: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, ok := cfg.Lookup(tt.key)
			if !ok {
				t.Fatalf("Lookup(%q): not found", tt.key)
			}
			if v.Raw != tt.expectedRaw || v.Origin.Source != tt.expectedSource || v.Origin.Ordinal != tt.expectedOrdinal {
				t.Errorf("Lookup(%q) = %+v, want raw=%q source=%q ordinal=%d",
					tt.key, v, tt.expectedRaw, tt.expectedSource, tt.expectedOrdinal)
			}
		})
	}
}

func TestLoad_EqualOrdinalLaterRegisteredWins(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"a": "first"}, source.WithName("test:first"))),
		gonf.WithSources(memory.New(map[string]string{"a": "second"}, source.WithName("test:second"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, v := range cfg.Candidates("a") {
		names = append(names, v.Origin.Source)
	}
	if expected := []string{"test:second", "test:first"}; !slices.Equal(names, expected) {
		t.Errorf("Candidates order = %v, want %v", names, expected)
	}
}

func TestLoad_CollectsAllSourceErrors(t *testing.T) {
	t.Parallel()
	_, err := gonf.Load(context.Background(), gonf.WithSources(
		failingSource{"test:one"},
		memory.New(map[string]string{"a": "ok"}),
		failingSource{"test:two"},
	))
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("err = %v, want joined errors", err)
	}
	var names []string
	for _, e := range joined.Unwrap() {
		var se *gonf.SourceError
		if !errors.As(e, &se) {
			t.Fatalf("error %v is not a SourceError", e)
		}
		names = append(names, se.Source)
	}
	slices.Sort(names)
	if expected := []string{"test:one", "test:two"}; !slices.Equal(names, expected) {
		t.Errorf("failed sources = %v, want %v", names, expected)
	}
}

func TestLoad_CanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := gonf.Load(ctx, gonf.WithSources(memory.New(nil)))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestLoad_InvalidSourceName(t *testing.T) {
	t.Parallel()
	_, err := gonf.Load(context.Background(), gonf.WithSources(
		memory.New(nil, source.WithName("no-scheme")),
	))
	if !errors.Is(err, gonf.ErrInvalidSourceName) {
		t.Fatalf("err = %v, want ErrInvalidSourceName", err)
	}
	var se *gonf.SourceError
	if !errors.As(err, &se) || se.Source != "no-scheme" {
		t.Errorf("err = %v, want SourceError for %q", err, "no-scheme")
	}
}

func TestLoad_InvalidProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		profiles      []string
		key           string
		expectedError string
	}{
		{name: "profile name with dot", profiles: []string{"a.b"}, expectedError: `invalid profile name "a.b"`},
		{name: "profile name with comma", profiles: []string{"a,b"}, expectedError: `invalid profile name "a,b"`},
		{name: "profile key without dot", key: "%dev", expectedError: `invalid profile key "%dev"`},
		{name: "profile key without name", key: "%.a", expectedError: `invalid profile key "%.a"`},
		{name: "profile key without base", key: "%dev.", expectedError: `invalid profile key "%dev."`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{}
			if tt.key != "" {
				values[tt.key] = "x"
			}
			_, err := gonf.Load(context.Background(),
				gonf.WithSources(memory.New(values)),
				gonf.WithProfiles(tt.profiles...),
			)
			if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

func TestConfig_Lookup(t *testing.T) {
	t.Parallel()
	var zero gonf.Config
	if _, ok := zero.Lookup("nope"); ok {
		t.Error("Lookup on zero Config returned ok")
	}
	cfg, err := gonf.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Lookup("nope"); ok {
		t.Error("Lookup on empty Config returned ok")
	}
}

func TestConfig_Lookup_KeyMapper(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(
			upperSource{"DB_URL": "mapped", "db.user": "exact", "DB_USER": "mapped"},
			memory.New(map[string]string{"%dev.db.url": "dev"}, source.WithName("test:a"), source.WithOrdinal(100)),
		),
		gonf.WithProfiles("dev"),
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		key         string
		expectedRaw string
		isFound     bool
	}{
		{name: "mapped key found", key: "db.url", expectedRaw: "mapped", isFound: true},
		{name: "exact key beats mapped key", key: "db.user", expectedRaw: "exact", isFound: true},
		{name: "profile key is not found directly", key: "%dev.db.url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, ok := cfg.Lookup(tt.key)
			if ok != tt.isFound || v.Raw != tt.expectedRaw {
				t.Errorf("Lookup(%q) = %q, %v, want %q, %v", tt.key, v.Raw, ok, tt.expectedRaw, tt.isFound)
			}
		})
	}
}

func TestConfig_Keys(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(
		memory.New(map[string]string{"c": "", "a": ""}),
		memory.Defaults(map[string]string{"b": "", "a": ""}),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, expected := cfg.Keys(), []string{"a", "b", "c"}; !slices.Equal(got, expected) {
		t.Errorf("Keys = %v, want %v", got, expected)
	}
}

func TestConfig_Keys_Profiles(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"a": "", "%dev.x": "", "%prod.y": ""})),
		gonf.WithProfiles("dev"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, expected := cfg.Keys(), []string{"a", "x"}; !slices.Equal(got, expected) {
		t.Errorf("Keys = %v, want %v", got, expected)
	}
}

func TestConfig_Candidates(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Candidates("nope"); len(got) != 0 {
		t.Errorf("Candidates = %v, want empty", got)
	}
}

func TestConfig_Candidates_Order(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(
			memory.Defaults(map[string]string{"a": "default"}),
			memory.New(map[string]string{"a": "one", "%dev.a": "one-dev"}, source.WithName("test:one"), source.WithOrdinal(100)),
			memory.New(map[string]string{"a": "two"}, source.WithName("test:two"), source.WithOrdinal(100)),
			upperSource{"A": "env"},
		),
		gonf.WithProfiles("dev"),
	)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range cfg.Candidates("a") {
		got = append(got, v.Raw)
	}
	// Ordinal first, then profile, then registration.
	if expected := []string{"env", "one-dev", "two", "one", "default"}; !slices.Equal(got, expected) {
		t.Errorf("Candidates = %v, want %v", got, expected)
	}
}

func TestConfig_Immutable(t *testing.T) {
	t.Parallel()
	values := map[string]string{"a": "1"}
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(values)))
	if err != nil {
		t.Fatal(err)
	}
	values["a"] = "changed"
	cfg.Keys()[0] = "mutated"
	cfg.Candidates("a")[0].Raw = "mutated"

	if v, _ := cfg.Lookup("a"); v.Raw != "1" {
		t.Errorf("value changed to %q", v.Raw)
	}
	if got := cfg.Keys(); got[0] != "a" {
		t.Errorf("keys changed to %v", got)
	}
}

func TestSourceError(t *testing.T) {
	t.Parallel()
	inner := errors.New("boom")
	err := &gonf.SourceError{Source: "file:x", Err: inner}
	if got, expected := err.Error(), `gonf: source "file:x": boom`; got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
	if !errors.Is(err, inner) {
		t.Error("errors.Is does not reach the wrapped error")
	}
}
