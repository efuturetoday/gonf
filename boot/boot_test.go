package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

// workDir creates a working directory with the given files.
func workDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Keys in these tests start with "boottest" so that real environment
// variables cannot interfere.

func TestLoad_Precedence(t *testing.T) {
	t.Setenv("BOOTTEST_ENV", "env")
	dir := workDir(t, map[string]string{
		".env":                  "BOOTTEST_ENV=dotenv\nBOOTTEST_DOTENV=dotenv\n",
		"config/app.properties": "boottest.env=config\nboottest.dotenv=config\nboottest.config=config\n",
		"config/app.json":       `{"boottest": {"json": "config-json"}}`,
		"app.properties":        "boottest.ignored=working-dir-root-is-not-read\n",
	})
	embedded := fstest.MapFS{"app.properties": {Data: []byte("boottest.config=embedded\nboottest.embedded=embedded\n")}}

	cfg, err := load(context.Background(), dir, WithFS(embedded))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name            string
		key             string
		expectedRaw     string
		expectedOrdinal int
	}{
		{name: "env beats .env", key: "boottest.env", expectedRaw: "env", expectedOrdinal: 300},
		{name: ".env beats config dir", key: "boottest.dotenv", expectedRaw: "dotenv", expectedOrdinal: 295},
		{name: "config dir beats embedded", key: "boottest.config", expectedRaw: "config", expectedOrdinal: ConfigDirOrdinal},
		{name: "embedded", key: "boottest.embedded", expectedRaw: "embedded", expectedOrdinal: EmbeddedOrdinal},
		{name: "json next to properties", key: "boottest.json", expectedRaw: "config-json", expectedOrdinal: ConfigDirOrdinal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := cfg.Lookup(tt.key)
			if !ok || v.Raw != tt.expectedRaw || v.Origin.Ordinal != tt.expectedOrdinal {
				t.Errorf("Lookup(%q) = %+v, want %q with ordinal %d", tt.key, v, tt.expectedRaw, tt.expectedOrdinal)
			}
		})
	}
	if _, ok := cfg.Lookup("boottest.ignored"); ok {
		t.Error("app.properties in the working directory root was read")
	}
}

func TestLoad_Profiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		files            map[string]string
		embedded         fstest.MapFS
		opts             []Option
		expectedProfiles []string
		expectedRaw      string
	}{
		{
			name: "no profile",
			files: map[string]string{
				"config/app.properties":     "boottest.url=base\n",
				"config/app-dev.properties": "boottest.url=dev\n",
			},
			expectedRaw: "base",
		},
		{
			name: "profile key in .env activates profile file",
			files: map[string]string{
				".env":                      "GONF_PROFILE=dev\n",
				"config/app.properties":     "boottest.url=base\n",
				"config/app-dev.properties": "boottest.url=dev\n",
			},
			expectedProfiles: []string{"dev"},
			expectedRaw:      "dev",
		},
		{
			name: "profile key in app.properties, last profile wins",
			files: map[string]string{
				"config/app.properties":       "gonf.profile=dev,local\nboottest.url=base\n",
				"config/app-dev.properties":   "boottest.url=dev\n",
				"config/app-local.properties": "boottest.url=local\n",
			},
			expectedProfiles: []string{"dev", "local"},
			expectedRaw:      "local",
		},
		{
			name: "files of inactive profiles are never read",
			files: map[string]string{
				".env":                 "GONF_PROFILE=dev\n",
				"config/app.json":      `{"boottest": {"url": "base"}}`,
				"config/app-prod.json": `{ broken`,
			},
			expectedProfiles: []string{"dev"},
			expectedRaw:      "base",
		},
		{
			name: "profile file in embedded fs",
			files: map[string]string{
				".env": "GONF_PROFILE=dev\n",
			},
			embedded: fstest.MapFS{
				"app.properties":     {Data: []byte("boottest.url=base\n")},
				"app-dev.properties": {Data: []byte("boottest.url=embedded-dev\n")},
			},
			expectedProfiles: []string{"dev"},
			expectedRaw:      "embedded-dev",
		},
		{
			name: "profile value of config dir loses to base value of .env",
			files: map[string]string{
				".env":                      "GONF_PROFILE=dev\nBOOTTEST_URL=dotenv\n",
				"config/app-dev.properties": "boottest.url=dev\n",
			},
			expectedProfiles: []string{"dev"},
			expectedRaw:      "dotenv",
		},
		{
			name: "with profiles wins over profile key",
			files: map[string]string{
				".env":                       "GONF_PROFILE=dev\n",
				"config/app-dev.properties":  "boottest.url=dev\n",
				"config/app-test.properties": "boottest.url=test\n",
			},
			opts:             []Option{With(gonf.WithProfiles("test"))},
			expectedProfiles: []string{"test"},
			expectedRaw:      "test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := tt.opts
			if tt.embedded != nil {
				opts = append(opts, WithFS(tt.embedded))
			}
			cfg, err := load(context.Background(), workDir(t, tt.files), opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Profiles(); !slices.Equal(got, tt.expectedProfiles) {
				t.Errorf("Profiles = %v, want %v", got, tt.expectedProfiles)
			}
			if v, _ := cfg.Lookup("boottest.url"); v.Raw != tt.expectedRaw {
				t.Errorf("boottest.url = %q, want %q", v.Raw, tt.expectedRaw)
			}
		})
	}
}

