package gonf_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/memory"
)

type explainDB struct {
	URL      gonf.Secret[string]
	User     string `default:"admin"`
	Password gonf.Secret[string]
	Pool     struct {
		MaxSize int `default:"10"`
	}
	Name  string              `default:"app"`
	Token gonf.Secret[string] `default:"dflt"`
}

// explainLocation is the Location of the defaults of explainMapping.
var explainMapping, explainLocation = gonf.NewMapping[explainDB]("db"), callerLine()

// callerLine returns file:line of its caller.
func callerLine() string {
	_, file, line, _ := runtime.Caller(1)
	return filepath.Base(file) + ":" + strconv.Itoa(line)
}

func explainConfig(t *testing.T) *gonf.Config {
	t.Helper()
	return load(t, map[string]string{
		"db.url":       "postgres://u:${db.password}@localhost/app",
		"%dev.db.url":  "postgres://u:${db.password}@dev/app",
		"%prod.db.url": "postgres://prod",
		"dsn":          "postgres://u:${db.password}@dev/app",
		"db.password":  "file-pw",
		"db.user":      "",
		"api-token":    "t0k3n",
		"plain":        "hello",
		"broken":       "${missing}",
	}, upperSource{
		"DB_PASSWORD":      "env-pw",
		"DB_POOL_MAX_SIZE": "20",
		"PATH":             "/bin",
	}, gonf.WithProfiles("dev"), gonf.WithMappings(explainMapping))
}

func TestConfig_Explain(t *testing.T) {
	t.Parallel()
	cfg := explainConfig(t)
	tests := []struct {
		name           string
		key            string
		expectedValue  string
		expectedSecret bool
		expectedString string
		expectedErr    error
	}{
		{
			name:           "references to secrets are masked",
			key:            "dsn",
			expectedValue:  "postgres://u:****@dev/app",
			expectedString: "dsn = postgres://u:****@dev/app (file:app.properties)",
		},
		{
			name:           "secret field with a reference",
			key:            "db.url",
			expectedValue:  "****",
			expectedSecret: true,
			expectedString: "db.url = **** (file:app.properties, profile dev)\n  overrides **** (file:app.properties)",
		},
		{
			name:           "secret field",
			key:            "db.password",
			expectedValue:  "****",
			expectedSecret: true,
			expectedString: "db.password = **** (test:upper)\n  overrides **** (file:app.properties)",
		},
		{
			name:           "env overrides default",
			key:            "db.pool.max-size",
			expectedValue:  "20",
			expectedString: "db.pool.max-size = 20 (test:upper)\n  default 10 (<loc>)",
		},
		{
			name:           "default only",
			key:            "db.name",
			expectedValue:  "app",
			expectedString: "db.name = app (default, <loc>)",
		},
		{
			name:           "empty value uses default",
			key:            "db.user",
			expectedValue:  "admin",
			expectedString: `db.user = admin (default, <loc>)` + "\n" + `  empty "" (file:app.properties)`,
		},
		{
			name:           "secret default",
			key:            "db.token",
			expectedValue:  "****",
			expectedSecret: true,
			expectedString: "db.token = **** (default, <loc>)",
		},
		{
			name:           "secret by name",
			key:            "api-token",
			expectedValue:  "****",
			expectedSecret: true,
			expectedString: "api-token = **** (file:app.properties)",
		},
		{
			name:           "expansion error",
			key:            "broken",
			expectedString: "broken: gonf: key \"broken\": unresolved reference \"missing\"\n  value ${missing} (file:app.properties)",
		},
		{
			name:           "unknown key",
			key:            "nope",
			expectedString: `nope: gonf: key not found: "nope"`,
			expectedErr:    gonf.ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := cfg.Explain(tt.key)
			if e.Key != tt.key || e.Value != tt.expectedValue || e.IsSecret != tt.expectedSecret {
				t.Errorf("Explain = %+v", e)
			}
			// <loc> stands for the Location of the defaults.
			expected := strings.ReplaceAll(tt.expectedString, "<loc>", explainLocation)
			if got := e.String(); got != expected {
				t.Errorf("String =\n%s\nwant\n%s", got, expected)
			}
			if tt.expectedErr != nil && !errors.Is(e.Err, tt.expectedErr) {
				t.Errorf("Err = %v, want %v", e.Err, tt.expectedErr)
			}
		})
	}
	if v, _ := cfg.Lookup("db.password"); v.Raw != "env-pw" {
		t.Errorf("Explain changed the Config: db.password = %q", v.Raw)
	}
}

