// Package env provides a gonf Source with the process environment.
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

// New returns a Source with all process environment variables.
//
// Keys are passed through unchanged (e.g. "DB_URL"); mapping them to
// property keys like "db.url" is not done here.
func New(opts ...source.Option) gonf.Source {
	return &envSource{Meta: source.NewMeta("env:", Ordinal, opts...)}
}

func (s *envSource) Load(context.Context) (map[string]gonf.Value, error) {
	environ := os.Environ()
	out := make(map[string]gonf.Value, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		out[k] = gonf.Value{Raw: v}
	}
	return out, nil
}
