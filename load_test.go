package gonf_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/memory"
)

type failingSource struct{ name string }

func (s failingSource) Name() string { return s.name }
func (s failingSource) Ordinal() int { return 100 }
func (s failingSource) Load(context.Context) (map[string]gonf.Value, error) {
	return nil, errors.New("boom")
}

// upperSource is a MappedSource with environment-style keys, so tests do
// not need the process environment.
type upperSource map[string]string

func (s upperSource) LoadMapped(ctx context.Context, keys []string) (map[string]gonf.Value, error) {
	values, err := s.Load(ctx)
	return env.Match(values, keys), err
}

func (s upperSource) Name() string { return "test:upper" }
func (s upperSource) Ordinal() int { return 300 }
func (s upperSource) Load(context.Context) (map[string]gonf.Value, error) {
	out := map[string]gonf.Value{}
	for k, v := range s {
		out[k] = gonf.Value{Raw: v}
	}
	return out, nil
}
func (s upperSource) MapKey(key string) string {
	return strings.ToUpper(strings.NewReplacer(".", "_", "%", "_", "-", "_", "[", "_", "]", "_").Replace(key))
}

// topLevel makes the keys a and c known, so that a MappedSource provides
// them.
type topLevel struct {
	A, C string `default:""`
}

// knownDB makes db.url, db.user and db.port known.
type knownDB struct {
	URL, User, Port string `default:""`
}

func withTopLevel() gonf.Option { return gonf.WithMappings(gonf.NewMapping[topLevel]("")) }

func withKnownDB() gonf.Option {
	return gonf.WithMappings(gonf.NewMapping[knownDB]("db", gonf.AllowUnknown()))
}

func TestWithProfiles(t *testing.T) {
	t.Parallel()
	mem := func(name string, ordinal int, values map[string]string) gonf.Source {
		return memory.New(values, source.WithName(name), source.WithOrdinal(ordinal))
	}
	tests := []struct {
		name            string
		sources         []gonf.Source
		profiles        []string
		expectedRaw     string
		expectedProfile string
	}{
		{
			name:        "no profile uses base",
			sources:     []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			expectedRaw: "base",
		},
		{
			name:            "active profile beats base in same source",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			profiles:        []string{"dev"},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name:        "inactive profile is ignored",
			sources:     []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			profiles:    []string{"prod"},
			expectedRaw: "base",
		},
		{
			name:            "last profile wins",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"%dev.a": "dev", "%local.a": "local"})},
			profiles:        []string{"dev", "local"},
			expectedRaw:     "local",
			expectedProfile: "local",
		},
		{
			name:            "duplicate profile counts at its last position",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"%dev.a": "dev", "%local.a": "local"})},
			profiles:        []string{"dev", "local", "dev"},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name: "profile beats later registered base at equal ordinal",
			sources: []gonf.Source{
				mem("test:profile", 100, map[string]string{"%dev.a": "dev"}),
				mem("test:base", 100, map[string]string{"a": "base"}),
			},
			profiles:        []string{"dev"},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name: "higher ordinal base beats profile",
			sources: []gonf.Source{
				mem("test:profile", 100, map[string]string{"%dev.a": "dev"}),
				mem("test:high", 200, map[string]string{"a": "high"}),
			},
			profiles:    []string{"dev"},
			expectedRaw: "high",
		},
		{
			name:            "names are trimmed and empty names ignored",
			sources:         []gonf.Source{mem("test:a", 100, map[string]string{"a": "base", "%dev.a": "dev"})},
			profiles:        []string{" dev ", ""},
			expectedRaw:     "dev",
			expectedProfile: "dev",
		},
		{
			name:            "mapped profile key",
			sources:         []gonf.Source{upperSource{"A": "env-base", "_DEV_A": "env-dev"}},
			profiles:        []string{"dev"},
			expectedRaw:     "env-dev",
			expectedProfile: "dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := gonf.Load(context.Background(), gonf.WithSources(tt.sources...), gonf.WithProfiles(tt.profiles...), withTopLevel())
			if err != nil {
				t.Fatal(err)
			}
			v, ok := cfg.Lookup("a")
			if !ok {
				t.Fatal("Lookup(a): not found")
			}
			if v.Raw != tt.expectedRaw || v.Origin.Profile != tt.expectedProfile {
				t.Errorf("Lookup(a) = %q (profile %q), want %q (profile %q)",
					v.Raw, v.Origin.Profile, tt.expectedRaw, tt.expectedProfile)
			}
		})
	}
}

func TestWithProfileKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		values           map[string]string
		profiles         []string
		expectedProfiles []string
		expectedRaw      string
	}{
		{
			name:             "key activates profiles",
			values:           map[string]string{"app.profile": "dev,local", "%local.a": "local"},
			expectedProfiles: []string{"dev", "local"},
			expectedRaw:      "local",
		},
		{
			name:             "value is expanded",
			values:           map[string]string{"app.profile": "${app.env:dev}", "%dev.a": "dev"},
			expectedProfiles: []string{"dev"},
			expectedRaw:      "dev",
		},
		{
			name:             "with profiles wins",
			values:           map[string]string{"app.profile": "dev", "%dev.a": "dev", "%test.a": "test"},
			profiles:         []string{"test"},
			expectedProfiles: []string{"test"},
			expectedRaw:      "test",
		},
		{
			name:             "empty with profiles does not win",
			values:           map[string]string{"app.profile": "dev", "%dev.a": "dev"},
			profiles:         []string{""},
			expectedProfiles: []string{"dev"},
			expectedRaw:      "dev",
		},
		{
			name:        "missing key activates nothing",
			values:      map[string]string{"a": "base", "%dev.a": "dev"},
			expectedRaw: "base",
		},
		{
			name:        "profile value of the key itself is ignored",
			values:      map[string]string{"a": "base", "%dev.a": "dev", "%dev.app.profile": "dev"},
			expectedRaw: "base",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := gonf.Load(context.Background(),
				gonf.WithSources(memory.New(tt.values)),
				gonf.WithProfiles(tt.profiles...),
				gonf.WithProfileKey("app.profile"),
			)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Profiles(); !slices.Equal(got, tt.expectedProfiles) {
				t.Errorf("Profiles = %v, want %v", got, tt.expectedProfiles)
			}
			if v, _ := cfg.Lookup("a"); v.Raw != tt.expectedRaw {
				t.Errorf("a = %q, want %q", v.Raw, tt.expectedRaw)
			}
		})
	}
}

