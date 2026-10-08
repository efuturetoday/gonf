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
// is known. The check is skipped for an empty prefix and for environment
// variable names such as DB_POOL_MAX_SIZ, which cannot be told apart from
// variables for other programs. Finally, the validator given with
// WithValidator checks the whole struct.
//
// A field that is no Secret but whose value or default refers to a secret
// key, as in url=postgres://app:${db.password}@db/app, is a problem: the
// field would hold the secret and print it. Make the field a Secret too.
// Secret keys are those that Explain masks.
//
// A field type or tag that Bind cannot read, such as a func field or a
// default for a struct, is a problem even inside an optional struct or a
// list or map without entries; Bind then reads no value.
//
// All problems are collected and returned together as a *BindError.
func Bind[T any](c *Config, prefix string, opts ...BindOption) (T, error) {
	var out T
	v := reflect.ValueOf(&out).Elem()
	m, err := c.converters.rootModel(prefix, v.Type())
	if err != nil {
		return out, err
	}
	b := c.bindRoot(prefix, m, v, opts)
	checkUnknown([]*binder{b})
	b.checkSecretReferences()
	if err := b.err(); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// bindRoot binds the struct v of model m to the keys under prefix and
// validates it. It does not check for unknown keys; see checkUnknown. If m
// has problems, it reports them and binds nothing.
func (c *Config) bindRoot(prefix string, m *model, v reflect.Value, opts []BindOption) *binder {
	b := &binder{
		c:          c,
		cs:         c.converters,
		prefix:     prefix,
		known:      map[string]struct{}{},
		referenced: map[string]struct{}{},
		secrets:    map[string]struct{}{},
		defaults:   map[string]string{},
	}
	for _, opt := range opts {
		opt(&b.options)
	}
	if problems := m.problems(prefix); len(problems) > 0 {
		// Without the fields bound, any key under prefix may be known.
		b.problems = problems
		b.open = append(b.open, prefix+".")
		return b
	}
	b.bindStruct(prefix, m, v)
	if len(b.problems) == 0 && c.validator != nil {
		if err := c.validator(v.Addr().Interface()); err != nil {
			b.fail(prefix, Origin{}, err)
		}
	}
	return b
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
	cs       converters
	prefix   string
	options  bindOptions
	problems []*Problem
	// known holds the keys of the bound fields, open the prefixes of
	// slices and maps, whose keys are all known.
	known map[string]struct{}
	open  []string
	// referenced holds the keys that ${...} expressions resolved, such as
	// db.host in db.url=postgres://${db.host}/app; references holds them
	// with the key whose value referred to them, in the order of binding.
	referenced map[string]struct{}
	references []reference
	// secrets holds the keys of fields that hold a Secret, defaults the
	// default tags by key.
	secrets  map[string]struct{}
	defaults map[string]string
}

// reference is a key that the value of another key refers to with ${...}.
type reference struct {
	from, to string
}

// referencesOf returns the function that records the keys that the value
// of key refers to.
func (b *binder) referencesOf(key string) func(to string) {
	return func(to string) {
		b.referenced[to] = struct{}{}
		b.references = append(b.references, reference{from: key, to: to})
	}
}

// checkSecretReferences reports every key that is no Secret but whose
// value refers to a secret key: the field would hold the secret and print
// it. Secret keys are those of Explain, so it runs after all binders of a
// Load have recorded their Secrets.
func (b *binder) checkSecretReferences() {
	reported := map[string]bool{}
	for _, r := range b.references {
		_, isSecretField := b.secrets[r.from]
		_, isSecretTarget := b.secrets[r.to]
		isSecretTarget = isSecretTarget || b.c.isSecret(r.to)
		if isSecretField || !isSecretTarget || reported[r.from] {
			continue
		}
		reported[r.from] = true
		v, _ := b.c.Lookup(r.from)
		b.fail(r.from, v.Origin, fmt.Errorf("refers to the secret %s, make the field a gonf.Secret", r.to))
	}
}

// validator is implemented by structs that check themselves after Bind.
type validator interface {
	Validate() error
}

// err returns the problems as a *BindError, or nil.
func (b *binder) err() error {
	if len(b.problems) == 0 {
		return nil
	}
	return &BindError{Prefix: b.prefix, Problems: b.problems}
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
	s, err := b.c.expand(key, v.Raw, b.referencesOf(key))
	if err != nil {
		return "", v.Origin, false, err
	}
	return s, v.Origin, s != "", nil
}

// bind binds key to v, of field f. bindRoot binds only models without
// problems, so f has a valid model.
func (b *binder) bind(key string, f field, v reflect.Value) {
	b.known[key] = struct{}{}
	if f.def != nil {
		b.defaults[key] = *f.def
	}
	if f.m.isSecret {
		b.secrets[key] = struct{}{}
	}
	switch f.m.kind {
	case kindPointer:
		b.bindPointer(key, f, v)
	case kindScalar:
		b.bindScalar(key, f, v)
	case kindList:
		b.bindSlice(key, f, v)
	case kindGroup:
		b.bindStruct(key, f.m, v)
	case kindMap:
		b.bindMap(key, f, v)
	}
}

// bindStruct binds the fields of v, then validates v if they had no
// problems.
func (b *binder) bindStruct(prefix string, m *model, v reflect.Value) {
	before := len(b.problems)
	for _, f := range m.fields {
		b.bind(join(prefix, f.name), f, v.FieldByIndex(f.index))
	}
	if len(b.problems) > before {
		return
	}
	if val, ok := reflect.TypeAssert[validator](v.Addr()); ok {
		if err := val.Validate(); err != nil {
			b.fail(prefix, Origin{}, err)
		}
	}
}

func (b *binder) bindScalar(key string, f field, v reflect.Value) {
	s, origin, ok, err := b.value(key)
	if err != nil {
		b.fail(key, origin, err)
		return
	}
	if ok {
		if err := b.cs.convert(s, v); err != nil {
			b.fail(key, origin, err)
		}
		return
	}
	switch {
	case f.isRequired:
		b.fail(key, Origin{}, ErrNotFound)
		return
	case f.def == nil:
		return
	}
	s, err = b.c.expand(key, *f.def, b.referencesOf(key))
	if err != nil {
		b.fail(key, Origin{}, fmt.Errorf("default: %w", err))
		return
	}
	if s == "" {
		return
	}
	if err := b.cs.convert(s, v); err != nil {
		b.fail(key, Origin{}, fmt.Errorf("default: %w", err))
	}
}

// bindPointer leaves v nil unless key has a value or a default.
func (b *binder) bindPointer(key string, f field, v reflect.Value) {
	if f.def == nil && !b.present(key, f.m, nil) {
		return
	}
	p := reflect.New(f.m.elem.typ)
	b.bind(key, f.pointee(), p.Elem())
	v.Set(p)
}

func (b *binder) bindSlice(key string, f field, v reflect.Value) {
	b.open = append(b.open, key+"[")
	s, origin, ok, err := b.value(key)
	switch {
	case err != nil:
		b.fail(key, origin, err)
		return
	case ok:
		b.bindList(key, s, origin, f.m, v)
		return
	}
	if indices := b.indices(key, f.m.elem); len(indices) > 0 {
		out := reflect.MakeSlice(v.Type(), len(indices), len(indices))
		for i, n := range indices {
			b.bind(index(key, n), f.element(), out.Index(i))
		}
		v.Set(out)
		return
	}
	if f.def == nil {
		return
	}
	s, err = b.c.expand(key, *f.def, b.referencesOf(key))
	if err != nil {
		b.fail(key, Origin{}, fmt.Errorf("default: %w", err))
		return
	}
	b.bindList(key, s, Origin{}, f.m, v)
}

// bindList binds the comma-separated list s to the slice v of model m.
func (b *binder) bindList(key, s string, origin Origin, m *model, v reflect.Value) {
	if m.elem.kind != kindScalar {
		err := fmt.Errorf("comma-separated value for a list of %s, use indexed keys such as %s", m.elem.typ, index(key, 0))
		b.fail(key, origin, err)
		return
	}
	parts := splitList(s)
	if len(parts) == 0 {
		return
	}
	out := reflect.MakeSlice(v.Type(), len(parts), len(parts))
	for i, part := range parts {
		if err := b.cs.convert(part, out.Index(i)); err != nil {
			b.fail(key, origin, fmt.Errorf("element %d: %w", i, err))
		}
	}
	v.Set(out)
}

func (b *binder) bindMap(key string, f field, v reflect.Value) {
	t := v.Type()
	isGroupValue := f.m.elem.isGroup()
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
		if !b.present(entry, f.m.elem, nil) {
			continue
		}
		mk := reflect.New(t.Key()).Elem()
		if err := b.cs.convert(unquote(name), mk); err != nil {
			b.fail(entry, Origin{}, fmt.Errorf("map key: %w", err))
			continue
		}
		mv := reflect.New(t.Elem()).Elem()
		b.bind(entry, f.entry(), mv)
		m.SetMapIndex(mk, mv)
	}
	if m.Len() > 0 {
		v.Set(m)
	}
}

