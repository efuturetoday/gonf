// Package gonf is a layered configuration library modeled on
// SmallRye/MicroProfile Config.
//
// Configuration is read from several Sources. Each Source has an ordinal;
// for every key the value from the Source with the highest ordinal wins.
// If two Sources have the same ordinal, the one registered later wins.
//
// A key prefixed with "%profile." such as "%dev.db.url" is a value for
// "db.url" that applies only while that profile is active (see
// WithProfiles). At equal ordinal a profile value beats the base value; a
// higher ordinal always wins, so an environment variable still overrides a
// profile value from a file.
//
// Values may reference other keys with ${key} or ${key:default}; Get expands
// them.
package gonf

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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

// Option configures Load.
type Option func(*loadOptions)

type loadOptions struct {
	sources  []Source
	profiles []string
}

// WithSources adds Sources. Can be passed multiple times; the order of
// registration breaks ties between equal ordinals (later wins).
func WithSources(srcs ...Source) Option {
	return func(o *loadOptions) {
		o.sources = append(o.sources, srcs...)
	}
}

// WithProfiles activates profiles. Can be passed multiple times; of several
// active profiles the last one wins. Names are trimmed and empty names are
// ignored, so the result of strings.Split(os.Getenv("APP_PROFILE"), ",") can
// be passed as is. A name must not contain '.' or ','.
func WithProfiles(names ...string) Option {
	return func(o *loadOptions) {
		o.profiles = append(o.profiles, names...)
	}
}

// Load reads all Sources once and merges them into a Config.
//
// Every Source is loaded even if an earlier one fails; all failures are
// returned together as SourceErrors joined with errors.Join.
func Load(ctx context.Context, opts ...Option) (*Config, error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}
	profiles, err := activeProfiles(o.profiles)
	if err != nil {
		return nil, err
	}

	// Stable sort keeps registration order for equal ordinals; iterating
	// from the end then yields "highest ordinal, latest registered" first.
	srcs := slices.Clone(o.sources)
	slices.SortStableFunc(srcs, func(a, b Source) int { return cmp.Compare(a.Ordinal(), b.Ordinal()) })

	cfg := &Config{profiles: profiles}
	keys := make(map[string]struct{})
	var errs []error
	for _, src := range slices.Backward(srcs) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := Scheme(src.Name()); !ok {
			errs = append(errs, &SourceError{Source: src.Name(), Err: ErrInvalidSourceName})
			continue
		}
		values, err := src.Load(ctx)
		if err == nil {
			err = cfg.add(src, values, keys)
		}
		if err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: err})
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	cfg.keys = slices.Sorted(maps.Keys(keys))
	return cfg, nil
}

// add appends the values of src and records their keys. Values of inactive
// profiles are validated and dropped.
func (c *Config) add(src Source, values map[string]Value, keys map[string]struct{}) error {
	loaded := loadedSource{ordinal: src.Ordinal(), values: make(map[string]Value, len(values))}
	loaded.mapper, _ = src.(KeyMapper)
	for key, v := range values {
		base := key
		if strings.HasPrefix(key, "%") {
			profile, rest, err := splitProfileKey(key)
			if err != nil {
				return err
			}
			if !slices.Contains(c.profiles, profile) {
				continue
			}
			base = rest
		}
		v.Origin.Source = src.Name()
		v.Origin.Ordinal = src.Ordinal()
		loaded.values[key] = v
		keys[base] = struct{}{}
	}
	c.sources = append(c.sources, loaded)
	return nil
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

// activeProfiles returns the valid, distinct profile names, strongest
// (last given) first.
func activeProfiles(names []string) ([]string, error) {
	var out []string
	for _, name := range slices.Backward(names) {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(out, name) {
			continue
		}
		if !validProfile(name) {
			return nil, fmt.Errorf("gonf: invalid profile name %q", name)
		}
		out = append(out, name)
	}
	return out, nil
}

// splitProfileKey splits "%dev.db.url" into "dev" and "db.url".
func splitProfileKey(key string) (profile, base string, err error) {
	profile, base, ok := strings.Cut(key[1:], ".")
	if !ok || !validProfile(profile) || base == "" {
		return "", "", fmt.Errorf("invalid profile key %q", key)
	}
	return profile, base, nil
}

// validProfile reports whether name can be used in a "%name." prefix. ','
// is reserved for keys of several profiles such as "%dev,test.x".
func validProfile(name string) bool {
	return name != "" && !strings.ContainsAny(name, ".,")
}

// ErrInvalidSourceName is reported, wrapped in a SourceError, for a Source
// whose name has no valid scheme (see Scheme).
var ErrInvalidSourceName = errors.New("gonf: source name must start with a scheme such as \"file:\"")

// SourceError reports that a Source failed to load.
type SourceError struct {
	// Source is the name of the failed Source.
	Source string
	// Err is the error returned by Source.Load, ErrInvalidSourceName, or
	// an error about an invalid profile key.
	Err error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("gonf: source %q: %v", e.Source, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }
