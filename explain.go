package gonf

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Explanation tells where the value of a key comes from and, if a Mapping
// reads the key, what the key is: its Property. Secret values are already
// replaced by "****" in all of its fields, so it can be printed, logged or
// served as is; Get returns the real value.
type Explanation struct {
	Key string
	// Value is what a Mapping binds: the winning value, expanded, or the
	// default of a Mapping if the key has no value or an empty one. A
	// reference to a secret key, as in postgres://u:${db.password}@host,
	// expands to "****".
	Value string
	// Origin is where Value comes from: the winning value's Origin or,
	// if IsDefault, the Mapping's, whose Source is "mapping:" followed by
	// its prefix and whose Location is the file and line of NewMapping.
	Origin Origin
	// IsDefault reports whether Value is the default of the Property.
	IsDefault bool
	// IsSecret reports whether the values are masked: the Property is
	// Secret, the name of the key contains "password", "secret", "token"
	// or "credential" in any case, or a value is encrypted.
	IsSecret bool
	// Candidates holds every value for the key, as Candidates does, with
	// Raw replaced by "****" if IsSecret.
	Candidates []Value
	// Property is the definition of the key, as Describe and Properties
	// list it, nil if no Mapping reads the key. Its Key has <name> and <n>
	// where the key has the entries of maps and lists; its Env is that of
	// the key itself, such as DB_POOLS_MAIN_URL for db.pools.main.url.
	Property *Property
	// Err is set if the key has neither a value nor a default, or its value
	// cannot be expanded.
	Err error
}

// Explain tells where the value of key comes from, including the values it
// overrides and the default of a Mapping, and what the key is.
func (c *Config) Explain(key string) Explanation {
	e := Explanation{Key: key, IsSecret: c.isSecret(key), Candidates: c.Candidates(key), Property: c.propertyOf(key)}
	var def *Value
	if v, ok := c.values[key]; ok && v.def != nil {
		def = &Value{Raw: *v.def, Origin: v.origin}
	}
	e.Value, e.Origin, e.Err = c.effective(key, e.Candidates, def)
	e.IsDefault = e.Err == nil && def != nil && e.Origin == def.Origin
	if e.IsSecret {
		e.Value = masked
		for i := range e.Candidates {
			e.Candidates[i].Raw = masked
		}
	}
	return e
}

// propertyOf returns the Property of key, with the environment name of key
// itself; nil if no Mapping binds key to a value.
func (c *Config) propertyOf(key string) *Property {
	v, ok := c.values[key]
	if !ok {
		return nil
	}
	i, ok := c.property[v.property]
	if !ok {
		return nil
	}
	p := c.properties[i]
	if m := c.keyMapper(); m != nil {
		p.Env = m.MapKey(key)
	}
	return &p
}

// Properties returns the Properties of the Mappings, in the order of the
// Mappings and their fields, as Describe returns them for the options of
// Load: every key a Mapping reads, also those without a value.
func (c *Config) Properties() []Property {
	return slices.Clone(c.properties)
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
		hasDefault := c.values[key].def != nil
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
	switch {
	case e.Err != nil:
	case e.IsDefault:
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
		case e.IsDefault:
			label = "empty"
		default:
			continue
		}
		fmt.Fprintf(&b, "\n  %s %s (%s)", label, show(v.Raw), where(v.Origin))
	}
	if p := e.Property; p != nil && p.Default != nil && !e.IsDefault {
		fmt.Fprintf(&b, "\n  default %s (%s)", show(*p.Default), cmp.Or(p.Location, "mapping:"+p.Mapping))
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

// isSecret reports whether the value of key must be masked: by its
// Property, its name, or an encrypted value.
func (c *Config) isSecret(key string) bool {
	if p := c.propertyOf(key); p != nil && p.Secret {
		return true
	}
	return isSecretName(key) || c.isEncrypted(key)
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
