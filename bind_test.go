package gonf_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

// load returns a Config with values from a file-like Source at ordinal 100
// and, if given, environment-style values at ordinal 300.
func load(t *testing.T, values map[string]string, env upperSource, opts ...gonf.Option) *gonf.Config {
	t.Helper()
	srcs := []gonf.Source{memory.New(values, source.WithName("file:app.properties"), source.WithOrdinal(100))}
	if env != nil {
		srcs = append(srcs, env)
	}
	cfg, err := gonf.Load(context.Background(), append(opts, gonf.WithSources(srcs...))...)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type Common struct {
	Timeout time.Duration `default:"5s"`
}

type pool struct {
	MaxSize int `default:"10"`
}

type DB struct {
	Common
	URL       string
	HTTPPort  uint16
	Ratio     float64
	IsEnabled bool `cfg:"enabled"`
	Addr      netip.Addr
	User      string `default:"${app.name}-user"`
	Comment   string `default:""`
	Pool      pool
	Optional  *pool
	Port      *int
	Retries   *int   `default:"3"`
	Skipped   string `cfg:"-"`
	ignored   string //nolint:unused // must be skipped by Bind
}

func TestBind(t *testing.T) {
	t.Parallel()
	cfg := load(t, map[string]string{
		"app.name":     "shop",
		"db.url":       "postgres://localhost/app",
		"db.http-port": "8080",
		"db.ratio":     "0.5",
		"db.enabled":   "ON",
		"db.addr":      "10.0.0.1",
		"db.timeout":   "1m30s",
		"db.skipped":   "nope",
		"db.ignored":   "nope",
	}, nil)
	got, err := bind[DB](cfg, "db", gonf.AllowUnknown())
	if err != nil {
		t.Fatal(err)
	}
	retries := 3
	expected := DB{
		Common:    Common{Timeout: 90 * time.Second},
		URL:       "postgres://localhost/app",
		HTTPPort:  8080,
		Ratio:     0.5,
		IsEnabled: true,
		Addr:      netip.MustParseAddr("10.0.0.1"),
		User:      "shop-user",
		Pool:      pool{MaxSize: 10},
		Retries:   &retries,
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Bind = %+v, want %+v", got, expected)
	}
}

func TestBind_Prefix(t *testing.T) {
	t.Parallel()
	type root struct {
		Name string
		DB   struct{ URL string } `cfg:"db"`
	}
	cfg := load(t, map[string]string{"name": "shop", "db.url": "u", "deep.name": "deep", "deep.db.url": "deep-u"}, nil)
	tests := []struct {
		name     string
		prefix   string
		expected string
	}{
		{name: "root", prefix: "", expected: "shop u"},
		{name: "nested", prefix: "deep", expected: "deep deep-u"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := bind[root](cfg, tt.prefix)
			if err != nil {
				t.Fatal(err)
			}
			if s := got.Name + " " + got.DB.URL; s != tt.expected {
				t.Errorf("got %q, want %q", s, tt.expected)
			}
		})
	}
}

func TestBind_Bool(t *testing.T) {
	t.Parallel()
	type flags struct{ On bool }
	tests := []struct {
		value    string
		expected bool
	}{
		{"true", true}, {"TRUE", true}, {"1", true}, {"yes", true}, {"On", true},
		{"false", false}, {"0", false}, {"No", false}, {"off", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			got, err := bind[flags](load(t, map[string]string{"x.on": tt.value}, nil), "x")
			if err != nil {
				t.Fatal(err)
			}
			if got.On != tt.expected {
				t.Errorf("On = %v, want %v", got.On, tt.expected)
			}
		})
	}
}

