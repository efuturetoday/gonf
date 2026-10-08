package gonf_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

// upper is a SecretHandler that "decrypts" by upper-casing and records the
// origins it was called with.
type upper struct {
	mu      sync.Mutex
	origins []string
}

func (u *upper) handle(_ *gonf.Config, value string, origin gonf.Origin) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.origins = append(u.origins, origin.Source)
	if value == "fail" {
		return "", errors.New("wrong key")
	}
	return strings.ToUpper(value), nil
}

func TestWithSecretHandler_Get(t *testing.T) {
	t.Parallel()
	var u upper
	cfg := load(t, map[string]string{
		"a":      "${upper::abc}",
		"b":      "x-${a}-y",
		"d":      "${c}",
		"broken": "${upper::fail}",
		"nested": "${upper::${a}}",
		"other":  "${lower::abc}",
	}, upperSource{"C": "${upper::env}"}, gonf.WithSecretHandler("upper", u.handle))

	tests := []struct {
		key      string
		expected string
		err      string
	}{
		{key: "a", expected: "ABC"},
		{key: "b", expected: "x-ABC-y"},
		{key: "c", expected: "ENV"},
		{key: "d", expected: "ENV"},
		{key: "broken", err: `gonf: key "broken": upper: wrong key`},
		{key: "nested", err: `gonf: key "nested": value of "nested": nested expression in a upper value is not supported`},
		{key: "other", err: `gonf: key "other": value of "other": unknown secret handler "lower"`},
	}
	for _, tt := range tests {
		got, err := cfg.Get(tt.key)
		switch {
		case tt.err != "" && (err == nil || err.Error() != tt.err):
			t.Errorf("Get(%q) error = %v, want %q", tt.key, err, tt.err)
		case tt.err == "" && (err != nil || got != tt.expected):
			t.Errorf("Get(%q) = %q, %v; want %q", tt.key, got, err, tt.expected)
		}
	}
	// The origin is where the expression is written, also behind a
	// reference.
	expected := []string{"file:app.properties", "file:app.properties", "test:upper", "test:upper", "file:app.properties"}
	if !slices.Equal(u.origins, expected) {
		t.Errorf("origins = %q, want %q", u.origins, expected)
	}
}

func TestWithSecretHandler_Invalid(t *testing.T) {
	t.Parallel()
	var u upper
	tests := []struct {
		name     string
		opts     []gonf.Option
		expected string
	}{
		{
			name:     "invalid name",
			opts:     []gonf.Option{gonf.WithSecretHandler("a:b", u.handle)},
			expected: `gonf: invalid secret handler name "a:b"`,
		},
		{
			name:     "nil handler",
			opts:     []gonf.Option{gonf.WithSecretHandler("x", nil)},
			expected: `gonf: nil secret handler "x"`,
		},
		{
			name:     "two handlers",
			opts:     []gonf.Option{gonf.WithSecretHandler("x", u.handle), gonf.WithSecretHandler("x", u.handle)},
			expected: `gonf: two secret handlers named "x"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Load(context.Background(), tt.opts...)
			if err == nil || err.Error() != tt.expected {
				t.Errorf("error = %v, want %q", err, tt.expected)
			}
		})
	}
}

func TestWithSecretHandler_Bind(t *testing.T) {
	t.Parallel()
	type db struct {
		Password gonf.Secret[string]
		URL      string
		User     string `default:"${upper::admin}"`
	}
	var u upper
	values := map[string]string{
		"db.password": "${upper::s3cret}",
		"db.url":      "postgres://app:${db.password}@db/app",
	}
	_, err := gonf.Bind[db](load(t, values, nil, gonf.WithSecretHandler("upper", u.handle)), "db")
	expected := `gonf: bind "db": 2 problems:
  db.url: refers to the secret db.password, make the field a gonf.Secret (from file:app.properties)
  db.user: is decrypted with upper, make the field a gonf.Secret`
	if err == nil || err.Error() != expected {
		t.Errorf("error\n%v\nwant\n%s", err, expected)
	}

	type fixed struct {
		Password gonf.Secret[string]
		User     gonf.Secret[string] `default:"${upper::admin}"`
	}
	got, err := gonf.Bind[fixed](load(t, values, nil, gonf.WithSecretHandler("upper", u.handle)), "db", gonf.AllowUnknown())
	if err != nil {
		t.Fatal(err)
	}
	if got.Password.Value() != "S3CRET" || got.User.Value() != "ADMIN" {
		t.Errorf("got %q, %q; want S3CRET, ADMIN", got.Password.Value(), got.User.Value())
	}
}

func TestWithSecretHandler_Explain(t *testing.T) {
	t.Parallel()
	called := false
	handler := func(*gonf.Config, string, gonf.Origin) (string, error) {
		called = true
		return "plain", nil
	}
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSecretHandler("upper", handler),
		gonf.WithSources(
			memory.New(map[string]string{"api.key": "${upper::abc}", "dsn": "db://${api.key}@h"},
				source.WithName("file:app.properties"), source.WithOrdinal(100)),
			memory.New(map[string]string{"api.key": "plain-override"}, source.WithName("env:"), source.WithOrdinal(300)),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		key      string
		expected string
	}{
		// A key with an encrypted value anywhere is secret, also when
		// another value wins.
		{key: "api.key", expected: "api.key = **** (env:)\n  overrides **** (file:app.properties)"},
		{key: "dsn", expected: "dsn = db://****@h (file:app.properties)"},
	}
	for _, tt := range tests {
		if got := cfg.Explain(tt.key).String(); got != tt.expected {
			t.Errorf("Explain(%q)\n%s\nwant\n%s", tt.key, got, tt.expected)
		}
	}
	if called {
		t.Error("Explain decrypted a value")
	}
}
