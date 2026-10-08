package gonf_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

type dbConfig struct {
	URL  string `default:"postgres://${db.host:localhost}/app"`
	Pool struct {
		MaxSize int `default:"10"`
	}
	Hosts    []string `default:"a,b"`
	Optional *struct {
		Size int `default:"1"`
	}
	Retries *int `default:"3"`
}

type cacheConfig struct {
	TTL string `default:"1m"`
}

// dbExtra shares the prefix "db" with dbConfig.
type dbExtra struct {
	Extra string `default:"x"`
}

func TestMapping_Get(t *testing.T) {
	t.Parallel()
	db := gonf.NewMapping[dbConfig]("db")
	if db.Prefix() != "db" {
		t.Errorf("Prefix = %q", db.Prefix())
	}
	cfg := load(t, map[string]string{"db.pool.max-size": "20"}, nil, gonf.WithMappings(db))
	got := db.Get(cfg)
	if got.URL != "postgres://localhost/app" || got.Pool.MaxSize != 20 || !slices.Equal(got.Hosts, []string{"a", "b"}) ||
		got.Optional != nil || got.Retries == nil || *got.Retries != 3 {
		t.Errorf("Get = %+v", got)
	}
	if again := db.Get(cfg); !reflect.DeepEqual(again, got) {
		t.Errorf("second Get = %+v, want %+v", again, got)
	}
	if _, err := cfg.Get("db.retries"); !errors.Is(err, gonf.ErrNotFound) {
		t.Errorf("Config.Get of a key with only a default tag: err = %v, want ErrNotFound", err)
	}
}

// copied has every kind of reference that Mapping.Get copies.
type copied struct {
	Hosts   []string
	Labels  map[string]string
	Port    *int
	Servers []struct{ Tags []string }
	Zones   map[string][]string
	Next    *copiedNode
	Plain   struct{ Name string }
}

type copiedNode struct {
	Name string
	Next *copiedNode
}

func TestMapping_Get_Copies(t *testing.T) {
	t.Parallel()
	m := gonf.NewMapping[copied]("x")
	cfg := load(t, map[string]string{
		"x.hosts":           "a,b",
		"x.labels.env":      "prod",
		"x.port":            "80",
		"x.servers[0].tags": "t1,t2",
		"x.zones.eu":        "z1,z2",
		"x.next.name":       "n1",
		"x.next.next.name":  "n2",
		"x.plain.name":      "p",
	}, nil, gonf.WithMappings(m))
	// bind fills a new value, which shares nothing with the Mapping.
	expected, err := bind[copied](cfg, "x")
	if err != nil {
		t.Fatal(err)
	}

	got := m.Get(cfg)
	got.Hosts[0] = "changed"
	got.Labels["env"] = "changed"
	*got.Port = 1
	got.Servers[0].Tags[0] = "changed"
	got.Zones["eu"][0] = "changed"
	got.Next.Name = "changed"
	got.Next.Next.Name = "changed"
	got.Plain.Name = "changed"

	if again := m.Get(cfg); !reflect.DeepEqual(again, expected) {
		t.Errorf("Get after modifying a copy = %+v, want %+v", again, expected)
	}
}

func TestMapping_SecretInExplain(t *testing.T) {
	t.Parallel()
	// A Secret field whose key does not sound secret is masked too.
	type api struct {
		Key gonf.Secret[string]
	}
	cfg := load(t, map[string]string{"api.key": "abc123"}, nil, gonf.WithMappings(gonf.NewMapping[api]("api")))
	if got, expected := cfg.Explain("api.key").String(), "api.key = **** (file:app.properties)"; got != expected {
		t.Errorf("Explain = %q, want %q", got, expected)
	}
}

func TestMapping_Get_NoCopyWithoutReferences(t *testing.T) {
	// Not parallel: AllocsPerRun changes GOMAXPROCS.
	m := gonf.NewMapping[benchDB]("db")
	cfg := load(t, benchValues, nil, gonf.WithMappings(m))
	if n := testing.AllocsPerRun(100, func() { _ = m.Get(cfg) }); n != 0 {
		t.Errorf("Get allocates %v times, want 0", n)
	}
}

func TestMapping_Get_NotRegistered(t *testing.T) {
	t.Parallel()
	cfg := load(t, nil, nil, gonf.WithMappings(gonf.NewMapping[cacheConfig]("cache")))
	defer func() {
		r := recover()
		if s, ok := r.(string); !ok || !strings.Contains(s, `mapping "db" not registered`) {
			t.Errorf("recovered %v, want a panic naming the mapping", r)
		}
	}()
	gonf.NewMapping[dbConfig]("db").Get(cfg)
}

func TestWithMappings(t *testing.T) {
	t.Parallel()
	db := gonf.NewMapping[dbConfig]("db")
	extra := gonf.NewMapping[dbExtra]("db")
	cache := gonf.NewMapping[cacheConfig]("cache")
	cfg := load(t, map[string]string{"db.extra": "y", "cache.ttl": "5m"}, nil, gonf.WithMappings(db, extra, cache, db))
	if got := extra.Get(cfg).Extra; got != "y" {
		t.Errorf("extra = %q, want y", got)
	}
	if got := cache.Get(cfg).TTL; got != "5m" {
		t.Errorf("ttl = %q, want 5m", got)
	}
	if got := db.Get(cfg).Pool.MaxSize; got != 10 {
		t.Errorf("max-size = %d, want 10", got)
	}
}

