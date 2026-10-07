package source_test

import (
	"testing"

	"github.com/efuturetoday/gonf/source"
)

func TestNewMeta(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		opts               []source.Option
		expectedName       string
		expectedOrdinal    int
		expectedIsOptional bool
	}{
		{name: "defaults", expectedName: "p", expectedOrdinal: 7},
		{name: "with name", opts: []source.Option{source.WithName("x")}, expectedName: "x", expectedOrdinal: 7},
		{name: "with ordinal", opts: []source.Option{source.WithOrdinal(42)}, expectedName: "p", expectedOrdinal: 42},
		{name: "optional", opts: []source.Option{source.Optional()}, expectedName: "p", expectedOrdinal: 7, expectedIsOptional: true},
		{name: "later option wins", opts: []source.Option{source.WithOrdinal(1), source.WithOrdinal(2)}, expectedName: "p", expectedOrdinal: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := source.NewMeta("p", 7, tt.opts...)
			if m.Name() != tt.expectedName || m.Ordinal() != tt.expectedOrdinal || m.IsOptional() != tt.expectedIsOptional {
				t.Errorf("got %q/%d/%v, want %q/%d/%v",
					m.Name(), m.Ordinal(), m.IsOptional(), tt.expectedName, tt.expectedOrdinal, tt.expectedIsOptional)
			}
		})
	}
}
