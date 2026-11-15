package gonf

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync"
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
	opts   []MappingOption
	// location is where NewMapping was called, the Location of defaults.
	location string
}

// NewMapping returns a Mapping of T, which must be a struct, to the keys
// under prefix. Load binds it as follows.
//
// Every exported field is bound to the key prefix.name. The name is the
// field name in kebab case (MaxSize becomes max-size) unless the field has a
// `cfg:"name"` tag; `cfg:"-"` skips the field. Values are read like Get, so
// profiles, ${...} expressions and environment variables (DB_POOL_MAX_SIZE)
// apply. An empty value counts as not set. A `desc:"..."` tag documents
// the field for Describe.
//
// A field without a value uses its `default:"..."` tag, which is expanded
// like a value; an empty default leaves the zero value. A field with neither
// is an error, except for:
//
//   - pointers, which stay nil, so *int or *Pool is optional
//   - slices and maps, which stay empty
//
// Field types:
//
//   - string, bool, integers, floats; bool accepts true/false, 1/0, yes/no
//     and on/off in any case; integers are decimal unless they have a
//     prefix as in Go (0b101, 0o644, 0x1f) and may contain underscores
//     (1_000); a leading zero as in 0644 is an error, as it reads as octal
//     in Go but as decimal elsewhere
//   - time.Duration in the format of time.ParseDuration, such as 5s or 1h30m
//   - any type whose pointer implements encoding.TextUnmarshaler
//   - any type with a converter given with WithConverter, which replaces
//     the rules above
//   - Secret[T] of any of these types
//   - structs, bound to the keys under prefix.name; an embedded struct
//     without a cfg tag is bound to the keys of its parent
//   - slices, from a comma-separated value (a,b,c; \, for a literal comma)
//     or from indexed keys such as name[0] and name[1].host; the
//     comma-separated value wins if both exist
//   - maps, whose keys are the segments after prefix.name, e.g. "team" for
//     db.labels.team; a segment containing dots is quoted:
//     db.servers."eu.west".host
//
// Map entries and indexed elements are found among the keys of the Sources
// (see Keys); environment variables can override them but, except for
// indexed elements, not add new ones.
//
// After its fields are bound without problems, a struct whose pointer
// implements
//
//	Validate() error
//
// is validated; inner structs come first. A key under prefix that no field
// binds, such as a typo "db.pool.max-szie", is an error naming the closest
// known key; AllowUnknown turns this check off. A key that a bound value
// references, such as db.host in db.url=postgres://${db.host}/app,
// is known. The check is skipped for an empty prefix. An environment
// variable with a typo, such as DB_POOL_MAX_SIZ, matches no key and is left
// out (see MappedSource). Finally, the validator given with
// WithValidator checks the whole struct.
//
// A field that is no Secret but whose value or default refers to a secret
// key, as in url=postgres://app:${db.password}@db/app, is a problem: the
// field would hold the secret and print it. Make the field a Secret too.
// Secret keys are those that Explain masks.
//
// A field type or tag that Load cannot read, such as a func field or a
// default for a struct, is a problem even inside an optional struct or a
// list or map without entries; Load then reads no value for the Mapping.
//
// All problems are collected; Load returns those of every Mapping
// together, each Mapping's as a *BindError.
//
// Defaults come from the Source "mapping:" + prefix; their Location is the
// file and line of the call to NewMapping, relative to the working
// directory if below it.
func NewMapping[T any](prefix string, opts ...MappingOption) *Mapping[T] {
	return &Mapping[T]{prefix: prefix, opts: slices.Clone(opts), location: callerLocation()}
}

// workDir returns the working directory at the first call, or "".
var workDir = sync.OnceValue(func() string {
	wd, _ := os.Getwd()
	return wd
})

// callerLocation returns file:line of the caller of the caller, relative
// to the working directory if below it, or "" if unknown.
func callerLocation() string {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return ""
	}
	if wd := workDir(); wd != "" {
		if rel, err := filepath.Rel(wd, file); err == nil && filepath.IsLocal(rel) {
			file = rel
		}
	}
	return file + ":" + strconv.Itoa(line)
}

// Prefix returns the prefix of the Mapping's keys.
func (m *Mapping[T]) Prefix() string { return m.prefix }

// Get returns the value bound when c was loaded. Its pointers, slices and
// maps are copies, so a caller may modify them without changing what other
// callers get; a type without them costs no copy. Values of types with a
// converter or UnmarshalText, such as *url.URL, are not copied and must
// not be modified. The copy is made on every call: read a Mapping once
// where it is used, not in a hot path.
//
// Get panics if m was not passed to Load with WithMappings: that is a
// programming error, while every configuration error is reported by Load.
func (m *Mapping[T]) Get(c *Config) T {
	b := c.mappings[m]
	v, ok := b.value.(T)
	if !ok {
		made := ""
		if m.location != "" {
			made = " (made at " + m.location + ")"
		}
		panic(fmt.Sprintf("gonf: mapping %q%s is not registered: pass it to Load with gonf.WithMappings, or boot.WithMappings", m.prefix, made))
	}
	if !b.model.hasReferences {
		return v
	}
	v, _ = reflect.TypeAssert[T](deepCopy(b.model, reflect.ValueOf(v)))
	return v
}

// boundMapping is the value of a Mapping and the model it was bound by.
type boundMapping struct {
	value any
	model *model
}

// deepCopy returns a copy of v, of model m, that shares no pointer, slice
// or map that Load filled.
func deepCopy(m *model, v reflect.Value) reflect.Value {
	if !m.hasReferences {
		return v
	}
	switch m.kind {
	case kindPointer:
		if v.IsNil() {
			return v
		}
		p := reflect.New(m.typ.Elem())
		p.Elem().Set(deepCopy(m.elem, v.Elem()))
		return p
	case kindList:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(m.typ, v.Len(), v.Len())
		if !m.elem.hasReferences {
			reflect.Copy(out, v)
			return out
		}
		for i := range v.Len() {
			out.Index(i).Set(deepCopy(m.elem, v.Index(i)))
		}
		return out
	case kindMap:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(m.typ, v.Len())
		for iter := v.MapRange(); iter.Next(); {
			out.SetMapIndex(iter.Key(), deepCopy(m.elem, iter.Value()))
		}
		return out
	default: // kindGroup
		out := reflect.New(m.typ).Elem()
		out.Set(v)
		for _, f := range m.fields {
			if f.m != nil && f.m.hasReferences {
				out.FieldByIndex(f.index).Set(deepCopy(f.m, v.FieldByIndex(f.index)))
			}
		}
		return out
	}
}

func (m *Mapping[T]) target() reflect.Type { return reflect.TypeFor[T]() }

func (m *Mapping[T]) options() []MappingOption { return m.opts }

func (m *Mapping[T]) origin() Origin {
	return Origin{Source: "mapping:" + m.prefix, Location: m.location}
}

// AnyMapping is a *Mapping of any type, as accepted by WithMappings.
type AnyMapping interface {
	Prefix() string
	target() reflect.Type
	options() []MappingOption
	// origin is the Origin of the Mapping's defaults.
	origin() Origin
}

// WithMappings registers Mappings. Load binds and validates all of them and
// reports the problems of all together. Keys under a prefix are unknown
// only if no Mapping binds them, so Mappings may share a prefix.
//
// Default tags stay with the struct: they are not values of the
// Config, so Get does not see them. A value that needs a fallback names it
// itself, as in ${db.pool.max-size:10}.
//
// SourceFactories run before the Mappings are bound and cannot Get them.
func WithMappings(mappings ...AnyMapping) Option {
	return func(o *loadOptions) {
		o.mappings = append(o.mappings, mappings...)
	}
}

// WithValidator sets a function that validates every struct bound by a
// Mapping, such as Struct of github.com/go-playground/validator. It
// gets a pointer to the struct after its own Validate methods succeeded.
func WithValidator(validate func(v any) error) Option {
	return func(o *loadOptions) { o.validator = validate }
}

// remember records what Explain needs to know about the keys b bound. Of
// two Mappings with the same key, the first defines it, as in describe.
func (c *Config) remember(b *binder) {
	maps.Copy(c.read, b.known)
	maps.Copy(c.read, b.referenced)
	for key, v := range b.values {
		if _, ok := c.values[key]; !ok {
			c.values[key] = v
		}
	}
}

var errNilMapping = errors.New("gonf: nil mapping")

// bindMappings binds every Mapping and stores the values in c.
func (c *Config) bindMappings(mappings []AnyMapping) error {
	var binders []*binder
	var values []reflect.Value
	var bound []AnyMapping
	var roots []*model
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
		binders = append(binders, c.bindRoot(m, root, v))
		values = append(values, v)
		roots = append(roots, root)
		bound = append(bound, m)
	}
	checkUnknown(binders)
	c.mappings = make(map[AnyMapping]boundMapping, len(bound))
	c.values = map[string]boundValue{}
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
		c.mappings[bound[i]] = boundMapping{value: values[i].Interface(), model: roots[i]}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	c.properties = describe(bound, roots, c.keyMapper(), c.secretWords)
	c.property = make(map[string]int, len(c.properties))
	for i, p := range c.properties {
		c.property[p.Key] = i
	}
	return nil
}
