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
// reference limit also bounds the depth. Function calls count as
// references, and their results count toward the length.
const (
	maxReferences  = 1000
	maxExpandedLen = 1 << 20 // bytes
)

// Get returns the value of key with all expressions expanded.
//
//	${key}          value of key, an error if it has none
//	${key:default}  value of key, or default (itself expanded) if it has none
//	${name::arg}    result of the Function name for arg (itself expanded)
//	$${             a literal "${"
//
// References are resolved like Lookup, so profiles apply. An environment
// variable is found only under a key it matched, such as ${db.port} for
// DB_PORT (see MappedSource); ${env::HOME} reads any variable if the env
// Function is registered. If key itself has no value, the error matches
// ErrNotFound; a missing reference, a cycle, a failing Function or a
// malformed expression is reported with the keys involved. One Get
// resolves at most 1000 references and builds at most 1 MiB; beyond that
// it returns an error.
func (c *Config) Get(key string) (string, error) {
	v, ok := c.Lookup(key)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	s, err := c.expand(key, v.Raw, v.Origin, hooks{call: invoke})
	if err != nil {
		return "", fmt.Errorf("gonf: key %q: %w", key, err)
	}
	return s, nil
}

// Eval expands the expressions in expr as Get expands a value, such as
//
//	cfg.Eval("${env::HOME}/logs")
//	cfg.Eval("postgres://${db.host:localhost}/app")
//
// A missing reference is an error that does not match ErrNotFound. Eval
// records no reads, and Functions get the zero Origin.
func (c *Config) Eval(expr string) (string, error) {
	s, err := c.expand("", expr, Origin{}, hooks{call: invoke})
	if err != nil {
		return "", fmt.Errorf("gonf: eval: %w", err)
	}
	return s, nil
}

// expand expands the expressions in s, which is the value of key or its
// default and comes from origin; key is "" for Eval. Its errors do not
// name key; callers add it. h connects the expressions to the layers
// above.
func (c *Config) expand(key, s string, origin Origin, h hooks) (string, error) {
	e := expander{c: c, chain: []string{key}, origin: origin, hooks: h}
	return e.expand(s)
}

// hooks connect expand to the layers above it, which give the expressions
// their meaning: expand only knows the syntax.
type hooks struct {
	// call resolves a ${name::arg} expression and reports whether the
	// result is a secret. It must not be nil. A plain function, not a
	// method value, keeps Get from allocating it.
	call func(c *Config, s callSite) (string, bool, error)
	// reference is called with every key that a ${key} expression
	// resolved. If it returns ok, the key is secret and replacement is
	// used instead of its value. It may be nil.
	reference func(key string) (replacement string, ok bool)
}

// expander expands the expressions of one Get call.
type expander struct {
	c *Config
	// chain holds the keys being expanded, outermost first; the first is
	// "" for Eval.
	chain []string
	// origin is where the outermost value comes from.
	origin Origin
	// references counts the resolved ${...} expressions.
	references int
	// isSecret reports whether a secret went into the text expanded since
	// the innermost Function argument began.
	isSecret bool
	hooks    hooks
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
				return "", fmt.Errorf("%s: unclosed \"${\"", e.value())
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
	if name, arg, ok := cutFunction(body); ok {
		return e.call(name, arg)
	}
	key, def, hasDefault := strings.Cut(body, ":")
	switch {
	case key == "":
		return "", fmt.Errorf("%s: empty reference", e.value())
	case strings.Contains(key, "${"):
		return "", fmt.Errorf("%s: nested expression in reference %q is not supported", e.value(), key)
	case slices.Contains(e.chain, key):
		return "", fmt.Errorf("cycle %s -> %s", e.path(), key)
	}
	v, ok := e.c.Lookup(key)
	if !ok {
		if hasDefault {
			return e.expand(def)
		}
		if path := e.path(); len(e.chain) > 1 && path != "" {
			return "", fmt.Errorf("unresolved reference %q (%s)", key, path)
		}
		return "", fmt.Errorf("unresolved reference %q", key)
	}
	if e.hooks.reference != nil {
		if replacement, ok := e.hooks.reference(key); ok {
			e.isSecret = true
			return replacement, nil
		}
	}
	e.chain = append(e.chain, key)
	defer func() { e.chain = e.chain[:len(e.chain)-1] }()
	return e.expand(v.Raw)
}

// cutFunction splits the body of a ${name::arg} expression: one whose
// first ':' starts "::". In ${key:${env::HOME}} the first ':' starts a
// default.
func cutFunction(body string) (name, arg string, ok bool) {
	i := strings.IndexByte(body, ':')
	if i < 0 || !strings.HasPrefix(body[i:], "::") {
		return "", "", false
	}
	return body[:i], body[i+2:], true
}

// call expands arg and resolves the ${name::arg} expression with
// hooks.call. The result is a secret if the Function or arg makes it one.
func (e *expander) call(name, arg string) (string, error) {
	outer := e.isSecret
	e.isSecret = false
	arg, err := e.expand(arg)
	isSecretArg := e.isSecret
	e.isSecret = outer
	if err != nil {
		return "", err
	}
	current := e.chain[len(e.chain)-1]
	origin := e.origin
	if len(e.chain) > 1 {
		v, _ := e.c.Lookup(current)
		origin = v.Origin
	}
	s, isSecret, err := e.hooks.call(e.c, callSite{key: current, name: name, arg: arg, origin: origin, isSecretArg: isSecretArg})
	if err != nil {
		return "", err
	}
	e.isSecret = e.isSecret || isSecret
	return s, nil
}

// value names the text being expanded in errors.
func (e *expander) value() string {
	if key := e.chain[len(e.chain)-1]; key != "" {
		return fmt.Sprintf("value of %q", key)
	}
	return "expression"
}

// path returns the chain of keys for errors, without the "" of Eval.
func (e *expander) path() string {
	chain := e.chain
	if chain[0] == "" {
		chain = chain[1:]
	}
	return strings.Join(chain, " -> ")
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
