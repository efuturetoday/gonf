// Package json provides the JSON format for gonf.
package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Format flattens a JSON object into dotted keys.
var Format source.Format = source.FormatFunc(parseJSON)

// parseJSON flattens a JSON object into dotted keys: nested objects become
// "a.b", arrays become "a[0]". Numbers keep their literal text. Every value
// has its file and line as Location. null values and empty arrays/objects
// produce no key. A name twice in one object, or two paths flattening to the
// same key (e.g. "a.b" and {"a":{"b":…}}), are an error.
func parseJSON(name string, data []byte) (map[string]gonf.Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	p := &parser{name: name, data: data, dec: dec, out: make(map[string]gonf.Value), line: 1}

	tok, err := dec.Token()
	if err != nil {
		return nil, p.wrap(err)
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("%s: top level must be a json object", name)
	}
	if err := p.object(""); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: unexpected data after top-level object", name)
	}
	return p.out, nil
}

// parser reads the tokens of one file and collects its keys.
type parser struct {
	name string
	data []byte
	dec  *json.Decoder
	out  map[string]gonf.Value

	// line is the line of offset; both only move forward.
	line   int
	offset int
}

// object reads the members of an object whose '{' was just read, up to and
// including its '}'. The decoder limits the nesting depth.
func (p *parser) object(prefix string) error {
	first := make(map[string]int) // name -> line
	for p.dec.More() {
		tok, err := p.dec.Token()
		if err != nil {
			return p.wrap(err)
		}
		name, ok := tok.(string)
		if !ok {
			return p.errorf("expected a name")
		}
		key := join(prefix, name)
		if line, dup := first[name]; dup {
			return p.errorf("key %q defined twice, first at %s:%d", key, p.name, line)
		}
		first[name] = p.lineNow()
		if err := p.value(key); err != nil {
			return err
		}
	}
	_, err := p.dec.Token()
	return p.wrap(err)
}

// array reads the elements of an array whose '[' was just read, up to and
// including its ']'.
func (p *parser) array(prefix string) error {
	for i := 0; p.dec.More(); i++ {
		if err := p.value(prefix + "[" + strconv.Itoa(i) + "]"); err != nil {
			return err
		}
	}
	_, err := p.dec.Token()
	return p.wrap(err)
}

// value reads the next value and stores it under key.
func (p *parser) value(key string) error {
	tok, err := p.dec.Token()
	if err != nil {
		return p.wrap(err)
	}
	switch tok := tok.(type) {
	case json.Delim:
		switch tok {
		case '{':
			return p.object(key)
		case '[':
			return p.array(key)
		}
	case string:
		return p.set(key, tok)
	case json.Number:
		return p.set(key, tok.String())
	case bool:
		return p.set(key, strconv.FormatBool(tok))
	case nil:
		return nil // Skipped; explicit empty/null handling is decided later.
	}
	return p.errorf("unexpected json value")
}

// set stores raw under key at the line of the token just read.
func (p *parser) set(key, raw string) error {
	location := p.name + ":" + strconv.Itoa(p.lineNow())
	if prev, dup := p.out[key]; dup {
		return p.errorf("key %q defined twice, first at %s", key, prev.Origin.Location)
	}
	p.out[key] = gonf.Value{Raw: raw, Origin: gonf.Origin{Location: location}}
	return nil
}

// lineNow returns the line on which the token just read ends.
func (p *parser) lineNow() int {
	return p.lineAt(int(p.dec.InputOffset()))
}

// lineAt returns the line of offset. Offsets only move forward; an earlier
// one gets the line of the last.
func (p *parser) lineAt(offset int) int {
	offset = min(offset, len(p.data))
	if offset > p.offset {
		p.line += bytes.Count(p.data[p.offset:offset], []byte("\n"))
		p.offset = offset
	}
	return p.line
}

// errorf returns an error at the line of the token just read.
func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", p.name, p.lineNow(), fmt.Sprintf(format, args...))
}

// wrap adds the file, and for a syntax error its line, to err.
func (p *parser) wrap(err error) error {
	if err == nil {
		return nil
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Errorf("%s:%d: %w", p.name, p.lineAt(int(syntax.Offset)), err)
	}
	return fmt.Errorf("%s: %w", p.name, err)
}

// join appends name to the key prefix.
func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
