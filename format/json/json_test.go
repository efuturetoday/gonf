package json

import (
	"maps"
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
		if v.Origin.Location != "app.json" {
			t.Errorf("%q location = %q", k, v.Origin.Location)
		}
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

func TestParseJSON_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "top level array", input: `[1,2]`},
		{name: "top level string", input: `"x"`},
		{name: "truncated", input: `{`},
		{name: "trailing data", input: `{} {}`},
		{name: "key collision", input: `{"a.b":1,"a":{"b":2}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseJSON("bad.json", []byte(tt.input)); err == nil {
				t.Errorf("parseJSON(%s): expected error", tt.input)
			}
		})
	}
}
