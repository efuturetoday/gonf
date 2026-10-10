package gonf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// Option configures Load.
type Option func(*loadOptions)

type loadOptions struct {
	sources   []Source
	factories []SourceFactory
	// profileSources are the functions of WithProfileSources.
	profileSources []func(profile string) []Source
	profiles       []string
	profileKey     string
	mappings       []AnyMapping
	validator      func(v any) error
	converters     []converter
	onReload       func(cfg *Config, err error)
	logger         *slog.Logger
	functions      []Function
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
// be passed as is. A name must not contain '.', ',' or whitespace.
func WithProfiles(names ...string) Option {
	return func(o *loadOptions) {
		o.profiles = append(o.profiles, names...)
	}
}

// WithProfileKey activates the profiles listed in the value of key, a
// comma-separated list such as "dev,local", unless WithProfiles names any.
// The value is read from the Sources given with WithSources, without
// profiles applied, and expanded like Get. Sources created by factories
// cannot change it. If passed more than once, the last key is used.
func WithProfileKey(key string) Option {
	return func(o *loadOptions) { o.profileKey = key }
}

// WithProfileSources adds Sources for every active profile, such as the
// file app-<profile>.yaml. Load calls sources once per active profile, in
// the order of the profiles, after the active profiles are settled, and
// loads the Sources it returns together with those of WithSources, so a
// SourceFactory sees them. They count as registered after the Sources of
// WithSources. gonf closes them like the Sources of a factory.
func WithProfileSources(sources func(profile string) []Source) Option {
	return func(o *loadOptions) {
		o.profileSources = append(o.profileSources, sources)
	}
}

// A SourceFactory creates Sources that need configuration themselves, e.g.
// a Vault Source whose address is configured in a file. Create one with
// NewFactory.
type SourceFactory interface {
	// Mapping returns the Mapping the factory reads its configuration
	// from.
	Mapping() AnyMapping
	create(ctx context.Context, cfg *Config) ([]Source, error)
}

// NewFactory returns a SourceFactory that creates Sources from the
// configuration in m. Load binds m before the factory runs, from the
// Sources of WithSources and WithProfileSources, and passes the value to
// create. m is registered like a Mapping of WithMappings: Describe,
// Explain, the schema and the environment know its keys.
//
// The Sources a factory creates must not set a key under the prefix of
// the Mapping of any factory, since that configuration was read before
// they were loaded: it is a Load error. NewFactory panics if m is nil or
// has no prefix, or if create is nil.
//
// A factory with nothing to contribute returns no Sources. Errors should
// say which factory failed.
//
// The Sources a factory returns belong to gonf. If one implements
// io.Closer, gonf closes it when it is no longer needed: Load right after
// loading it, Watch when a reload replaces it or fails, and when watching
// stops. Load ignores errors from Close, Watch logs them. A factory may
// return a Source it returned before; Watch keeps it open while it is in
// use. Only Watch benefits from that, since Load closes every Source it
// got.
//
// Separate calls of Load or Watch may run a factory concurrently, so state
// it keeps between calls must be safe for concurrent use. Within one Watch
// a factory runs on one goroutine at a time.
func NewFactory[T any](m *Mapping[T], create func(ctx context.Context, v T) ([]Source, error)) SourceFactory {
	switch {
	case m == nil:
		panic("gonf: NewFactory needs a Mapping")
	case m.Prefix() == "":
		panic(fmt.Sprintf("gonf: the Mapping of a source factory needs a prefix (%s)", where(m.origin())))
	case create == nil:
		panic("gonf: NewFactory needs a create function")
	}
	return &factory[T]{mapping: m, fn: create}
}

type factory[T any] struct {
	mapping *Mapping[T]
	fn      func(ctx context.Context, v T) ([]Source, error)
}

func (f *factory[T]) Mapping() AnyMapping { return f.mapping }

func (f *factory[T]) create(ctx context.Context, cfg *Config) ([]Source, error) {
	return f.fn(ctx, f.mapping.Get(cfg))
}

// WithFactories adds SourceFactories and registers their Mappings. They
// run after the Sources of WithSources and WithProfileSources have been
// loaded; their Sources count as registered after those.
func WithFactories(factories ...SourceFactory) Option {
	return func(o *loadOptions) {
		o.factories = append(o.factories, factories...)
		for _, f := range factories {
			if f != nil {
				o.mappings = append(o.mappings, f.Mapping())
			}
		}
	}
}

// Load reads all Sources once and merges them into a Config.
//
// Load works in two phases. First it loads the Sources given with
// WithSources, settles the active profiles, loads the Sources of
// WithProfileSources and binds the Mappings of the factories. Then it runs
// every SourceFactory and loads the Sources they return. If the first phase
// fails, no factory runs. Finally it binds all Mappings and returns the
// BindErrors of all of them together.
//
// Every Source is loaded even if an earlier one fails; all failures are
// returned together, as SourceErrors joined with errors.Join.
func Load(ctx context.Context, opts ...Option) (*Config, error) {
	cfg, created, err := load(ctx, opts)
	_ = release(created, nil) // the Sources are no longer used either way
	return cfg, err
}

// load is Load without closing the Sources that factories created, which
// it returns, also on error.
func load(ctx context.Context, opts []Option) (*Config, []Source, error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}
	profiles, err := activeProfiles(o.profiles)
	if err != nil {
		return nil, nil, fmt.Errorf("gonf: %w", err)
	}
	cs, err := newConverters(o.converters)
	if err != nil {
		return nil, nil, err
	}
	fns, err := newFunctions(o.functions)
	if err != nil {
		return nil, nil, err
	}

	in := loadInput{
		keys:     sync.OnceValue(func() []string { return knownKeys(cs, o.mappings, o.profileKey) }),
		prefixes: mappingPrefixes(o.mappings),
	}
	loaded, err := loadSources(ctx, o.sources, in)
	if err != nil {
		return nil, nil, err
	}
	if len(profiles) == 0 && o.profileKey != "" {
		if profiles, err = profilesFromKey(loaded, o.profileKey, fns); err != nil {
			return nil, nil, err
		}
	}
	var created []Source
	for _, profile := range slices.Backward(profiles) { // profiles is strongest first
		for _, sources := range o.profileSources {
			created = append(created, sources(profile)...)
		}
	}
	if len(created) > 0 {
		ps, err := loadSources(ctx, created, in)
		if err != nil {
			return nil, created, err
		}
		loaded = append(loaded, ps...)
	}
	cfg := newConfig(loaded, profiles)
	cfg.validator, cfg.converters, cfg.functions = o.validator, cs, fns
	if len(o.factories) > 0 {
		late, made, err := runFactories(ctx, o.factories, cfg, in)
		created = append(created, made...)
		if err != nil {
			return nil, created, err
		}
		cfg = newConfig(append(loaded, late...), profiles)
		cfg.validator, cfg.converters, cfg.functions = o.validator, cs, fns
	}
	if err := cfg.bindMappings(o.mappings); err != nil {
		return nil, created, err
	}
	cfg.warnings = append(cfg.warnings, conflictWarnings(cfg.properties, cfg.keyMapperSource())...)
	return cfg, created, nil
}

