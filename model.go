package gonf

import (
	"fmt"
	"reflect"
	"slices"
)

// model describes how Load reads a Go type, built from the type alone,
// without any value. Load fills a value by it and Describe lists its keys,
// so the documentation cannot drift from what Load does. The model reports
// a type Load cannot read before any value is read.
type model struct {
	typ  reflect.Type
	kind kind
	// elem is the model of the type a pointer points to, of a list element
	// or of a map value.
	elem *model
	// fields are the fields of a group.
	fields []field
	// err is why Load cannot read typ, if kind is kindInvalid.
	err error
	// isSecret reports whether the value is a Secret, also behind pointers
	// and in lists.
	isSecret bool
	// hasReferences reports whether the value holds pointers, slices or
	// maps that Load fills, which a copy must not share.
	hasReferences bool
}

// kind is how Load reads a type.
type kind int

const (
	kindInvalid kind = iota
	// kindScalar is read from a single value. A pointer type with a
	// converter is a scalar too.
	kindScalar
	// kindPointer stays nil without a value and is otherwise read as elem.
	kindPointer
	// kindList is a slice: a comma-separated value or indexed keys.
	kindList
	// kindMap has one entry per key segment under its key.
	kindMap
	// kindGroup is a struct with one key per field under its key.
	kindGroup
)

// field is a key that Load binds: a field of a group, or the element a
// pointer, list or map holds.
type field struct {
	name  string
	index []int
	// def is the default tag, nil if there is none; desc is the desc tag.
	def  *string
	desc string
	// m is the model of the field's type; nil if err is set.
	m *model
	// err is why Load cannot bind the field whatever its type, such as a
	// default for a group.
	err        error
	isRequired bool
	// isSecret reports a secret tag: the field holds a secret, as a
	// Secret does, without being one.
	isSecret bool
}

// isRequired reports whether Load fails without a value for a key of model
// m with the default tag def: a single value that is no pointer and has no
// default.
func isRequired(m *model, def *string) bool {
	return m.kind == kindScalar && m.typ.Kind() != reflect.Pointer && def == nil
}

// pointee returns the field that a pointer field f is read as. A pointer
// is optional, and so is what it points to.
func (f field) pointee() field {
	return field{def: f.def, desc: f.desc, m: f.m.elem}
}

// element returns the field of an element of the list field f.
func (f field) element() field {
	return field{desc: f.desc, m: f.m.elem, isRequired: isRequired(f.m.elem, nil)}
}

// entry returns the field of an entry of the map field f. An entry exists
// only with a value, so it is never required.
func (f field) entry() field {
	return field{desc: f.desc, m: f.m.elem}
}

// rootModel returns the model of t, which Load binds to prefix, or an error
// unless t is a struct.
func (cs converters) rootModel(prefix string, t reflect.Type) (*model, error) {
	mb := &modeler{cs: cs, models: map[reflect.Type]*model{}}
	m := mb.build(t)
	if m.kind != kindGroup {
		return nil, fmt.Errorf("gonf: bind %q: %s is not a struct", prefix, t)
	}
	mb.findReferences()
	return m, nil
}

// modeler builds models. models holds the models built so far, so a
// recursive type such as a list node with a Next *node refers to itself.
type modeler struct {
	cs     converters
	models map[reflect.Type]*model
}

func (mb *modeler) build(t reflect.Type) *model {
	if m, ok := mb.models[t]; ok {
		return m
	}
	m := &model{typ: t}
	mb.models[t] = m
	switch {
	case mb.cs.isScalar(t):
		m.kind, m.isSecret = kindScalar, isSecret(t)
	case t.Kind() == reflect.Pointer:
		m.kind, m.elem = kindPointer, mb.build(t.Elem())
		m.isSecret = m.elem.isSecret
	case isSecret(t):
		m.err = fmt.Errorf("unsupported type %s, a secret must hold a single value", t)
	case t.Kind() == reflect.Slice:
		m.kind, m.elem = kindList, mb.build(t.Elem())
		m.isSecret = m.elem.isSecret
	case t.Kind() == reflect.Map && !mb.cs.isScalar(t.Key()):
		m.err = fmt.Errorf("unsupported map key type %s", t.Key())
	case t.Kind() == reflect.Map:
		m.kind, m.elem = kindMap, mb.build(t.Elem())
	case t.Kind() == reflect.Struct:
		m.kind, m.fields = kindGroup, mb.fields(t)
	default:
		m.err = fmt.Errorf("unsupported type %s", t)
	}
	if containsItself(m) {
		m.kind, m.elem, m.isSecret = kindInvalid, nil, false
		m.err = fmt.Errorf("unsupported type %s, it contains itself", t)
	}
	return m
}

