package memory_test

import (
	"context"
	"maps"
	"testing"

	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

func TestNew(t *testing.T) {
	t.Parallel()
	values := map[string]string{"a": "1"}
	src := memory.New(values)
	values["a"] = "changed" // the source must not see later changes

	if src.Name() != "memory:" || src.Ordinal() != memory.Ordinal {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
	got, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raws := map[string]string{}
	for k, v := range got {
		raws[k] = v.Raw
	}
	if expected := map[string]string{"a": "1"}; !maps.Equal(raws, expected) {
		t.Errorf("got %v, want %v", raws, expected)
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		opts            []source.Option
		expectedName    string
		expectedOrdinal int
	}{
		{name: "defaults", expectedName: "memory:defaults", expectedOrdinal: memory.DefaultsOrdinal},
		{name: "ordinal override keeps name", opts: []source.Option{source.WithOrdinal(5)}, expectedName: "memory:defaults", expectedOrdinal: 5},
		{name: "name override keeps ordinal", opts: []source.Option{source.WithName("lib:x")}, expectedName: "lib:x", expectedOrdinal: memory.DefaultsOrdinal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := memory.Defaults(nil, tt.opts...)
			if src.Name() != tt.expectedName || src.Ordinal() != tt.expectedOrdinal {
				t.Errorf("name/ordinal = %q/%d, want %q/%d", src.Name(), src.Ordinal(), tt.expectedName, tt.expectedOrdinal)
			}
		})
	}
}
