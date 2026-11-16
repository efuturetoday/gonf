package gonf_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/function/base64"
	"github.com/efuturetoday/gonf/function/uuid"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/memory"
)

// recorder is a Function that upper-cases its argument and records the
// origins and arguments it was called with. "fail" fails.
type recorder struct {
	mu      sync.Mutex
	origins []string
	args    []string
}

func (r *recorder) call(_ *gonf.Config, arg string, origin gonf.Origin) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.origins = append(r.origins, origin.Source)
	r.args = append(r.args, arg)
	if arg == "fail" {
		return "", errors.New("wrong key")
	}
	return strings.ToUpper(arg), nil
}

func (r *recorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.args)
}

// upper returns r as the Function upper.
func (r *recorder) upper(isSecret bool) gonf.Function {
	return gonf.Function{Name: "upper", IsSecret: isSecret, Call: r.call}
}

// constant returns a Function that returns s.
func constant(name, s string) gonf.Function {
	return gonf.Function{Name: name, Call: func(*gonf.Config, string, gonf.Origin) (string, error) { return s, nil }}
}

func TestWithFunctions_Get(t *testing.T) {
	t.Parallel()
	var r recorder
	cfg := load(t, map[string]string{
		"a":        "${upper::abc}",
		"b":        "x-${a}-y",
		"d":        "${c}",
		"broken":   "${upper::fail}",
		"nested":   "${upper::${a}}",
		"other":    "${lower::abc}",
		"escaped":  "${upper::$${a}}",
		"fallback": "${missing:${upper::home}}",
		"set":      "${a:${upper::unused}}",
		"invalid":  "${a.b::c}",
	}, upperSource{"C": "${upper::env}"}, gonf.WithFunctions(r.upper(false)),
		gonf.WithMappings(gonf.NewMapping[struct {
			C string `default:""`
		}]("", gonf.AllowUnknown())))

	tests := []struct {
		key      string
		expected string
		err      string
	}{
		{key: "a", expected: "ABC"},
		{key: "b", expected: "x-ABC-y"},
		{key: "c", expected: "ENV"},
		{key: "d", expected: "ENV"},
		{key: "nested", expected: "ABC"},
		{key: "escaped", expected: "${A}"},
		{key: "fallback", expected: "HOME"},
		{key: "set", expected: "ABC"},
		{key: "broken", err: `gonf: key "broken": value of "broken" (file:app.properties): upper: wrong key`},
		{key: "other", err: `gonf: key "other": value of "other" (file:app.properties): lower: unknown function`},
		{key: "invalid", err: `gonf: key "invalid": value of "invalid" (file:app.properties): a.b: unknown function`},
	}
	for _, tt := range tests {
		got, err := cfg.Get(tt.key)
		switch {
		case tt.err != "" && (err == nil || err.Error() != tt.err):
			t.Errorf("Get(%q) error = %v, want %q", tt.key, err, tt.err)
		case tt.err == "" && (err != nil || got != tt.expected):
			t.Errorf("Get(%q) = %q, %v; want %q", tt.key, got, err, tt.expected)
		}
	}
	// The origin is where the expression is written, also behind a
	// reference. Results are cached by argument, errors are not: the first
	// call is Load binding c, "abc" is called once, "fail" every time, and
	// "unused" never.
	expectedArgs := []string{"env", "abc", "ABC", "${a}", "home", "fail"}
	expectedOrigins := []string{"test:upper", "file:app.properties", "file:app.properties", "file:app.properties", "file:app.properties", "file:app.properties"}
	if !slices.Equal(r.args, expectedArgs) || !slices.Equal(r.origins, expectedOrigins) {
		t.Errorf("calls = %q from %q, want %q from %q", r.args, r.origins, expectedArgs, expectedOrigins)
	}
}

