// Package yaml provides the YAML format for gonf. It is a module of its
// own, so that gonf itself depends on the standard library only.
//
// With boot, it reads app.yaml and app.yml next to app.properties:
//
//	boot.Load(ctx,
//		boot.WithFormat("yaml", yaml.Format),
//		boot.WithFormat("yml", yaml.Format),
//	)
package yaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	yaml "go.yaml.in/yaml/v3"
)

// Format flattens a YAML mapping into dotted keys:
//
//	db:
//	  url: postgres://localhost/app   # db.url
//	  hosts: [a, b]                   # db.hosts[0], db.hosts[1]
//	  pool.max-size: 10               # db.pool.max-size
//	servers:
//	  '"eu.west"': {port: 8080}       # servers."eu.west".port
//	"%dev":
//	  db: {url: postgres://dev/app}   # %dev.db.url
//
// Every name is a part of a key as in a .properties file and is joined
// with a dot as it is, so db.url: x is the same as db: {url: x}. A map
// entry whose name contains a dot is quoted, as in .properties.
//
// Values keep the text they have in the file: yes stays yes and 0644 stays
// 0644, so Bind reads them by its own rules. null produces no key.
// Anchors, aliases and merge keys (<<) work.
//
// Every value has its file and line as Location. A file may hold several
// documents separated by ---; a key defined twice, in one document or in
// two, is an error.
var Format source.Format = source.FormatFunc(parse)

// maxNodes limits the nodes a file expands to, so that aliases cannot
// blow a small file up to a huge configuration.
const maxNodes = 100_000

// parse flattens the documents in data, read from the file name.
func parse(name string, data []byte) (map[string]gonf.Value, error) {
	p := parser{name: name, out: map[string]gonf.Value{}}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return p.out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := p.document(&doc); err != nil {
			return nil, err
		}
	}
}

// parser flattens the documents of one file into out.
type parser struct {
	name  string
	out   map[string]gonf.Value
	nodes int
}

func (p *parser) document(doc *yaml.Node) error {
	if len(doc.Content) == 0 {
		return nil
	}
	root := resolve(doc.Content[0])
	switch {
	case isNull(root):
		return nil
	case root.Kind != yaml.MappingNode:
		return p.errorf(root, "top level must be a mapping")
	}
	return p.flatten("", root)
}

// count counts the node n against maxNodes.
func (p *parser) count(n *yaml.Node) error {
	p.nodes++
	if p.nodes > maxNodes {
		return p.errorf(n, "more than %d values, are aliases nested too deep?", maxNodes)
	}
	return nil
}

// flatten adds the values of n under key.
func (p *parser) flatten(key string, n *yaml.Node) error {
	if err := p.count(n); err != nil {
		return err
	}
	n = resolve(n)
	switch n.Kind {
	case yaml.MappingNode:
		pairs, err := p.pairs(n)
		if err != nil {
			return err
		}
		for _, pair := range pairs {
			if err := p.flatten(join(key, pair.name), pair.value); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			if err := p.flatten(key+"["+strconv.Itoa(i)+"]", item); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if isNull(n) {
			return nil
		}
		if prev, ok := p.out[key]; ok {
			return p.errorf(n, "key %q defined twice, first at %s", key, prev.Origin.Location)
		}
		location := p.name + ":" + strconv.Itoa(n.Line)
		p.out[key] = gonf.Value{Raw: n.Value, Origin: gonf.Origin{Location: location}}
	}
	return nil
}

// pair is an entry of a mapping.
type pair struct {
	name  string
	value *yaml.Node
}

// pairs returns the entries of the mapping n with the entries of its merge
// keys (<<), which the entries of n itself override. Of several merged
// mappings the first wins, as YAML defines.
func (p *parser) pairs(n *yaml.Node) ([]pair, error) {
	var own, merged []pair
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if err := p.count(k); err != nil {
			return nil, err
		}
		if k.ShortTag() == "!!merge" {
			m, err := p.merge(v)
			if err != nil {
				return nil, err
			}
			merged = append(merged, m...)
			continue
		}
		name, err := p.keyName(k)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, p.errorf(k, "key %q defined twice", k.Value)
		}
		seen[name] = true
		own = append(own, pair{name: name, value: v})
	}
	for _, m := range merged {
		if !seen[m.name] {
			seen[m.name] = true
			own = append(own, m)
		}
	}
	return own, nil
}

// merge returns the entries that the value v of a merge key brings: a
// mapping or a list of mappings.
func (p *parser) merge(v *yaml.Node) ([]pair, error) {
	v = resolve(v)
	sources := []*yaml.Node{v}
	if v.Kind == yaml.SequenceNode {
		sources = v.Content
	}
	var out []pair
	for _, s := range sources {
		s = resolve(s)
		if s.Kind != yaml.MappingNode {
			return nil, p.errorf(s, "merge key << needs a mapping or a list of mappings")
		}
		pairs, err := p.pairs(s)
		if err != nil {
			return nil, err
		}
		out = append(out, pairs...)
	}
	return out, nil
}

// keyName returns the part of a key that the mapping key k names.
func (p *parser) keyName(k *yaml.Node) (string, error) {
	k = resolve(k)
	if k.Kind != yaml.ScalarNode || isNull(k) || k.Value == "" {
		return "", p.errorf(k, "a key must be a non-empty scalar")
	}
	return k.Value, nil
}

func (p *parser) errorf(n *yaml.Node, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", p.name, n.Line, fmt.Sprintf(format, args...))
}

// resolve follows aliases to the node they refer to.
func resolve(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
