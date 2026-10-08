package yaml_test

import (
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf/format/yaml"
)

func TestFormat(t *testing.T) {
	t.Parallel()
	input := `defaults: &defaults
  pool:
    max-size: 10
  user: app
db.name: app
db:
  <<: *defaults
  url: postgres://localhost/app
  user: admin
  hosts: [a, b]
  enabled: yes
  mode: 0644
  ratio: 1.50
  since: 2001-12-14
  quoted: "x: y"
  empty: ""
  nothing: null
  none: []
  no-map: {}
  note: |
    two
    lines
servers:
  eu.west:
    port: 8080
  '"us.east"':
    port: 9090
  list:
    - host: h1
    - host: h2
both:
  <<: [{a: first}, {a: second, b: second}]
"%dev":
  db:
    url: postgres://dev/app
---
second: document
`
	got, err := yaml.Format.Parse("app.yaml", []byte(input))
	if err != nil {
		t.Fatal(err)
	}
	raws := make(map[string]string, len(got))
	for k, v := range got {
		raws[k] = v.Raw
	}
	expected := map[string]string{
		"defaults.pool.max-size": "10",
		"defaults.user":          "app",
		"db.pool.max-size":       "10",
		"db.url":                 "postgres://localhost/app",
		"db.user":                "admin",
		"db.hosts[0]":            "a",
		"db.hosts[1]":            "b",
		"db.enabled":             "yes",
		"db.mode":                "0644",
		"db.ratio":               "1.50",
		"db.since":               "2001-12-14",
		"db.quoted":              "x: y",
		"db.empty":               "",
		"db.note":                "two\nlines\n",
		"db.name":                "app",
		"servers.eu.west.port":   "8080",
		`servers."us.east".port`: "9090",
		"servers.list[0].host":   "h1",
		"servers.list[1].host":   "h2",
		"both.a":                 "first",
		"both.b":                 "second",
		"%dev.db.url":            "postgres://dev/app",
		"second":                 "document",
	}
	if !maps.Equal(raws, expected) {
		t.Errorf("got  %v\nwant %v", raws, expected)
	}
	locations := map[string]string{
		"db.url":           "app.yaml:8",
		"db.pool.max-size": "app.yaml:3",
		"second":           "app.yaml:37",
	}
	for key, expected := range locations {
		if got := got[key].Origin.Location; got != expected {
			t.Errorf("%s: location = %q, want %q", key, got, expected)
		}
	}
}

func TestFormat_Empty(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "# only a comment\n", "~\n", "---\n---\n"} {
		got, err := yaml.Format.Parse("app.yaml", []byte(input))
		if err != nil || len(got) != 0 {
			t.Errorf("Parse(%q) = %v, %v, want no keys", input, got, err)
		}
	}
}

func TestFormat_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		input         string
		expectedError string
	}{
		{name: "top level list", input: "- a\n", expectedError: "app.yaml:1: top level must be a mapping"},
		{name: "top level scalar", input: "a\n", expectedError: "app.yaml:1: top level must be a mapping"},
		{name: "syntax", input: "a: [b\n", expectedError: "app.yaml: yaml: line 1: did not find expected ',' or ']'"},
		{name: "key twice", input: "a: 1\nb: 2\na: 3\n", expectedError: `app.yaml:3: key "a" defined twice`},
		{
			name:          "key in two documents",
			input:         "a: 1\n---\na: 2\n",
			expectedError: `app.yaml:3: key "a" defined twice, first at app.yaml:1`,
		},
		{
			name:          "nested and dotted key",
			input:         "a:\n  b[0]: 1\n  b: [2]\n",
			expectedError: `app.yaml:3: key "a.b[0]" defined twice, first at app.yaml:2`,
		},
		{
			name:          "dotted and nested key",
			input:         "a.b: 1\na:\n  b: 2\n",
			expectedError: `app.yaml:3: key "a.b" defined twice, first at app.yaml:1`,
		},
		{name: "list as key", input: "? [a]\n: x\n", expectedError: "app.yaml:1: a key must be a non-empty scalar"},
		{name: "empty key", input: "\"\": x\n", expectedError: "app.yaml:1: a key must be a non-empty scalar"},
		{name: "null key", input: "~: x\n", expectedError: "app.yaml:1: a key must be a non-empty scalar"},
		{name: "merge of a scalar", input: "a:\n  <<: x\n", expectedError: "app.yaml:2: merge key << needs a mapping or a list of mappings"},
		{name: "key twice in a merged mapping", input: "a:\n  <<: {b: 1, b: 2}\n", expectedError: `app.yaml:2: key "b" defined twice`},
		{name: "aliases blow up", input: aliasBomb(), expectedError: "more than 100000 values, are aliases nested too deep?"},
		{name: "merge keys blow up", input: mergeBomb(), expectedError: "more than 100000 values, are aliases nested too deep?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := yaml.Format.Parse("app.yaml", []byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

// aliasBomb returns a document of a few lines whose aliases expand to 10^9
// values.
func aliasBomb() string {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 9; i++ {
		prev := "*a" + string(rune('0'+i-1))
		name := "a" + string(rune('0'+i))
		b.WriteString(name + ": &" + name + " [" + strings.Repeat(prev+", ", 9) + prev + "]\n")
	}
	return b.String()
}

// mergeBomb returns a document whose merge keys each merge the previous
// mapping twice, which takes 2^40 steps unless they are counted.
func mergeBomb() string {
	var b strings.Builder
	b.WriteString("m0: &m0 {a: 1}\n")
	for i := 1; i <= 40; i++ {
		prev := "*m" + strconv.Itoa(i-1)
		name := "m" + strconv.Itoa(i)
		b.WriteString(name + ": &" + name + " {<<: [" + prev + ", " + prev + "]}\n")
	}
	return b.String()
}

func FuzzFormat(f *testing.F) {
	for _, seed := range []string{
		"a: 1\n",
		"a: {b: [1, 2]}\n",
		"a: &x {b: 1}\nc: *x\nd:\n  <<: *x\n",
		"a.b: 1\n---\nc: 2\n",
		"- a\n",
		"? [a]\n: b\n",
		".: 0\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got, err := yaml.Format.Parse("app.yaml", []byte(input))
		if err != nil {
			return
		}
		for key, v := range got {
			// Like a .properties file, YAML takes names as they are, so a
			// key may be "." or "a..b", but never empty.
			if key == "" {
				t.Errorf("Parse(%q): invalid key %q", input, key)
			}
			if !strings.HasPrefix(v.Origin.Location, "app.yaml:") {
				t.Errorf("Parse(%q): %q has location %q", input, key, v.Origin.Location)
			}
		}
	})
}
