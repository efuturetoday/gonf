package gonf

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNotFound is returned by Get when the requested key has no value.
var ErrNotFound = errors.New("gonf: key not found")

// Get returns the value of key with all expressions expanded.
//
//	${key}          value of key, an error if it has none
//	${key:default}  value of key, or default (itself expanded) if it has none
//	$${             a literal "${"
//
// References are resolved like Lookup, so profiles and KeyMappers apply:
// ${HOME} finds the environment variable. If key itself has no value, the
// error matches ErrNotFound; a missing reference, a cycle or a malformed
// expression is reported with the chain of keys involved.
func (c *Config) Get(key string) (string, error) {
	v, ok := c.Lookup(key)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	e := expander{c: c, chain: []string{key}}
	return e.expand(v.Raw)
}

// expander expands the expressions of one Get call.
type expander struct {
	c *Config
	// chain holds the keys being expanded, outermost first.
	chain []string
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
				return "", e.errorf("value of %q: unclosed \"${\"", e.chain[len(e.chain)-1])
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
	}
	return b.String(), nil
}

// reference resolves the body of one ${...} expression.
func (e *expander) reference(body string) (string, error) {
	key, def, hasDefault := strings.Cut(body, ":")
	current := e.chain[len(e.chain)-1]
	switch {
	case key == "":
		return "", e.errorf("value of %q: empty reference", current)
	case strings.Contains(key, "${"):
		return "", e.errorf("value of %q: nested expression in reference %q is not supported", current, key)
	case slices.Contains(e.chain, key):
		return "", e.errorf("cycle %s -> %s", strings.Join(e.chain, " -> "), key)
	}
	v, ok := e.c.Lookup(key)
	if !ok {
		if hasDefault {
			return e.expand(def)
		}
		return "", e.errorf("unresolved reference %q (%s)", key, strings.Join(e.chain, " -> "))
	}
	e.chain = append(e.chain, key)
	defer func() { e.chain = e.chain[:len(e.chain)-1] }()
	return e.expand(v.Raw)
}

func (e *expander) errorf(format string, args ...any) error {
	return fmt.Errorf("gonf: key %q: %s", e.chain[0], fmt.Sprintf(format, args...))
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
