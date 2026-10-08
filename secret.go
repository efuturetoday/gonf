package gonf

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
)

// masked replaces a secret value in every output.
const masked = "****"

// Secret holds a configuration value that must not show up in output:
//
//	type DB struct {
//		User     string
//		Password gonf.Secret[string]
//	}
//
// Bind converts the value as for a field of type T, which must be a type
// Bind converts from a single value, such as string, int or a type with
// UnmarshalText. An invalid value is reported without the value itself.
// Use *Secret[T] for an optional secret.
//
// fmt, encoding/json and other encodings using MarshalText, and log/slog
// print "****"; Value returns the value. Keys bound to a Secret are masked
// in Explain. A field that refers to a secret key with ${...} must be a
// Secret too; Bind reports it otherwise.
type Secret[T any] struct {
	value T
}

// NewSecret returns a Secret holding v, e.g. for tests.
func NewSecret[T any](v T) Secret[T] { return Secret[T]{value: v} }

// Value returns the secret value.
func (s Secret[T]) Value() T { return s.value }

// String returns "****".
func (s Secret[T]) String() string { return masked }

// Format prints "****" for every verb, also %#v.
func (s Secret[T]) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(masked)) }

// MarshalText returns "****", so encodings such as JSON do not reveal the
// value.
func (s Secret[T]) MarshalText() ([]byte, error) { return []byte(masked), nil }

// LogValue returns "****" for log/slog.
func (s Secret[T]) LogValue() slog.Value { return slog.StringValue(masked) }

func (s Secret[T]) secretType() reflect.Type { return reflect.TypeFor[T]() }

func (s *Secret[T]) setSecret(v reflect.Value) {
	if x, ok := v.Interface().(T); ok {
		s.value = x
	}
}

// secret is implemented by *Secret[T]; Bind fills it through these methods.
type secret interface {
	secretType() reflect.Type
	setSecret(v reflect.Value)
}

var secretType = reflect.TypeFor[secret]()

// isSecret reports whether t is a Secret[T].
func isSecret(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && reflect.PointerTo(t).Implements(secretType)
}

// secretElem returns T of the Secret[T] type t.
func secretElem(t reflect.Type) reflect.Type {
	s, _ := reflect.New(t).Interface().(secret)
	return s.secretType()
}

// secretWords mark a key as secret by its name, also without a Secret
// field, as in "db.password", "api-token" or "AWS_SECRET_ACCESS_KEY".
var secretWords = []string{"password", "secret", "token", "credential"}

// isSecretName reports whether key contains one of secretWords, in any case.
func isSecretName(key string) bool {
	key = strings.ToLower(key)
	for _, w := range secretWords {
		if strings.Contains(key, w) {
			return true
		}
	}
	return false
}
