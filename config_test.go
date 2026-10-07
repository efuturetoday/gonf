package gonf_test

import (
	"context"
	"errors"
	"slices"
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
