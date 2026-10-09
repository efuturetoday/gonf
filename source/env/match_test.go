package env

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/efuturetoday/gonf"
)

func TestMatch(t *testing.T) {
	t.Parallel()
	keys := []string{
		"db.url",
		"db.pools.<name>.url",
		"db.pools.<name>.address.street",
		"db.users.<name>.roles", "db.users.<name>.roles[<n>]",
		"db.hosts", "db.hosts[<n>]",
		"db.servers[<n>].host",
		"db.labels.<name>",
		"db.lists.<name>", "db.lists.<name>[<n>]",
		"db.nested.<name>.inner.<name>.x",
		"db.same-name", "db.same.name",
	}
	tests := []struct {
		name     string
		expected string // "" for no match
	}{
		{name: "DB_URL", expected: "db.url"},
		{name: "DB_POOLS_MAIN_URL", expected: "db.pools.main.url"},
		{name: "DB_POOLS_EU_WEST_URL", expected: "db.pools.eu_west.url"},
		{name: "DB_POOLS__EU_WEST__URL", expected: `db.pools."eu_west".url`},
		{name: "DB_POOLS_MAIN_ADDRESS_STREET", expected: "db.pools.main.address.street"},
		{name: "DB_USERS_ALICE_ROLES", expected: "db.users.alice.roles"},
		{name: "DB_USERS_ALICE_ROLES_2_", expected: "db.users.alice.roles[2]"},
		{name: "DB_HOSTS", expected: "db.hosts"},
		{name: "DB_HOSTS_10_", expected: "db.hosts[10]"},
		{name: "DB_SERVERS_1__HOST", expected: "db.servers[1].host"},
		{name: "DB_LABELS_TEAM", expected: "db.labels.team"},
		{name: "DB_LISTS_A_0_", expected: "db.lists.a[0]"},
		{name: "DB_NESTED_A_INNER_B_X", expected: "db.nested.a.inner.b.x"},
		{name: "_DEV_DB_URL", expected: "%dev.db.url"},
		{name: "_MY_ENV_DB_POOLS_MAIN_URL", expected: "%my_env.db.pools.main.url"},
		{name: "DB_SAME_NAME", expected: "db.same-name"},
		{name: "DB_POOLS_main_URL"},
		{name: "DB_HOSTS_A_"},
		{name: "DB_POOLS_URL"},
		{name: "PATH"},
		{name: "db.url"},
		{name: "_DEV_"},
	}
	values := map[string]gonf.Value{}
	for _, tt := range tests {
		values[tt.name] = gonf.Value{Raw: tt.name}
	}
	matched, unmatched := match(values, keys)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.expected == "" {
				if !slices.Contains(unmatched, tt.name) {
					t.Errorf("%s matched, want unmatched", tt.name)
				}
				return
			}
			if v, ok := matched[tt.expected]; !ok || v.Raw != tt.name {
				t.Errorf("%s = %q, want the value of %s", tt.expected, v.Raw, tt.name)
			}
		})
	}
	if len(matched)+len(unmatched) != len(values) {
		t.Errorf("matched %v and unmatched %v, want every name once", slices.Sorted(maps.Keys(matched)), unmatched)
	}
}

// Not parallel: t.Setenv modifies the process environment.
func TestNew_LoadMapped(t *testing.T) {
	t.Setenv("GONF_TEST_DB_URL", "u")
	t.Setenv("GONF_TEST_POOLS_A_SIZE", "5")
	src, ok := New().(gonf.MappedSource)
	if !ok {
		t.Fatal("env Source is no gonf.MappedSource")
	}
	got, err := src.LoadMapped(context.Background(), []string{"gonf-test.db.url", "gonf-test.pools.<name>.size"})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"gonf-test.db.url", "gonf-test.pools.a.size"}
	if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, expected) {
		t.Errorf("keys = %q, want %q", keys, expected)
	}
	if loc := got["gonf-test.db.url"].Origin.Location; loc != "env:GONF_TEST_DB_URL" {
		t.Errorf("Location = %q, want env:GONF_TEST_DB_URL", loc)
	}
}

// FuzzMatch checks that Match never panics and returns a name only under
// a key whose name it is.
func FuzzMatch(f *testing.F) {
	f.Add("DB_POOLS__EU_WEST__URL", "db.pools.<name>.url")
	f.Add("_DEV_DB_HOSTS_0_", "db.hosts[<n>]")
	f.Add("A_B_C", "a.<name>.<name>")
	f.Fuzz(func(t *testing.T, name, key string) {
		for k, v := range Match(map[string]gonf.Value{name: {Raw: name}}, []string{key, "db.url"}) {
			if MapKey(k) != v.Raw {
				t.Errorf("%q matched %q, whose name is %q", v.Raw, k, MapKey(k))
			}
		}
	})
}
