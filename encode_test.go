package gonf_test

import (
	"maps"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/memory"
)

type encodeCommon struct {
	Name string
}

type encodeAll struct {
	encodeCommon
	Text     string
	Flag     bool
	Count    int8
	Size     uint64
	Ratio    float64
	Timeout  time.Duration
	Addr     netip.Addr
	Password gonf.Secret[string]
	Port     gonf.Secret[int]
	Optional *string
	Missing  *string
	Server   struct {
		Host string
	}
	Tags    []string
	Ports   []int
	Servers []struct{ Host string }
	Labels  map[string]string
	Users   map[string]struct{ Roles []string }
	Empty   string `default:""`
	Hidden  string `cfg:"-"`
}

// Encoding and binding again give the value back, also with "${" in it.
func TestEncode_roundTrip(t *testing.T) {
	t.Parallel()
	optional := "${not.a.reference}"
	in := encodeAll{
		encodeCommon: encodeCommon{Name: "app"},
		Text:         "a$${b}${c}",
		Flag:         true,
		Count:        -8,
		Size:         1 << 40,
		Ratio:        0.1,
		Timeout:      90 * time.Second,
		Addr:         netip.MustParseAddr("::1"),
		Password:     gonf.NewSecret("p${w}"),
		Port:         gonf.NewSecret(5432),
		Optional:     &optional,
		Hidden:       "not encoded",
	}
	in.Server.Host = "db"
	in.Tags = []string{" a,b ", "c"}
	in.Ports = []int{1, 2}
	in.Servers = []struct{ Host string }{{Host: "h1"}, {Host: "h2"}}
	in.Labels = map[string]string{"team": "x", "eu.west": "y"}
	in.Users = map[string]struct{ Roles []string }{"alice": {Roles: []string{"admin", "user"}}, "a.b": {Roles: []string{"user"}}}
	values, err := gonf.Encode("x", in)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"x.name": "app", "x.text": "a$${b}${c}", "x.flag": "true", "x.count": "-8",
		"x.size": "1099511627776", "x.ratio": "0.1", "x.timeout": "1m30s", "x.addr": "::1",
		"x.password": "p${w}", "x.port": "5432", "x.optional": "${not.a.reference}", "x.server.host": "db",
		"x.tags[0]": " a,b ", "x.tags[1]": "c", "x.ports[0]": "1", "x.ports[1]": "2",
		"x.servers[0].host": "h1", "x.servers[1].host": "h2", "x.labels.team": "x", `x.labels."eu.west"`: "y",
		"x.users.alice.roles[0]": "admin", "x.users.alice.roles[1]": "user", `x.users."a.b".roles[0]`: "user",
	}
	if !maps.Equal(values, expected) {
		t.Errorf("Encode = %v, want %v", values, expected)
	}

	escaped := map[string]string{}
	for k, v := range values {
		escaped[k] = gonf.Escape(v)
	}
	m := gonf.NewMapping[encodeAll]("x")
	cfg, err := gonf.Load(t.Context(), gonf.WithSources(memory.New(escaped)), gonf.WithMappings(m))
	if err != nil {
		t.Fatal(err)
	}
	out := m.Get(cfg)
	in.Hidden = ""
	if out.Password.Value() != in.Password.Value() || out.Port.Value() != in.Port.Value() {
		t.Errorf("secrets = %q, %d", out.Password.Value(), out.Port.Value())
	}
	out.Password, out.Port, in.Password, in.Port = gonf.Secret[string]{}, gonf.Secret[int]{}, gonf.Secret[string]{}, gonf.Secret[int]{}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("bound = %+v, want %+v", out, in)
	}
}

type onlyUnmarshal struct{ s string }

func (o *onlyUnmarshal) UnmarshalText(b []byte) error { o.s = string(b); return nil }

func TestEncode_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{name: "nil", value: nil, expected: `gonf: encode "x": nil`},
		{name: "no struct", value: "a", expected: `gonf: encode "x": string is not a struct`},
		{name: "map key like an index", value: struct{ Labels map[string]string }{Labels: map[string]string{"a[0]": "x"}}, expected: `gonf: encode "x": x.labels: map key "a[0]" ends like an index`},
		{name: "empty element", value: struct{ Tags []string }{Tags: []string{"a", ""}}, expected: `gonf: encode "x": x.tags[1]: empty, which Load reads as a missing element`},
		{name: "no MarshalText", value: struct{ V onlyUnmarshal }{}, expected: `gonf: encode "x": x.v: gonf_test.onlyUnmarshal has UnmarshalText but no MarshalText`},
		{name: "unsupported type", value: struct{ C chan int }{}, expected: `gonf: bind "x": x.c: unsupported type chan int`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Encode("x", tt.value)
			if err == nil || !strings.Contains(err.Error(), tt.expected) {
				t.Errorf("Encode = %v, want %q", err, tt.expected)
			}
		})
	}
}

func TestEscape(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "plain", "${a}", "$${a}", "a$", "$$", "${", "x${y}$${z}$"} {
		cfg, err := gonf.Load(t.Context(), gonf.WithSources(memory.New(map[string]string{"k": gonf.Escape(s)})))
		if err != nil {
			t.Fatal(err)
		}
		if got, err := cfg.Get("k"); err != nil || got != s {
			t.Errorf("Get(Escape(%q)) = %q, %v", s, got, err)
		}
	}
}