func TestWithMappings_Errors(t *testing.T) {
	t.Parallel()
	type required struct{ N int }
	tests := []struct {
		name          string
		values        map[string]string
		mappings      []gonf.AnyMapping
		expectedError string
	}{
		{
			name:   "problems of all mappings",
			values: map[string]string{"db.pool.max-size": "ten", "db.typo": "x"},
			mappings: []gonf.AnyMapping{
				gonf.NewMapping[dbConfig]("db"),
				gonf.NewMapping[required]("req"),
			},
			expectedError: `gonf: bind "db": 2 problems:
  db.pool.max-size: invalid int "ten" (from file:app.properties)
  db.typo: unknown key (from file:app.properties)
gonf: bind "req": req.n: required, no value`,
		},
		{
			name:          "allow unknown",
			values:        map[string]string{"req.n": "1", "req.other": "x"},
			mappings:      []gonf.AnyMapping{gonf.NewMapping[required]("req", gonf.AllowUnknown())},
			expectedError: "",
		},
		{
			name:          "nil and not a struct",
			mappings:      []gonf.AnyMapping{nil, gonf.NewMapping[int]("n")},
			expectedError: "gonf: nil mapping\ngonf: bind \"n\": int is not a struct",
		},
		{
			name:   "secret of another mapping",
			values: map[string]string{"db.password": "s3cret", "app.dsn": "postgres://u:${db.password}@db/app"},
			mappings: []gonf.AnyMapping{
				gonf.NewMapping[struct{ DSN string }]("app"),
				gonf.NewMapping[struct{ Password gonf.Secret[string] }]("db"),
			},
			expectedError: `gonf: bind "app": app.dsn: refers to the secret db.password, make the field a gonf.Secret (from file:app.properties)`,
		},
		{
			name:   "invalid mapping keeps its keys known",
			values: map[string]string{"db.extra": "x", "db.broken": "1"},
			mappings: []gonf.AnyMapping{
				gonf.NewMapping[dbExtra]("db"),
				gonf.NewMapping[struct{ Broken func() }]("db"),
			},
			expectedError: `gonf: bind "db": db.broken: unsupported type func()`,
		},
		{
			name:          "embedded pointer",
			mappings:      []gonf.AnyMapping{gonf.NewMapping[struct{ *cacheConfig }]("e")},
			expectedError: `gonf: bind "e": e.cache-config: embedded pointer *gonf_test.cacheConfig not supported, embed the struct`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := memory.New(tt.values, source.WithName("file:app.properties"), source.WithOrdinal(100))
			_, err := gonf.Load(context.Background(), gonf.WithSources(src), gonf.WithMappings(tt.mappings...))
			if tt.expectedError == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() != tt.expectedError {
				t.Errorf("error\n%v\nwant\n%s", err, tt.expectedError)
			}
		})
	}
}

// checked records what the validator saw.
type checked struct {
	Name string
}

func (c *checked) Validate() error {
	if c.Name == "self" {
		return errors.New("rejected by Validate")
	}
	return nil
}

func TestWithValidator(t *testing.T) {
	t.Parallel()
	validator := gonf.WithValidator(func(v any) error {
		c, ok := v.(*checked)
		if !ok {
			return fmt.Errorf("got %T, want *checked", v)
		}
		if c.Name == "external" {
			return errors.New("rejected by validator")
		}
		return nil
	})
	tests := []struct {
		name          string
		value         string
		expectedError string
	}{
		{name: "valid", value: "ok"},
		{name: "rejected", value: "external", expectedError: `gonf: bind "x": x: rejected by validator`},
		{name: "not called after Validate fails", value: "self", expectedError: `gonf: bind "x": x: rejected by Validate`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := load(t, map[string]string{"x.name": tt.value}, nil, validator)
			_, err := bind[checked](cfg, "x")
			if tt.expectedError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tt.expectedError {
				t.Errorf("Bind error = %v, want %q", err, tt.expectedError)
			}
			_, err = gonf.Load(context.Background(), validator,
				gonf.WithSources(memory.New(map[string]string{"x.name": tt.value})),
				gonf.WithMappings(gonf.NewMapping[checked]("x")))
			if (err == nil) != (tt.expectedError == "") || err != nil && err.Error() != tt.expectedError {
				t.Errorf("Load error = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

func BenchmarkMapping_Get(b *testing.B) {
	b.Run("no references", func(b *testing.B) {
		benchmarkGet(b, gonf.NewMapping[benchDB]("db"), benchValues)
	})
	b.Run("slice and map", func(b *testing.B) {
		type hosts struct {
			Hosts  []string
			Labels map[string]string
		}
		benchmarkGet(b, gonf.NewMapping[hosts]("x"), map[string]string{"x.hosts": "a,b,c", "x.labels.env": "prod"})
	})
}

func benchmarkGet[T any](b *testing.B, m *gonf.Mapping[T], values map[string]string) {
	b.Helper()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(values)), gonf.WithMappings(m))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = m.Get(cfg)
		}
	})
}