func TestConfig_Explanations(t *testing.T) {
	t.Parallel()
	cfg := explainConfig(t)
	var keys []string
	var all strings.Builder
	for _, e := range cfg.Explanations() {
		keys = append(keys, e.Key)
		all.WriteString(e.String())
	}
	expected := "api-token,broken,db.name,db.password,db.pool.max-size,db.token,db.url,db.user,dsn,plain"
	if got := strings.Join(keys, ","); got != expected {
		t.Errorf("keys = %s, want %s", got, expected)
	}
	for _, leak := range []string{"env-pw", "file-pw", "t0k3n", "dflt", "/bin"} {
		if strings.Contains(all.String(), leak) {
			t.Errorf("Explanations reveal %q:\n%s", leak, all.String())
		}
	}
}

type explainedPool struct {
	URL     string `desc:"Address of the pool."`
	MaxSize int    `default:"10"`
}

type explained struct {
	Host  string `default:"localhost" desc:"Host of the database."`
	Token string
	Pools map[string]explainedPool
	Tags  []string
}

var explainedMapping = gonf.NewMapping[explained]("app")

// loadExplained returns the options that load explainedMapping from
// values and the environment, which names the keys.
func loadExplained(values map[string]string) []gonf.Option {
	return []gonf.Option{gonf.WithSources(memory.New(values), env.New()), gonf.WithMappings(explainedMapping)}
}

// An Explanation carries the Property of its key, with the environment
// name of the key itself; a key of a map entry gets the Property with
// <name>.
func TestConfig_Explain_property(t *testing.T) {
	t.Parallel()
	opts := loadExplained(map[string]string{"app.token": "t", "app.pools.main.url": "postgres://main"})
	cfg, err := gonf.Load(t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	host := cfg.Explain("app.host")
	if !host.IsDefault || host.Value != "localhost" || host.Property == nil {
		t.Fatalf("app.host = %+v", host)
	}
	if p := host.Property; p.Key != "app.host" || p.Env != "APP_HOST" || p.Type != "string" || *p.Default != "localhost" || p.Required || p.Doc != "Host of the database." || p.Mapping != "app" {
		t.Errorf("app.host property = %+v", *p)
	}
	token := cfg.Explain("app.token")
	if token.IsDefault || !token.IsSecret || token.Property == nil || !token.Property.Secret || !token.Property.Required || token.Property.Default != nil {
		t.Errorf("app.token = %+v, property %+v", token, token.Property)
	}
	url := cfg.Explain("app.pools.main.url")
	if p := url.Property; p == nil || p.Key != "app.pools.<name>.url" || p.Env != "APP_POOLS_MAIN_URL" || p.Doc != "Address of the pool." {
		t.Errorf("app.pools.main.url property = %+v", p)
	}
	if p := cfg.Explain("app.pools.main.max-size").Property; p == nil || *p.Default != "10" {
		t.Errorf("app.pools.main.max-size property = %+v", p)
	}
	if p := cfg.Explain("other.key").Property; p != nil {
		t.Errorf("a key no Mapping reads has the property %+v", p)
	}
}

// The definition of a key is the same, whether Describe lists it, the
// Config lists it, or an Explanation carries it: one derivation, no drift.
func TestConfig_Properties_noDrift(t *testing.T) {
	t.Parallel()
	opts := loadExplained(map[string]string{"app.token": "t", "app.pools.main.url": "postgres://main"})
	cfg, err := gonf.Load(t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	described, err := gonf.Describe(opts...)
	if err != nil {
		t.Fatal(err)
	}
	props := cfg.Properties()
	if !reflect.DeepEqual(props, described) {
		t.Fatalf("Properties =\n%+v\nDescribe =\n%+v", props, described)
	}
	byKey := map[string]gonf.Property{}
	for _, p := range props {
		byKey[p.Key] = p
	}
	explained := 0
	for _, e := range cfg.Explanations() {
		if e.Property == nil {
			continue
		}
		explained++
		p := *e.Property
		p.Env = byKey[p.Key].Env // the Env of the key, not of <name>
		if !reflect.DeepEqual(p, byKey[p.Key]) {
			t.Errorf("%s: property %+v, Properties %+v", e.Key, p, byKey[p.Key])
		}
	}
	if explained == 0 {
		t.Error("no Explanation has a Property")
	}
}
