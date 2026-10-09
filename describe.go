package gonf

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Property describes one key that a Mapping reads: its definition, from the
// struct alone, for documentation such as the configuration reference of
// an application and next to the value in an Explanation.
type Property struct {
	// Key is the key, with <name> for the key of a map entry and <n> for
	// the index of a list element: db.pools.<name>.url, db.hosts[<n>].port.
	Key string `json:"key"`
	// Env is the key as the first Source that is a KeyMapper looks it up,
	// such as DB_POOL_MAX_SIZE for env.New; empty if there is no such
	// Source or the key contains <name> or <n>.
	Env string `json:"env,omitempty"`
	// Type is the Go type of the value; a Secret[T] is described as T with
	// Secret set.
	Type string `json:"type"`
	// Default is the default tag, not expanded, nil without one; masked if
	// Secret.
	Default *string `json:"default,omitempty"`
	// Required reports whether Load fails without a value for the key. Inside
	// an optional struct (a pointer), a map entry or a list element, it
	// applies only if that exists.
	Required bool `json:"required"`
	// Secret reports whether the value is a Secret or the key is named like
	// one, such as db.password; Explain masks it.
	Secret bool `json:"secret,omitempty"`
	// Doc is the desc tag of the field.
	Doc string `json:"doc,omitempty"`
	// Mapping is the prefix of the Mapping that reads the key, Location the
	// file and line of its NewMapping.
	Mapping  string `json:"mapping"`
	Location string `json:"location,omitempty"`
}

// Describe returns the Properties of the Mappings given with WithMappings,
// in the order of the Mappings and their fields. It takes the same options
// as Load, so an application can document its configuration with the
// options it loads it with:
//
//	opts := []gonf.Option{gonf.WithSources(env.New()), gonf.WithMappings(db.Mapping)}
//	if *configDoc {
//		props, err := gonf.Describe(opts...)
//		...
//	}
//	cfg, err := gonf.Load(ctx, opts...)
//
// Describe reads no Source and runs no SourceFactory, so it works without
// the values, e.g. in a build. It uses WithConverter to tell single values
// from structs and the Sources only for Property.Env. Describe and Load read
// a struct the same way, so a field type Load cannot read is the same
// *BindError here as in Load. Of two Mappings with the same key, the first
// describes it.
//
// A field documents itself with a desc tag:
//
//	MaxSize int `default:"10" desc:"Maximum number of open connections."`
func Describe(opts ...Option) ([]Property, error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}
	cs, err := newConverters(o.converters)
	if err != nil {
		return nil, err
	}
	var mapper KeyMapper
	for _, src := range o.sources {
		if m, ok := src.(KeyMapper); ok {
			mapper = m
			break
		}
	}
	var mappings []AnyMapping
	var roots []*model
	var errs []error
	for _, m := range o.mappings {
		if m == nil {
			errs = append(errs, errNilMapping)
			continue
		}
		root, err := cs.rootModel(m.Prefix(), m.target())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if problems := root.problems(m.Prefix()); len(problems) > 0 {
			errs = append(errs, &BindError{Prefix: m.Prefix(), Problems: problems})
			continue
		}
		mappings = append(mappings, m)
		roots = append(roots, root)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return describe(mappings, roots, mapper), nil
}

// describe returns the Properties of mappings, read as the models roots, in
// their order; of two Mappings with the same key, the first describes it.
// Describe and Config.Properties both use it, so that the documentation and
// the explanation of a key cannot drift.
func describe(mappings []AnyMapping, roots []*model, mapper KeyMapper) []Property {
	out := []Property{}
	seen := map[string]struct{}{}
	for i, m := range mappings {
		walk(m.Prefix(), field{m: roots[i]}, nil, func(key string, f field) {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			p := propertyOf(key, f, m)
			if mapper != nil && !strings.Contains(key, "<") {
				p.Env = mapper.MapKey(key)
			}
			out = append(out, p)
		})
	}
	return out
}

// propertyOf returns the Property of key, which the Mapping m reads as
// field f; key has <name> and <n> for the entries of maps and lists.
func propertyOf(key string, f field, m AnyMapping) Property {
	t := f.m.typ
	p := Property{Key: key, Type: t.String(), Required: f.isRequired, Doc: f.desc, Mapping: m.Prefix(), Location: m.origin().Location}
	if isSecret(t) {
		p.Type = secretElem(t).String()
	}
	p.Secret = f.m.isSecret || isSecretName(key)
	if f.def != nil {
		def := *f.def
		if p.Secret && def != "" {
			def = masked
		}
		p.Default = &def
	}
	return p
}

// WriteMarkdown writes props as a Markdown table, such as for a README.
// A required key shows "required" as its default.
func WriteMarkdown(w io.Writer, props []Property) error {
	var b strings.Builder
	b.WriteString("| Key | Environment variable | Type | Default | Description |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, p := range props {
		typ := codeSpan(p.Type)
		if p.Secret {
			typ += " (secret)"
		}
		def := ""
		if p.Default != nil {
			def = codeSpan(*p.Default)
		}
		if p.Required {
			def = "required"
		}
		fmt.Fprintf(
			&b,
			"| %s | %s | %s | %s | %s |\n",
			codeSpan(p.Key),
			codeSpan(p.Env),
			typ,
			def,
			tableCell(p.Doc),
		)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// codeSpan formats s as a code span in a table cell; "" stays empty.
func codeSpan(s string) string {
	if s == "" {
		return ""
	}
	s = tableCell(s)
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// tableCell escapes s for a table cell, which must stay on one line and treats
// | as a column break, even in a code span.
func tableCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}
