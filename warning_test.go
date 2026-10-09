package gonf_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/format/properties"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/file"
	"github.com/efuturetoday/gonf/source/memory"
)

type warnDB struct {
	URL  string `default:"x"`
	Port int    `default:"5432"`
}

// Not parallel: t.Setenv modifies the process environment.
func TestConfig_Warnings_UnknownName(t *testing.T) {
	tests := []struct {
		name     string
		environ  map[string]string
		expected []gonf.Warning
	}{
		{name: "no variable, no warning"},
		{name: "known and foreign names are fine", environ: map[string]string{"WDB_URL": "u", "WDB_PORT": "1", "WOTHER_X": "1"}},
		{
			name:    "service link variable",
			environ: map[string]string{"WDB_SERVICE_HOST": "10.0.0.1"},
			expected: []gonf.Warning{
				{Kind: gonf.WarnUnknownName, Source: "env:", Name: "WDB_SERVICE_HOST", Message: "WDB_SERVICE_HOST matches no key"},
			},
		},
		{
			name:    "typo gets a suggestion, sorted by name",
			environ: map[string]string{"WDB_PROT": "1", "WDB_ABC_DEF_GHI": "1"},
			expected: []gonf.Warning{
				{Kind: gonf.WarnUnknownName, Source: "env:", Name: "WDB_ABC_DEF_GHI", Message: "WDB_ABC_DEF_GHI matches no key"},
				{Kind: gonf.WarnUnknownName, Source: "env:", Name: "WDB_PROT", Message: "WDB_PROT matches no key, did you mean WDB_PORT?"},
			},
		},
		{
			name:    "profile name keeps its profile in the suggestion",
			environ: map[string]string{"_DEV_WDB_UR": "1"},
			expected: []gonf.Warning{
				{Kind: gonf.WarnUnknownName, Source: "env:", Name: "_DEV_WDB_UR", Message: "_DEV_WDB_UR matches no key, did you mean _DEV_WDB_URL?"},
			},
		},
		{name: "profile name of a known key is fine", environ: map[string]string{"_DEV_WDB_URL": "u"}},
		{name: "name without the underscore is not under the prefix", environ: map[string]string{"WDBX": "1", "WDB": "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.environ {
				t.Setenv(k, v)
			}
			cfg, err := gonf.Load(context.Background(),
				gonf.WithSources(env.New()),
				gonf.WithMappings(gonf.NewMapping[warnDB]("wdb")),
			)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Warnings(); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Warnings() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

type warnConflict struct {
	MaxSize int `cfg:"max-size" default:"1"`
	Max     struct {
		Size int `default:"2"`
	}
}

func TestConfig_Warnings_NameConflict(t *testing.T) {
	t.Parallel()
	m := gonf.NewMapping[warnConflict]("wc")
	tests := []struct {
		name     string
		sources  []gonf.Source
		expected []gonf.Warning
	}{
		{name: "no env source, no names, no conflict", sources: []gonf.Source{memory.New(nil)}},
		{
			name:    "env source",
			sources: []gonf.Source{memory.New(nil), env.New()},
			expected: []gonf.Warning{{
				Kind: gonf.WarnNameConflict, Source: "env:", Name: "WC_MAX_SIZE", Keys: []string{"wc.max-size", "wc.max.size"},
				Message: "keys wc.max-size, wc.max.size are all WC_MAX_SIZE, a value cannot tell which one it is for",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := gonf.Load(context.Background(), gonf.WithSources(tt.sources...), gonf.WithMappings(m))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Warnings(); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Warnings() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestDescribe_NameConflict(t *testing.T) {
	t.Parallel()
	m := gonf.NewMapping[warnConflict]("wc")
	_, err := gonf.Describe(gonf.WithSources(env.New()), gonf.WithMappings(m))
	if !errors.Is(err, gonf.ErrNameConflict) {
		t.Fatalf("err = %v, want ErrNameConflict", err)
	}
	if want := "gonf: keys with the same name: wc.max-size, wc.max.size are all WC_MAX_SIZE"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	// Without a Source that names keys there is nothing to conflict.
	if _, err := gonf.Describe(gonf.WithMappings(m)); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestConfig_Sources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "app.properties")
	if err := os.WriteFile(base, []byte("wdb.url=u\nwdb.port=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dev := filepath.Join(dir, "app-dev.properties")
	cfg, err := gonf.Load(context.Background(),
		gonf.WithProfiles("dev"),
		gonf.WithSources(
			memory.Defaults(map[string]string{"wdb.url": "d"}),
			file.New(base, properties.Format),
			file.New(dev, properties.Format, source.Optional(), source.WithProfile("dev")),
			memory.New(map[string]string{"wdb.port": "2"}),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := []gonf.SourceInfo{
		{Name: "memory:", Ordinal: 500, Keys: 1},
		{Name: "file:" + dev, Ordinal: file.Ordinal, Profile: "dev", IsOptional: true, IsMissing: true},
		{Name: "file:" + base, Ordinal: file.Ordinal, Keys: 2},
		{Name: "memory:defaults", Ordinal: 0, Keys: 1},
	}
	if got := cfg.Sources(); !reflect.DeepEqual(got, expected) {
		t.Errorf("Sources() = %+v, want %+v", got, expected)
	}
}

func TestConfig_Sources_OptionalPresent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "app-dev.properties")
	if err := os.WriteFile(path, []byte("wdb.url=u\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(
		file.New(path, properties.Format, source.Optional(), source.WithProfile("dev")),
	))
	if err != nil {
		t.Fatal(err)
	}
	expected := []gonf.SourceInfo{{Name: "file:" + path, Ordinal: file.Ordinal, Profile: "dev", Keys: 1, IsOptional: true}}
	if got := cfg.Sources(); !reflect.DeepEqual(got, expected) {
		t.Errorf("Sources() = %+v, want %+v", got, expected)
	}
}