// release closes every Source in srcs that implements io.Closer and is not
// in keep, and returns the errors of Close as SourceErrors. They do not
// make a configuration invalid: the Source is no longer used.
func release(srcs, keep []Source) error {
	var errs []error
	for _, src := range srcs {
		c, ok := src.(io.Closer)
		if !ok || slices.ContainsFunc(keep, func(k Source) bool { return isSame(k, src) }) {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: fmt.Errorf("close: %w", err)})
		}
	}
	return errors.Join(errs...)
}

// isSame reports whether a and b are the same Source. Sources of a type
// that == cannot compare, such as a map, are never the same.
func isSame(a, b Source) bool {
	ta := reflect.TypeOf(a)
	return ta == reflect.TypeOf(b) && ta.Comparable() && a == b
}

// runFactories binds the Mappings of factories on cfg, runs every factory
// with its configuration and loads the Sources they return. It runs all
// factories and reports all failures together. It returns the Sources the
// factories created, also on error.
func runFactories(ctx context.Context, factories []SourceFactory, cfg *Config, in loadInput) ([]loadedSource, []Source, error) {
	var mappings []AnyMapping
	for _, f := range factories {
		if f == nil {
			return nil, nil, errNilFactory
		}
		mappings = append(mappings, f.Mapping())
	}
	if err := cfg.bindMappings(mappings); err != nil {
		return nil, nil, err
	}
	var created []Source
	var errs []error
	for _, f := range factories {
		if err := ctx.Err(); err != nil {
			return nil, created, err
		}
		srcs, err := f.create(ctx, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("gonf: source factory: %w", err))
			continue
		}
		created = append(created, srcs...)
	}
	late, err := loadSources(ctx, created, in)
	if err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, factoryKeyErrors(late, mappings)...)
	return late, created, errors.Join(errs...)
}

