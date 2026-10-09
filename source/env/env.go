// Package env provides the process environment as a gonf Source (New), the
// environment variable naming scheme (MapKey) and the matching of variable
// names against keys (Match), which .env files use too (see format/dotenv).
package env

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Ordinal is the default ordinal of the env Source.
const Ordinal = 300

type envSource struct {
	source.Meta
}

var (
	_ gonf.MappedSource = (*envSource)(nil)
	_ gonf.KeyMapper    = (*envSource)(nil)
	_ gonf.NameReporter = (*envSource)(nil)
)

// New returns a Source with the process environment variables.
//
// gonf.Load calls LoadMapped, which returns the variables under the keys
// they match (see Match): DB_URL as db.url, DB_POOLS_MAIN_URL as
// db.pools.main.url. Variables that match no key, such as PATH, are left
// out. Load returns all variables under their names.
func New(opts ...source.Option) gonf.Source {
	return &envSource{Meta: source.NewMeta("env:", Ordinal, opts...)}
}

// MapKey implements gonf.KeyMapper with the package function MapKey, so
// Describe shows the variable of a key.
func (s *envSource) MapKey(key string) string { return MapKey(key) }

// MapKey returns the environment variable name for key: letters upper case,
// digits kept, every other character replaced by '_'. "db.pool.max-size"
// becomes "DB_POOL_MAX_SIZE", "%dev.db.url" becomes "_DEV_DB_URL" and
// "hosts[0]" becomes "HOSTS_0_".
func MapKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		switch {
		case 'a' <= r && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case 'A' <= r && r <= 'Z', '0' <= r && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// LoadMapped implements gonf.MappedSource: Load, then Match.
func (s *envSource) LoadMapped(ctx context.Context, keys []string) (map[string]gonf.Value, error) {
	values, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	return Match(values, keys), nil
}

// LoadReport implements gonf.NameReporter. A name is unknown if it starts
// with MapKey(prefix)+"_" for a prefix, or with that after a profile, as
// _DEV_DB_ for db, and matches no key: DB_PORT for the Mapping "db", when
// it has no port. Such a variable is often a typo, but a Kubernetes
// service named db also sets DB_PORT and DB_SERVICE_HOST.
func (s *envSource) LoadReport(ctx context.Context, keys, prefixes []string) (map[string]gonf.Value, []string, error) {
	values, err := s.Load(ctx)
	if err != nil {
		return nil, nil, err
	}
	matched, unmatched := match(values, keys)
	return matched, underPrefixes(unmatched, prefixes), nil
}

// underPrefixes returns the names that start with the environment form of
// one of prefixes followed by '_', also after a profile; sorted.
func underPrefixes(names, prefixes []string) []string {
	var out []string
	for _, name := range names {
		for _, p := range prefixes {
			if isUnder(name, MapKey(p)+"_") {
				out = append(out, name)
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

// isUnder reports whether name starts with prefix, or with _PROFILE_ and
// then prefix.
func isUnder(name, prefix string) bool {
	if strings.HasPrefix(name, prefix) {
		return true
	}
	if !strings.HasPrefix(name, "_") {
		return false
	}
	for i := 2; i < len(name)-1; i++ {
		if name[i] == '_' && strings.HasPrefix(name[i+1:], prefix) {
			return true
		}
	}
	return false
}

func (s *envSource) Load(context.Context) (map[string]gonf.Value, error) {
	environ := os.Environ()
	out := make(map[string]gonf.Value, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		out[k] = gonf.Value{Raw: v, Origin: gonf.Origin{Location: "env:" + k}}
	}
	return out, nil
}
