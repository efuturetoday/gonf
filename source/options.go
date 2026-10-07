package source

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

// Meta holds the name, ordinal and optionality of a Source. Providers embed
// it to implement the Name and Ordinal methods of gonf.Source and to support
// the common Options.
type Meta struct {
	name       string
	ordinal    int
	isOptional bool
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
