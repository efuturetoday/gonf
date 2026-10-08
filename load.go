package gonf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"strings"
)

// Option configures Load.
type Option func(*loadOptions)

type loadOptions struct {
	sources    []Source
	factories  []SourceFactory
	profiles   []string
	profileKey string
	mappings   []AnyMapping
	validator  func(v any) error
	converters []converter
	onReload   func(cfg *Config, err error)
	logger     *slog.Logger
	handlers   []namedHandler
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

// SourceFactory creates Sources from configuration that is already loaded,
// e.g. a Vault Source whose address is configured in a file. cfg holds the
// Sources given with WithSources and the active profiles; it does not
// contain Sources of other factories. A factory with nothing to contribute
// returns no Sources. Errors should say which factory failed.
//
// The Sources a factory returns belong to gonf. If one implements
// io.Closer, gonf closes it when it is no longer needed: Load right after
// loading it, Watch when a reload replaces it or fails, and when watching
// stops. Load ignores errors from Close, Watch logs them. A factory may return a Source it
// returned before; Watch keeps it open while it is in use. Only Watch
// benefits from that, since Load closes every Source it got.
//
// Separate calls of Load or Watch may run a factory concurrently, so state
// it keeps between calls must be safe for concurrent use. Within one Watch
// a factory runs on one goroutine at a time.
type SourceFactory func(ctx context.Context, cfg *Config) ([]Source, error)

// WithFactories adds SourceFactories. They run after the Sources given with
// WithSources have been loaded; their Sources count as registered after
// those.
func WithFactories(factories ...SourceFactory) Option {
	return func(o *loadOptions) {
		o.factories = append(o.factories, factories...)
	}
}

// Load reads all Sources once and merges them into a Config.
//
// Load works in two phases. First it loads the Sources given with
// WithSources and settles the active profiles. Then it passes the result to
// every SourceFactory and loads the Sources they return. If the first phase
// fails, no factory runs. Finally it binds the Mappings given with
// WithMappings and returns the BindErrors of all of them together.
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
	handlers, err := newHandlers(o.handlers)
	if err != nil {
		return nil, nil, err
	}

	loaded, err := loadSources(ctx, o.sources)
	if err != nil {
		return nil, nil, err
	}
	if len(profiles) == 0 && o.profileKey != "" {
		if profiles, err = profilesFromKey(loaded, o.profileKey); err != nil {
			return nil, nil, err
		}
	}
	cfg := newConfig(loaded, profiles)
	cfg.validator, cfg.converters, cfg.handlers = o.validator, cs, handlers
	var created []Source
	if len(o.factories) > 0 {
		var late []loadedSource
		late, created, err = runFactories(ctx, o.factories, cfg)
		if err != nil {
			return nil, created, err
		}
		cfg = newConfig(append(loaded, late...), profiles)
		cfg.validator, cfg.converters, cfg.handlers = o.validator, cs, handlers
	}
	if err := cfg.bindMappings(o.mappings); err != nil {
		return nil, created, err
	}
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

// runFactories passes cfg to every factory and loads the Sources they
// return. It runs all factories and reports all failures together. It
// returns the Sources the factories created, also on error.
func runFactories(ctx context.Context, factories []SourceFactory, cfg *Config) ([]loadedSource, []Source, error) {
	var created []Source
	var errs []error
	for _, factory := range factories {
		if err := ctx.Err(); err != nil {
			return nil, created, err
		}
		if factory == nil {
			errs = append(errs, errNilFactory)
			continue
		}
		srcs, err := factory(ctx, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("gonf: source factory: %w", err))
			continue
		}
		created = append(created, srcs...)
	}
	late, err := loadSources(ctx, created)
	if err != nil {
		errs = append(errs, err)
	}
	return late, created, errors.Join(errs...)
}

// loadSources loads srcs in registration order.
func loadSources(ctx context.Context, srcs []Source) ([]loadedSource, error) {
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
		values, err := src.Load(ctx)
		if err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: err})
			continue
		}
		s, err := newLoadedSource(src, values)
		if err != nil {
			errs = append(errs, &SourceError{Source: src.Name(), Err: err})
			continue
		}
		loaded = append(loaded, s)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return loaded, nil
}

// newLoadedSource validates the profile keys of values and fills in the
// Origin of every value.
func newLoadedSource(src Source, values map[string]Value) (loadedSource, error) {
	s := loadedSource{src: src, ordinal: src.Ordinal(), values: make(map[string]Value, len(values))}
	s.mapper, _ = src.(KeyMapper)
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
func profilesFromKey(loaded []loadedSource, key string) ([]string, error) {
	value, err := newConfig(loaded, nil).Get(key)
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
