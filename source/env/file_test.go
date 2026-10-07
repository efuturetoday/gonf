package env_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
)

// writeFile writes content to a .env file in a temporary directory.
func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFile(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `# comment
DB_URL=jdbc:postgresql://localhost/app
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
`)
	src := env.File(path)
	if src.Name() != "file:"+path || src.Ordinal() != env.FileOrdinal {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
	got, err := src.Load(context.Background())
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
			if tt.expectedLocation != "" && v.Origin.Location != "file:"+path+tt.expectedLocation {
				t.Errorf("%s location = %q, want suffix %q", tt.key, v.Origin.Location, tt.expectedLocation)
			}
		})
	}
}

func TestFile_EdgeCases(t *testing.T) {
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
			values, err := env.File(writeFile(t, tt.content)).Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for k, v := range values {
				got[k] = v.Raw
			}
			if !maps.Equal(got, tt.expected) {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFile_Errors(t *testing.T) {
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
			_, err := env.File(writeFile(t, tt.content)).Load(context.Background())
			if err == nil || !strings.HasSuffix(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want suffix %q", err, tt.expectedError)
			}
		})
	}
}

func TestFile_Optional(t *testing.T) {
	t.Parallel()
	got, err := env.File(filepath.Join(t.TempDir(), ".env"), source.Optional()).Load(context.Background())
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty and no error", got, err)
	}
}

func TestFile_MapKey(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "DB_URL=jdbc:dotenv\n_DEV_DB_URL=jdbc:dev\n")
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(env.File(path)), gonf.WithProfiles("dev"))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := cfg.Lookup("db.url")
	if !ok || v.Raw != "jdbc:dev" || v.Origin.Profile != "dev" {
		t.Errorf("db.url = %+v, want jdbc:dev from profile dev", v)
	}
}

func FuzzFile(f *testing.F) {
	for _, seed := range []string{"", "A=1", "export A=\"x\\\"y\"", "A='b\nc'", "A=\"", "# c\n\nB=2 # x", "=", "A=\"\\"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		values, err := env.File(path).Load(context.Background())
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
