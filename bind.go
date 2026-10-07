package gonf

import (
	"cmp"
	"encoding"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Bind maps the keys under prefix onto a new T, which must be a struct.
//
// Every exported field is bound to the key prefix.name. The name is the
// field name in kebab case (MaxSize becomes max-size) unless the field has a
// `cfg:"name"` tag; `cfg:"-"` skips the field. Values are read like Get, so
// profiles, ${...} expressions and environment variables (DB_POOL_MAX_SIZE)
// apply. An empty value counts as not set.
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
//     and on/off in any case
//   - time.Duration in the format of time.ParseDuration, such as 5s or 1h30m
//   - any type whose pointer implements encoding.TextUnmarshaler
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
// references, such as db.host in db.url=jdbc:postgresql://${db.host}/app,
// is known. It is skipped for an empty
// prefix and for environment variable names such as DB_POOL_MAX_SIZ, which
// cannot be told apart from variables for other programs.
//
// All problems are collected and returned together as a *BindError.
func Bind[T any](c *Config, prefix string, opts ...BindOption) (T, error) {
	var out T
	v := reflect.ValueOf(&out).Elem()
	if v.Kind() != reflect.Struct || isText(v.Type()) {
		return out, fmt.Errorf("gonf: bind %q: %s is not a struct", prefix, v.Type())
	}
	var o bindOptions
	for _, opt := range opts {
		opt(&o)
	}
	b := binder{c: c, known: map[string]struct{}{}, referenced: map[string]struct{}{}}
	b.bindStruct(prefix, v)
	if prefix != "" && !o.isUnknownAllowed {
		b.checkUnknown(prefix)
	}
	if len(b.problems) > 0 {
		var zero T
		return zero, &BindError{Prefix: prefix, Problems: b.problems}
	}
	return out, nil
}

// BindOption configures Bind.
type BindOption func(*bindOptions)

type bindOptions struct {
	isUnknownAllowed bool
}

// AllowUnknown turns off the check for unknown keys under the prefix, e.g.
// when several structs bind parts of the same prefix.
func AllowUnknown() BindOption {
	return func(o *bindOptions) { o.isUnknownAllowed = true }
}

// BindError reports every key that Bind could not bind.
type BindError struct {
	Prefix   string
	Problems []*Problem
}

// Error lists the Problems, one per line if there are several.
func (e *BindError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "gonf: bind %q: ", e.Prefix)
	if len(e.Problems) == 1 {
		b.WriteString(e.Problems[0].Error())
		return b.String()
	}
	fmt.Fprintf(&b, "%d problems:", len(e.Problems))
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.Error())
	}
	return b.String()
}

// Unwrap returns the Problems, so errors.Is(err, ErrNotFound) reports a
// missing value.
func (e *BindError) Unwrap() []error {
	errs := make([]error, len(e.Problems))
	for i, p := range e.Problems {
		errs[i] = p
	}
	return errs
}

// Problem is one key that Bind could not bind.
type Problem struct {
	Key string
	// Origin is where the offending value came from; zero if the key has no
	// value or the default is invalid.
	Origin Origin
	// Err matches ErrNotFound if a required value is missing.
	Err error
}

// Error names the key, the problem and, if known, where the value came from.
func (p *Problem) Error() string {
	if p.Key == "" {
		return p.Err.Error()
	}
	if errors.Is(p.Err, ErrNotFound) {
		return p.Key + ": required, no value"
	}
	msg := p.Key + ": " + p.Err.Error()
	if where := cmp.Or(p.Origin.Location, p.Origin.Source); where != "" {
		msg += " (from " + where + ")"
	}
	return msg
}

// Unwrap returns Err.
func (p *Problem) Unwrap() error { return p.Err }

