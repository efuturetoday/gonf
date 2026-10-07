// Package memory provides gonf Sources backed by a map, for defaults and
// tests.
package memory

import (
	"context"
	"maps"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Default ordinals of memory Sources.
const (
	// Ordinal is high so that New overrides everything else, e.g. in tests.
	Ordinal = 500
	// DefaultsOrdinal is the lowest built-in ordinal.
	DefaultsOrdinal = 0
)

type memorySource struct {
	source.Meta
	values map[string]string
}

// New returns a Source backed by a copy of values, named "memory:".
func New(values map[string]string, opts ...source.Option) gonf.Source {
	return &memorySource{
		Meta:   source.NewMeta("memory:", Ordinal, opts...),
		values: maps.Clone(values),
	}
}

// Defaults returns a Source named "memory:defaults" with DefaultsOrdinal, meant for
// default values that every other Source overrides.
func Defaults(values map[string]string, opts ...source.Option) gonf.Source {
	opts = append([]source.Option{source.WithName("memory:defaults"), source.WithOrdinal(DefaultsOrdinal)}, opts...)
	return New(values, opts...)
}

func (s *memorySource) Load(context.Context) (map[string]gonf.Value, error) {
	out := make(map[string]gonf.Value, len(s.values))
	for k, v := range s.values {
		out[k] = gonf.Value{Raw: v}
	}
	return out, nil
}
