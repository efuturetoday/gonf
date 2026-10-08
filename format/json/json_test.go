package json

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func TestParseJSON(t *testing.T) {
	t.Parallel()
	input := `{
		"db": {"url": "postgres://x", "pool": {"max-size": 10, "ratio": 0.5}},
		"tags": ["a", "b"],
		"servers": [{"host": "h1"}, {"host": "h2"}],
		"enabled": true,
		"big": 12345678901234567890,
		"nothing": null,
		"none": [],
		"empty": {}
	}`
	got, err := parseJSON("app.json", []byte(input))
	if err != nil {
		t.Fatal(err)
	}

	raws := make(map[string]string, len(got))
	for k, v := range got {
		raws[k] = v.Raw
	}
	expected := map[string]string{
		"db.url":           "postgres://x",
		"db.pool.max-size": "10",
		"db.pool.ratio":    "0.5",
		"tags[0]":          "a",
		"tags[1]":          "b",
		"servers[0].host":  "h1",
		"servers[1].host":  "h2",
		"enabled":          "true",
		"big":              "12345678901234567890",
	}
	if !maps.Equal(raws, expected) {
		t.Errorf("got  %v\nwant %v", raws, expected)
	}
}

func TestParseJSON_Location(t *testing.T) {
	t.Parallel()
	input := "{\n" +
		`  "db": {` + "\n" +
		`    "url": "postgres://x",` + "\n" +
		`    "hosts": [` + "\n" +
		`      "a",` + "\n" +
		`      "b"` + "\n" +
		"    ]\n" +
		"  },\n" +
		`  "port": 80, "debug": true` + "\n" +
		"}\n"
	got, err := parseJSON("app.json", []byte(input))
	if err != nil {
		t.Fatal(err)
	}

	locations := make(map[string]string, len(got))
	for k, v := range got {
		locations[k] = v.Origin.Location
	}
	expected := map[string]string{
		"db.url":      "app.json:3",
		"db.hosts[0]": "app.json:5",
		"db.hosts[1]": "app.json:6",
		"port":        "app.json:9",
		"debug":       "app.json:9",
	}
	if !maps.Equal(locations, expected) {
		t.Errorf("got  %v\nwant %v", locations, expected)
	}
}

func TestParseJSON_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: ``, expected: "bad.json: EOF"},
		{name: "top level array", input: `[1,2]`, expected: "bad.json: top level must be a json object"},
		{name: "top level string", input: `"x"`, expected: "bad.json: top level must be a json object"},
		// The decoder's text for this differs between Go versions.
		{name: "truncated", input: `{`},
		{name: "trailing data", input: `{} {}`, expected: "bad.json: unexpected data after top-level object"},
		{
			name:     "syntax error with line",
			input:    "{\n\"a\": 1,\n\"b\" 2}",
			expected: "bad.json:3: invalid character '2' after object key",
		},
		{
			name:     "name twice",
			input:    "{\n\"port\": 80,\n\"port\": 8080\n}",
			expected: `bad.json:3: key "port" defined twice, first at bad.json:2`,
		},
		{
			name:     "name twice with object values",
			input:    `{"db": {"a": {"x": 1}, "a": {"y": 2}}}`,
			expected: `bad.json:1: key "db.a" defined twice, first at bad.json:1`,
		},
		{
			name:     "key collision",
			input:    "{\"a.b\": 1,\n\"a\": {\"b\": 2}}",
			expected: `bad.json:2: key "a.b" defined twice, first at bad.json:1`,
		},
		{
			name:     "nesting too deep",
			input:    strings.Repeat(`{"a":[`, maxDepth/2+1) + "1" + strings.Repeat("]}", maxDepth/2+1),
			expected: "bad.json:1: nesting deeper than 1000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseJSON("bad.json", []byte(tt.input))
			if err == nil || tt.expected != "" && err.Error() != tt.expected {
				t.Errorf("error = %v, want %q", err, tt.expected)
			}
		})
	}
}

func FuzzParseJSON(f *testing.F) {
	f.Add(`{"db": {"url": "x", "hosts": ["a", "b"]}, "port": 80}`)
	f.Add(`{"a.b": 1, "a": {"b": 2}}`)
	f.Add(`{"a": 1, "a": 2}`)
	f.Add("{\n\"a\": [null, true, 1e3]\n}")
	f.Fuzz(func(t *testing.T, s string) {
		got, err := parseJSON("fuzz.json", []byte(s))
		if err != nil {
			return
		}
		if !json.Valid([]byte(s)) {
			t.Fatalf("parseJSON(%q) accepted invalid json", s)
		}
		for k, v := range got {
			if !strings.HasPrefix(v.Origin.Location, "fuzz.json:") {
				t.Errorf("%q location = %q", k, v.Origin.Location)
			}
		}
	})
}
