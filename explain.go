package gonf

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Explanation tells where the value of a key comes from. Secret values are
// already replaced by "****" in all of its fields, so it can be printed,
// logged or served as is; Get returns the real value.
type Explanation struct {
	Key string
	// Value is what a Mapping binds: the winning value, expanded, or the
	// default of a Mapping if the key has no value or an empty one. A
	// reference to a secret key, as in postgres://u:${db.password}@host,
	// expands to "****".
	Value string
	// Origin is where Value comes from: the winning value's Origin or
	// Default's.
	Origin Origin
	// IsSecret reports whether the key is secret: a Mapping binds it to a
	// Secret, or its name contains "password", "secret", "token" or
	// "credential" in any case.
	IsSecret bool
	// Candidates holds every value for the key, as Candidates does, with
	// Raw replaced by "****" if IsSecret.
	Candidates []Value
	// Default is the default tag of a Mapping for the key, nil if there is
	// none. Its Origin.Source is "mapping:" followed by the Mapping's
	// prefix, its Origin.Location the file and line of NewMapping.
	Default *Value
	// Err is set if the key has neither a value nor a default, or its value
	// cannot be expanded.
	Err error
}

// Explain tells where the value of key comes from, including the values it
// overrides and the default of a Mapping.
func (c *Config) Explain(key string) Explanation {
	e := Explanation{Key: key, IsSecret: c.isSecret(key), Candidates: c.Candidates(key)}
	if def, ok := c.defaults[key]; ok {
		e.Default = &def
	}
	e.Value, e.Origin, e.Err = c.effective(key, e.Candidates, e.Default)
	if e.IsSecret {
		e.Value = masked
		for i := range e.Candidates {
			e.Candidates[i].Raw = masked
		}
		if e.Default != nil {
			e.Default.Raw = masked
		}
	}
	return e
}

// effective returns the value a Mapping binds for key and where it comes
// from, expanding references to secret keys to "****".
func (c *Config) effective(key string, candidates []Value, def *Value) (string, Origin, error) {
	if len(candidates) > 0 {
		s, err := c.expandMasked(key, candidates[0].Raw)
		if err != nil {
			return "", candidates[0].Origin, fmt.Errorf("gonf: key %q: %w", key, err)
		}
		if s != "" || def == nil {
			return s, candidates[0].Origin, nil
		}
	}
	if def == nil {
		return "", Origin{}, fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	s, err := c.expandMasked(key, def.Raw)
	if err != nil {
		return "", def.Origin, fmt.Errorf("gonf: key %q: default: %w", key, err)
	}
	return s, def.Origin, nil
}

// Explanations explains every key of the Sources, except those of a
// KeyMapper Source such as the environment, and every key a Mapping bound
// or referenced that has a value or a default, sorted by key. Environment
// variables that no Mapping reads, such as PATH, are left out.
func (c *Config) Explanations() []Explanation {
	keys := map[string]struct{}{}
	for _, s := range c.sources {
		if s.mapper != nil {
			continue
		}
		for key := range s.values {
			if base, ok := activeKey(key, c.profiles); ok {
				keys[base] = struct{}{}
			}
		}
	}
	for key := range c.read {
		_, hasDefault := c.defaults[key]
		if _, ok := c.Lookup(key); ok || hasDefault {
			keys[key] = struct{}{}
		}
	}
	out := make([]Explanation, 0, len(keys))
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		out = append(out, c.Explain(key))
	}
	return out
}

// String shows the value, where it comes from and what it overrides: the
// winning value of a key without errors, else every candidate.
//
//	db.url = postgres://prod/app (env:DB_URL)
//	  overrides postgres://localhost/app (config/app.properties:3, profile dev)
//	  default postgres://localhost/test (db/db.go:12)
func (e Explanation) String() string {
	var b strings.Builder
	if e.Err != nil {
		fmt.Fprintf(&b, "%s: %v", e.Key, e.Err)
	}
	isDefaultUsed := e.Err == nil && e.Default != nil && e.Origin == e.Default.Origin
	switch {
	case e.Err != nil:
	case isDefaultUsed:
		fmt.Fprintf(&b, "%s = %s (default, %s)", e.Key, show(e.Value), where(e.Origin))
	default:
		fmt.Fprintf(&b, "%s = %s (%s)", e.Key, show(e.Value), where(e.Origin))
	}
	for i, v := range e.Candidates {
		label := "overrides"
		switch {
		case i > 0:
		case e.Err != nil:
			label = "value"
		case isDefaultUsed:
			label = "empty"
		default:
			continue
		}
		fmt.Fprintf(&b, "\n  %s %s (%s)", label, show(v.Raw), where(v.Origin))
	}
	if e.Default != nil && !isDefaultUsed {
		fmt.Fprintf(&b, "\n  default %s (%s)", show(e.Default.Raw), where(e.Default.Origin))
	}
	return b.String()
}

// show prints an empty value as "".
func show(s string) string { return cmp.Or(s, `""`) }

// where names the place of o, with its profile if any.
func where(o Origin) string {
	s := cmp.Or(o.Location, o.Source)
	if o.Profile != "" {
		s += ", profile " + o.Profile
	}
	return s
}

// isSecret reports whether the value of key must be masked.
func (c *Config) isSecret(key string) bool {
	_, ok := c.secrets[key]
	return ok || isSecretName(key) || c.isEncrypted(key)
}

// expandMasked expands s like Get, but references to secret keys expand to
// "****", and nothing is decrypted.
func (c *Config) expandMasked(key, s string) (string, error) {
	return c.expand(key, s, Origin{}, hooks{
		// A key with an encrypted value is secret and masked before its
		// value is expanded; this keeps any other expression from
		// decrypting.
		call: func(*Config, string, string, string, Origin) (string, error) { return masked, nil },
		reference: func(key string) (string, bool) {
			return masked, c.isSecret(key)
		},
	})
}
