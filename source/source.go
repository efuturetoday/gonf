// Package source is the common base for gonf Sources.
//
// Document-based Sources are built from independent parts: a ReadFunc that
// delivers the bytes (where), a Format that parses them (how) and,
// optionally, a key naming scheme (WithKeyMatcher). New combines them.
// Providers live in subpackages (source/file,
// source/env, source/memory), formats in format/... packages.
package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"unicode"

	"github.com/efuturetoday/gonf"
)

// Format parses raw bytes into flat keys. name is used in error messages
// and Origin.Location.
type Format interface {
	Parse(name string, data []byte) (map[string]gonf.Value, error)
}

// FormatFunc adapts an ordinary function to the Format interface.
type FormatFunc func(name string, data []byte) (map[string]gonf.Value, error)

// Parse calls f(name, data).
func (f FormatFunc) Parse(name string, data []byte) (map[string]gonf.Value, error) {
	return f(name, data)
}

// ReadFunc delivers the raw content of a config document. Returning an error
// matching fs.ErrNotExist means "not present", which Optional() turns into
// an empty Source. While gonf.Watch watches the Source, the ReadFunc is
// also called to check for changes, possibly concurrently with Load.
type ReadFunc func(ctx context.Context) ([]byte, error)

type docSource struct {
	Meta
	read   ReadFunc
	format Format

	mu sync.Mutex
	// seen identifies the content last loaded or reported as changed.
	seen string
	// missing tells whether the last Load found an optional document
	// missing.
	missing bool
}

var _ gonf.OptionalSource = (*docSource)(nil)

// IsMissing implements gonf.OptionalSource.
func (s *docSource) IsMissing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.missing
}

var _ gonf.Watcher = (*docSource)(nil)

// New returns a Source that reads content with read and parses it with
// format. Reading and parsing happen on Load, not here. name identifies the
// Source in Origin and error messages.
func New(name string, read ReadFunc, format Format, opts ...Option) gonf.Source {
	s := &docSource{
		Meta:   NewMeta(name, DefaultOrdinal, opts...),
		read:   read,
		format: format,
	}
	if s.match != nil {
		return mappedDocSource{s}
	}
	return s
}

// mappedDocSource is a docSource with WithKeyMatcher. It is a separate type
// so that Sources without a matcher do not implement gonf.MappedSource.
type mappedDocSource struct {
	*docSource
}

var _ gonf.MappedSource = mappedDocSource{}

// LoadMapped implements gonf.MappedSource: it parses the content, matches
// the keys, then applies the profile.
func (s mappedDocSource) LoadMapped(ctx context.Context, keys []string) (map[string]gonf.Value, error) {
	values, err := s.parse(ctx)
	if err != nil {
		return nil, err
	}
	return s.withProfile(s.match(values, keys))
}

func (s *docSource) Load(ctx context.Context) (map[string]gonf.Value, error) {
	values, err := s.parse(ctx)
	if err != nil {
		return nil, err
	}
	return s.withProfile(values)
}

// parse reads and parses the content; a missing optional document has no
// values.
func (s *docSource) parse(ctx context.Context) (map[string]gonf.Value, error) {
	data, err := s.read(ctx)
	s.see(data, err)
	missing := err != nil && s.IsOptional() && errors.Is(err, fs.ErrNotExist)
	s.mu.Lock()
	s.missing = missing
	s.mu.Unlock()
	if err != nil {
		if missing {
			return map[string]gonf.Value{}, nil
		}
		return nil, err
	}
	// Tolerate a UTF-8 BOM written by some editors.
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	return s.format.Parse(s.Name(), data)
}

// withProfile applies the profile given with WithProfile to values.
func (s *docSource) withProfile(values map[string]gonf.Value) (map[string]gonf.Value, error) {
	if s.Profile() == "" {
		return values, nil
	}
	return withProfile(s.Profile(), values)
}

// Watch implements gonf.Watcher. Whenever the Trigger given with WithTrigger
// says so, it reads the content and calls changed if the content differs
// from what Load read last. A change is reported once, not on every check.
func (s *docSource) Watch(ctx context.Context, changed func()) error {
	trigger := s.trigger
	if trigger == nil {
		trigger = Every(DefaultPollInterval)
	}
	return trigger(ctx, func() {
		data, err := s.read(ctx)
		if ctx.Err() == nil && s.see(data, err) {
			changed()
		}
	})
}

// see records the result of reading the content and reports whether it
// differs from the previous one. A missing document and a read error count
// as content too, so a file that appears or becomes readable is a change.
func (s *docSource) see(data []byte, err error) bool {
	var id string
	switch {
	case errors.Is(err, fs.ErrNotExist):
		id = "missing"
	case err != nil:
		id = "error: " + err.Error()
	default:
		sum := sha256.Sum256(data)
		id = string(sum[:])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	isChanged := s.seen != id
	s.seen = id
	return isChanged
}

// withProfile prefixes every key with "%profile.". The name rule matches
// gonf's: no '.', ',' or whitespace. A key with a profile of its own, such
// as %dev.db.url or _DEV_DB_URL in a .env file, is an error.
func withProfile(profile string, values map[string]gonf.Value) (map[string]gonf.Value, error) {
	if strings.ContainsAny(profile, ".,") || strings.IndexFunc(profile, unicode.IsSpace) >= 0 {
		return nil, fmt.Errorf("invalid profile name %q", profile)
	}
	out := make(map[string]gonf.Value, len(values))
	for k, v := range values {
		if strings.HasPrefix(k, "%") {
			return nil, fmt.Errorf("key %q has a profile, but every key of the source belongs to profile %q", k, profile)
		}
		out["%"+profile+"."+k] = v
	}
	return out, nil
}
