package env_test

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
)

// Not parallel: t.Setenv modifies the process environment.
func TestNew(t *testing.T) {
	t.Setenv("GONF_TEST_DB_URL", "jdbc:env")
	t.Setenv("GONF_TEST_WITH_EQUALS", "a=b")

	src := env.New()
	if src.Name() != "env:" || src.Ordinal() != env.Ordinal {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
	got, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		key         string
		expectedRaw string
	}{
		{name: "raw key is kept", key: "GONF_TEST_DB_URL", expectedRaw: "jdbc:env"},
		{name: "value may contain equals sign", key: "GONF_TEST_WITH_EQUALS", expectedRaw: "a=b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got[tt.key].Raw != tt.expectedRaw {
				t.Errorf("%s = %q, want %q", tt.key, got[tt.key].Raw, tt.expectedRaw)
			}
			if loc := got[tt.key].Origin.Location; loc != "env:"+tt.key {
				t.Errorf("%s location = %q, want %q", tt.key, loc, "env:"+tt.key)
			}
		})
	}
}

func TestNew_Options(t *testing.T) {
	t.Parallel()
	src := env.New(source.WithName("env:my"), source.WithOrdinal(42))
	if src.Name() != "env:my" || src.Ordinal() != 42 {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
}

func TestEnvSource_MapKey(t *testing.T) {
	t.Parallel()
	mapper, ok := env.New().(gonf.KeyMapper)
	if !ok {
		t.Fatal("env Source does not implement gonf.KeyMapper")
	}
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{name: "dots", key: "db.url", expected: "DB_URL"},
		{name: "dashes", key: "db.pool.max-size", expected: "DB_POOL_MAX_SIZE"},
		{name: "profile prefix", key: "%dev.db.url", expected: "_DEV_DB_URL"},
		{name: "digits and index", key: "servers[0].host2", expected: "SERVERS_0__HOST2"},
		{name: "non ascii letter", key: "größe", expected: "GR__E"},
		{name: "already mapped", key: "DB_URL", expected: "DB_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mapper.MapKey(tt.key); got != tt.expected {
				t.Errorf("method MapKey(%q) = %q, want %q", tt.key, got, tt.expected)
			}
			if got := env.MapKey(tt.key); got != tt.expected {
				t.Errorf("MapKey(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func FuzzMapKey(f *testing.F) {
	for _, seed := range []string{"", "db.pool.max-size", "%dev.a", "größe", "\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		got := env.MapKey(key)
		for _, c := range []byte(got) {
			if !('A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_') {
				t.Fatalf("MapKey(%q) = %q contains %q", key, got, c)
			}
		}
		if len(got) != utf8.RuneCountInString(key) {
			t.Errorf("MapKey(%q) = %q, want one character per rune", key, got)
		}
		if again := env.MapKey(got); again != got {
			t.Errorf("MapKey not idempotent: %q -> %q", got, again)
		}
	})
}
