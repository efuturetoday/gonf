// Package json provides the JSON format for gonf.
package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Format flattens a JSON object into dotted keys.
var Format source.Format = source.FormatFunc(parseJSON)

// parseJSON flattens a JSON object into dotted keys: nested objects become
// "a.b", arrays become "a[0]". Numbers keep their literal text.
// null values and empty arrays/objects produce no key. Two paths flattening
// to the same key (e.g. "a.b" and {"a":{"b":…}}) are an error.
func parseJSON(name string, data []byte) (map[string]gonf.Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s: unexpected data after top-level object", name)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: top level must be a json object", name)
	}

	out := make(map[string]gonf.Value)
	origin := gonf.Origin{Location: name}
	set := func(key, raw string) error {
		if _, dup := out[key]; dup {
			return fmt.Errorf("key %q defined twice", key)
		}
		out[key] = gonf.Value{Raw: raw, Origin: origin}
		return nil
	}
	var flatten func(prefix string, v any) error
	flatten = func(prefix string, v any) error {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}
				if err := flatten(key, child); err != nil {
					return err
				}
			}
		case []any:
			for i, child := range v {
				if err := flatten(prefix+"["+strconv.Itoa(i)+"]", child); err != nil {
					return err
				}
			}
		case string:
			return set(prefix, v)
		case json.Number:
			return set(prefix, v.String())
		case bool:
			return set(prefix, strconv.FormatBool(v))
		case nil:
			// Skipped; explicit empty/null handling is decided later.
		default:
			return errors.New("unexpected json value")
		}
		return nil
	}
	if err := flatten("", obj); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}
