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
		expectedProfile    string
	}{
		{name: "defaults", expectedName: "p", expectedOrdinal: 7},
		{name: "with name", opts: []source.Option{source.WithName("x")}, expectedName: "x", expectedOrdinal: 7},
		{name: "with ordinal", opts: []source.Option{source.WithOrdinal(42)}, expectedName: "p", expectedOrdinal: 42},
		{name: "optional", opts: []source.Option{source.Optional()}, expectedName: "p", expectedOrdinal: 7, expectedIsOptional: true},
		{name: "profile", opts: []source.Option{source.WithProfile("dev")}, expectedName: "p", expectedOrdinal: 7, expectedProfile: "dev"},
		{name: "later option wins", opts: []source.Option{source.WithOrdinal(1), source.WithOrdinal(2)}, expectedName: "p", expectedOrdinal: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := source.NewMeta("p", 7, tt.opts...)
			if m.Name() != tt.expectedName || m.Ordinal() != tt.expectedOrdinal ||
				m.IsOptional() != tt.expectedIsOptional || m.Profile() != tt.expectedProfile {
				t.Errorf("got %q/%d/%v/%q, want %q/%d/%v/%q",
					m.Name(), m.Ordinal(), m.IsOptional(), m.Profile(),
					tt.expectedName, tt.expectedOrdinal, tt.expectedIsOptional, tt.expectedProfile)
			}
		})
	}
}

func TestSetting(t *testing.T) {
	t.Parallel()
	a, b := source.NewSetting[int](), source.NewSetting[int]()
	m := source.NewMeta("p", 0, a.Set(3))
	if v, ok := a.Get(m); !ok || v != 3 {
		t.Errorf("a = %d, %v, want 3, true", v, ok)
	}
	if v, ok := b.Get(m); ok {
		t.Errorf("b = %d, true, want unset", v)
	}
}
