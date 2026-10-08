package gonf_test

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/memory"
)

type described struct {
	URL      string `desc:"JDBC-like URL."`
	Password gonf.Secret[string]
	APIToken string        `default:"dev"`
	Timeout  time.Duration `default:"5s" desc:"Wait | then fail."`
	Hosts    []string
	Labels   map[string]string
	Pool     *struct {
		Name    string
		MaxSize int `default:"10"`
	}
	Replicas []struct {
		Host string
	}
	Servers map[string]struct {
		Port *int
	}
	Proxy   *url.URL
	Ignored string `cfg:"-"`
	Self    *described
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	shared := gonf.NewMapping[struct{ URL string }]("db")
	props, err := gonf.Describe(
		gonf.WithSources(memory.New(nil), env.New()),
		gonf.WithMappings(gonf.NewMapping[described]("db"), shared),
		gonf.WithConverter(url.Parse),
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := []gonf.Property{
		{Key: "db.url", Env: "DB_URL", Type: "string", Required: true, Doc: "JDBC-like URL."},
		{Key: "db.password", Env: "DB_PASSWORD", Type: "string", Required: true, Secret: true},
		{Key: "db.api-token", Env: "DB_API_TOKEN", Type: "string", Default: "****", Secret: true},
		{Key: "db.timeout", Env: "DB_TIMEOUT", Type: "time.Duration", Default: "5s", Doc: "Wait | then fail."},
		{Key: "db.hosts", Env: "DB_HOSTS", Type: "[]string"},
		{Key: "db.labels.<name>", Type: "string"},
		{Key: "db.pool.name", Env: "DB_POOL_NAME", Type: "string", Required: true},
		{Key: "db.pool.max-size", Env: "DB_POOL_MAX_SIZE", Type: "int", Default: "10"},
		{Key: "db.replicas[<n>].host", Type: "string", Required: true},
		{Key: "db.servers.<name>.port", Type: "int"},
		{Key: "db.proxy", Env: "DB_PROXY", Type: "*url.URL"},
	}
	if !reflect.DeepEqual(props, expected) {
		t.Errorf("got\n%+v\nwant\n%+v", props, expected)
	}
}

// TestDescribe_RequiredMatchesLoad checks that Describe marks as required
// exactly the keys that Load reports as missing. Every optional struct, list
// element and map entry exists, so their fields count too.
func TestDescribe_RequiredMatchesLoad(t *testing.T) {
	t.Parallel()
	type entry struct {
		Host string
		Port int `default:"1"`
	}
	type target struct {
		Name     string
		Port     *int
		Size     int    `default:"1"`
		Empty    string `default:""`
		Hosts    []string
		Labels   map[string]string
		Token    gonf.Secret[string]
		Proxy    *url.URL
		Optional *string `default:"x"`
		Pool     *entry
		Replicas []entry
		Servers  map[string]*entry
		Nested   struct{ Name string }
	}
	values := map[string]string{"x.pool.port": "2", "x.replicas[0].port": "2", "x.servers.a.port": "2"}
	opts := []gonf.Option{
		gonf.WithMappings(gonf.NewMapping[target]("x")),
		gonf.WithConverter(url.Parse),
	}

	props, err := gonf.Describe(opts...)
	if err != nil {
		t.Fatal(err)
	}
	var described []string
	for _, p := range props {
		if p.Required {
			key := strings.NewReplacer("<n>", "0", "<name>", "a").Replace(p.Key)
			described = append(described, key)
		}
	}

	_, err = gonf.Load(context.Background(), append(opts, gonf.WithSources(memory.New(values)))...)
	var bindErr *gonf.BindError
	if !errors.As(err, &bindErr) {
		t.Fatalf("err = %v, want a *BindError", err)
	}
	var missing []string
	for _, p := range bindErr.Problems {
		if !errors.Is(p.Err, gonf.ErrNotFound) {
			t.Fatalf("unexpected problem %v", p)
		}
		missing = append(missing, p.Key)
	}

	slices.Sort(described)
	slices.Sort(missing)
	if !slices.Equal(described, missing) {
		t.Errorf("Describe marks %v as required, Load misses %v", described, missing)
	}
}

func TestDescribe_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		opts          []gonf.Option
		expectedError string
	}{
		{
			name:          "not a struct",
			opts:          []gonf.Option{gonf.WithMappings(gonf.NewMapping[int]("x"))},
			expectedError: `gonf: bind "x": int is not a struct`,
		},
		{
			name:          "invalid type",
			opts:          []gonf.Option{gonf.WithMappings(gonf.NewMapping[struct{ L []struct{ F func() } }]("x"))},
			expectedError: `gonf: bind "x": x.l[<n>].f: unsupported type func()`,
		},
		{
			name:          "nil mapping",
			opts:          []gonf.Option{gonf.WithMappings(nil)},
			expectedError: "gonf: nil mapping",
		},
		{
			name:          "two converters for a type",
			opts:          []gonf.Option{gonf.WithConverter(url.Parse), gonf.WithConverter(url.Parse)},
			expectedError: "gonf: two converters for *url.URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Describe(tt.opts...)
			if err == nil || err.Error() != tt.expectedError {
				t.Errorf("err = %v, want %q", err, tt.expectedError)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteMarkdown(t *testing.T) {
	t.Parallel()
	props := []gonf.Property{
		{Key: "db.url", Env: "DB_URL", Type: "string", Required: true, Doc: "JDBC-like\nURL."},
		{Key: "db.password", Type: "string", Default: "****", Secret: true},
		{Key: "db.sep", Type: "string", Default: "a|`b`"},
	}
	var b strings.Builder
	if err := gonf.WriteMarkdown(&b, props); err != nil {
		t.Fatal(err)
	}
	expected := "| Key | Environment variable | Type | Default | Description |\n" +
		"| --- | --- | --- | --- | --- |\n" +
		"| `db.url` | `DB_URL` | `string` | required | JDBC-like URL. |\n" +
		"| `db.password` |  | `string` (secret) | `****` |  |\n" +
		"| `db.sep` |  | `string` | `` a\\|`b` `` |  |\n"
	if b.String() != expected {
		t.Errorf("got\n%s\nwant\n%s", b.String(), expected)
	}
	if err := gonf.WriteMarkdown(failingWriter{}, props); err == nil {
		t.Error("err = nil, want the error of the writer")
	}
}
