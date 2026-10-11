package gonf

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Encode returns the values of v, a struct, under the keys that a Mapping
// of its type at prefix binds: the inverse of Load, so that a Source
// holding the values, escaped with Escape, binds to v again. A Secret
// gives its value; a nil pointer gives no key.
//
//	type Conn struct {
//		URL      string
//		Password gonf.Secret[string]
//	}
//	gonf.Encode("db", Conn{URL: "postgres://db/app", Password: gonf.NewSecret("s3cret")})
//	// map[db.password:s3cret db.url:postgres://db/app]
//
// Encode takes the types Load reads without a converter: strings,
// booleans, numbers, time.Duration, a type with MarshalText and
// UnmarshalText, Secret[T] of these, pointers, structs, lists, given as
// indexed keys such as hosts[0], and maps. An empty list or map gives no
// key, so it binds as nil. An empty value gives no key either, as Load
// reads an empty value as none: the field gets its default. An empty
// element of a list is an error.
func Encode(prefix string, v any) (map[string]string, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, fmt.Errorf("gonf: encode %q: nil", prefix)
	}
	root, err := converters{}.rootModel(prefix, rv.Type())
	if err != nil {
		return nil, fmt.Errorf("gonf: encode %q: %s is not a struct", prefix, rv.Type())
	}
	if problems := root.problems(prefix); len(problems) > 0 {
		return nil, &BindError{Prefix: prefix, Problems: problems}
	}
	out := map[string]string{}
	if err := encode(prefix, root, rv, out); err != nil {
		return nil, fmt.Errorf("gonf: encode %q: %w", prefix, err)
	}
	return out, nil
}

// encode adds the values of v, of model m, under key to out.
func encode(key string, m *model, v reflect.Value, out map[string]string) error {
	switch m.kind {
	case kindGroup:
		for _, f := range m.fields {
			if err := encode(join(key, f.name), f.m, v.FieldByIndex(f.index), out); err != nil {
				return err
			}
		}
		return nil
	case kindPointer:
		if v.IsNil() {
			return nil
		}
		return encode(key, m.elem, v.Elem(), out)
	case kindScalar:
		s, err := encodeScalar(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if s != "" {
			out[key] = s
		}
		return nil
	case kindList:
		for i := range v.Len() {
			if err := encode(index(key, i), m.elem, v.Index(i), out); err != nil {
				return err
			}
			if m.elem.kind == kindScalar && out[index(key, i)] == "" {
				return fmt.Errorf("%s: empty, which Load reads as a missing element", index(key, i))
			}
		}
		return nil
	case kindMap:
		for iter := v.MapRange(); iter.Next(); {
			name, err := encodeScalar(iter.Key())
			if err != nil {
				return fmt.Errorf("%s: map key: %w", key, err)
			}
			if mapEntry(name, false) != name {
				return fmt.Errorf("%s: map key %q ends like an index", key, name)
			}
			if strings.Contains(name, ".") {
				name = `"` + name + `"`
			}
			if err := encode(key+"."+name, m.elem, iter.Value(), out); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s: %s", key, m.err)
	}
}

// encodeScalar returns v as Load reads it, in the order convert tries.
func encodeScalar(v reflect.Value) (string, error) {
	t := v.Type()
	if sec, ok := reflect.TypeAssert[interface{ secretValue() reflect.Value }](v); ok {
		return encodeScalar(sec.secretValue())
	}
	if isText(t) {
		p := reflect.New(t) // MarshalText may have a pointer receiver
		p.Elem().Set(v)
		m, ok := reflect.TypeAssert[encoding.TextMarshaler](p)
		if !ok {
			return "", fmt.Errorf("%s has UnmarshalText but no MarshalText", t)
		}
		b, err := m.MarshalText()
		if err != nil {
			return "", fmt.Errorf("%s: %w", t, err)
		}
		return string(b), nil
	}
	if t == durationType {
		return fmt.Sprint(v.Interface()), nil
	}
	switch t.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, t.Bits()), nil
	}
	return "", fmt.Errorf("unsupported type %s", t)
}

// Escape returns s with every "${" written as "$${", so that a Source
// holding it gives s back as it is, not expanded: for values that come
// from elsewhere, such as those of Encode or of another process.
func Escape(s string) string { return strings.ReplaceAll(s, "${", "$${") }
