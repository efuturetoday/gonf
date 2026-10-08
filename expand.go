package gonf

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNotFound is returned by Get when the requested key has no value.
var ErrNotFound = errors.New("gonf: key not found")

// Limits of one Get call. They stop values such as a=${b}${b}, b=${c}${c},
// ... from growing exponentially. A cycle is detected separately, and the
// reference limit also bounds the depth.
const (
	maxReferences  = 1000
	maxExpandedLen = 1 << 20 // bytes
)

// Get returns the value of key with all expressions expanded.
//
//	${key}          value of key, an error if it has none
//	${key:default}  value of key, or default (itself expanded) if it has none
//	${name::value}  value decrypted by the SecretHandler name
//	$${             a literal "${"
//
// References are resolved like Lookup, so profiles and KeyMappers apply:
// ${HOME} finds the environment variable. If key itself has no value, the
// error matches ErrNotFound; a missing reference, a cycle or a malformed
// expression is reported with the chain of keys involved. One Get resolves
// at most 1000 references and builds at most 1 MiB; beyond that it returns
// an error.
func (c *Config) Get(key string) (string, error) {
	v, ok := c.Lookup(key)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	s, err := c.expand(key, v.Raw, v.Origin, hooks{call: decrypt})
	if err != nil {
		return "", fmt.Errorf("gonf: key %q: %w", key, err)
	}
	return s, nil
}

// expand expands the expressions in s, which is the value of key or its
// default and comes from origin. Its errors do not name key; callers add
// it. h connects the expressions to the layers above.
func (c *Config) expand(key, s string, origin Origin, h hooks) (string, error) {
	e := expander{c: c, chain: []string{key}, origin: origin, hooks: h}
	return e.expand(s)
}

// hooks connect expand to the layers above it, which give the expressions
// their meaning: expand only knows the syntax.
type hooks struct {
	// call resolves a ${name::arg} expression in the value of key; origin
	// is where the expression is written. It must not be nil. A plain
	// function, not a method value, keeps Get free of allocations.
	call func(c *Config, key, name, arg string, origin Origin) (string, error)
	// reference is called with every key that a ${key} expression
	// resolved. If it returns ok, replacement is used instead of the
	// value of key. It may be nil.
	reference func(key string) (replacement string, ok bool)
}

// expander expands the expressions of one Get call.
type expander struct {
	c *Config
	// chain holds the keys being expanded, outermost first.
	chain []string
	// origin is where the outermost value comes from.
	origin Origin
	// references counts the resolved ${...} expressions.
	references int
	hooks      hooks
}

func (e *expander) expand(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "$${"):
			b.WriteString("${")
			i += 3
		case strings.HasPrefix(s[i:], "${"):
			end := closingBrace(s, i+2)
			if end < 0 {
				return "", fmt.Errorf("value of %q: unclosed \"${\"", e.chain[len(e.chain)-1])
			}
			v, err := e.reference(s[i+2 : end])
			if err != nil {
				return "", err
			}
			b.WriteString(v)
			i = end + 1
		default:
			b.WriteByte(s[i])
			i++
		}
		if b.Len() > maxExpandedLen {
			return "", fmt.Errorf("expansion too large: more than %d bytes", maxExpandedLen)
		}
	}
	return b.String(), nil
}

// reference resolves the body of one ${...} expression.
func (e *expander) reference(body string) (string, error) {
	e.references++
	if e.references > maxReferences {
		return "", fmt.Errorf("expansion too large: more than %d references", maxReferences)
	}
	current := e.chain[len(e.chain)-1]
	if name, value, ok := strings.Cut(body, "::"); ok {
		return e.call(name, value)
	}
	key, def, hasDefault := strings.Cut(body, ":")
	switch {
	case key == "":
		return "", fmt.Errorf("value of %q: empty reference", current)
	case strings.Contains(key, "${"):
		return "", fmt.Errorf("value of %q: nested expression in reference %q is not supported", current, key)
	case slices.Contains(e.chain, key):
		return "", fmt.Errorf("cycle %s -> %s", strings.Join(e.chain, " -> "), key)
	}
	v, ok := e.c.Lookup(key)
	if !ok {
		if hasDefault {
			return e.expand(def)
		}
		if len(e.chain) == 1 {
			return "", fmt.Errorf("unresolved reference %q", key)
		}
		return "", fmt.Errorf("unresolved reference %q (%s)", key, strings.Join(e.chain, " -> "))
	}
	if e.hooks.reference != nil {
		if replacement, ok := e.hooks.reference(key); ok {
			return replacement, nil
		}
	}
	e.chain = append(e.chain, key)
	defer func() { e.chain = e.chain[:len(e.chain)-1] }()
	return e.expand(v.Raw)
}

// call resolves a ${name::arg} expression with hooks.call.
func (e *expander) call(name, arg string) (string, error) {
	current := e.chain[len(e.chain)-1]
	if strings.Contains(arg, "${") {
		return "", fmt.Errorf("value of %q: nested expression in a %s value is not supported", current, name)
	}
	origin := e.origin
	if len(e.chain) > 1 {
		v, _ := e.c.Lookup(current)
		origin = v.Origin
	}
	return e.hooks.call(e.c, current, name, arg, origin)
}

// closingBrace returns the index of the '}' that closes the expression whose
// body starts at start, or -1. Nested "${" (also escaped "$${") open a level.
func closingBrace(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "${"):
			depth++
			i++
		case s[i] == '}':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}
