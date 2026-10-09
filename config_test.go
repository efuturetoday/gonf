package gonf_test

import (
	"context"
	"slices"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

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

func TestConfig_Lookup_MappedSource(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(
			upperSource{"DB_URL": "mapped", "db.user": "dotted", "DB_USER": "mapped", "DB_NAME": "unknown key"},
			memory.New(map[string]string{"%dev.db.url": "dev"}, source.WithName("test:a"), source.WithOrdinal(100)),
		),
		gonf.WithProfiles("dev"),
		withKnownDB(),
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
		{name: "only names that match a key", key: "db.user", expectedRaw: "mapped", isFound: true},
		{name: "name of no known key", key: "db.name"},
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

func TestConfig_Profiles(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(), gonf.WithProfiles("dev", "local"))
	if err != nil {
		t.Fatal(err)
	}
	if got, expected := cfg.Profiles(), []string{"dev", "local"}; !slices.Equal(got, expected) {
		t.Errorf("Profiles = %v, want %v", got, expected)
	}
	cfg.Profiles()[0] = "mutated"
	if got := cfg.Profiles()[0]; got != "dev" {
		t.Errorf("Profiles changed to %q", got)
	}
	var zero gonf.Config
	if got := zero.Profiles(); len(got) != 0 {
		t.Errorf("zero Config Profiles = %v", got)
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
		withTopLevel(),
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