func TestWithProfileKey_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		value         string
		expectedError string
	}{
		{name: "invalid profile name", value: "a.b", expectedError: `gonf: key "app.profile": invalid profile name "a.b"`},
		{name: "unresolved reference", value: "${nope}", expectedError: `gonf: key "app.profile": unresolved reference "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Load(context.Background(),
				gonf.WithSources(memory.New(map[string]string{"app.profile": tt.value})),
				gonf.WithProfileKey("app.profile"),
			)
			if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

// factoryOf returns a SourceFactory with a Mapping without keys.
func factoryOf(create func(context.Context) ([]gonf.Source, error)) gonf.SourceFactory {
	return gonf.NewFactory(gonf.NewMapping[struct{}]("test.factory"),
		func(ctx context.Context, _ struct{}) ([]gonf.Source, error) { return create(ctx) })
}

type vaultConfig struct {
	Addr string
}

func TestWithFactories(t *testing.T) {
	t.Parallel()
	vault := gonf.NewFactory(gonf.NewMapping[vaultConfig]("vault"),
		func(_ context.Context, v vaultConfig) ([]gonf.Source, error) {
			return []gonf.Source{memory.New(map[string]string{
				"db.password":      "secret-from-" + v.Addr,
				"%dev.db.password": "dev-secret",
				"app.profile":      "prod",
			}, source.WithName("vault:"+v.Addr), source.WithOrdinal(100))}, nil
		})
	cfg, err := gonf.Load(context.Background(),
		gonf.WithFactories(vault),
		gonf.WithSources(memory.New(map[string]string{
			"vault.addr":  "${net.host}:8200",
			"net.host":    "localhost",
			"db.password": "from-file",
			"db.user":     "app",
		}, source.WithName("file:app"), source.WithOrdinal(100))),
		gonf.WithProfileKey("app.profile"),
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		key            string
		expectedRaw    string
		expectedSource string
	}{
		{name: "factory source wins tie against earlier source", key: "db.password", expectedRaw: "secret-from-localhost:8200", expectedSource: "vault:localhost:8200"},
		{name: "first phase values remain", key: "db.user", expectedRaw: "app", expectedSource: "file:app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := cfg.Lookup(tt.key)
			if !ok || v.Raw != tt.expectedRaw || v.Origin.Source != tt.expectedSource {
				t.Errorf("Lookup(%q) = %+v, want %q from %q", tt.key, v, tt.expectedRaw, tt.expectedSource)
			}
		})
	}
	t.Run("profile key from factory source is ignored", func(t *testing.T) {
		if got := cfg.Profiles(); len(got) != 0 {
			t.Errorf("Profiles = %v, want none", got)
		}
	})
	t.Run("factory Mapping is registered", func(t *testing.T) {
		e := cfg.Explain("vault.addr")
		if e.Property == nil || e.Value != "localhost:8200" {
			t.Errorf("Explain(vault.addr) = %+v, want a Property and localhost:8200", e)
		}
	})
}

type configmapConfig struct {
	Name string
}

func TestWithFactories_SeesProfiles(t *testing.T) {
	t.Parallel()
	var seen string
	factory := gonf.NewFactory(gonf.NewMapping[configmapConfig]("configmap"),
		func(_ context.Context, v configmapConfig) ([]gonf.Source, error) {
			seen = v.Name
			return nil, nil
		})
	tests := []struct {
		name     string
		opts     []gonf.Option
		expected string
	}{
		{
			name: "profile value",
			opts: []gonf.Option{gonf.WithSources(memory.New(map[string]string{
				"configmap.name": "app", "%dev.configmap.name": "app-dev",
			}))},
			expected: "app-dev",
		},
		{
			name: "profile source",
			opts: []gonf.Option{
				gonf.WithSources(memory.New(map[string]string{"configmap.name": "app"})),
				gonf.WithProfileSources(func(profile string) []gonf.Source {
					return []gonf.Source{memory.New(map[string]string{"%" + profile + ".configmap.name": "app-" + profile},
						source.WithName("test:"+profile))}
				}),
			},
			expected: "app-dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen = ""
			_, err := gonf.Load(context.Background(),
				append(tt.opts, gonf.WithProfiles("dev"), gonf.WithFactories(factory))...)
			if err != nil {
				t.Fatal(err)
			}
			if seen != tt.expected {
				t.Errorf("factory saw configmap.name %q, want %q", seen, tt.expected)
			}
		})
	}
}

func TestWithProfileSources(t *testing.T) {
	t.Parallel()
	var calls []string
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"a": "base"})),
		gonf.WithProfiles("dev", "local"),
		gonf.WithProfileSources(func(profile string) []gonf.Source {
			calls = append(calls, profile)
			return []gonf.Source{memory.New(map[string]string{"%" + profile + ".a": profile},
				source.WithName("test:"+profile))}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"dev", "local"}) {
		t.Errorf("called for %v, want [dev local]", calls)
	}
	if v, _ := cfg.Get("a"); v != "local" {
		t.Errorf("a = %q, want local, the last profile", v)
	}
}

func TestWithFactories_FactoryKeys(t *testing.T) {
	t.Parallel()
	first := factoryOf(func(context.Context) ([]gonf.Source, error) {
		return []gonf.Source{memory.New(map[string]string{
			"vault.addr":        "other",
			"%dev.vault.addr":   "other-dev",
			"vaulted.unrelated": "x",
		}, source.WithName("test:first"))}, nil
	})
	vault := gonf.NewFactory(gonf.NewMapping[vaultConfig]("vault"),
		func(context.Context, vaultConfig) ([]gonf.Source, error) { return nil, nil })
	_, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"vault.addr": "localhost"})),
		gonf.WithFactories(first, vault))
	var se *gonf.SourceError
	if !errors.As(err, &se) || se.Source != "test:first" {
		t.Fatalf("err = %v, want a SourceError for test:first", err)
	}
	for _, want := range []string{`sets "vault.addr", a key of a source factory`, `sets "vault.addr"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "vaulted") {
		t.Errorf("err = %v, names a key outside the prefix", err)
	}
	if n := strings.Count(err.Error(), "sets "); n != 2 {
		t.Errorf("err = %v, want 2 keys, also the profile key", err)
	}
}

