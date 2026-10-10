// Package schema turns the Properties of gonf.Describe into a JSON Schema
// (draft 2020-12) of the configuration, so that editors offer completion,
// type checks and descriptions for YAML and JSON configuration files.
//
//	props, err := gonf.Describe(opts...)
//	doc, err := schema.New(props)
//
// The schema describes what a file may hold, not what an application needs
// from all its Sources: a required key is required inside an object that
// exists, but a missing object is not reported, since the value may come from
// the environment.
//
// Objects below the prefix of a Mapping reject unknown keys, like Load does,
// unless the Mapping was made with gonf.AllowUnknown; the root stays open
// for other prefixes.
// Profile sections ("%dev": {...}) are allowed at the root with the same
// properties and without required keys; the flat form "%dev.db.url" is
// accepted without checking its value.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/efuturetoday/gonf"
)

// Dialect is the value of "$schema" in the generated document.
const Dialect = "https://json-schema.org/draft/2020-12/schema"

// object is a JSON object that encoding/json writes with sorted keys.
type object = map[string]any

// New returns the JSON Schema of props, indented and with sorted keys, so
// that the same Properties give the same document.
func New(props []gonf.Property) ([]byte, error) {
	root := &node{}
	for _, p := range props {
		if err := root.insert(p); err != nil {
			return nil, err
		}
	}
	doc, err := root.render(false)
	if err != nil {
		return nil, err
	}
	doc["$schema"] = Dialect
	// The root is open: it also holds other prefixes and profiles.
	delete(doc, "additionalProperties")
	if len(root.props) > 0 {
		profile, err := root.render(true)
		if err != nil {
			return nil, err
		}
		doc["patternProperties"] = object{
			`^%[^.]+$`:  object{"type": "object", "properties": profile["properties"]},
			`^%[^.]+\.`: true,
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// step is one part of a key: a name, <name> for any map key or [<n>] for
// any list index.
type step struct {
	kind stepKind
	name string
}

type stepKind int

const (
	stepName stepKind = iota
	stepAny
	stepIndex
)

// steps splits key, such as db.hosts[<n>].port, into its steps.
func steps(key string) []step {
	if key == "" {
		return nil
	}
	var out []step
	for seg := range strings.SplitSeq(key, ".") {
		indexes := 0
		for strings.HasSuffix(seg, "[<n>]") {
			seg = strings.TrimSuffix(seg, "[<n>]")
			indexes++
		}
		switch seg {
		case "":
		case "<name>":
			out = append(out, step{kind: stepAny})
		default:
			out = append(out, step{kind: stepName, name: seg})
		}
		for range indexes {
			out = append(out, step{kind: stepIndex})
		}
	}
	return out
}

// node is a position in the document: a single value, a list or an object.
type node struct {
	leaf     *gonf.Property
	props    map[string]*node
	any      *node
	items    *node
	required map[string]bool
	// closed reports whether the object rejects unknown keys.
	closed bool
}

func (n *node) hasChildren() bool {
	return len(n.props) > 0 || n.any != nil || n.items != nil
}

// insert adds p at the position of its key.
func (n *node) insert(p gonf.Property) error {
	path := steps(p.Key)
	if len(path) == 0 {
		return fmt.Errorf("schema: empty key in property of mapping %q", p.Mapping)
	}
	known := max(len(steps(p.Mapping)), 1)
	cur := n
	for i, s := range path {
		if cur.leaf != nil {
			return fmt.Errorf("schema: %s: a shorter key is a single value and holds keys", p.Key)
		}
		if i >= known && !p.IsUnknownAllowed {
			cur.closed = true
		}
		var next **node
		switch s.kind {
		case stepName:
			if cur.props == nil {
				cur.props = map[string]*node{}
			}
			if cur.props[s.name] == nil {
				cur.props[s.name] = &node{}
			}
			if i == len(path)-1 && p.Required {
				if cur.required == nil {
					cur.required = map[string]bool{}
				}
				cur.required[s.name] = true
			}
			cur = cur.props[s.name]
			continue
		case stepAny:
			next = &cur.any
		case stepIndex:
			next = &cur.items
		}
		if *next == nil {
			*next = &node{}
		}
		cur = *next
	}
	if cur.hasChildren() {
		return fmt.Errorf("schema: %s: key is a single value here and holds keys elsewhere", p.Key)
	}
	cur.leaf = &p
	return nil
}

// render returns the schema of n. A profile section has no required keys.
func (n *node) render(profile bool) (object, error) {
	switch {
	case n.leaf != nil:
		return leaf(*n.leaf), nil
	case n.items != nil:
		if len(n.props) > 0 || n.any != nil {
			return nil, errors.New("schema: a key is a list and a map or struct at once")
		}
		items, err := n.items.render(profile)
		if err != nil {
			return nil, err
		}
		return object{"type": "array", "items": items}, nil
	}
	out := object{"type": "object"}
	if len(n.props) > 0 {
		props := object{}
		for name, child := range n.props {
			s, err := child.render(profile)
			if err != nil {
				return nil, err
			}
			props[name] = s
		}
		out["properties"] = props
	}
	switch {
	case n.any != nil:
		s, err := n.any.render(profile)
		if err != nil {
			return nil, err
		}
		out["additionalProperties"] = s
	case n.closed:
		out["additionalProperties"] = false
	}
	if !profile && len(n.required) > 0 {
		req := make([]string, 0, len(n.required))
		for name := range n.required {
			req = append(req, name)
		}
		slices.Sort(req)
		out["required"] = req
	}
	return out, nil
}

// leaf returns the schema of the single value or list of scalars p.
func leaf(p gonf.Property) object {
	s := typeSchema(p.Type)
	if p.Doc != "" {
		s["description"] = p.Doc
		s["markdownDescription"] = p.Doc
	}
	switch {
	case p.Secret:
		s["writeOnly"] = true
	case p.Default != nil:
		s["default"] = defaultValue(p.Type, *p.Default)
	}
	return s
}

// expression is what a ${...} expression in a value of any type looks like
// before Load expands it.
var expression = object{"type": "string", "pattern": `\$\{`}

// typeSchema returns the schema of the Go type t as Property.Type names it.
// A value of a type other than string may also be written as an expression.
func typeSchema(t string) object {
	t = strings.TrimPrefix(t, "*")
	if elem, ok := strings.CutPrefix(t, "[]"); ok {
		// A list is also written as a comma-separated string.
		return object{"anyOf": []any{
			object{"type": "array", "items": typeSchema(elem)},
			object{"type": "string"},
		}}
	}
	switch t {
	case "string", "time.Duration", "time.Time":
		return object{"type": "string"}
	case "bool":
		return orExpression(object{"type": "boolean"})
	case "float32", "float64":
		return orExpression(object{"type": "number"})
	case "int", "int64":
		return orExpression(object{"type": "integer"})
	case "uint", "uint64":
		return orExpression(object{"type": "integer", "minimum": 0})
	case "int8":
		return orExpression(bounds(math.MinInt8, math.MaxInt8))
	case "int16":
		return orExpression(bounds(math.MinInt16, math.MaxInt16))
	case "int32":
		return orExpression(bounds(math.MinInt32, math.MaxInt32))
	case "uint8":
		return orExpression(bounds(0, math.MaxUint8))
	case "uint16":
		return orExpression(bounds(0, math.MaxUint16))
	case "uint32":
		return orExpression(bounds(0, math.MaxUint32))
	}
	// A type with its own text format, or a named type of a basic type: the
	// schema cannot tell, so it checks no more than the kind of the value.
	return object{"type": []any{"string", "number", "boolean"}}
}

func bounds(lo, hi int64) object {
	return object{"type": "integer", "minimum": lo, "maximum": hi}
}

func orExpression(s object) object {
	return object{"anyOf": []any{s, expression}}
}

// defaultValue returns the default tag s as a JSON value of type t; it stays
// a string if it does not read as t, such as an expression.
func defaultValue(t, s string) any {
	switch strings.TrimPrefix(t, "*") {
	case "bool":
		if s == "true" || s == "false" {
			return s == "true"
		}
	case "int", "int8", "int16", "int32", "int64":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			return n
		}
	case "float32", "float64":
		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return f
		}
	}
	return s
}
