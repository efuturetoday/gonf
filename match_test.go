package gonf_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/memory"
)

// envStyle is a MappedSource with the names and rules of the environment,
// so tests do not need the process environment.
type envStyle map[string]string

func (s envStyle) LoadMapped(ctx context.Context, keys []string) (map[string]gonf.Value, error) {
	values, err := s.Load(ctx)
	return env.Match(values, keys), err
}

func (s envStyle) Name() string             { return "env:" }
func (s envStyle) Ordinal() int             { return env.Ordinal }
func (s envStyle) MapKey(key string) string { return env.MapKey(key) }
func (s envStyle) Load(context.Context) (map[string]gonf.Value, error) {
	out := make(map[string]gonf.Value, len(s))
	for k, v := range s {
		out[k] = gonf.Value{Raw: v, Origin: gonf.Origin{Location: "env:" + k}}
	}
	return out, nil
}

type matchAddress struct {
	Street string `default:""`
}

type matchUser struct {
	Name    string `default:""`
	Roles   []string
	Address matchAddress
}

type matchServer struct {
	Host string
	Port int `default:"80"`
}

type matchConfig struct {
	URL     string `default:""`
	Users   map[string]matchUser
	Labels  map[string]string
	Hosts   []string
	Servers []matchServer
}

func loadMatch(t *testing.T, file map[string]string, environ envStyle, opts ...gonf.Option) (*gonf.Config, matchConfig) {
	t.Helper()
	mapping := gonf.NewMapping[matchConfig]("x")
	cfg, err := gonf.Load(context.Background(), append(opts,
		gonf.WithSources(memory.New(file, source.WithName("file:app.properties"), source.WithOrdinal(100)), environ),
		gonf.WithMappings(mapping),
	)...)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, mapping.Get(cfg)
}

func TestLoad_MatchesEnvironmentNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		file         map[string]string
		environ      envStyle
		profiles     []string
		expected     matchConfig
		expectedKeys []string
	}{
		{
			name:         "fixed key",
			environ:      envStyle{"X_URL": "env"},
			expected:     matchConfig{URL: "env"},
			expectedKeys: []string{"x.url"},
		},
		{
			name:         "map entry",
			environ:      envStyle{"X_USERS_ALICE_ROLES": "a,b"},
			expected:     matchConfig{Users: map[string]matchUser{"alice": {Roles: []string{"a", "b"}}}},
			expectedKeys: []string{"x.users.alice.roles"},
		},
		{
			name:         "map entry with a name containing _ and a value of several segments",
			environ:      envStyle{"X_USERS_BOB_JR_ADDRESS_STREET": "main", "X_USERS_BOB_JR_NAME": "Bob"},
			expected:     matchConfig{Users: map[string]matchUser{"bob_jr": {Name: "Bob", Address: matchAddress{Street: "main"}}}},
			expectedKeys: []string{"x.users.bob_jr.address.street", "x.users.bob_jr.name"},
		},
		{
			name:         "quoted name between double underscores",
			environ:      envStyle{"X_USERS__ALICE_SMITH__ROLES": "r"},
			expected:     matchConfig{Users: map[string]matchUser{"alice_smith": {Roles: []string{"r"}}}},
			expectedKeys: []string{`x.users."alice_smith".roles`},
		},
		{
			name:         "map entry with indexed list",
			environ:      envStyle{"X_USERS_ALICE_ROLES_1_": "b", "X_USERS_ALICE_ROLES_0_": "a"},
			expected:     matchConfig{Users: map[string]matchUser{"alice": {Roles: []string{"a", "b"}}}},
			expectedKeys: []string{"x.users.alice.roles[0]", "x.users.alice.roles[1]"},
		},
		{
			name:         "map of single values",
			environ:      envStyle{"X_LABELS_TEAM_A": "core"},
			expected:     matchConfig{Labels: map[string]string{"team_a": "core"}},
			expectedKeys: []string{"x.labels.team_a"},
		},
		{
			name:    "list indices with a gap",
			environ: envStyle{"X_HOSTS_3_": "d", "X_HOSTS_0_": "a", "X_SERVERS_2__HOST": "s"},
			expected: matchConfig{
				Hosts:   []string{"a", "d"},
				Servers: []matchServer{{Host: "s", Port: 80}},
			},
			expectedKeys: []string{"x.hosts[0]", "x.hosts[3]", "x.servers[2].host"},
		},
		{
			// The environment does not see the names in the file, so
			// alice-smith and alice_smith are two entries.
			name:    "same entry as in a file",
			file:    map[string]string{"x.users.alice.name": "Alice", `x.users."alice-smith".name`: "Smith"},
			environ: envStyle{"X_USERS_ALICE_ROLES": "a", "X_USERS__ALICE_SMITH__ROLES": "s"},
			expected: matchConfig{Users: map[string]matchUser{
				"alice":       {Name: "Alice", Roles: []string{"a"}},
				"alice-smith": {Name: "Smith"},
				"alice_smith": {Roles: []string{"s"}},
			}},
			expectedKeys: []string{
				`x.users."alice-smith".name`, `x.users."alice_smith".roles`, "x.users.alice.name", "x.users.alice.roles",
			},
		},
		{
			name:         "environment wins over a file",
			file:         map[string]string{"x.users.alice.name": "file", "x.hosts[1]": "file"},
			environ:      envStyle{"X_USERS_ALICE_NAME": "env", "X_HOSTS_1_": "env"},
			expected:     matchConfig{Users: map[string]matchUser{"alice": {Name: "env"}}, Hosts: []string{"env"}},
			expectedKeys: []string{"x.hosts[1]", "x.users.alice.name"},
		},
		{
			name:         "profile",
			environ:      envStyle{"_DEV_X_USERS_CAROL_NAME": "dev", "_PROD_X_USERS_DAN_NAME": "prod"},
			profiles:     []string{"dev"},
			expected:     matchConfig{Users: map[string]matchUser{"carol": {Name: "dev"}}},
			expectedKeys: []string{"x.users.carol.name"},
		},
		{
			name:     "unrelated names ignored",
			environ:  envStyle{"PATH": "/bin", "HOME": "/home/a", "X_USERS": "no entry", "X_USERSX_A_NAME": "other key", "X_HOSTS_A_": "no index"},
			expected: matchConfig{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, got := loadMatch(t, tt.file, tt.environ, gonf.WithProfiles(tt.profiles...))
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %+v, want %+v", got, tt.expected)
			}
			if keys := cfg.Keys(); !slices.Equal(keys, tt.expectedKeys) {
				t.Errorf("Keys = %q, want %q", keys, tt.expectedKeys)
			}
		})
	}
}

