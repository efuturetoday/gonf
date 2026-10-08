package gonf_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
)

type explainDB struct {
	URL      string
	User     string `default:"admin"`
	Password gonf.Secret[string]
	Pool     struct {
		MaxSize int `default:"10"`
	}
	Name  string              `default:"app"`
	Token gonf.Secret[string] `default:"dflt"`
}

func explainConfig(t *testing.T) *gonf.Config {
	t.Helper()
	return load(t, map[string]string{
		"db.url":       "postgres://u:${db.password}@localhost/app",
		"%dev.db.url":  "postgres://u:${db.password}@dev/app",
		"%prod.db.url": "postgres://prod",
		"db.password":  "file-pw",
		"db.user":      "",
		"api-token":    "t0k3n",
		"plain":        "hello",
		"broken":       "${missing}",
	}, upperSource{
		"DB_PASSWORD":      "env-pw",
		"DB_POOL_MAX_SIZE": "20",
		"PATH":             "/bin",
	}, gonf.WithProfiles("dev"), gonf.WithMappings(gonf.NewMapping[explainDB]("db")))
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
			name:          "references to secrets are masked",
			key:           "db.url",
			expectedValue: "postgres://u:****@dev/app",
			expectedString: "db.url = postgres://u:****@dev/app (file:app.properties, profile dev)\n" +
				"  overrides postgres://u:${db.password}@localhost/app (file:app.properties)",
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
			expectedString: "db.pool.max-size = 20 (test:upper)\n  default 10 (mapping:db)",
		},
		{
			name:           "default only",
			key:            "db.name",
			expectedValue:  "app",
			expectedString: "db.name = app (mapping:db)",
		},
		{
			name:           "empty value uses default",
			key:            "db.user",
			expectedValue:  "admin",
			expectedString: `db.user = admin (mapping:db)` + "\n" + `  empty "" (file:app.properties)`,
		},
		{
			name:           "secret default",
			key:            "db.token",
			expectedValue:  "****",
			expectedSecret: true,
			expectedString: "db.token = **** (mapping:db)",
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
			if got := e.String(); got != tt.expectedString {
				t.Errorf("String =\n%s\nwant\n%s", got, tt.expectedString)
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
	expected := "api-token,broken,db.name,db.password,db.pool.max-size,db.token,db.url,db.user,plain"
	if got := strings.Join(keys, ","); got != expected {
		t.Errorf("keys = %s, want %s", got, expected)
	}
	for _, leak := range []string{"env-pw", "file-pw", "t0k3n", "dflt", "/bin"} {
		if strings.Contains(all.String(), leak) {
			t.Errorf("Explanations reveal %q:\n%s", leak, all.String())
		}
	}
}
