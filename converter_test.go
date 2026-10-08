package gonf_test

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/memory"
)

// seconds reads a bare number as seconds, like Quarkus does for durations.
func seconds(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(s)
}

// upperLevel replaces the UnmarshalText of level.
func upperLevel(s string) (level, error) {
	return level(len(s)), nil
}

type endpoints struct {
	Primary  *url.URL `default:"http://localhost"`
	Backup   *url.URL
	Mirrors  []*url.URL
	Regions  map[string]*url.URL
	Admin    gonf.Secret[*url.URL]
	Timeout  time.Duration
	Level    level
	ByLevel  map[level]string
	Optional *url.URL
}

func TestWithConverter(t *testing.T) {
	t.Parallel()
	cfg := load(t, map[string]string{
		"api.backup":        "http://backup",
		"api.mirrors":       "http://m1,http://m2",
		"api.regions.eu":    "http://eu",
		"api.admin":         "http://admin:pw@host",
		"api.timeout":       "30",
		"api.level":         "abc",
		"api.by-level.abcd": "four",
	}, nil, gonf.WithConverter(url.Parse), gonf.WithConverter(seconds), gonf.WithConverter(upperLevel))
	got, err := gonf.Bind[endpoints](cfg, "api")
	if err != nil {
		t.Fatal(err)
	}
	mirrors := make([]string, 0, len(got.Mirrors))
	for _, m := range got.Mirrors {
		mirrors = append(mirrors, m.Host)
	}
	tests := []struct {
		name     string
		got      any
		expected any
	}{
		{name: "pointer from default", got: got.Primary.Host, expected: "localhost"},
		{name: "pointer from value", got: got.Backup.Host, expected: "backup"},
		{name: "slice elements", got: strings.Join(mirrors, ","), expected: "m1,m2"},
		{name: "map values", got: got.Regions["eu"].Host, expected: "eu"},
		{name: "secret", got: got.Admin.Value().User.Username(), expected: "admin"},
		{name: "replaces built-in duration", got: got.Timeout, expected: 30 * time.Second},
		{name: "replaces UnmarshalText", got: got.Level, expected: level(3)},
		{name: "map keys", got: got.ByLevel[4], expected: "four"},
		{name: "pointer stays optional", got: got.Optional == nil, expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.expected {
				t.Errorf("got %v, want %v", tt.got, tt.expected)
			}
		})
	}
}

func TestWithConverter_Errors(t *testing.T) {
	t.Parallel()
	t.Run("invalid value", func(t *testing.T) {
		t.Parallel()
		cfg := load(t, map[string]string{"api.backup": "::", "api.admin": "::pw"}, nil, gonf.WithConverter(url.Parse))
		_, err := gonf.Bind[struct {
			Backup *url.URL
			Admin  gonf.Secret[*url.URL]
		}](cfg, "api")
		expected := `gonf: bind "api": 2 problems:
  api.backup: invalid *url.URL "::": parse "::": missing protocol scheme (from file:app.properties)
  api.admin: invalid *url.URL, value hidden (from file:app.properties)`
		if err == nil || err.Error() != expected {
			t.Errorf("err = %v, want\n%s", err, expected)
		}
	})
	tests := []struct {
		name          string
		opts          []gonf.Option
		expectedError string
	}{
		{
			name:          "two converters for a type",
			opts:          []gonf.Option{gonf.WithConverter(url.Parse), gonf.WithConverter(url.ParseRequestURI)},
			expectedError: "gonf: two converters for *url.URL",
		},
		{
			name:          "nil converter",
			opts:          []gonf.Option{gonf.WithConverter[int](nil)},
			expectedError: "gonf: nil converter for int",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Load(context.Background(), append(tt.opts, gonf.WithSources(memory.New(nil)))...)
			if err == nil || err.Error() != tt.expectedError {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}
