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
	// "file:/etc/app/application.properties".
	Source string
	// Ordinal is the priority of the Source. Higher wins.
	Ordinal int
	// Location is an optional, more precise position such as
	// "file:/etc/app/application.properties:12".
	Location string
}

// Source delivers configuration values.
//
// Name identifies the Source and must start with a scheme naming the kind
// of origin, e.g. "file:/etc/app.properties", "env:" or
// "k8s:configmap/my-namespace/app-config" (see Scheme).
//
// Load returns flat keys (e.g. "db.pool.max-size"). Sources only need to set
// Value.Raw and optionally Value.Origin.Location; Source and Ordinal of the
// Origin are filled in by gonf.Load.
type Source interface {
	Name() string
	Ordinal() int
	Load(ctx context.Context) (map[string]Value, error)
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
		if !isLetter(c) && !('0' <= c && c <= '9') && c != '+' && c != '-' && c != '.' {
			return "", false
		}
	}
	return scheme, true
}

func isLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
