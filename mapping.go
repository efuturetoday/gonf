package gonf

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
)

// Mapping is the configuration of one module, bound to the keys under a
// prefix. A library declares it once:
//
//	var Mapping = gonf.NewMapping[Config]("db")
//
// The application passes it to Load with WithMappings, which binds and
// validates it with all other Mappings, and reads it with Get.
type Mapping[T any] struct {
	prefix string
	opts   []BindOption
}

// NewMapping returns a Mapping of T, which must be a struct, to the keys
// under prefix. opts apply as in Bind.
func NewMapping[T any](prefix string, opts ...BindOption) *Mapping[T] {
	return &Mapping[T]{prefix: prefix, opts: slices.Clone(opts)}
}

// Prefix returns the prefix of the Mapping's keys.
func (m *Mapping[T]) Prefix() string { return m.prefix }

// Get returns the value bound when c was loaded. Slices and maps in it are
// shared between calls and must not be modified.
//
// Get panics if m was not passed to Load with WithMappings: that is a
// programming error, while every configuration error is reported by Load.
func (m *Mapping[T]) Get(c *Config) T {
	v, ok := c.mappings[m].(T)
	if !ok {
		panic(fmt.Sprintf("gonf: mapping %q not registered, add it with gonf.WithMappings", m.prefix))
	}
	return v
}

func (m *Mapping[T]) target() reflect.Type { return reflect.TypeFor[T]() }

func (m *Mapping[T]) options() []BindOption { return m.opts }

// AnyMapping is a *Mapping of any type, as accepted by WithMappings.
type AnyMapping interface {
	Prefix() string
	target() reflect.Type
	options() []BindOption
}

// WithMappings registers Mappings. Load binds and validates all of them and
// reports the problems of all together. Keys under a prefix are unknown
// only if no Mapping binds them, so Mappings may share a prefix.
//
// Default tags stay with the struct, as in Bind: they are not values of the
// Config, so Get does not see them. A value that needs a fallback names it
// itself, as in ${db.pool.max-size:10}.
//
// SourceFactories run before the Mappings are bound and cannot Get them.
func WithMappings(mappings ...AnyMapping) Option {
	return func(o *loadOptions) {
		o.mappings = append(o.mappings, mappings...)
	}
}

// WithValidator sets a function that validates every struct bound by Bind
// or a Mapping, such as Struct of github.com/go-playground/validator. It
// gets a pointer to the struct after its own Validate methods succeeded.
func WithValidator(validate func(v any) error) Option {
	return func(o *loadOptions) { o.validator = validate }
}

// remember records what Explain needs to know about the keys b bound. Of
// two Mappings with a default for the same key, the first wins.
func (c *Config) remember(b *binder) {
	maps.Copy(c.secrets, b.secrets)
	maps.Copy(c.read, b.known)
	maps.Copy(c.read, b.referenced)
	for key, def := range b.defaults {
		if _, ok := c.defaults[key]; !ok {
			c.defaults[key] = Value{Raw: def, Origin: Origin{Source: "mapping:" + b.prefix}}
		}
	}
}

var errNilMapping = errors.New("gonf: nil mapping")

// bindMappings binds every Mapping and stores the values in c.
func (c *Config) bindMappings(mappings []AnyMapping) error {
	var binders []*binder
	var values []reflect.Value
	var bound []AnyMapping
	var errs []error
	for _, m := range mappings {
		if m == nil {
			errs = append(errs, errNilMapping)
			continue
		}
		if slices.Contains(bound, m) {
			continue
		}
		root, err := c.converters.rootModel(m.Prefix(), m.target())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		v := reflect.New(m.target()).Elem()
		binders = append(binders, c.bindRoot(m.Prefix(), root, v, m.options()))
		values = append(values, v)
		bound = append(bound, m)
	}
	checkUnknown(binders)
	c.mappings = make(map[AnyMapping]any, len(bound))
	c.secrets = map[string]struct{}{}
	c.defaults = map[string]Value{}
	c.read = map[string]struct{}{}
	for _, b := range binders {
		c.remember(b)
	}
	for i, b := range binders {
		b.checkSecretReferences()
		if err := b.err(); err != nil {
			errs = append(errs, err)
			continue
		}
		c.mappings[bound[i]] = values[i].Interface()
	}
	return errors.Join(errs...)
}
