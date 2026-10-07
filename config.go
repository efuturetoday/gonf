// Package gonf is a layered configuration library modeled on
// SmallRye/MicroProfile Config.
//
// Configuration is read from several Sources. Each Source has an ordinal;
// for every key the value from the Source with the highest ordinal wins.
// If two Sources have the same ordinal, the one registered later wins.
package gonf

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
)

// Config is an immutable snapshot of all loaded values. It is safe for
// concurrent use.
type Config struct {
	// candidates holds every value per key, highest priority first.
	candidates map[string][]Value
	keys       []string
}

// Option configures Load.
type Option func(*loadOptions)

type loadOptions struct {
	sources []Source
}

// WithSources adds Sources. Can be passed multiple times; the order of
// registration breaks ties between equal ordinals (later wins).
func WithSources(srcs ...Source) Option {
	return func(o *loadOptions) {
		o.sources = append(o.sources, srcs...)
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

	// Stable sort keeps registration order for equal ordinals; iterating
	// from the end then yields "highest ordinal, latest registered" first.
	srcs := slices.Clone(o.sources)
	slices.SortStableFunc(srcs, func(a, b Source) int { return cmp.Compare(a.Ordinal(), b.Ordinal()) })

	cfg := &Config{candidates: make(map[string][]Value)}
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
		if err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: err})
			continue
		}
		for key, v := range values {
			v.Origin.Source = src.Name()
			v.Origin.Ordinal = src.Ordinal()
			cfg.candidates[key] = append(cfg.candidates[key], v)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	cfg.keys = make([]string, 0, len(cfg.candidates))
	for key := range cfg.candidates {
		cfg.keys = append(cfg.keys, key)
	}
	slices.Sort(cfg.keys)
	return cfg, nil
}

// Lookup returns the winning value for key.
func (c *Config) Lookup(key string) (Value, bool) {
	vs := c.candidates[key]
	if len(vs) == 0 {
		return Value{}, false
	}
	return vs[0], true
}

// Keys returns all known keys, sorted.
func (c *Config) Keys() []string {
	return slices.Clone(c.keys)
}

// Candidates returns every value for key from all Sources, the winner first.
func (c *Config) Candidates(key string) []Value {
	return slices.Clone(c.candidates[key])
}

// ErrInvalidSourceName is reported, wrapped in a SourceError, for a Source
// whose name has no valid scheme (see Scheme).
var ErrInvalidSourceName = errors.New("gonf: source name must start with a scheme such as \"file:\"")

// SourceError reports that a Source failed to load.
type SourceError struct {
	// Source is the name of the failed Source.
	Source string
	// Err is the error returned by Source.Load, or ErrInvalidSourceName.
	Err error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("gonf: source %q: %v", e.Source, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }
