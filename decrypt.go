package gonf

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// A SecretHandler decrypts the values of ${name::value} expressions, such as
// db.password=${aes-gcm::AhB3...}, for the name it is registered under with
// WithSecretHandler. value is the text after "::"; origin tells where the
// expression is written. cfg is the Config being read, e.g. to look up a
// key; a SecretHandler must not expand values of cfg that may contain
// expressions of its own name.
//
// The error must not contain the decrypted value.
type SecretHandler func(cfg *Config, value string, origin Origin) (string, error)

// WithSecretHandler makes ${name::value} expressions decrypt value with
// handler. A value decrypted this way is a secret: Explain masks it, and a
// field that is no Secret but holds it, also through ${...} references, is
// a Load error. Two handlers for the same name are a Load error.
//
// A name consists of letters, digits and '-', such as "aes-gcm".
func WithSecretHandler(name string, handler SecretHandler) Option {
	return func(o *loadOptions) {
		o.handlers = append(o.handlers, namedHandler{name: name, handler: handler})
	}
}

type namedHandler struct {
	name    string
	handler SecretHandler
}

// newHandlers checks the handlers given with WithSecretHandler and returns
// them by name.
func newHandlers(list []namedHandler) (map[string]SecretHandler, error) {
	handlers := make(map[string]SecretHandler, len(list))
	var errs []error
	for _, h := range list {
		_, isTwice := handlers[h.name]
		switch {
		case !isHandlerName(h.name):
			errs = append(errs, fmt.Errorf("gonf: invalid secret handler name %q", h.name))
		case h.handler == nil:
			errs = append(errs, fmt.Errorf("gonf: nil secret handler %q", h.name))
		case isTwice:
			errs = append(errs, fmt.Errorf("gonf: two secret handlers named %q", h.name))
		default:
			handlers[h.name] = h.handler
		}
	}
	return handlers, errors.Join(errs...)
}

// isHandlerName reports whether name is a valid secret handler name.
func isHandlerName(name string) bool {
	return name != "" && strings.IndexFunc(name, func(r rune) bool {
		return r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) < 0
}

// decrypt resolves the ${name::value} expression in the value of key with
// the SecretHandler name. It is the hooks.call of expand.
func decrypt(c *Config, key, name, value string, origin Origin) (string, error) {
	h, ok := c.handlers[name]
	if !ok {
		return "", fmt.Errorf("value of %q: unknown secret handler %q", key, name)
	}
	s, err := h(c, value, origin)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return s, nil
}

// isEncrypted reports whether a value of key, the winning one or another,
// or the default tag of a Mapping, contains a ${name::value} expression of
// a registered SecretHandler.
func (c *Config) isEncrypted(key string) bool {
	if def := c.values[key].def; def != nil && c.hasHandlerExpression(*def) {
		return true
	}
	for v := range c.all(key) {
		if c.hasHandlerExpression(v.Raw) {
			return true
		}
	}
	return false
}

// hasHandlerExpression reports whether s contains a ${name::value}
// expression of a registered SecretHandler.
func (c *Config) hasHandlerExpression(s string) bool {
	for name := range c.handlers {
		if strings.Contains(s, "${"+name+"::") {
			return true
		}
	}
	return false
}