var (
	durationType = reflect.TypeFor[time.Duration]()
	textType     = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// binder binds the fields of one Bind call and collects its problems.
type binder struct {
	c        *Config
	problems []*Problem
	// known holds the keys of the bound fields, open the prefixes of
	// slices and maps, whose keys are all known.
	known map[string]struct{}
	open  []string
	// referenced holds the keys that ${...} expressions resolved, such as
	// db.host in db.url=jdbc:postgresql://${db.host}/app.
	referenced map[string]struct{}
}

func (b *binder) reference(key string) {
	b.referenced[key] = struct{}{}
}

// validator is implemented by structs that check themselves after Bind.
type validator interface {
	Validate() error
}

func (b *binder) fail(key string, origin Origin, err error) {
	b.problems = append(b.problems, &Problem{Key: key, Origin: origin, Err: err})
}

// value returns the expanded value of key and whether it is set. An empty
// value counts as not set.
func (b *binder) value(key string) (string, Origin, bool, error) {
	v, ok := b.c.Lookup(key)
	if !ok {
		return "", Origin{}, false, nil
	}
	s, err := b.c.expand(key, v.Raw, b.reference)
	if err != nil {
		return "", v.Origin, false, err
	}
	return s, v.Origin, s != "", nil
}

// bind binds key to v. def is the default tag, nil if there is none.
func (b *binder) bind(key string, def *string, v reflect.Value) {
	b.known[key] = struct{}{}
	t := v.Type()
	switch {
	case isScalar(t):
		b.bindScalar(key, def, v)
	case t.Kind() == reflect.Pointer:
		b.bindPointer(key, def, v)
	case t.Kind() == reflect.Slice:
		b.bindSlice(key, def, v)
	case def != nil:
		b.fail(key, Origin{}, fmt.Errorf("default not supported for %s", t))
	case t.Kind() == reflect.Struct:
		b.bindStruct(key, v)
	case t.Kind() == reflect.Map:
		b.bindMap(key, v)
	default:
		b.fail(key, Origin{}, fmt.Errorf("unsupported type %s", t))
	}
}

// bindStruct binds the fields of v, then validates v if they had no
// problems.
func (b *binder) bindStruct(prefix string, v reflect.Value) {
	before := len(b.problems)
	for _, f := range fields(v.Type()) {
		key := join(prefix, f.name)
		if f.isEmbeddedPointer {
			b.fail(key, Origin{}, fmt.Errorf("embedded pointer %s not supported, embed the struct", f.typ))
			continue
		}
		b.bind(key, f.def, v.FieldByIndex(f.index))
	}
	if len(b.problems) > before {
		return
	}
	if val, ok := v.Addr().Interface().(validator); ok {
		if err := val.Validate(); err != nil {
			b.fail(prefix, Origin{}, err)
		}
	}
}

func (b *binder) bindScalar(key string, def *string, v reflect.Value) {
	s, origin, ok, err := b.value(key)
	if err != nil {
		b.fail(key, origin, err)
		return
	}
	if ok {
		if err := convert(s, v); err != nil {
			b.fail(key, origin, err)
		}
		return
	}
	if def == nil {
		b.fail(key, Origin{}, ErrNotFound)
		return
	}
	s, err = b.c.expand(key, *def, b.reference)
	if err != nil {
		b.fail(key, Origin{}, fmt.Errorf("default: %w", err))
		return
	}
	if s == "" {
		return
	}
	if err := convert(s, v); err != nil {
		b.fail(key, Origin{}, fmt.Errorf("default: %w", err))
	}
}

// bindPointer leaves v nil unless key has a value or a default.
func (b *binder) bindPointer(key string, def *string, v reflect.Value) {
	elem := v.Type().Elem()
	if def == nil && !b.present(key, elem, nil) {
		return
	}
	p := reflect.New(elem)
	b.bind(key, def, p.Elem())
	v.Set(p)
}

func (b *binder) bindSlice(key string, def *string, v reflect.Value) {
	b.open = append(b.open, key+"[")
	s, origin, ok, err := b.value(key)
	switch {
	case err != nil:
		b.fail(key, origin, err)
		return
	case ok:
		b.bindList(key, s, origin, v)
		return
	}
	if indices := b.indices(key, v.Type().Elem()); len(indices) > 0 {
		out := reflect.MakeSlice(v.Type(), len(indices), len(indices))
		for i, n := range indices {
			b.bind(index(key, n), nil, out.Index(i))
		}
		v.Set(out)
		return
	}
	if def == nil {
		return
	}
	s, err = b.c.expand(key, *def, b.reference)
	if err != nil {
		b.fail(key, Origin{}, fmt.Errorf("default: %w", err))
		return
	}
	b.bindList(key, s, Origin{}, v)
}

// bindList binds the comma-separated list s to the slice v.
func (b *binder) bindList(key, s string, origin Origin, v reflect.Value) {
	elem := v.Type().Elem()
	if !isScalar(elem) {
		err := fmt.Errorf("comma-separated value for a list of %s, use indexed keys such as %s", elem, index(key, 0))
		b.fail(key, origin, err)
		return
	}
	parts := splitList(s)
	if len(parts) == 0 {
		return
	}
	out := reflect.MakeSlice(v.Type(), len(parts), len(parts))
	for i, part := range parts {
		if err := convert(part, out.Index(i)); err != nil {
			b.fail(key, origin, fmt.Errorf("element %d: %w", i, err))
		}
	}
	v.Set(out)
}

func (b *binder) bindMap(key string, v reflect.Value) {
	t := v.Type()
	if !isScalar(t.Key()) {
		b.fail(key, Origin{}, fmt.Errorf("unsupported map key type %s", t.Key()))
		return
	}
	isGroupValue := isGroup(t.Elem())
	prefix := key + "."
	b.open = append(b.open, prefix)
	var names []string
	for _, k := range b.c.keys {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		if name := mapEntry(rest, isGroupValue); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	m := reflect.MakeMapWithSize(t, len(names))
	for _, name := range names {
		entry := prefix + name
		if !b.present(entry, t.Elem(), nil) {
			continue
		}
		mk := reflect.New(t.Key()).Elem()
		if err := convert(unquote(name), mk); err != nil {
			b.fail(entry, Origin{}, fmt.Errorf("map key: %w", err))
			continue
		}
		mv := reflect.New(t.Elem()).Elem()
		b.bind(entry, nil, mv)
		m.SetMapIndex(mk, mv)
	}
	if m.Len() > 0 {
		v.Set(m)
	}
}

// checkUnknown reports the keys under prefix that no field binds.
func (b *binder) checkUnknown(prefix string) {
	under := prefix + "."
	for _, k := range b.c.keys {
		if !strings.HasPrefix(k, under) || b.isKnown(k) {
			continue
		}
		err := errors.New("unknown key")
		if s := b.suggest(k); s != "" {
			err = fmt.Errorf("unknown key, did you mean %s?", s)
		}
		v, _ := b.c.Lookup(k)
		b.fail(k, v.Origin, err)
	}
}

func (b *binder) isKnown(key string) bool {
	if _, ok := b.known[key]; ok {
		return true
	}
	if _, ok := b.referenced[key]; ok {
		return true
	}
	return slices.ContainsFunc(b.open, func(p string) bool { return strings.HasPrefix(key, p) })
}

// maxSuggestDistance is the largest edit distance between an unknown key
// and a known key suggested for it.
const maxSuggestDistance = 3

// suggest returns the known key closest to key, or "" if none is close.
func (b *binder) suggest(key string) string {
	best, bestDistance := "", maxSuggestDistance+1
	for _, k := range slices.Sorted(maps.Keys(b.known)) {
		if d := distance(key, k); d < bestDistance {
			best, bestDistance = k, d
		}
	}
	return best
}

// distance returns the Levenshtein distance between a and b.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// indices returns the indices of key[n] in ascending order: those found
// among the keys and those found by trying key[0], key[1], ..., which finds
// environment variables such as HOSTS_0_.
func (b *binder) indices(key string, elem reflect.Type) []int {
	var out []int
	open := key + "["
	for _, k := range b.c.keys {
		rest, ok := strings.CutPrefix(k, open)
		if !ok {
			continue
		}
		digits, after, ok := strings.Cut(rest, "]")
		if !ok || !isDigits(digits) || after != "" && after[0] != '.' && after[0] != '[' {
			continue
		}
		if n, err := strconv.Atoi(digits); err == nil && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	for n := 0; slices.Contains(out, n) || b.present(index(key, n), elem, nil); n++ {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// present reports whether key has a value of type t: a value for a scalar,
// any key under it for a struct, slice or map. Struct fields are tried one
// by one, so environment variables count too. stack holds the struct types
// being checked and stops recursive types.
func (b *binder) present(key string, t reflect.Type, stack []reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case isScalar(t):
		_, _, ok, err := b.value(key)
		return ok || err != nil
	case t.Kind() == reflect.Struct:
		if b.hasKeysWithPrefix(key + ".") {
			return true
		}
		if slices.Contains(stack, t) {
			return false
		}
		stack = append(stack, t)
		for _, f := range fields(t) {
			if !f.isEmbeddedPointer && b.present(join(key, f.name), f.typ, stack) {
				return true
			}
		}
		return false
	case t.Kind() == reflect.Slice:
		_, _, ok, err := b.value(key)
		return ok || err != nil || b.hasKeysWithPrefix(key+"[") || b.present(index(key, 0), t.Elem(), stack)
	case t.Kind() == reflect.Map:
		return b.hasKeysWithPrefix(key + ".")
	}
	return false
}

func (b *binder) hasKeysWithPrefix(prefix string) bool {
	return slices.ContainsFunc(b.c.keys, func(k string) bool { return strings.HasPrefix(k, prefix) })
}

// structField is a field Bind binds.
type structField struct {
	name  string
	index []int
	typ   reflect.Type
	// def is the default tag, nil if there is none.
	def               *string
	isEmbeddedPointer bool
}

// fields returns the fields of struct type t, with embedded structs
// flattened.
func fields(t reflect.Type) []structField {
	var out []structField
	for i := range t.NumField() {
		f := t.Field(i)
		name, hasTag := f.Tag.Lookup("cfg")
		if name == "-" {
			continue
		}
		if f.Anonymous && !hasTag {
			switch {
			case f.Type.Kind() == reflect.Struct && !isText(f.Type):
				for _, sub := range fields(f.Type) {
					sub.index = append([]int{i}, sub.index...)
					out = append(out, sub)
				}
				continue
			case f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct:
				out = append(out, structField{name: kebab(f.Name), typ: f.Type, isEmbeddedPointer: true})
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = kebab(f.Name)
		}
		sf := structField{name: name, index: []int{i}, typ: f.Type}
		if def, ok := f.Tag.Lookup("default"); ok {
			sf.def = &def
		}
		out = append(out, sf)
	}
	return out
}

// convert parses s into v, which must be addressable and of a scalar type.
func convert(s string, v reflect.Value) error {
	t := v.Type()
	if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
		if err := u.UnmarshalText([]byte(s)); err != nil {
			return fmt.Errorf("invalid %s %q: %w", t, s, err)
		}
		return nil
	}
	if t == durationType {
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q, want e.g. 5s or 1h30m", s)
		}
		v.SetInt(int64(d))
		return nil
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		isTrue, err := parseBool(s)
		if err != nil {
			return err
		}
		v.SetBool(isTrue)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return numberError(s, t, err)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return numberError(s, t, err)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return numberError(s, t, err)
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("unsupported type %s", t)
	}
	return nil
}

func numberError(s string, t reflect.Type, err error) error {
	if errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("%q out of range for %s", s, t)
	}
	return fmt.Errorf("invalid %s %q", t, s)
}

// parseBool is stricter than SmallRye, which reads every unknown value as
// false, so a typo such as "ture" is an error here.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("invalid bool %q, want true/false, 1/0, yes/no or on/off", s)
}

// isScalar reports whether a value of type t is converted from a single
// string.
func isScalar(t reflect.Type) bool {
	if isText(t) || t == durationType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func isText(t reflect.Type) bool {
	return reflect.PointerTo(t).Implements(textType)
}

// isGroup reports whether values of type t live under several keys, so that
// a map entry of type t is named by one key segment only.
func isGroup(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case isScalar(t):
		return false
	case t.Kind() == reflect.Slice:
		return isGroup(t.Elem())
	}
	return t.Kind() == reflect.Struct || t.Kind() == reflect.Map
}

// mapEntry returns the name of the map entry that the key rest (the part
// after the map's prefix) belongs to: the first segment for group values,
// all of rest otherwise, both without a trailing [n].
func mapEntry(rest string, isGroupValue bool) string {
	name := rest
	if isGroupValue {
		end := strings.IndexByte(rest, '.')
		if strings.HasPrefix(rest, `"`) {
			if q := strings.IndexByte(rest[1:], '"'); q >= 0 {
				end = strings.IndexByte(rest[q+2:], '.')
				if end >= 0 {
					end += q + 2
				}
			}
		}
		if end >= 0 {
			name = rest[:end]
		}
	}
	open := strings.LastIndexByte(name, '[')
	if open > 0 && strings.HasSuffix(name, "]") && isDigits(name[open+1:len(name)-1]) {
		name = name[:open]
	}
	return name
}

func unquote(name string) string {
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		return name[1 : len(name)-1]
	}
	return name
}

// splitList splits s at commas not escaped as \, and drops empty elements.
// Elements are trimmed.
func splitList(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if part := strings.TrimSpace(cur.String()); part != "" {
			out = append(out, part)
		}
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == ',':
			cur.WriteByte(',')
			i++
		case s[i] == ',':
			flush()
		default:
			cur.WriteByte(s[i])
		}
	}
	flush()
	return out
}

// kebab converts a Go field name to a key: MaxSize becomes max-size,
// HTTPPort http-port, URL url and IPV6Addr ipv6-addr. The rules follow
// SmallRye's StringUtil.skewer; like there, IPv6Addr becomes i-pv6-addr.
func kebab(name string) string {
	r := []rune(name)
	var b strings.Builder
	for i, c := range r {
		if !unicode.IsUpper(c) {
			b.WriteRune(c)
			continue
		}
		if i > 0 {
			prev := r[i-1]
			isNextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && isNextLower {
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func index(key string, n int) string {
	return key + "[" + strconv.Itoa(n) + "]"
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