func TestBind_Integers(t *testing.T) {
	t.Parallel()
	type numbers struct {
		N int
		U uint32
	}
	tests := []struct {
		name          string
		value         string
		expectedN     int
		expectedU     uint32
		expectedError string
	}{
		{name: "decimal", value: "99", expectedN: 99, expectedU: 99},
		{name: "zero", value: "0", expectedN: 0, expectedU: 0},
		{name: "octal prefix", value: "0o644", expectedN: 0o644, expectedU: 0o644},
		{name: "hex prefix", value: "0x1F", expectedN: 31, expectedU: 31},
		{name: "binary prefix", value: "0b101", expectedN: 5, expectedU: 5},
		{name: "underscores", value: "1_000", expectedN: 1000, expectedU: 1000},
		{
			name:  "leading zero",
			value: "0644",
			expectedError: `gonf: bind "x": 2 problems:
  x.n: ambiguous leading zero in "0644", write 0o644 for octal or 644 for decimal (from file:app.properties)
  x.u: ambiguous leading zero in "0644", write 0o644 for octal or 644 for decimal (from file:app.properties)`,
		},
		{
			name:  "only zeros",
			value: "00",
			expectedError: `gonf: bind "x": 2 problems:
  x.n: ambiguous leading zero in "00", write 0o0 for octal or 0 for decimal (from file:app.properties)
  x.u: ambiguous leading zero in "00", write 0o0 for octal or 0 for decimal (from file:app.properties)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := load(t, map[string]string{"x.n": tt.value, "x.u": tt.value}, nil)
			got, err := bind[numbers](cfg, "x")
			if tt.expectedError != "" {
				if err == nil || err.Error() != tt.expectedError {
					t.Fatalf("err = %v, want\n%s", err, tt.expectedError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.N != tt.expectedN || got.U != tt.expectedU {
				t.Errorf("got %+v, want N=%d U=%d", got, tt.expectedN, tt.expectedU)
			}
		})
	}
}

func TestBind_Names(t *testing.T) {
	t.Parallel()
	type names struct {
		MaxSize   int
		HTTPPort  int
		URL       int
		V2Config  int
		Port2     int
		IPV6Addr  int
		Renamed   int `cfg:"other"`
		EmptyName int `cfg:""`
	}
	cfg := load(t, map[string]string{
		"max-size": "1", "http-port": "2", "url": "3", "v2-config": "4", "port2": "5", "ipv6-addr": "8", "other": "6", "empty-name": "7",
	}, nil)
	got, err := bind[names](cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	expected := names{1, 2, 3, 4, 5, 8, 6, 7}
	if got != expected {
		t.Errorf("got %+v, want %+v", got, expected)
	}
}

func TestBind_EmptyValue(t *testing.T) {
	t.Parallel()
	type target struct {
		Host    string `default:"localhost"`
		Port    *int
		Hosts   []string
		Labels  map[string]string
		Timeout time.Duration `default:""`
	}
	cfg := load(t, map[string]string{"x.host": "file", "x.port": "1", "x.hosts": "a", "x.labels.a": "1", "x.labels.b": ""},
		upperSource{"X_HOST": "", "X_PORT": "", "X_HOSTS": "", "X_TIMEOUT": ""})
	got, err := bind[target](cfg, "x")
	if err != nil {
		t.Fatal(err)
	}
	expected := target{Host: "localhost", Labels: map[string]string{"a": "1"}}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %+v, want %+v", got, expected)
	}
}

func TestBind_EnvAndProfiles(t *testing.T) {
	t.Parallel()
	type db struct {
		URL  string
		Pool struct{ MaxSize int } `cfg:"pool"`
	}
	cfg := load(t, map[string]string{"db.url": "prod", "%dev.db.url": "dev", "db.pool.max-size": "10"},
		upperSource{"DB_POOL_MAX_SIZE": "20"}, gonf.WithProfiles("dev"))
	got, err := bind[db](cfg, "db")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "dev" || got.Pool.MaxSize != 20 {
		t.Errorf("got %+v, want URL dev and MaxSize 20", got)
	}
}

func TestBind_Slices(t *testing.T) {
	t.Parallel()
	type server struct {
		Host string
		Port int `default:"80"`
	}
	type lists struct {
		Hosts []string
		Ports []int `default:"1,2"`
	}
	type servers struct{ Servers []server }
	type optional struct{ Hosts *[]string }
	tests := []struct {
		name     string
		values   map[string]string
		env      upperSource
		bind     func(*gonf.Config) (any, error)
		expected any
	}{
		{
			name:     "comma separated, escaped, trimmed, empty dropped",
			values:   map[string]string{"x.hosts": ` a , b\,c ,, d,`, "x.ports": "8080"},
			bind:     bindAs[lists],
			expected: lists{Hosts: []string{"a", "b,c", "d"}, Ports: []int{8080}},
		},
		{
			name:     "indexed with gap",
			values:   map[string]string{"x.hosts[0]": "a", "x.hosts[2]": "c"},
			bind:     bindAs[lists],
			expected: lists{Hosts: []string{"a", "c"}, Ports: []int{1, 2}},
		},
		{
			name:     "comma separated wins over indexed",
			values:   map[string]string{"x.hosts": "a,b", "x.hosts[0]": "z"},
			bind:     bindAs[lists],
			expected: lists{Hosts: []string{"a", "b"}, Ports: []int{1, 2}},
		},
		{
			name:     "indexed from env only",
			env:      upperSource{"X_HOSTS_0_": "a", "X_HOSTS_1_": "b"},
			bind:     bindAs[lists],
			expected: lists{Hosts: []string{"a", "b"}, Ports: []int{1, 2}},
		},
		{
			name:     "env overrides indexed element",
			values:   map[string]string{"x.hosts[0]": "a", "x.hosts[1]": "b"},
			env:      upperSource{"X_HOSTS_1_": "env"},
			bind:     bindAs[lists],
			expected: lists{Hosts: []string{"a", "env"}, Ports: []int{1, 2}},
		},
		{
			name:     "malformed indexed keys ignored",
			values:   map[string]string{"x.hosts[a]": "a", "x.hosts[1": "b", "x.hosts[2]x": "c", "x.hosts[3]": "d"},
			bind:     bindAs[lists],
			expected: lists{Hosts: []string{"d"}, Ports: []int{1, 2}},
		},
		{
			name:     "only empty elements",
			values:   map[string]string{"x.hosts": ",,"},
			bind:     bindAs[lists],
			expected: lists{Ports: []int{1, 2}},
		},
		{
			name:   "structs",
			values: map[string]string{"x.servers[0].host": "a", "x.servers[1].host": "b", "x.servers[1].port": "81"},
			env:    upperSource{"X_SERVERS_2__HOST": "c"},
			bind:   bindAs[servers],
			expected: servers{Servers: []server{
				{Host: "a", Port: 80}, {Host: "b", Port: 81}, {Host: "c", Port: 80},
			}},
		},
		{
			name:     "optional missing",
			bind:     bindAs[optional],
			expected: optional{},
		},
		{
			name:     "optional indexed",
			values:   map[string]string{"x.hosts[0]": "a"},
			bind:     bindAs[optional],
			expected: optional{Hosts: &[]string{"a"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.bind(load(t, tt.values, tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestBind_Maps(t *testing.T) {
	t.Parallel()
	type server struct {
		Host string
		Tags []string
	}
	type maps struct {
		Labels  map[string]string
		Limits  map[int]time.Duration
		Servers map[string]server
		Nested  map[string]map[string]int
		Lists   map[string][]string
		Opt     map[string]*server
	}
	cfg := load(t, map[string]string{
		"x.labels.team":               "core",
		"x.labels.a.b":                "dotted",
		"x.limits.1":                  "1s",
		"x.servers.eu.host":           "eu-host",
		`x.servers."us.east".host`:    "us-host",
		`x.servers."us.east".tags[0]`: "t1",
		"x.nested.a.b":                "1",
		"x.lists.a[0]":                "1",
		"x.lists.a[1]":                "2",
		"x.lists.b":                   "3,4",
		"x.opt.o.host":                "o-host",
		"xx.labels.other":             "ignored",
	}, upperSource{"X_LABELS_TEAM": "env"})
	got, err := bind[maps](cfg, "x")
	if err != nil {
		t.Fatal(err)
	}
	expected := maps{
		Labels:  map[string]string{"team": "env", "a.b": "dotted"},
		Limits:  map[int]time.Duration{1: time.Second},
		Servers: map[string]server{"eu": {Host: "eu-host"}, "us.east": {Host: "us-host", Tags: []string{"t1"}}},
		Nested:  map[string]map[string]int{"a": {"b": 1}},
		Lists:   map[string][]string{"a": {"1", "2"}, "b": {"3", "4"}},
		Opt:     map[string]*server{"o": {Host: "o-host"}},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %+v\nwant %+v", got, expected)
	}
}

// node is a recursive type; Bind must stop where the keys end.
type node struct {
	Name string
	Next *node
}

func TestBind_Recursive(t *testing.T) {
	t.Parallel()
	cfg := load(t, map[string]string{"n.name": "a", "n.next.name": "b"}, nil)
	got, err := bind[node](cfg, "n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "a" || got.Next == nil || got.Next.Name != "b" || got.Next.Next != nil {
		t.Errorf("got %+v", got)
	}
}

// selfPointer and selfList contain themselves, which Bind cannot read.
type (
	selfPointer *selfPointer
	selfList    []selfList
)

func TestBind_Errors(t *testing.T) {
	t.Parallel()
	type small struct{ N int8 }
	type unsupported struct {
		F func()
		M map[[2]int]string
	}
	type groupDefault struct {
		Pool pool          `default:"x"`
		D    time.Duration `default:"5"`
	}
	type badDefaults struct {
		D time.Duration `default:"5"`
		R string        `default:"${missing}"`
		L []int         `default:"${missing}"`
	}
	type hiddenInvalid struct {
		Pool *struct{ F func() }
		L    []struct{ C chan int }
		M    map[string]struct{ C chan int }
		P    selfPointer
		S    selfList
		N    int
	}
	type embeddedPointer struct{ *Common }
	type structList struct{ S []pool }
	type required struct{ N int }
	type requiredList struct{ S []required }
	type intKeys struct{ M map[int]string }
	tests := []struct {
		name     string
		values   map[string]string
		bind     func(*gonf.Config) (any, error)
		expected string
	}{
		{
			name:     "missing",
			bind:     bindAs[required],
			expected: `gonf: bind "x": x.n: required, no value`,
		},
		{
			name: "invalid values with origin",
			values: map[string]string{
				"x.url": "u", "x.http-port": "99999", "x.ratio": "half",
				"x.enabled": "ture", "x.addr": "nope", "x.timeout": "30",
			},
			bind: bindAs[DB],
			expected: `gonf: bind "x": 6 problems:
  x.timeout: invalid duration "30", want e.g. 5s or 1h30m (from file:app.properties)
  x.http-port: "99999" out of range for uint16 (from file:app.properties)
  x.ratio: invalid float64 "half" (from file:app.properties)
  x.enabled: invalid bool "ture", want true/false, 1/0, yes/no or on/off (from file:app.properties)
  x.addr: invalid netip.Addr "nope": ParseAddr("nope"): unable to parse IP (from file:app.properties)
  x.user: default: unresolved reference "app.name"`,
		},
		{
			name:     "out of range",
			values:   map[string]string{"x.n": "128"},
			bind:     bindAs[small],
			expected: `gonf: bind "x": x.n: "128" out of range for int8 (from file:app.properties)`,
		},
		{
			name:     "invalid int",
			values:   map[string]string{"x.n": "1x"},
			bind:     bindAs[small],
			expected: `gonf: bind "x": x.n: invalid int8 "1x" (from file:app.properties)`,
		},
		{
			name:     "ambiguous leading zero",
			values:   map[string]string{"x.n": "-0_7"},
			bind:     bindAs[small],
			expected: `gonf: bind "x": x.n: ambiguous leading zero in "-0_7", write -0o_7 for octal or -7 for decimal (from file:app.properties)`,
		},
		{
			name:   "unsupported types",
			values: map[string]string{"x.f": "1"},
			bind:   bindAs[unsupported],
			expected: `gonf: bind "x": 2 problems:
  x.f: unsupported type func()
  x.m: unsupported map key type [2]int`,
		},
		{
			name: "invalid types without values",
			bind: bindAs[hiddenInvalid],
			expected: `gonf: bind "x": 5 problems:
  x.pool.f: unsupported type func()
  x.l[<n>].c: unsupported type chan int
  x.m.<name>.c: unsupported type chan int
  x.p: unsupported type gonf_test.selfPointer, it contains itself
  x.s: unsupported type gonf_test.selfList, it contains itself`,
		},
		{
			name:     "default for a group comes before values",
			bind:     bindAs[groupDefault],
			expected: `gonf: bind "x": x.pool: default not supported for gonf_test.pool`,
		},
		{
			name: "bad defaults",
			bind: bindAs[badDefaults],
			expected: `gonf: bind "x": 3 problems:
  x.d: default: invalid duration "5", want e.g. 5s or 1h30m
  x.r: default: unresolved reference "missing"
  x.l: default: unresolved reference "missing"`,
		},
		{
			name:     "embedded pointer",
			bind:     bindAs[embeddedPointer],
			expected: `gonf: bind "x": x.common: embedded pointer *gonf_test.Common not supported, embed the struct`,
		},
		{
			name:     "comma separated structs",
			values:   map[string]string{"x.s": "a,b"},
			bind:     bindAs[structList],
			expected: `gonf: bind "x": x.s: comma-separated value for a list of gonf_test.pool, use indexed keys such as x.s[0] (from file:app.properties)`,
		},
		{
			name:     "list element",
			values:   map[string]string{"x.ports": "1,b"},
			bind:     bindAs[struct{ Ports []int }],
			expected: `gonf: bind "x": x.ports: element 1: invalid int "b" (from file:app.properties)`,
		},
		{
			name:     "indexed element missing field",
			values:   map[string]string{"x.s[0].other": "1"},
			bind:     bindAs[requiredList],
			expected: "gonf: bind \"x\": 2 problems:\n  x.s[0].n: required, no value\n  x.s[0].other: unknown key (from file:app.properties)",
		},
		{
			name:     "map key",
			values:   map[string]string{"x.m.one": "1"},
			bind:     bindAs[intKeys],
			expected: `gonf: bind "x": x.m.one: map key: invalid int "one"`,
		},
		{
			name:     "expansion",
			values:   map[string]string{"x.max-size": "${x.max-size}"},
			bind:     bindAs[pool],
			expected: `gonf: bind "x": x.max-size: cycle x.max-size -> x.max-size (from file:app.properties)`,
		},
		{
			name:     "expansion in list",
			values:   map[string]string{"x.ports": "${nope}"},
			bind:     bindAs[struct{ Ports []int }],
			expected: `gonf: bind "x": x.ports: unresolved reference "nope" (from file:app.properties)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.bind(load(t, tt.values, nil))
			if err == nil {
				t.Fatal("no error")
			}
			if err.Error() != tt.expected {
				t.Errorf("error\n%s\nwant\n%s", err, tt.expected)
			}
		})
	}
}

