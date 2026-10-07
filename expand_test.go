package gonf_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

func TestConfig_Get(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(map[string]string{
		"db.host":            "localhost",
		"db.url":             "jdbc://${db.host}/app",
		"chain":              "${db.url}",
		"multi":              "${db.host}:${db.port:5432}",
		"with.default":       "${missing:fallback}",
		"default.with.colon": "${missing:http://x}",
		"nested.default":     "${missing:${db.host}}",
		"empty.default":      "x${missing:}y",
		"escaped":            "$${db.host}",
		"escaped.in.default": "${missing:$${x}}",
		"plain.dollars":      "a$b $ {c} }",
		"unresolved":         "${missing}",
		"cycle.a":            "${cycle.b}",
		"cycle.b":            "${cycle.a}",
		"self":               "${self:x}",
		"unclosed":           "${db.host",
		"empty.reference":    "${}",
		"nested.key":         "${${db.host}}",
		"via":                "${unresolved}",
	})))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		key           string
		expected      string
		expectedError string
	}{
		{name: "plain value", key: "db.host", expected: "localhost"},
		{name: "reference", key: "db.url", expected: "jdbc://localhost/app"},
		{name: "reference chain", key: "chain", expected: "jdbc://localhost/app"},
		{name: "several expressions", key: "multi", expected: "localhost:5432"},
		{name: "default", key: "with.default", expected: "fallback"},
		{name: "default may contain colon", key: "default.with.colon", expected: "http://x"},
		{name: "default is expanded", key: "nested.default", expected: "localhost"},
		{name: "empty default", key: "empty.default", expected: "xy"},
		{name: "escape", key: "escaped", expected: "${db.host}"},
		{name: "escape in default", key: "escaped.in.default", expected: "${x}"},
		{name: "dollars without braces stay", key: "plain.dollars", expected: "a$b $ {c} }"},
		{name: "unresolved reference", key: "unresolved", expectedError: `gonf: key "unresolved": unresolved reference "missing" (unresolved)`},
		{name: "unresolved reference names the chain", key: "via", expectedError: `gonf: key "via": unresolved reference "missing" (via -> unresolved)`},
		{name: "cycle", key: "cycle.a", expectedError: `gonf: key "cycle.a": cycle cycle.a -> cycle.b -> cycle.a`},
		{name: "self reference is a cycle despite default", key: "self", expectedError: `cycle self -> self`},
		{name: "unclosed expression", key: "unclosed", expectedError: `value of "unclosed": unclosed "${"`},
		{name: "empty reference", key: "empty.reference", expectedError: `empty reference`},
		{name: "nested key", key: "nested.key", expectedError: `nested expression in reference "${db.host}" is not supported`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := cfg.Get(tt.key)
			if tt.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
					t.Fatalf("Get(%q) err = %v, want %q", tt.key, err, tt.expectedError)
				}
				if errors.Is(err, gonf.ErrNotFound) {
					t.Errorf("Get(%q) err matches ErrNotFound", tt.key)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get(%q): %v", tt.key, err)
			}
			if got != tt.expected {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestConfig_Get_NotFound(t *testing.T) {
	t.Parallel()
	var cfg gonf.Config
	_, err := cfg.Get("db.url")
	if !errors.Is(err, gonf.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestConfig_Get_ResolvesLikeLookup(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(
			memory.New(map[string]string{
				"db.url":       "${db.host}:${db.port}",
				"db.host":      "localhost",
				"%dev.db.host": "devhost",
			}, source.WithOrdinal(100)),
			upperSource{"DB_PORT": "6543"},
		),
		gonf.WithProfiles("dev"),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.Get("db.url")
	if err != nil {
		t.Fatal(err)
	}
	if expected := "devhost:6543"; got != expected {
		t.Errorf("Get = %q, want %q", got, expected)
	}
}

func FuzzConfig_Get(f *testing.F) {
	for _, seed := range []string{"", "plain", "${b}", "${x:${y:z}}", "$${a}", "${", "}${", "${a}", "$$${c}", "${:}"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(map[string]string{
			"a": s,
			"b": "${a}",
			"c": "x",
		})))
		if err != nil {
			t.Fatal(err)
		}
		got, err := cfg.Get("a")
		if !strings.Contains(s, "${") && (err != nil || got != s) {
			t.Errorf("Get = %q, %v, want %q unchanged", got, err, s)
		}
		_, _ = cfg.Get("b")
	})
}
