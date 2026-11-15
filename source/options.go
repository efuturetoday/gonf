package source

import "github.com/efuturetoday/gonf"

// DefaultOrdinal is the ordinal of a Source built with New unless
// overridden with WithOrdinal. Providers define their own defaults.
const DefaultOrdinal = 100

// Option customizes a Source.
type Option func(*Meta)

// WithName overrides the Source name shown in Origin.
func WithName(name string) Option {
	return func(m *Meta) { m.name = name }
}

// WithOrdinal overrides the Source ordinal.
func WithOrdinal(ordinal int) Option {
	return func(m *Meta) { m.ordinal = ordinal }
}

// Optional makes a document Source (New and providers built on it)
// yield no values instead of an error when its content does not exist
// (fs.ErrNotExist). Ignored by other Sources.
func Optional() Option {
	return func(m *Meta) { m.isOptional = true }
}

// Verbatim makes every value of the Source text: ${...} in it is never
// expanded, so it reads no file, no variable and calls no Function. Use it
// for Sources whose values others may write, such as a shared key store.
// See gonf.VerbatimSource.
func Verbatim() Option {
	return func(m *Meta) { m.isVerbatim = true }
}

// WithProfile makes every key of a document Source (New and providers built
// on it) belong to profile, as if written "%profile.key". Use it for
// profile files such as app-dev.properties. A key with a profile of its
// own is an error of Load. Ignored by other Sources.
func WithProfile(profile string) Option {
	return func(m *Meta) { m.profile = profile }
}

// KeyMatcher returns values, whose keys follow another naming scheme,
// under the keys of gonf they match; see gonf.MappedSource. env.Match is
// one.
type KeyMatcher func(values map[string]gonf.Value, keys []string) map[string]gonf.Value

// WithKeyMatcher makes a document Source (New and providers built on it) a
// gonf.MappedSource whose keys follow another naming scheme: it parses the
// content, then passes the values to match. With env.Match, "DB_URL" in a
// .env file becomes "db.url". match must be safe for concurrent use.
// Ignored by other Sources.
func WithKeyMatcher(match KeyMatcher) Option {
	return func(m *Meta) { m.match = match }
}

// WithTrigger makes a document Source (New and providers built on it) check
// whether its content changed whenever trigger says so, while gonf.Watch
// watches it. Without it, or with nil, the Source checks every
// DefaultPollInterval. Ignored by other Sources.
func WithTrigger(trigger Trigger) Option {
	return func(m *Meta) { m.trigger = trigger }
}

// A Setting is a provider's own option of type T, stored in Meta. A
// provider declares one per option in a package variable, such as
// dir.TrimNewline, offers Set as an Option and reads it with Get. Every
// Setting is its own key, so no other package can clash with it. Other
// Sources ignore it.
type Setting[T any] struct {
	// _ makes every Setting a distinct address.
	_ byte
}

// NewSetting returns a new Setting.
func NewSetting[T any]() *Setting[T] { return &Setting[T]{} }

// Set returns an Option that gives the Setting the value v.
func (s *Setting[T]) Set(v T) Option {
	return func(m *Meta) {
		if m.settings == nil {
			m.settings = map[any]any{}
		}
		m.settings[s] = v
	}
}

// Get returns the value of the Setting in m, and false if it was not set.
func (s *Setting[T]) Get(m Meta) (T, bool) {
	v, ok := m.settings[s].(T)
	return v, ok
}

// Meta holds the name, ordinal, optionality, verbatim and profile of a Source. Providers embed
// it to implement the Name and Ordinal methods of gonf.Source and to support
// the common Options.
type Meta struct {
	name       string
	ordinal    int
	isOptional bool
	isVerbatim bool
	profile    string
	match      KeyMatcher
	trigger    Trigger
	settings   map[any]any
}

// NewMeta returns a Meta with the provider's defaults, overridden by opts.
func NewMeta(name string, ordinal int, opts ...Option) Meta {
	m := Meta{name: name, ordinal: ordinal}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// Name returns the Source name.
func (m Meta) Name() string { return m.name }

// Ordinal returns the Source ordinal.
func (m Meta) Ordinal() int { return m.ordinal }

// IsOptional reports whether Optional was given.
func (m Meta) IsOptional() bool { return m.isOptional }

// IsVerbatim reports whether Verbatim was given.
func (m Meta) IsVerbatim() bool { return m.isVerbatim }

// Profile returns the profile given with WithProfile, or "".
func (m Meta) Profile() string { return m.profile }