// window validates itself; its pointer receiver must be found too.
type window struct {
	Min int
	Max int
}

func (w *window) Validate() error {
	if w.Min > w.Max {
		return fmt.Errorf("min %d is greater than max %d", w.Min, w.Max)
	}
	return nil
}

// limits validates itself with a value receiver.
type limits struct {
	Window  window
	Windows []window
	Named   map[string]window
	Name    string
}

func (l limits) Validate() error {
	if l.Name == "bad" {
		return errors.New("bad name")
	}
	return nil
}

func TestBind_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		prefix        string
		values        map[string]string
		expectedError string
	}{
		{
			name:   "valid",
			prefix: "x",
			values: map[string]string{"x.window.min": "1", "x.window.max": "2", "x.name": "ok"},
		},
		{
			name:   "nested, elements and parent",
			prefix: "x",
			values: map[string]string{
				"x.window.min": "1", "x.window.max": "2", "x.name": "bad",
				"x.windows[0].min": "3", "x.windows[0].max": "4",
				"x.windows[1].min": "5", "x.windows[1].max": "4",
				"x.named.a.min": "9", "x.named.a.max": "1",
			},
			expectedError: `gonf: bind "x": 2 problems:
  x.windows[1]: min 5 is greater than max 4
  x.named.a: min 9 is greater than max 1`,
		},
		{
			name:          "parent after valid children",
			prefix:        "x",
			values:        map[string]string{"x.window.min": "1", "x.window.max": "2", "x.name": "bad"},
			expectedError: `gonf: bind "x": x: bad name`,
		},
		{
			name:          "not called after binding problems",
			prefix:        "x",
			values:        map[string]string{"x.window.min": "9", "x.name": "bad"},
			expectedError: `gonf: bind "x": x.window.max: required, no value`,
		},
		{
			name:          "root",
			prefix:        "",
			values:        map[string]string{"window.min": "1", "window.max": "2", "name": "bad"},
			expectedError: `gonf: bind "": bad name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := bind[limits](load(t, tt.values, nil), tt.prefix)
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

func TestBind_Unknown(t *testing.T) {
	t.Parallel()
	type pool struct {
		MaxSize int `default:"10"`
	}
	type db struct {
		URL     string
		Pool    pool
		Hosts   []string
		Labels  map[string]string
		Skipped string `cfg:"-"`
		// Replicas and Shards have entries that are groups.
		Replicas map[string]pool
		Shards   []pool
	}
	tests := []struct {
		name          string
		prefix        string
		values        map[string]string
		env           upperSource
		opts          []gonf.MappingOption
		expectedError string
	}{
		{
			name:   "known keys",
			prefix: "db",
			values: map[string]string{
				"db.url": "postgres://${db.host}/${db.name:app}", "db.host": "h",
				"db.hosts": "a", "db.hosts[0]": "ignored", "db.labels.a.b": "1",
				"db.replicas.eu.max-size": "3", "db.shards[0].max-size": "4",
				"other.key": "outside the prefix", "dbx.key": "outside the prefix",
			},
			env: upperSource{"DB_URLL": "env names are not checked"},
		},
		{
			name:   "typos with suggestion",
			prefix: "db",
			values: map[string]string{"db.url": "u", "db.pool.max-szie": "5", "db.ulr": "u"},
			expectedError: `gonf: bind "db": 2 problems:
  db.pool.max-szie: unknown key, did you mean db.pool.max-size? (from file:app.properties)
  db.ulr: unknown key, did you mean db.url? (from file:app.properties)`,
		},
		{
			name:   "no close key and skipped field",
			prefix: "db",
			values: map[string]string{"db.url": "u", "db.completely-different": "x", "db.skipped": "x"},
			expectedError: `gonf: bind "db": 2 problems:
  db.completely-different: unknown key (from file:app.properties)
  db.skipped: unknown key (from file:app.properties)`,
		},
		{
			name:   "typos in entries of a map and a list",
			prefix: "db",
			values: map[string]string{"db.url": "u", "db.replicas.eu.max-szie": "3", "db.shards[0].max-szie": "4"},
			expectedError: `gonf: bind "db": 2 problems:
  db.replicas.eu.max-szie: unknown key, did you mean db.replicas.eu.max-size? (from file:app.properties)
  db.shards[0].max-szie: unknown key, did you mean db.shards[0].max-size? (from file:app.properties)`,
		},
		{
			name:          "too far for a suggestion",
			prefix:        "db",
			values:        map[string]string{"db.url": "u", "db.user": "x"},
			expectedError: `gonf: bind "db": db.user: unknown key (from file:app.properties)`,
		},
		{
			name:          "reported after binding problems",
			prefix:        "db",
			values:        map[string]string{"db.urll": "u"},
			expectedError: "gonf: bind \"db\": 2 problems:\n  db.url: required, no value\n  db.urll: unknown key, did you mean db.url? (from file:app.properties)",
		},
		{
			name:   "allow unknown",
			prefix: "db",
			values: map[string]string{"db.url": "u", "db.extra": "x"},
			opts:   []gonf.MappingOption{gonf.AllowUnknown()},
		},
		{
			name:   "empty prefix is not checked",
			prefix: "",
			values: map[string]string{"url": "u", "extra": "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := bind[db](load(t, tt.values, tt.env), tt.prefix, tt.opts...)
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

func TestBind_SecretReferences(t *testing.T) {
	t.Parallel()
	type target struct {
		Password gonf.Secret[string]
		URL      string
		Chain    string
		Hosts    []string
		Header   string
		Fallback string `default:"${x.password}"`
		Safe     gonf.Secret[string]
		Plain    string
	}
	cfg := load(t, map[string]string{
		"x.password": "s3cret",
		"x.url":      "postgres://u:${x.password}@db/app",
		"x.chain":    "${x.url}",
		"x.hosts":    "a,${x.password}",
		"x.header":   "Bearer ${api-token}",
		"x.safe":     "${x.password}",
		"x.plain":    "${other}",
		"api-token":  "t0k3n",
		"other":      "o",
	}, nil)
	_, err := bind[target](cfg, "x")
	expected := `gonf: bind "x": 5 problems:
  x.url: refers to the secret x.password, make the field a gonf.Secret (from file:app.properties)
  x.chain: refers to the secret x.password, make the field a gonf.Secret (from file:app.properties)
  x.hosts: refers to the secret x.password, make the field a gonf.Secret (from file:app.properties)
  x.header: refers to the secret api-token, make the field a gonf.Secret (from file:app.properties)
  x.fallback: refers to the secret x.password, make the field a gonf.Secret`
	if err == nil || err.Error() != expected {
		t.Errorf("err = %v, want\n%s", err, expected)
	}
}

func TestBind_ErrorTypes(t *testing.T) {
	t.Parallel()
	cfg := load(t, map[string]string{"x.url": "${nope}", "app.name": "shop"}, nil)
	got, err := bind[DB](cfg, "x")
	if !reflect.DeepEqual(got, DB{}) {
		t.Errorf("Bind returned %+v on error, want zero value", got)
	}
	var bindErr *gonf.BindError
	if !errors.As(err, &bindErr) {
		t.Fatalf("error %T is not a *BindError", err)
	}
	if bindErr.Prefix != "x" || len(bindErr.Problems) != 5 {
		t.Fatalf("BindError = %+v, want prefix x and 5 problems", bindErr)
	}
	if p := bindErr.Problems[0]; p.Key != "x.url" || p.Origin.Source != "file:app.properties" {
		t.Errorf("first problem = %+v, want x.url from file:app.properties", p)
	}
	if !errors.Is(err, gonf.ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) = false, want true for the missing x.http-port")
	}
}

func TestBind_NotStruct(t *testing.T) {
	t.Parallel()
	cfg := load(t, nil, nil)
	if _, err := bind[int](cfg, "x"); err == nil || !strings.Contains(err.Error(), "int is not a struct") {
		t.Errorf("Bind[int] error = %v", err)
	}
	_, err := bind[netip.Addr](cfg, "x")
	if err == nil || !strings.Contains(err.Error(), "netip.Addr is not a struct") {
		t.Errorf("Bind[netip.Addr] error = %v", err)
	}
}

func bindAs[T any](cfg *gonf.Config) (any, error) {
	return bind[T](cfg, "x")
}

// bind binds T to the keys under prefix of cfg the way Load binds a
// Mapping.
func bind[T any](cfg *gonf.Config, prefix string, opts ...gonf.MappingOption) (T, error) {
	return gonf.BindMapping(cfg, gonf.NewMapping[T](prefix, opts...))
}

// FuzzBind checks that Bind never panics, whatever the keys and values.
func FuzzBind(f *testing.F) {
	type fuzzTarget struct {
		DB
		Hosts   []string
		Servers []node
		Labels  map[string]string
		Groups  map[string]map[int]*node
	}
	f.Add("x.hosts[0]", "a")
	f.Add(`x.groups."a.b".1.next.name`, "n")
	f.Add("x.servers[1].next.name", "${x.url}")
	f.Add("x.labels.a[0]", `a\,b`)
	f.Add("x.pool.max-size", "-0_7")
	f.Add("x.groups.1.next.name", "0x")
	f.Fuzz(func(t *testing.T, key, value string) {
		cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(map[string]string{key: value})))
		if err != nil {
			return
		}
		_, _ = bind[fuzzTarget](cfg, "x")
	})
}

// benchValues are the values of the benchmarks: a plain key, a key with
// expressions and a number.
var benchValues = map[string]string{
	"db.host":          "localhost",
	"db.url":           "postgres://${db.host}:${db.port:5432}/app",
	"db.pool.max-size": "10",
}

type benchDB struct {
	Host string
	URL  string
	Pool struct {
		MaxSize int
	}
}

func BenchmarkBind(b *testing.B) {
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(benchValues)))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := bind[benchDB](cfg, "db"); err != nil {
			b.Fatal(err)
		}
	}
}
