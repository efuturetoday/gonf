// Package env provides the process environment as a gonf Source (New) and
// the environment variable naming scheme (MapKey), which .env files use too
// (see format/dotenv).
package env

import (
	"context"
	"os"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Ordinal is the default ordinal of the env Source.
const Ordinal = 300

type envSource struct {
	source.Meta
}

var _ gonf.KeyMapper = (*envSource)(nil)

// New returns a Source with all process environment variables.
//
// Keys are kept as they are (e.g. "DB_URL"). The Source implements
// gonf.KeyMapper, so a lookup of "db.url" finds DB_URL (see MapKey).
func New(opts ...source.Option) gonf.Source {
	return &envSource{Meta: source.NewMeta("env:", Ordinal, opts...)}
}

// MapKey implements gonf.KeyMapper with the package function MapKey.
func (s *envSource) MapKey(key string) string { return MapKey(key) }

// MapKey returns the environment variable name for key: letters upper case,
// digits kept, every other character replaced by '_'. "db.pool.max-size"
// becomes "DB_POOL_MAX_SIZE", "%dev.db.url" becomes "_DEV_DB_URL" and
// "hosts[0]" becomes "HOSTS_0_", as in SmallRye Config. Pass it to
// source.WithKeyMapper for other Sources with environment-style keys.
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
