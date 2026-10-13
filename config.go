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
	// validator is set with WithValidator, converters with WithConverter,
	// functions with WithFunctions.
	validator  func(v any) error
	converters converters
	functions  *functions
	// secretWords are those of WithSecretWords.
	secretWords []string
	// mappings holds the bound value of every Mapping given with
	// WithMappings.
	mappings map[AnyMapping]boundMapping
	// properties are the Properties of the Mappings, in their order, and
	// property their index by key; values holds the keys the Mappings
	// bound to a value, read every key they bound or referenced.
	properties []Property
	property   map[string]int
	values     map[string]boundValue
	read       map[string]struct{}
	// warnings are those of the Sources and, after binding, of the
	// Mappings.
	warnings []Warning
}

type loadedSource struct {
	src     Source
	ordinal int
	values  map[string]Value
	// isMissing is set for an optional Source whose document is missing;
	// warnings are what the Source found out about its names.
	isMissing bool
	warnings  []Warning
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
			if base, ok := activeKey(key, profiles); ok {
				keys[base] = struct{}{}
			}
		}
	}
	c := &Config{sources: srcs, profiles: profiles, keys: slices.Sorted(maps.Keys(keys))}
	for _, s := range srcs {
		c.warnings = append(c.warnings, s.warnings...)
	}
	return c
}

// activeKey returns the key without its profile prefix, and false for a
// key of an inactive profile, which is never looked up.
func activeKey(key string, profiles []string) (string, bool) {
	if !strings.HasPrefix(key, "%") {
		return key, true
	}
	// Validated in newLoadedSource.
	profile, base, _ := splitProfileKey(key)
	return base, slices.Contains(profiles, profile)
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
// under their base key. Environment variables appear under the keys they
// matched (see MappedSource), such as db.url for DB_URL.
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
				v, ok := src.values[name]
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

// keyMapper returns the Source of the highest priority that is a
// KeyMapper, nil if there is none. It names Property.Env only.
func (c *Config) keyMapper() KeyMapper {
	if s := c.keyMapperSource(); s != nil {
		return s.(KeyMapper)
	}
	return nil
}

// keyMapperSource returns the Source keyMapper returns, nil if there is
// none.
func (c *Config) keyMapperSource() Source {
	for _, s := range c.sources {
		if _, ok := s.src.(KeyMapper); ok {
			return s.src
		}
	}
	return nil
}

// Sources returns the loaded Sources, the highest priority first: the order
// in which they win a key. Of equal ordinals the Source registered later
// comes first.
func (c *Config) Sources() []SourceInfo {
	out := make([]SourceInfo, len(c.sources))
	for i, s := range c.sources {
		info := SourceInfo{Name: s.src.Name(), Ordinal: s.ordinal, Keys: len(s.values), IsMissing: s.isMissing}
		if p, ok := s.src.(ProfileSource); ok {
			info.Profile = p.Profile()
		}
		if o, ok := s.src.(OptionalSource); ok {
			info.IsOptional = o.IsOptional()
		}
		out[i] = info
	}
	return out
}

// Warnings returns what Load noticed and did not treat as an error, see
// Warning: first those of the Sources in priority order, by name, then the
// name conflicts of the Mappings.
func (c *Config) Warnings() []Warning {
	return slices.Clone(c.warnings)
}
