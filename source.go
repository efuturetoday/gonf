package gonf

import (
	"context"
	"strings"
)

// Value is a single raw configuration value as delivered by one Source.
//
// All values are strings, independent of where they come from. Conversion
// to typed values happens only when reading, so a value from a file and an
// override from the environment always go through the same path.
type Value struct {
	// Raw is the unconverted value.
	Raw    string
	Origin Origin
}

// Origin describes where a Value came from.
type Origin struct {
	// Source is the name of the Source, e.g. "env:" or
	// "file:/etc/app/app.properties".
	Source string
	// Ordinal is the priority of the Source. Higher wins.
	Ordinal int
	// Profile is the profile the value belongs to, e.g. "dev" for a value
	// from a "%dev.db.url" key. Empty for a base value.
	Profile string
	// Location is an optional, more precise position such as
	// "file:/etc/app/app.properties:12".
	Location string
}

// Source delivers configuration values.
//
// Name identifies the Source and must start with a scheme naming the kind
// of origin, e.g. "file:/etc/app.properties", "env:" or
// "k8s:configmap/my-namespace/app-config" (see Scheme).
//
// Load returns flat keys (e.g. "db.pool.max-size"). A key prefixed with
// "%profile." (e.g. "%dev.db.url") belongs to that profile. Sources only need
// to set Value.Raw and optionally Value.Origin.Location; the other Origin
// fields are filled in by gonf.
type Source interface {
	Name() string
	Ordinal() int
	Load(ctx context.Context) (map[string]Value, error)
}

// MappedSource is a Source whose names are not keys, such as environment
// variables. Load calls LoadMapped instead of Load, with the keys it knows:
// the keys of the Mappings given with WithMappings and the key of
// WithProfileKey. Other names are not reachable through such a Source. In a key, <name> stands for the name of a map entry and
// <n> for the index of a list element, as in db.pools.<name>.url; a list
// of single values comes as db.hosts and db.hosts[<n>]. LoadMapped returns
// its values under the keys their names match, with <name> and <n> filled
// in, such as db.pools.main.url for DB_POOLS_MAIN_URL, and "%profile.key"
// for a value of a profile. Names that match no key are left out.
type MappedSource interface {
	Source
	LoadMapped(ctx context.Context, keys []string) (map[string]Value, error)
}

// KeyMapper is implemented by Sources whose names follow their own naming
// scheme, such as the environment. MapKey(key) is the name of key in that
// scheme, e.g. "DB_POOL_MAX_SIZE" for "db.pool.max-size". Describe and
// Explain show it as Property.Env; values are never looked up by it.
type KeyMapper interface {
	MapKey(key string) string
}

// VerbatimSource is implemented by Sources whose values are text, never
// expressions, such as values others may write. If IsVerbatim reports true,
// Load escapes every value (see Escape), so ${file::...} or ${env::...} in
// it stays as written and calls no Function. Lookup and Explain show the
// escaped value, Get the value as the Source delivered it.
type VerbatimSource interface {
	IsVerbatim() bool
}

// Scheme returns the scheme of a Source name, the part before the first
// ':', and whether name has a valid scheme. A valid scheme has at least two
// characters, starts with a letter and contains only letters, digits, '+',
// '-' and '.' (RFC 3986). The two-character minimum keeps Windows paths
// such as "C:\app.properties" from passing as a scheme.
func Scheme(name string) (string, bool) {
	scheme, _, ok := strings.Cut(name, ":")
	if !ok || len(scheme) < 2 || !isLetter(scheme[0]) {
		return "", false
	}
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		isDigit := '0' <= c && c <= '9'
		if !isLetter(c) && !isDigit && strings.IndexByte("+-.", c) < 0 {
			return "", false
		}
	}
	return scheme, true
}

func isLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