func TestWithFormat(t *testing.T) {
	t.Parallel()
	// upper is a toy format: every line "k=v" with the value upper-cased.
	upper := source.FormatFunc(func(_ string, data []byte) (map[string]gonf.Value, error) {
		out := map[string]gonf.Value{}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			k, v, _ := strings.Cut(line, "=")
			out[k] = gonf.Value{Raw: strings.ToUpper(v)}
		}
		return out, nil
	})
	dir := workDir(t, map[string]string{
		".env":                  "GONF_PROFILE=dev\n",
		"config/app.kv":         "boottest.kv=custom",
		"config/app-dev.kv":     "boottest.kv.dev=custom-dev",
		"config/app.properties": "boottest.props=replaced",
	})
	cfg, err := load(context.Background(), dir, WithFormat(".kv", upper), WithFormat("properties", upper))
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"boottest.kv":     "CUSTOM",
		"boottest.kv.dev": "CUSTOM-DEV",
		"boottest.props":  "REPLACED",
	} {
		if v, _ := cfg.Lookup(key); v.Raw != expected {
			t.Errorf("%s = %q, want %q", key, v.Raw, expected)
		}
	}
}

func TestWith(t *testing.T) {
	t.Parallel()
	dir := workDir(t, map[string]string{"config/app.properties": "boottest.a=file\n"})
	cfg, err := load(context.Background(), dir, With(
		gonf.WithSources(memory.New(map[string]string{"boottest.a": "memory"}, source.WithOrdinal(ConfigDirOrdinal))),
	))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := cfg.Lookup("boottest.a"); v.Raw != "memory" {
		t.Errorf("boottest.a = %q, want later registered source to win", v.Raw)
	}
}

func TestWithMappings(t *testing.T) {
	t.Parallel()
	dir := workDir(t, map[string]string{"config/app.properties": "boottest.port=80\n"})
	m := gonf.NewMapping[struct{ Port int }]("boottest")
	cfg, err := load(context.Background(), dir, WithMappings(m))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get(cfg).Port; got != 80 {
		t.Errorf("port = %d, want 80", got)
	}
}

func TestLoad_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		files         map[string]string
		expectedError string
	}{
		{name: "broken base file", files: map[string]string{"config/app.json": "{"}, expectedError: "config/app.json"},
		{name: "broken active profile file", files: map[string]string{
			".env":                "GONF_PROFILE=dev\n",
			"config/app-dev.json": "{",
		}, expectedError: "config/app-dev.json"},
		{name: "broken .env", files: map[string]string{".env": "NOPE"}, expectedError: ".env:1: missing '='"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := load(context.Background(), workDir(t, tt.files))
			var se *gonf.SourceError
			if !errors.As(err, &se) || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want SourceError mentioning %q", err, tt.expectedError)
			}
		})
	}
}

func TestLoad_WorkingDirectory(t *testing.T) {
	t.Parallel()
	// Load uses the working directory; without files there it still works.
	if _, err := Load(context.Background()); err != nil {
		t.Fatal(err)
	}
}
