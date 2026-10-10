package gonf

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

// A Function gives ${name::arg} expressions their meaning, such as
// ${env::HOME} or ${aes-gcm::AhB3...}. Register Functions with
// WithFunctions; gonf itself registers none.
type Function struct {
	// Name is the name in ${name::arg}: letters, digits and '-', such as
	// "aes-gcm".
	Name string
	// IsSecret marks the results as secrets, as for a decrypted value or
	// the content of a secret file: Explain masks every key that calls the
	// Function, and a field that is no Secret but holds a result, also
	// through ${...} references, is a Load error. A result computed from a
	// secret argument, as in ${base64::${file::/run/secrets/key}}, is a
	// secret too.
	IsSecret bool
	// Call returns the result for arg, the text after "::" with its
	// expressions already expanded. origin tells where the expression is
	// written; it is the zero Origin for Eval. cfg is the Config being
	// read, e.g. to look up a key; Call must not expand values of cfg that
	// may contain expressions of its own name.
	//
	// The error must not contain a secret result.
	Call func(cfg *Config, arg string, origin Origin) (string, error)
	// Mapping declares the keys Call reads, nil if none. WithFunctions
	// registers it like a Mapping of WithMappings, so Describe, the
	// schema and the environment know the keys. Call still reads them
	// from cfg, as it may run before Mappings are bound.
	Mapping AnyMapping
}

// WithFunctions registers Functions for ${name::arg} expressions and their
// Mappings. Two Functions with the same name are a Load error.
//
// The results are cached per Load, keyed by name and expanded argument:
// within one Config, ${uuid::} gives the same value everywhere, also in
// Explain and Eval, and every Watch reload calls the Function anew. Only
// results are cached, not errors. A Function that must return the same
// result for the whole process caches itself.
func WithFunctions(fns ...Function) Option {
	return func(o *loadOptions) {
		o.functions = append(o.functions, fns...)
		for _, fn := range fns {
			if fn.Mapping != nil {
				o.mappings = append(o.mappings, fn.Mapping)
			}
		}
	}
}

// functions holds the registered Functions of one Load and the results of
// their calls. It is shared by the Configs of that Load and safe for
// concurrent use.
type functions struct {
	byName map[string]Function

	mu      sync.Mutex
	results map[callKey]string
}

type callKey struct{ name, arg string }

// newFunctions checks the Functions given with WithFunctions.
func newFunctions(list []Function) (*functions, error) {
	fns := &functions{byName: make(map[string]Function, len(list)), results: map[callKey]string{}}
	var errs []error
	for _, fn := range list {
		_, isTwice := fns.byName[fn.Name]
		switch {
		case !isFunctionName(fn.Name):
			errs = append(errs, fmt.Errorf("gonf: invalid function name %q", fn.Name))
		case fn.Call == nil:
			errs = append(errs, fmt.Errorf("gonf: function %q has no Call", fn.Name))
		case isTwice:
			errs = append(errs, fmt.Errorf("gonf: two functions named %q", fn.Name))
		default:
			fns.byName[fn.Name] = fn
		}
	}
	return fns, errors.Join(errs...)
}

// isFunctionName reports whether name is a valid Function name.
func isFunctionName(name string) bool {
	return name != "" && strings.IndexFunc(name, func(r rune) bool {
		return r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) < 0
}

// lookup returns the Function name; f may be nil.
func (f *functions) lookup(name string) (Function, bool) {
	if f == nil {
		return Function{}, false
	}
	fn, ok := f.byName[name]
	return fn, ok
}

// call returns the cached result of fn for arg, or calls fn. The lock is
// not held during the call, since fn may read cfg; of two concurrent first
// calls, the result stored first wins.
func (f *functions) call(cfg *Config, fn Function, arg string, origin Origin) (string, error) {
	k := callKey{name: fn.Name, arg: arg}
	f.mu.Lock()
	s, ok := f.results[k]
	f.mu.Unlock()
	if ok {
		return s, nil
	}
	s, err := fn.Call(cfg, arg, origin)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if first, ok := f.results[k]; ok {
		return first, nil
	}
	f.results[k] = s
	return s, nil
}

// callSite is one ${name::arg} expression, with arg expanded.
type callSite struct {
	// key is the key whose value holds the expression, "" for Eval;
	// origin is where that value comes from.
	key, name, arg string
	origin         Origin
	// isSecretArg reports whether arg holds a secret.
	isSecretArg bool
}

// invoke resolves a ${name::arg} expression with the Function name and
// reports whether the result is a secret. It is the hooks.call of Get.
func invoke(c *Config, s callSite) (string, bool, error) {
	fn, ok := c.functions.lookup(s.name)
	if !ok {
		return "", false, s.fail(errors.New("unknown function"))
	}
	v, err := c.functions.call(c, fn, s.arg, s.origin)
	if err != nil {
		return "", false, s.fail(err)
	}
	return v, fn.IsSecret || s.isSecretArg, nil
}

// fail names the Function, the key with the expression and its place in
// err.
func (s callSite) fail(err error) error {
	switch place := where(s.origin); {
	case s.key == "":
		return fmt.Errorf("%s: %w", s.name, err)
	case place == "":
		return fmt.Errorf("value of %q: %s: %w", s.key, s.name, err)
	default:
		return fmt.Errorf("value of %q (%s): %s: %w", s.key, place, s.name, err)
	}
}

// callsSecret reports whether a value of key, the winning one or another,
// or the default tag of a Mapping, calls a secret Function.
func (c *Config) callsSecret(key string) bool {
	if def := c.values[key].def; def != nil && c.hasSecretCall(*def) {
		return true
	}
	for v := range c.all(key) {
		if c.hasSecretCall(v.Raw) {
			return true
		}
	}
	return false
}

// hasSecretCall reports whether s contains a ${name:: expression of a
// secret Function.
func (c *Config) hasSecretCall(s string) bool {
	if c.functions == nil || !strings.Contains(s, "::") {
		return false
	}
	for name, fn := range c.functions.byName {
		if fn.IsSecret && strings.Contains(s, "${"+name+"::") {
			return true
		}
	}
	return false
}
