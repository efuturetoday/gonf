package gonf

import (
	"context"
	"slices"
)

// LiveDone returns a channel that is closed when watching has stopped.
func LiveDone(l *Live) <-chan struct{} { return l.done }

// BindMapping binds m to the values of c the way Load binds its Mappings,
// on a copy of c, and returns the bound value. Tests use it to bind many
// structs to one loaded Config. As in Load, every MappedSource is loaded
// again with the keys of m.
func BindMapping[T any](c *Config, m *Mapping[T]) (T, error) {
	var zero T
	keys := knownKeys(c.converters, []AnyMapping{m})
	loaded := slices.Clone(c.sources)
	// Reversed, the Sources are in the order newConfig expects.
	slices.Reverse(loaded)
	for i, s := range loaded {
		ms, ok := s.src.(MappedSource)
		if !ok {
			continue
		}
		values, err := ms.LoadMapped(context.Background(), keys)
		if err != nil {
			return zero, err
		}
		if loaded[i], err = newLoadedSource(s.src, values); err != nil {
			return zero, err
		}
	}
	cp := newConfig(loaded, c.profiles)
	cp.validator, cp.converters, cp.functions = c.validator, c.converters, c.functions
	if err := cp.bindMappings([]AnyMapping{m}); err != nil {
		return zero, err
	}
	return m.Get(cp), nil
}