func TestWithFunctions_Invalid(t *testing.T) {
	t.Parallel()
	var r recorder
	tests := []struct {
		name     string
		opts     []gonf.Option
		expected string
	}{
		{
			name:     "invalid name",
			opts:     []gonf.Option{gonf.WithFunctions(gonf.Function{Name: "a:b", Call: r.call})},
			expected: `gonf: invalid function name "a:b"`,
		},
		{
			name:     "empty name",
			opts:     []gonf.Option{gonf.WithFunctions(gonf.Function{Call: r.call})},
			expected: `gonf: invalid function name ""`,
		},
		{
			name:     "no Call",
			opts:     []gonf.Option{gonf.WithFunctions(gonf.Function{Name: "x"})},
			expected: `gonf: function "x" has no Call`,
		},
		{
			name:     "two functions with one name",
			opts:     []gonf.Option{gonf.WithFunctions(r.upper(false)), gonf.WithFunctions(r.upper(true))},
			expected: `gonf: two functions named "upper"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := gonf.Load(context.Background(), tt.opts...)
			if err == nil || err.Error() != tt.expected {
				t.Errorf("error = %v, want %q", err, tt.expected)
			}
		})
	}
}

func TestWithFunctions_Bind(t *testing.T) {
	t.Parallel()
	type db struct {
		Password gonf.Secret[string]
		URL      string
		User     string `default:"${upper::admin}"`
		Cert     string
	}
	var r recorder
	opts := []gonf.Option{gonf.WithFunctions(r.upper(true), base64.Encode())}
	values := map[string]string{
		"db.password": "${upper::s3cret}",
		"db.url":      "postgres://app:${db.password}@db/app",
		"db.cert":     "${base64::${upper::pem}}",
	}
	_, err := bind[db](load(t, values, nil, opts...), "db")
	expected := `gonf: bind "db": 3 problems:
  db.url: refers to the secret db.password, make the field a gonf.Secret (from file:app.properties)
  db.user: comes from the secret function upper, make the field a gonf.Secret
  db.cert: comes from the secret function upper, make the field a gonf.Secret (from file:app.properties)`
	if err == nil || err.Error() != expected {
		t.Errorf("error\n%v\nwant\n%s", err, expected)
	}

	type fixed struct {
		Password gonf.Secret[string]
		User     gonf.Secret[string] `default:"${upper::admin}"`
		Cert     gonf.Secret[string]
	}
	got, err := bind[fixed](load(t, values, nil, opts...), "db", gonf.AllowUnknown())
	if err != nil {
		t.Fatal(err)
	}
	if got.Password.Value() != "S3CRET" || got.User.Value() != "ADMIN" || got.Cert.Value() != "UEVN" {
		t.Errorf("got %q, %q, %q; want S3CRET, ADMIN, UEVN", got.Password.Value(), got.User.Value(), got.Cert.Value())
	}

	// A secret tag makes a plain field take secrets too, for types that
	// must not know gonf.
	type tagged struct {
		Password string `secret:"true"`
		User     string `default:"${upper::admin}" secret:"true"`
		Cert     string `secret:"true"`
	}
	plain, err := bind[tagged](load(t, values, nil, opts...), "db", gonf.AllowUnknown())
	if err != nil {
		t.Fatal(err)
	}
	if plain.Password != "S3CRET" || plain.User != "ADMIN" || plain.Cert != "UEVN" {
		t.Errorf("tagged: %+v", plain)
	}
}

func TestWithFunctions_Explain(t *testing.T) {
	t.Parallel()
	var secret, plain recorder
	cfg, err := gonf.Load(context.Background(),
		gonf.WithFunctions(secret.upper(true), gonf.Function{Name: "lower", Call: plain.call}, base64.Encode()),
		gonf.WithSources(
			memory.New(map[string]string{
				"api.key":   "${upper::abc}",
				"dsn":       "db://${api.key}@h",
				"cert":      "${base64::${upper::pem}}",
				"cert.ref":  "${base64::${api.key}}",
				"home":      "${lower::x}/logs",
				"both":      "${base64::${lower::y}}",
				"unknown":   "${nope::x}",
				"user.name": "${missing:${lower::z}}",
			}, source.WithName("file:app.properties"), source.WithOrdinal(100)),
			memory.New(map[string]string{"api.key": "plain-override"}, source.WithName("env:"), source.WithOrdinal(300)),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		key      string
		expected string
	}{
		// A key that calls a secret Function anywhere is secret, also
		// when another value wins.
		{key: "api.key", expected: "api.key = **** (env:)\n  overrides **** (file:app.properties)"},
		{key: "dsn", expected: "dsn = db://****@h (file:app.properties)"},
		{key: "cert", expected: "cert = **** (file:app.properties, via upper, base64)"},
		// A secret argument makes the result secret.
		{key: "cert.ref", expected: "cert.ref = **** (file:app.properties, via base64)"},
		// Functions that are no secret are called.
		{key: "home", expected: "home = X/logs (file:app.properties, via lower)"},
		{key: "both", expected: "both = WQ== (file:app.properties, via lower, base64)"},
		{key: "user.name", expected: "user.name = Z (file:app.properties, via lower)"},
		{key: "unknown", expected: "unknown: gonf: key \"unknown\": value of \"unknown\" (file:app.properties): nope: unknown function\n  value ${nope::x} (file:app.properties)"},
	}
	for _, tt := range tests {
		if got := cfg.Explain(tt.key).String(); got != tt.expected {
			t.Errorf("Explain(%q)\n%s\nwant\n%s", tt.key, got, tt.expected)
		}
	}
	if got := cfg.Explain("cert").Functions; !slices.Equal(got, []string{"upper", "base64"}) {
		t.Errorf("Functions = %q, want [upper base64]", got)
	}
	if calls := secret.calls(); len(calls) > 0 {
		t.Errorf("Explain called the secret function with %q", calls)
	}
}

func TestWithFunctions_Cache(t *testing.T) {
	t.Parallel()
	values := map[string]string{"a": "${uuid::}", "b": "${uuid::}", "c": "${uuid::other}", "d": "${a}"}
	cfg := load(t, values, nil, gonf.WithFunctions(uuid.Function()))
	get := func(cfg *gonf.Config, key string) string {
		t.Helper()
		v, err := cfg.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a := get(cfg, "a")
	if len(a) != 36 || get(cfg, "b") != a || get(cfg, "d") != a {
		t.Errorf("uuid differs within one Config: %q, %q, %q", a, get(cfg, "b"), get(cfg, "d"))
	}
	if get(cfg, "c") == a {
		t.Errorf("${uuid::other} = ${uuid::} = %q", a)
	}
	if e, _ := cfg.Eval("${uuid::}"); e != a {
		t.Errorf("Eval = %q, want %q", e, a)
	}
	if v := cfg.Explain("a").Value; v != a {
		t.Errorf("Explain = %q, want %q", v, a)
	}
	if other := get(load(t, values, nil, gonf.WithFunctions(uuid.Function())), "a"); other == a {
		t.Errorf("two Loads share the uuid %q", a)
	}

	// Concurrent first calls agree on one result.
	var r recorder
	cfg = load(t, map[string]string{"x": "${upper::v}", "id": "${uuid::}"}, nil, gonf.WithFunctions(r.upper(false), uuid.Function()))
	var wg sync.WaitGroup
	ids := make([]string, 8)
	for i := range ids {
		wg.Go(func() {
			ids[i], _ = cfg.Get("id")
			_, _ = cfg.Get("x")
		})
	}
	wg.Wait()
	if ids[0] == "" || slices.ContainsFunc(ids, func(s string) bool { return s != ids[0] }) {
		t.Errorf("concurrent Gets returned %q", ids)
	}
}

func TestWithFunctions_Limits(t *testing.T) {
	t.Parallel()
	big := constant("big", strings.Repeat("x", 1<<20+1))
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{name: "result too long", value: "${big::}", expected: `gonf: key "a": expansion too large: more than 1048576 bytes`},
		{name: "argument too long", value: "${upper::${big::}}", expected: `gonf: key "a": expansion too large: more than 1048576 bytes`},
		{name: "too many calls", value: strings.Repeat("${upper::x}", 1001), expected: `gonf: key "a": expansion too large: more than 1000 references`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var r recorder
			cfg := load(t, map[string]string{"a": tt.value}, nil, gonf.WithFunctions(big, r.upper(false)))
			if _, err := cfg.Get("a"); err == nil || err.Error() != tt.expected {
				t.Errorf("error = %v, want %q", err, tt.expected)
			}
		})
	}
}

func TestConfig_Eval(t *testing.T) {
	t.Parallel()
	var r recorder
	cfg := load(t, map[string]string{
		"db.host": "db",
		"loop.a":  "${loop.b}",
		"loop.b":  "${loop.a}",
		"via":     "${missing}",
	}, nil, gonf.WithFunctions(r.upper(false)))
	tests := []struct {
		expr     string
		expected string
		err      string
	}{
		{expr: "", expected: ""},
		{expr: "plain", expected: "plain"},
		{expr: "postgres://${db.host}/app", expected: "postgres://db/app"},
		{expr: "${upper::${db.host}}/logs", expected: "DB/logs"},
		{expr: "${db.port:5432}", expected: "5432"},
		{expr: "$${db.host}", expected: "${db.host}"},
		{expr: "${missing}", err: `gonf: eval: unresolved reference "missing"`},
		{expr: "${via}", err: `gonf: eval: unresolved reference "missing" (via)`},
		{expr: "${loop.a}", err: `gonf: eval: cycle loop.a -> loop.b -> loop.a`},
		{expr: "${upper::fail}", err: `gonf: eval: upper: wrong key`},
		{expr: "${nope::x}", err: `gonf: eval: nope: unknown function`},
		{expr: "${db.host", err: `gonf: eval: expression: unclosed "${"`},
		{expr: "${}", err: `gonf: eval: expression: empty reference`},
	}
	for _, tt := range tests {
		got, err := cfg.Eval(tt.expr)
		switch {
		case tt.err != "" && (err == nil || err.Error() != tt.err):
			t.Errorf("Eval(%q) error = %v, want %q", tt.expr, err, tt.err)
		case tt.err == "" && (err != nil || got != tt.expected):
			t.Errorf("Eval(%q) = %q, %v; want %q", tt.expr, got, err, tt.expected)
		}
		if errors.Is(err, gonf.ErrNotFound) {
			t.Errorf("Eval(%q) error matches ErrNotFound", tt.expr)
		}
	}
}

func TestWithFunctions_ProfileKey(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithFunctions(constant("profiles", "dev")),
		gonf.WithProfileKey("app.profile"),
		gonf.WithSources(memory.New(map[string]string{"app.profile": "${profiles::}"})),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profiles(); !slices.Equal(got, []string{"dev"}) {
		t.Errorf("Profiles = %q, want [dev]", got)
	}
}