// findReferences sets hasReferences of every model built. A group has
// references if a field has; recursive types need several rounds.
func (mb *modeler) findReferences() {
	for isChanged := true; isChanged; {
		isChanged = false
		for _, m := range mb.models {
			if m.hasReferences {
				continue
			}
			switch m.kind {
			case kindPointer, kindList, kindMap:
				m.hasReferences = true
			case kindGroup:
				m.hasReferences = slices.ContainsFunc(m.fields, func(f field) bool {
					return f.m != nil && f.m.hasReferences
				})
			}
			isChanged = isChanged || m.hasReferences
		}
	}
}

// containsItself reports whether the elements of m lead back to m without
// a struct in between, as for type P *P or type L []L. Such a type has no
// end; a struct ends it, as a nil pointer or an empty list in a field.
func containsItself(m *model) bool {
	for e := m.elem; e != nil; e = e.elem {
		if e == m {
			return true
		}
	}
	return false
}

// fields returns the fields of struct type t, with embedded structs
// flattened.
func (mb *modeler) fields(t reflect.Type) []field {
	var out []field
	for i := range t.NumField() {
		f := t.Field(i)
		name, hasTag := f.Tag.Lookup("cfg")
		if name == "-" {
			continue
		}
		if f.Anonymous && !hasTag {
			switch {
			case f.Type.Kind() == reflect.Struct && !mb.cs.isScalar(f.Type):
				for _, sub := range mb.fields(f.Type) {
					sub.index = append([]int{i}, sub.index...)
					out = append(out, sub)
				}
				continue
			case f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct:
				err := fmt.Errorf("embedded pointer %s not supported, embed the struct", f.Type)
				out = append(out, field{name: kebab(f.Name), index: []int{i}, err: err})
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = kebab(f.Name)
		}
		sf := field{name: name, index: []int{i}, desc: f.Tag.Get("desc"), m: mb.build(f.Type), isSecret: f.Tag.Get("secret") == "true"}
		if def, ok := f.Tag.Lookup("default"); ok {
			sf.def = &def
		}
		if sf.def != nil && (sf.m.kind == kindGroup || sf.m.kind == kindMap) {
			sf.err = fmt.Errorf("default not supported for %s", f.Type)
		}
		sf.isRequired = isRequired(sf.m, sf.def)
		out = append(out, sf)
	}
	return out
}

// walk calls visit for every key under key that holds a value of field f:
// the scalars, the lists of scalars, and the fields and types Load cannot
// read. Keys of list elements contain [<n>], keys of map entries <name>.
// stack holds the groups being walked and stops recursive types.
func walk(key string, f field, stack []*model, visit func(key string, f field)) {
	if f.err != nil {
		visit(key, f)
		return
	}
	m := f.m
	switch {
	case m.kind == kindInvalid, m.kind == kindScalar:
		visit(key, f)
	case m.kind == kindPointer:
		walk(key, f.pointee(), stack, visit)
	case m.kind == kindList && m.elem.kind == kindScalar:
		visit(key, f)
	case m.kind == kindList:
		walk(key+"[<n>]", f.element(), stack, visit)
	case m.kind == kindMap:
		walk(key+".<name>", f.entry(), stack, visit)
	case m.kind == kindGroup && !slices.Contains(stack, m):
		stack = append(stack, m)
		for _, sub := range m.fields {
			walk(join(key, sub.name), sub, stack, visit)
		}
	}
}

// problems returns a Problem for every field under prefix that Load
// cannot bind because of its type or tags, in the order of the fields.
func (m *model) problems(prefix string) []*Problem {
	var out []*Problem
	walk(prefix, field{m: m}, nil, func(key string, f field) {
		switch {
		case f.err != nil:
			out = append(out, &Problem{Key: key, Err: f.err})
		case f.m.kind == kindInvalid:
			out = append(out, &Problem{Key: key, Err: f.m.err})
		}
	})
	return out
}

// isGroup reports whether values of model m live under several keys, so
// that a map entry of m is named by one key segment only.
func (m *model) isGroup() bool {
	switch m.kind {
	case kindPointer, kindList:
		return m.elem.isGroup()
	case kindGroup, kindMap:
		return true
	}
	return false
}
