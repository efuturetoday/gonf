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
	URL  string `default:"jdbc://${db.host:localhost}/app"`
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
	if got.URL != "jdbc://localhost/app" || got.Pool.MaxSize != 20 || !slices.Equal(got.Hosts, []string{"a", "b"}) ||
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
			_, err := gonf.Bind[checked](cfg, "x")
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
