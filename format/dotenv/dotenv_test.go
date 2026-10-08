package dotenv_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf/format/dotenv"
)

// parse parses content as a file named ".env".
func parse(content string) (map[string]string, error) {
	values, err := dotenv.Format.Parse(".env", []byte(content))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range values {
		out[k] = v.Raw
	}
	return out, nil
}

func TestFormat(t *testing.T) {
	t.Parallel()
	got, err := dotenv.Format.Parse(".env", []byte(`# comment
DB_URL=postgres://localhost/app
export DB_USER = app
TRAILING=value   # comment
HASH=a#b
EMPTY=
DOUBLE="say \"hi\"\n\ttab \\ \$x"
SINGLE='literal \n ${x}'
MULTI="line one
line two"
db.pool.max-size=7
DB_URL=override
`))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name             string
		key              string
		expectedRaw      string
		expectedLocation string
	}{
		{name: "later assignment wins", key: "DB_URL", expectedRaw: "override", expectedLocation: ":12"},
		{name: "export prefix and spaces around equals", key: "DB_USER", expectedRaw: "app", expectedLocation: ":3"},
		{name: "trailing comment", key: "TRAILING", expectedRaw: "value"},
		{name: "hash without space is kept", key: "HASH", expectedRaw: "a#b"},
		{name: "empty value", key: "EMPTY", expectedRaw: ""},
		{name: "double quotes with escapes", key: "DOUBLE", expectedRaw: "say \"hi\"\n\ttab \\ \\$x"},
		{name: "single quotes are literal", key: "SINGLE", expectedRaw: `literal \n ${x}`},
		{name: "multi line value", key: "MULTI", expectedRaw: "line one\nline two", expectedLocation: ":9"},
		{name: "property style key", key: "db.pool.max-size", expectedRaw: "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, ok := got[tt.key]
			if !ok || v.Raw != tt.expectedRaw {
				t.Errorf("%s = %q (found %v), want %q", tt.key, v.Raw, ok, tt.expectedRaw)
			}
			if tt.expectedLocation != "" && v.Origin.Location != ".env"+tt.expectedLocation {
				t.Errorf("%s location = %q, want suffix %q", tt.key, v.Origin.Location, tt.expectedLocation)
			}
		})
	}
}

func TestFormat_EdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{name: "crlf line endings", content: "A=1\r\nB=\"2\"\r\n# c\r\n", expected: map[string]string{"A": "1", "B": "2"}},
		{name: "comment after quoted value", content: "A=\"x\" # note\nB='y'\t# note", expected: map[string]string{"A": "x", "B": "y"}},
		{name: "quoted value at end of file", content: "A=\"x\"", expected: map[string]string{"A": "x"}},
		{name: "empty value at end of file", content: "A=", expected: map[string]string{"A": ""}},
		{name: "comment at end of file", content: "A=1\n# last", expected: map[string]string{"A": "1"}},
		{name: "escaped line break keeps backslash", content: "A=\"x\\\ny\"\nB=2", expected: map[string]string{"A": "x\\\ny", "B": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parse(tt.content)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(got, tt.expected) {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormat_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		content       string
		expectedError string
	}{
		{name: "missing equals", content: "A=1\nNOPE\n", expectedError: ":2: missing '='"},
		{name: "invalid key", content: "MY KEY=1", expectedError: `:1: invalid key "MY KEY"`},
		{name: "empty key", content: "=1", expectedError: `:1: invalid key ""`},
		{name: "unclosed double quote", content: "A=1\nB=\"open\nstill open", expectedError: ":2: unclosed \""},
		{name: "unclosed single quote", content: "B='open", expectedError: ":1: unclosed '"},
		{name: "text after quoted value", content: `A="x" y`, expectedError: ":1: unexpected text after quoted value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse(tt.content)
			if err == nil || !strings.HasSuffix(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want suffix %q", err, tt.expectedError)
			}
		})
	}
}

func FuzzFormat(f *testing.F) {
	for _, seed := range []string{"", "A=1", "export A=\"x\\\"y\"", "A='b\nc'", "A=\"", "# c\n\nB=2 # x", "=", "A=\"\\"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		values, err := parse(content)
		if err != nil {
			return
		}
		for key := range values {
			if key == "" || strings.ContainsAny(key, " \t\n=") {
				t.Errorf("invalid key %q from %q", key, content)
			}
		}
	})
}
