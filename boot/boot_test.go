package boot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/secret/aesgcm"
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

// load is Load with workDir as the working directory.
func load(workDir string, opts ...Option) (*gonf.Config, error) {
	return gonf.Load(context.Background(), build(workDir, opts)...)
}

// Keys in these tests start with "boottest" so that real environment
// variables cannot interfere.

// bootKeys makes keys known, so that the environment and .env provide them.
type bootKeys struct {
	Env, Dotenv, URL string `default:""`
}

func TestLoad_Precedence(t *testing.T) {
	t.Setenv("BOOTTEST_ENV", "env")
	dir := workDir(t, map[string]string{
		".env":                  "BOOTTEST_ENV=dotenv\nBOOTTEST_DOTENV=dotenv\n",
		"config/app.properties": "boottest.env=config\nboottest.dotenv=config\nboottest.config=config\n",
		"config/app.json":       `{"boottest": {"json": "config-json"}}`,
		"app.properties":        "boottest.ignored=working-dir-root-is-not-read\n",
	})
	embedded := fstest.MapFS{"app.properties": {Data: []byte("boottest.config=embedded\nboottest.embedded=embedded\n")}}

	cfg, err := load(dir, WithFS(embedded), WithMappings(gonf.NewMapping[bootKeys]("boottest", gonf.AllowUnknown())))
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
			opts := append(slices.Clone(tt.opts), WithMappings(gonf.NewMapping[bootKeys]("boottest", gonf.AllowUnknown())))
			if tt.embedded != nil {
				opts = append(opts, WithFS(tt.embedded))
			}
			cfg, err := load(workDir(t, tt.files), opts...)
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
	cfg, err := load(dir, WithFormat(".kv", upper), WithFormat("properties", upper))
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

func TestWithFunctions(t *testing.T) {
	t.Parallel()
	encrypted, err := aesgcm.Encrypt("k3y", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	upper := gonf.Function{Name: "upper", Call: func(_ *gonf.Config, value string, _ gonf.Origin) (string, error) {
		return strings.ToUpper(value), nil
	}}
	secretFile := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretFile, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		files    map[string]string
		opts     []Option
		key      string
		expected string
		// sameAs is a key with the same value, instead of expected.
		sameAs string
	}{
		{
			name: "aes-gcm by default, key from .env",
			files: map[string]string{
				".env":                  "GONF_SECRET_AES_GCM_KEY=k3y\n",
				"config/app.properties": "boottest.password=${aes-gcm::" + encrypted + "}\n",
			},
			key:      "boottest.password",
			expected: "s3cret",
		},
		{
			name:     "aes-gcm replaced",
			files:    map[string]string{"config/app.properties": "boottest.password=${aes-gcm::" + encrypted + "}\n"},
			opts:     []Option{WithFunctions(aesgcm.WithKey("k3y"))},
			key:      "boottest.password",
			expected: "s3cret",
		},
		{
			name:     "function added",
			files:    map[string]string{"config/app.properties": "boottest.token=${upper::abc}\n"},
			opts:     []Option{WithFunctions(upper)},
			key:      "boottest.token",
			expected: "ABC",
		},
		{
			name:     "env by default",
			files:    map[string]string{"config/app.properties": "boottest.home=${env::HOME}/logs\n"},
			key:      "boottest.home",
			expected: os.Getenv("HOME") + "/logs",
		},
		{
			name:     "file and base64 by default",
			files:    map[string]string{"config/app.properties": "boottest.b64=${base64::${file::" + secretFile + "}}\n"},
			key:      "boottest.b64",
			expected: "ZnJvbS1maWxl",
		},
		{
			name:     "base64-decode by default",
			files:    map[string]string{"config/app.properties": "boottest.text=${base64-decode::YQ==}\n"},
			key:      "boottest.text",
			expected: "a",
		},
		{
			name:   "uuid by default, the same within one Config",
			files:  map[string]string{"config/app.properties": "boottest.id=${uuid::}\nboottest.same=${uuid::}\n"},
			key:    "boottest.id",
			sameAs: "boottest.same",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(workDir(t, tt.files), tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			expected := tt.expected
			if tt.sameAs != "" {
				if expected, err = cfg.Get(tt.sameAs); err != nil || expected == "" {
					t.Fatalf("Get(%q) = %q, %v", tt.sameAs, expected, err)
				}
			}
			if got, err := cfg.Get(tt.key); err != nil || got != expected {
				t.Errorf("Get(%q) = %q, %v; want %q", tt.key, got, err, expected)
			}
		})
	}
}

func TestWith(t *testing.T) {
	t.Parallel()
	dir := workDir(t, map[string]string{"config/app.properties": "boottest.a=file\n"})
	cfg, err := load(dir, With(
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
	cfg, err := load(dir, WithMappings(m))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get(cfg).Port; got != 80 {
		t.Errorf("port = %d, want 80", got)
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	type db struct {
		URL string `desc:"Address."`
	}
	props, err := Describe(WithMappings(gonf.NewMapping[db]("boottest.db")))
	if err != nil {
		t.Fatal(err)
	}
	// The key of the default secret handler comes first.
	if len(props) != 2 || props[0].Key != aesgcm.Key || props[0].Env != "GONF_SECRET_AES_GCM_KEY" || !props[0].Secret {
		t.Fatalf("Describe = %+v, want the aes-gcm key and boottest.db.url", props)
	}
	expected := gonf.Property{Key: "boottest.db.url", Env: "BOOTTEST_DB_URL", Type: "string", Required: true, Doc: "Address.", Mapping: "boottest.db", Location: "boot_test.go:349"}
	if !reflect.DeepEqual(props[1], expected) {
		t.Errorf("Describe = %+v, want %+v", props[1], expected)
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
			_, err := load(workDir(t, tt.files))
			var se *gonf.SourceError
			if !errors.As(err, &se) || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want SourceError mentioning %q", err, tt.expectedError)
			}
		})
	}
}

func TestWithDir(t *testing.T) {
	t.Parallel()
	dir := workDir(t, map[string]string{"config/app.properties": "boottest.config=from-dir\n"})
	cfg, err := load(t.TempDir(), WithDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := cfg.Get("boottest.config"); v != "from-dir" {
		t.Errorf("boottest.config = %q, want from-dir", v)
	}
}

func TestWithoutDotenv(t *testing.T) {
	t.Setenv("BOOTTEST_ENV", "env")
	dir := workDir(t, map[string]string{
		".env":                  "BOOTTEST_DOTENV=dotenv\n",
		"config/app.properties": "boottest.env=config\nboottest.dotenv=config\n",
	})
	cfg, err := load(dir, WithoutDotenv(), WithMappings(gonf.NewMapping[bootKeys]("boottest")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{name: "environment is read", key: "boottest.env", expected: "env"},
		{name: ".env is not read", key: "boottest.dotenv", expected: "config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.Explain(tt.key).Value; got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.key, got, tt.expected)
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

func TestWatch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := workDir(t, map[string]string{
			"config/app.properties":     "boottest.a=1",
			"config/app-dev.properties": "boottest.a=dev",
		})
		embedded := fstest.MapFS{"app.properties": {Data: []byte("boottest.b=embedded")}}
		var mu sync.Mutex
		var checked []string
		trigger := func(path string) source.Trigger {
			mu.Lock()
			defer mu.Unlock()
			checked = append(checked, path)
			return source.Every(time.Second)
		}
		reloads := make(chan *gonf.Config, 10)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		live, err := gonf.Watch(ctx, build(dir, []Option{
			WithFS(embedded),
			WithFileTrigger(trigger),
			With(
				gonf.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
				gonf.OnReload(func(cfg *gonf.Config, err error) {
					if err == nil {
						reloads <- cfg
					}
				}),
			),
		})...)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := live.Config().Get("boottest.a"); v != "1" {
			t.Fatalf("boottest.a = %q, want 1", v)
		}

		// A reload is a complete load, so a new profile reads its files.
		write := filepath.Join(dir, "config", "app.properties")
		if err := os.WriteFile(write, []byte("boottest.a=2\ngonf.profile=dev"), 0o600); err != nil {
			t.Fatal(err)
		}
		select {
		case cfg := <-reloads:
			if v, _ := cfg.Get("boottest.a"); v != "dev" {
				t.Errorf("boottest.a = %q, want dev from app-dev.properties", v)
			}
		case <-time.After(time.Minute):
			t.Fatal("no reload")
		}

		mu.Lock()
		defer mu.Unlock()
		for _, want := range []string{
			filepath.Join(dir, "config", "app.properties"),
			filepath.Join(dir, "config", "app-dev.properties"),
			filepath.Join(dir, ".env"),
		} {
			if !slices.Contains(checked, want) {
				t.Errorf("%s is not checked; checked: %q", want, checked)
			}
		}
		for _, path := range checked {
			if !strings.HasPrefix(path, dir) {
				t.Errorf("%s is checked, want only files on disk", path)
			}
		}
	})
}
