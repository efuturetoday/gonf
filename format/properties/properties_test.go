package properties

import (
	"testing"
)

func TestParseProperties(t *testing.T) {
	t.Parallel()
	input := "# comment\n" +
		"! also comment\n" +
		"\n" +
		"a=1\n" +
		"b : 2\n" +
		"c 3\n" +
		"  d   =   spaced value  \n" +
		"empty=\n" +
		"multi=one, \\\n" +
		"      two\n" +
		"esc\\=key=x\\ty\n" +
		"unicode=\\u00e4\n" +
		"backslash=c:\\\\temp\\\\\n" +
		"dup=first\n" +
		"dup=second\n" +
		"utf8=grün\r\n"

	got, err := parseProperties("app.properties", []byte(input))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name             string
		key              string
		expectedRaw      string
		expectedLocation string
	}{
		{name: "equals separator", key: "a", expectedRaw: "1", expectedLocation: "app.properties:4"},
		{name: "colon separator", key: "b", expectedRaw: "2", expectedLocation: "app.properties:5"},
		{name: "space separator", key: "c", expectedRaw: "3", expectedLocation: "app.properties:6"},
		{name: "trailing spaces kept", key: "d", expectedRaw: "spaced value  ", expectedLocation: "app.properties:7"},
		{name: "empty value", key: "empty", expectedRaw: "", expectedLocation: "app.properties:8"},
		{name: "line continuation", key: "multi", expectedRaw: "one, two", expectedLocation: "app.properties:9"},
		{name: "escaped key and tab", key: "esc=key", expectedRaw: "x\ty", expectedLocation: "app.properties:11"},
		{name: "unicode escape", key: "unicode", expectedRaw: "ä", expectedLocation: "app.properties:12"},
		{name: "escaped backslash", key: "backslash", expectedRaw: `c:\temp\`, expectedLocation: "app.properties:13"},
		{name: "duplicate last wins", key: "dup", expectedRaw: "second", expectedLocation: "app.properties:15"},
		{name: "raw utf-8 with crlf", key: "utf8", expectedRaw: "grün", expectedLocation: "app.properties:16"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, ok := got[tt.key]
			if !ok {
				t.Fatalf("key %q missing", tt.key)
			}
			if v.Raw != tt.expectedRaw || v.Origin.Location != tt.expectedLocation {
				t.Errorf("%q = (%q, %q), want (%q, %q)", tt.key, v.Raw, v.Origin.Location, tt.expectedRaw, tt.expectedLocation)
			}
		})
	}
	if len(got) != len(tests) {
		t.Errorf("got %d keys, want %d: %v", len(got), len(tests), got)
	}
}

func TestParseProperties_SurrogatePair(t *testing.T) {
	t.Parallel()
	got, err := parseProperties("x.properties", []byte(`e=\uD83D\uDE00`))
	if err != nil {
		t.Fatal(err)
	}
	if got["e"].Raw != "😀" {
		t.Errorf("e = %q, want 😀", got["e"].Raw)
	}
}

func TestParseProperties_MalformedUnicode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "too short", input: `a=\u12`},
		{name: "not hex", input: `a=\uZZZZ`},
		{name: "high surrogate alone", input: `a=\uD83D`},
		{name: "high surrogate followed by text", input: `a=\uD83Dx`},
		{name: "high surrogate followed by non-surrogate", input: `a=\uD83D\u0041`},
		{name: "low surrogate alone", input: `a=\uDE00`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseProperties("x.properties", []byte(tt.input)); err == nil {
				t.Errorf("%s: expected error", tt.input)
			}
		})
	}
}

func FuzzParseProperties(f *testing.F) {
	f.Add("a=1\nb: 2\\\n 3\n")
	f.Add("\\u00e4=\\\\\n")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = parseProperties("fuzz.properties", []byte(s))
	})
}