func TestExplain_EnvironmentEntry(t *testing.T) {
	t.Parallel()
	cfg, _ := loadMatch(t, map[string]string{"x.users.alice.name": "file"}, envStyle{"X_USERS_ALICE_NAME": "env", "X_USERS_BOB_NAME": "bob"})
	tests := []struct {
		key              string
		expectedValue    string
		expectedLocation string
		expectedOverride int
	}{
		{key: "x.users.alice.name", expectedValue: "env", expectedLocation: "env:X_USERS_ALICE_NAME", expectedOverride: 1},
		{key: "x.users.bob.name", expectedValue: "bob", expectedLocation: "env:X_USERS_BOB_NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			e := cfg.Explain(tt.key)
			if e.Err != nil || e.Value != tt.expectedValue || e.Origin.Location != tt.expectedLocation || e.Origin.Source != "env:" {
				t.Errorf("Explain = %+v, want %q from %s", e, tt.expectedValue, tt.expectedLocation)
			}
			if len(e.Candidates) != tt.expectedOverride+1 {
				t.Errorf("Candidates = %v, want %d", e.Candidates, tt.expectedOverride+1)
			}
			if e.Property == nil || e.Property.Key != "x.users.<name>.name" || e.Property.Env != env.MapKey(tt.key) {
				t.Errorf("Property = %+v, want x.users.<name>.name with Env %s", e.Property, env.MapKey(tt.key))
			}
		})
	}
	var keys []string
	for _, e := range cfg.Explanations() {
		keys = append(keys, e.Key)
	}
	// Keys with a default are explained too.
	expected := []string{"x.url", "x.users.alice.address.street", "x.users.alice.name", "x.users.bob.address.street", "x.users.bob.name"}
	if !slices.Equal(keys, expected) {
		t.Errorf("Explanations keys = %q, want %q", keys, expected)
	}
}

func TestGet_UnmatchedName(t *testing.T) {
	t.Parallel()
	cfg, _ := loadMatch(t, nil, envStyle{"HOME": "/home/a"})
	if _, err := cfg.Get("HOME"); !errors.Is(err, gonf.ErrNotFound) {
		t.Errorf("Get(HOME) error = %v, want ErrNotFound", err)
	}
}