// factoryKeyErrors reports every key of late, a Source of a factory, under
// the prefix of one of mappings, the Mappings of the factories.
func factoryKeyErrors(late []loadedSource, mappings []AnyMapping) []error {
	var errs []error
	for _, s := range late {
		for _, key := range slices.Sorted(maps.Keys(s.values)) {
			name := key
			if strings.HasPrefix(key, "%") {
				_, name, _ = splitProfileKey(key)
			}
			for _, m := range mappings {
				if name == m.Prefix() || strings.HasPrefix(name, m.Prefix()+".") {
					errs = append(errs, &SourceError{Source: s.src.Name(),
						Err: fmt.Errorf("sets %q, a key of a source factory, which was read before the factory ran", name)})
					break
				}
			}
		}
	}
	return errs
}

// loadInput is what a MappedSource gets: the known keys, computed when
// first needed, and the prefixes of the Mappings.
type loadInput struct {
	keys     func() []string
	prefixes []string
}

// mappingPrefixes returns the distinct, non-empty prefixes of mappings.
func mappingPrefixes(mappings []AnyMapping) []string {
	var out []string
	for _, m := range mappings {
		if m != nil && m.Prefix() != "" && !slices.Contains(out, m.Prefix()) {
			out = append(out, m.Prefix())
		}
	}
	return out
}

// loadSources loads srcs in registration order. A MappedSource gets in.
func loadSources(ctx context.Context, srcs []Source, in loadInput) ([]loadedSource, error) {
	loaded := make([]loadedSource, 0, len(srcs))
	var errs []error
	for _, src := range srcs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if src == nil {
			errs = append(errs, errNilSource)
			continue
		}
		if _, ok := Scheme(src.Name()); !ok {
			errs = append(errs, &SourceError{Source: src.Name(), Err: ErrInvalidSourceName})
			continue
		}
		values, unknown, err := loadValues(ctx, src, in)
		if err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: err})
			continue
		}
		s, err := newLoadedSource(src, values)
		if err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: err})
			continue
		}
		s.warnings = unknownWarnings(src, unknown, in)
		if o, ok := src.(OptionalSource); ok {
			s.isMissing = o.IsMissing()
		}
		loaded = append(loaded, s)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return loaded, nil
}

// loadValues loads src; a MappedSource gets the keys, a NameReporter the
// prefixes too and returns the names it found no key for.
func loadValues(ctx context.Context, src Source, in loadInput) (map[string]Value, []string, error) {
	if r, ok := src.(NameReporter); ok && len(in.prefixes) > 0 {
		return r.LoadReport(ctx, in.keys(), in.prefixes)
	}
	if m, ok := src.(MappedSource); ok {
		values, err := m.LoadMapped(ctx, in.keys())
		return values, nil, err
	}
	values, err := src.Load(ctx)
	return values, nil, err
}

// newLoadedSource validates the profile keys of values and fills in the
// Origin of every value.
func newLoadedSource(src Source, values map[string]Value) (loadedSource, error) {
	s := loadedSource{src: src, ordinal: src.Ordinal(), values: make(map[string]Value, len(values))}
	for key, v := range values {
		if strings.HasPrefix(key, "%") {
			if _, _, err := splitProfileKey(key); err != nil {
				return loadedSource{}, err
			}
		}
		v.Origin.Source = src.Name()
		v.Origin.Ordinal = s.ordinal
		s.values[key] = v
	}
	return s, nil
}

// profilesFromKey reads the active profiles from key.
func profilesFromKey(loaded []loadedSource, key string, fns *functions) ([]string, error) {
	cfg := newConfig(loaded, nil)
	cfg.functions = fns
	value, err := cfg.Get(key)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	profiles, err := activeProfiles(strings.Split(value, ","))
	if err != nil {
		return nil, fmt.Errorf("gonf: key %q: %w", key, err)
	}
	return profiles, nil
}

// ErrInvalidSourceName is reported, wrapped in a SourceError, for a Source
// whose name has no valid scheme (see Scheme).
var ErrInvalidSourceName = errors.New("gonf: source name must start with a scheme such as \"file:\"")

var (
	errNilSource  = errors.New("gonf: nil source")
	errNilFactory = errors.New("gonf: nil source factory")
)

// SourceError reports that a Source failed to load.
type SourceError struct {
	// Source is the name of the failed Source.
	Source string
	// Err is the error returned by Source.Load, ErrInvalidSourceName, an
	// error about an invalid profile key, or the error returned by
	// Watcher.Watch or by Close.
	Err error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("gonf: source %q: %v", e.Source, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }
