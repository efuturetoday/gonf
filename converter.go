package gonf

import (
	"errors"
	"fmt"
	"reflect"
)

// converters maps a type to the function converting a string into it.
type converters map[reflect.Type]func(s string) (reflect.Value, error)

// converter is one WithConverter option. Load collects them in a list, not
// directly in converters, so that it can report two for the same type.
type converter struct {
	typ     reflect.Type
	convert func(s string) (reflect.Value, error)
}

// WithConverter registers a function that converts a value into a T for
// Mappings, for a type that cannot implement
// encoding.TextUnmarshaler because it belongs to another package:
//
//	gonf.WithConverter(url.Parse) // fields of type *url.URL
//
// A type that Load would read by its underlying kind needs one if its
// values are written differently, such as fs.FileMode, a uint32 written in
// octal:
//
//	gonf.WithConverter(func(s string) (fs.FileMode, error) {
//		n, err := strconv.ParseUint(s, 8, 32)
//		return fs.FileMode(n), err
//	})
//
// It applies wherever a T is converted from a single value: fields of type
// T, *T and Secret[T], elements of slices and keys and values of maps. It
// replaces the built-in conversion of T, e.g. to read a bare number as
// seconds for time.Duration. A field of a pointer type T stays optional.
//
// Only exactly T matches: a converter for *url.URL is not used for
// url.URL. Registering two converters for the same type is an error of
// Load.
func WithConverter[T any](convert func(s string) (T, error)) Option {
	c := converter{typ: reflect.TypeFor[T]()}
	if convert != nil {
		c.convert = func(s string) (reflect.Value, error) {
			v, err := convert(s)
			return reflect.ValueOf(&v).Elem(), err
		}
	}
	return func(o *loadOptions) { o.converters = append(o.converters, c) }
}

// newConverters indexes list by type and reports nil converters and two
// converters for the same type.
func newConverters(list []converter) (converters, error) {
	cs := make(converters, len(list))
	var errs []error
	for _, c := range list {
		switch _, ok := cs[c.typ]; {
		case c.convert == nil:
			errs = append(errs, fmt.Errorf("gonf: nil converter for %s", c.typ))
		case ok:
			errs = append(errs, fmt.Errorf("gonf: two converters for %s", c.typ))
		default:
			cs[c.typ] = c.convert
		}
	}
	return cs, errors.Join(errs...)
}
