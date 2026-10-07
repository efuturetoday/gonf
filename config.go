package gonf

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strings"
)

// Config is an immutable snapshot of all loaded values. It is safe for
// concurrent use.
type Config struct {
	// sources holds the loaded Sources, highest priority first.
	sources []loadedSource
	// profiles holds the active profiles, strongest first.
	profiles []string
	keys     []string
}

type loadedSource struct {
	ordinal int
	values  map[string]Value
	// mapper is the Source itself if it implements KeyMapper.
	mapper KeyMapper
}

// lookup returns the value for key, trying MapKey(key) if key is missing.
func (s loadedSource) lookup(key string) (Value, bool) {
	if v, ok := s.values[key]; ok {
		return v, true
	}
	if s.mapper == nil {
		return Value{}, false
	}
	v, ok := s.values[s.mapper.MapKey(key)]
	return v, ok
}

// newConfig builds a Config from Sources in registration order.
func newConfig(loaded []loadedSource, profiles []string) *Config {
	// Reversing first makes the stable sort put later registered Sources
	// ahead of earlier ones with the same ordinal.
	srcs := slices.Clone(loaded)
	slices.Reverse(srcs)
	slices.SortStableFunc(srcs, func(a, b loadedSource) int { return cmp.Compare(b.ordinal, a.ordinal) })

	keys := make(map[string]struct{})
	for _, s := range srcs {
		for key := range s.values {
			if !strings.HasPrefix(key, "%") {
				keys[key] = struct{}{}
				continue
			}
			// Validated in newLoadedSource. Keys of inactive profiles are
			// never looked up and do not show in Keys.
			profile, base, _ := splitProfileKey(key)
			if slices.Contains(profiles, profile) {
				keys[base] = struct{}{}
			}
		}
	}
	return &Config{sources: srcs, profiles: profiles, keys: slices.Sorted(maps.Keys(keys))}
}

// Lookup returns the winning value for key. The value is raw: expressions
// such as ${db.host} are not expanded (see Get). Keys with a profile prefix
// are not found directly, only through their base key.
func (c *Config) Lookup(key string) (Value, bool) {
	for v := range c.all(key) {
		return v, true
	}
	return Value{}, false
}

// Keys returns all known keys, sorted. Values of active profiles appear
// under their base key; keys of a KeyMapper Source appear as the Source
// delivers them (e.g. "DB_URL").
func (c *Config) Keys() []string {
	return slices.Clone(c.keys)
}

// Profiles returns the active profiles in the order given, so the last one
// wins.
func (c *Config) Profiles() []string {
	profiles := slices.Clone(c.profiles)
	slices.Reverse(profiles)
	return profiles
}

// Candidates returns every value for key from all Sources and active
// profiles, the winner first.
func (c *Config) Candidates(key string) []Value {
	return slices.Collect(c.all(key))
}

// all yields the values for key in priority order: by ordinal, then by
// profile (strongest first, base last), then by registration (latest first).
func (c *Config) all(key string) iter.Seq[Value] {
	return func(yield func(Value) bool) {
		if strings.HasPrefix(key, "%") {
			return
		}
		// emit yields the value of name from every Source in group.
		emit := func(group []loadedSource, name, profile string) bool {
			for _, src := range group {
				v, ok := src.lookup(name)
				if !ok {
					continue
				}
				v.Origin.Profile = profile
				if !yield(v) {
					return false
				}
			}
			return true
		}
		for start := 0; start < len(c.sources); {
			end := start + 1
			for end < len(c.sources) && c.sources[end].ordinal == c.sources[start].ordinal {
				end++
			}
			group := c.sources[start:end]
			for _, p := range c.profiles {
				if !emit(group, "%"+p+"."+key, p) {
					return
				}
			}
			if !emit(group, key, "") {
				return
			}
			start = end
		}
	}
}
