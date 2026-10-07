package env_test

import (
	"context"
	"testing"

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