func TestNewFactory_Panics(t *testing.T) {
	t.Parallel()
	create := func(context.Context, vaultConfig) ([]gonf.Source, error) { return nil, nil }
	tests := []struct {
		name     string
		call     func()
		expected string
	}{
		{name: "nil mapping", call: func() { gonf.NewFactory(nil, create) }, expected: "needs a Mapping"},
		{name: "no prefix", call: func() { gonf.NewFactory(gonf.NewMapping[vaultConfig](""), create) }, expected: "needs a prefix (load_test.go:"},
		{name: "nil create", call: func() { gonf.NewFactory(gonf.NewMapping[vaultConfig]("vault"), nil) }, expected: "needs a create function"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), tt.expected) {
					t.Errorf("recovered %v, want a panic with %q", r, tt.expected)
				}
			}()
			tt.call()
		})
	}
}

func TestWithFactories_Errors(t *testing.T) {
	t.Parallel()
	failing := factoryOf(func(context.Context) ([]gonf.Source, error) {
		return nil, errors.New("vault: unreachable")
	})
	badSource := factoryOf(func(context.Context) ([]gonf.Source, error) {
		return []gonf.Source{failingSource{"test:remote"}}, nil
	})

	t.Run("all factory errors are collected", func(t *testing.T) {
		t.Parallel()
		_, err := gonf.Load(context.Background(), gonf.WithFactories(failing, badSource))
		if err == nil {
			t.Fatal("expected error")
		}
		var se *gonf.SourceError
		if !strings.Contains(err.Error(), "gonf: source factory: vault: unreachable") || !errors.As(err, &se) || se.Source != "test:remote" {
			t.Errorf("err = %v, want factory error and SourceError for test:remote", err)
		}
	})
	t.Run("first phase error stops factories", func(t *testing.T) {
		t.Parallel()
		var isCalled bool
		factory := factoryOf(func(context.Context) ([]gonf.Source, error) {
			isCalled = true
			return nil, nil
		})
		_, err := gonf.Load(context.Background(),
			gonf.WithSources(failingSource{"test:broken"}),
			gonf.WithFactories(factory),
		)
		if err == nil || isCalled {
			t.Errorf("err = %v, factory called = %v; want error and no call", err, isCalled)
		}
	})
	t.Run("invalid factory configuration stops factories", func(t *testing.T) {
		t.Parallel()
		var isCalled bool
		factory := gonf.NewFactory(gonf.NewMapping[portConfig]("app"),
			func(context.Context, portConfig) ([]gonf.Source, error) {
				isCalled = true
				return nil, nil
			})
		_, err := gonf.Load(context.Background(),
			gonf.WithSources(memory.New(map[string]string{"app.port": "eighty"})),
			gonf.WithFactories(factory),
		)
		var be *gonf.BindError
		if !errors.As(err, &be) || be.Prefix != "app" || isCalled {
			t.Errorf("err = %v, factory called = %v; want a BindError for app and no call", err, isCalled)
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := gonf.Load(ctx, gonf.WithFactories(badSource))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestWithFactories_Closes(t *testing.T) {
	t.Parallel()
	own := newLiveSource(map[string]string{"app.port": "80"})
	factory, created := sourceFactory()
	app := gonf.NewMapping[portConfig]("app")

	if _, err := gonf.Load(context.Background(), gonf.WithSources(own), gonf.WithFactories(factory), gonf.WithMappings(app)); err != nil {
		t.Fatal(err)
	}
	own.values["app.port"] = "eighty"
	if _, err := gonf.Load(context.Background(), gonf.WithSources(own), gonf.WithFactories(factory), gonf.WithMappings(app)); err == nil {
		t.Fatal("Load succeeded, want a BindError")
	}

	for i, src := range created() {
		if n := src.closed(); n != 1 {
			t.Errorf("Source %d of the factory closed %d times, want 1", i, n)
		}
	}
	if n := own.closed(); n != 0 {
		t.Errorf("a Source of WithSources was closed %d times", n)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(
		memory.New(map[string]string{"a": "high"}, source.WithName("test:high"), source.WithOrdinal(200)),
		memory.Defaults(map[string]string{"a": "default", "b": "only-default"}),
	))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		key             string
		expectedRaw     string
		expectedSource  string
		expectedOrdinal int
	}{
		{name: "higher ordinal wins", key: "a", expectedRaw: "high", expectedSource: "test:high", expectedOrdinal: 200},
		{name: "single source", key: "b", expectedRaw: "only-default", expectedSource: "memory:defaults", expectedOrdinal: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, ok := cfg.Lookup(tt.key)
			if !ok {
				t.Fatalf("Lookup(%q): not found", tt.key)
			}
			if v.Raw != tt.expectedRaw || v.Origin.Source != tt.expectedSource || v.Origin.Ordinal != tt.expectedOrdinal {
				t.Errorf("Lookup(%q) = %+v, want raw=%q source=%q ordinal=%d",
					tt.key, v, tt.expectedRaw, tt.expectedSource, tt.expectedOrdinal)
			}
		})
	}
}

func TestLoad_EqualOrdinalLaterRegisteredWins(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"a": "first"}, source.WithName("test:first"))),
		gonf.WithSources(memory.New(map[string]string{"a": "second"}, source.WithName("test:second"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, v := range cfg.Candidates("a") {
		names = append(names, v.Origin.Source)
	}
	if expected := []string{"test:second", "test:first"}; !slices.Equal(names, expected) {
		t.Errorf("Candidates order = %v, want %v", names, expected)
	}
}

func TestLoad_CollectsAllSourceErrors(t *testing.T) {
	t.Parallel()
	_, err := gonf.Load(context.Background(), gonf.WithSources(
		failingSource{"test:one"},
		memory.New(map[string]string{"a": "ok"}),
		failingSource{"test:two"},
	))
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("err = %v, want joined errors", err)
	}
	var names []string
	for _, e := range joined.Unwrap() {
		var se *gonf.SourceError
		if !errors.As(e, &se) {
			t.Fatalf("error %v is not a SourceError", e)
		}
		names = append(names, se.Source)
	}
	slices.Sort(names)
	if expected := []string{"test:one", "test:two"}; !slices.Equal(names, expected) {
		t.Errorf("failed sources = %v, want %v", names, expected)
	}
}

func TestLoad_CanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := gonf.Load(ctx, gonf.WithSources(memory.New(nil)))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestLoad_InvalidSourceName(t *testing.T) {
	t.Parallel()
	_, err := gonf.Load(context.Background(), gonf.WithSources(
		memory.New(nil, source.WithName("no-scheme")),
	))
	if !errors.Is(err, gonf.ErrInvalidSourceName) {
		t.Fatalf("err = %v, want ErrInvalidSourceName", err)
	}
	var se *gonf.SourceError
	if !errors.As(err, &se) || se.Source != "no-scheme" {
		t.Errorf("err = %v, want SourceError for %q", err, "no-scheme")
	}
}

func TestLoad_Nil(t *testing.T) {
	t.Parallel()
	nilSource := factoryOf(func(context.Context) ([]gonf.Source, error) {
		return []gonf.Source{nil}, nil
	})
	tests := []struct {
		name          string
		opts          []gonf.Option
		expectedError string
	}{
		{name: "nil source", opts: []gonf.Option{gonf.WithSources(nil)}, expectedError: "gonf: nil source"},
		{name: "nil source from factory", opts: []gonf.Option{gonf.WithFactories(nilSource)}, expectedError: "gonf: nil source"},
		{name: "nil factory", opts: []gonf.Option{gonf.WithFactories(nil)}, expectedError: "gonf: nil source factory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Load(context.Background(), tt.opts...)
			if err == nil || err.Error() != tt.expectedError {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

func TestLoad_InvalidProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		profiles      []string
		key           string
		expectedError string
	}{
		{name: "profile name with dot", profiles: []string{"a.b"}, expectedError: `invalid profile name "a.b"`},
		{name: "profile name with comma", profiles: []string{"a,b"}, expectedError: `invalid profile name "a,b"`},
		{name: "profile name with inner space", profiles: []string{"de v"}, expectedError: `invalid profile name "de v"`},
		{name: "profile key with space", key: "% dev.a", expectedError: `invalid profile key "% dev.a"`},
		{name: "profile key without dot", key: "%dev", expectedError: `invalid profile key "%dev"`},
		{name: "profile key without name", key: "%.a", expectedError: `invalid profile key "%.a"`},
		{name: "profile key without base", key: "%dev.", expectedError: `invalid profile key "%dev."`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{}
			if tt.key != "" {
				values[tt.key] = "x"
			}
			_, err := gonf.Load(context.Background(),
				gonf.WithSources(memory.New(values)),
				gonf.WithProfiles(tt.profiles...),
			)
			if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

func TestSourceError(t *testing.T) {
	t.Parallel()
	inner := errors.New("boom")
	err := &gonf.SourceError{Source: "file:x", Err: inner}
	if got, expected := err.Error(), `gonf: source "file:x": boom`; got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
	if !errors.Is(err, inner) {
		t.Error("errors.Is does not reach the wrapped error")
	}
}

// A verbatim Source gives its values as written: they call no Function and
// refer to no key, also when another key refers to them.
func TestLoad_Verbatim(t *testing.T) {
	t.Parallel()
	called := false
	spy := gonf.Function{Name: "spy", Call: func(*gonf.Config, string, gonf.Origin) (string, error) {
		called = true
		return "read", nil
	}}
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(
			memory.New(map[string]string{"host": "db", "url": "postgres://${host}"}),
			memory.New(map[string]string{"hook": "https://x/?t=${spy::token}", "note": "${host} $${x}"},
				source.Verbatim(), source.WithName("memory:shared"), source.WithOrdinal(600)),
			memory.New(map[string]string{"copy": "${hook}"}, source.WithName("memory:copy")),
		),
		gonf.WithFunctions(spy),
	)
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"hook": "https://x/?t=${spy::token}",
		"note": "${host} $${x}",
		"copy": "https://x/?t=${spy::token}",
		"url":  "postgres://db",
	} {
		if got, err := cfg.Get(key); err != nil || got != expected {
			t.Errorf("Get(%q) = %q, %v, want %q", key, got, err, expected)
		}
	}
	if called {
		t.Error("a verbatim value called a Function")
	}
	if v, _ := cfg.Lookup("hook"); v.Raw != "https://x/?t=$${spy::token}" {
		t.Errorf("Lookup(hook).Raw = %q, want it escaped", v.Raw)
	}
}