// checkUnknown reports, for every binder that checks for unknown keys, the
// keys under its prefix that none of the binders knows. Binders that share
// a prefix thus do not report each other's keys.
func checkUnknown(binders []*binder) {
	for _, b := range binders {
		if b.prefix != "" && !b.options.isUnknownAllowed {
			b.checkUnknown(binders)
		}
	}
}

func (b *binder) checkUnknown(binders []*binder) {
	under := b.prefix + "."
	for _, k := range b.c.keys {
		isKnown := slices.ContainsFunc(binders, func(o *binder) bool { return o.isKnown(k) })
		if !strings.HasPrefix(k, under) || isKnown {
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
const maxSuggestDistance = 2

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
func (b *binder) indices(key string, elem *model) []int {
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

// present reports whether key has a value of model m: a value for a
// scalar, any key under it for a group, list or map. Fields are tried one
// by one, so environment variables count too. stack holds the groups being
// checked and stops recursive types.
func (b *binder) present(key string, m *model, stack []*model) bool {
	switch m.kind {
	case kindScalar:
		_, _, ok, err := b.value(key)
		return ok || err != nil
	case kindPointer:
		return b.present(key, m.elem, stack)
	case kindGroup:
		if b.hasKeysWithPrefix(key + ".") {
			return true
		}
		if slices.Contains(stack, m) {
			return false
		}
		stack = append(stack, m)
		for _, f := range m.fields {
			if b.present(join(key, f.name), f.m, stack) {
				return true
			}
		}
		return false
	case kindList:
		_, _, ok, err := b.value(key)
		return ok || err != nil || b.hasKeysWithPrefix(key+"[") || b.present(index(key, 0), m.elem, stack)
	case kindMap:
		return b.hasKeysWithPrefix(key + ".")
	}
	return false
}

func (b *binder) hasKeysWithPrefix(prefix string) bool {
	return slices.ContainsFunc(b.c.keys, func(k string) bool { return strings.HasPrefix(k, prefix) })
}

// convert parses s into v, which must be addressable and of a scalar type.
func (cs converters) convert(s string, v reflect.Value) error {
	t := v.Type()
	if fn, ok := cs[t]; ok {
		out, err := fn(s)
		if err != nil {
			return fmt.Errorf("invalid %s %q: %w", t, s, err)
		}
		v.Set(out)
		return nil
	}
	if sec, ok := reflect.TypeAssert[secret](v.Addr()); ok {
		inner := reflect.New(sec.secretType()).Elem()
		if err := cs.convert(s, inner); err != nil {
			return fmt.Errorf("invalid %s, value hidden", inner.Type())
		}
		sec.setSecret(inner)
		return nil
	}
	if u, ok := reflect.TypeAssert[encoding.TextUnmarshaler](v.Addr()); ok {
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
		if err := checkLeadingZero(s); err != nil {
			return err
		}
		n, err := strconv.ParseInt(s, 0, t.Bits())
		if err != nil {
			return numberError(s, t, err)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if err := checkLeadingZero(s); err != nil {
			return err
		}
		n, err := strconv.ParseUint(s, 0, t.Bits())
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

// checkLeadingZero reports an integer such as 0644 as ambiguous: Go reads it
// as octal, many other tools as decimal. Without the leading zero or with a
// prefix (0o644) it is clear.
func checkLeadingZero(s string) error {
	sign, digits := "", s
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign, digits = s[:1], s[1:]
	}
	if len(digits) < 2 || digits[0] != '0' {
		return nil
	}
	// A letter after the zero is a prefix such as 0x.
	isDigit := '0' <= digits[1] && digits[1] <= '9'
	if !isDigit && digits[1] != '_' {
		return nil
	}
	decimal := cmp.Or(strings.TrimLeft(digits, "0_"), "0")
	return fmt.Errorf("ambiguous leading zero in %q, write %s0o%s for octal or %s%s for decimal", s, sign, digits[1:], sign, decimal)
}

func numberError(s string, t reflect.Type, err error) error {
	if errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("%q out of range for %s", s, t)
	}
	return fmt.Errorf("invalid %s %q", t, s)
}

// parseBool reports an unknown value such as the typo "ture" as an error
// rather than reading it as false.
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
func (cs converters) isScalar(t reflect.Type) bool {
	if _, ok := cs[t]; ok {
		return true
	}
	if isSecret(t) {
		return cs.isScalar(secretElem(t))
	}
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
// HTTPPort http-port, URL url and IPV6Addr ipv6-addr. IPv6Addr becomes
// i-pv6-addr; give such a field a cfg tag.
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
