// Package source is the common base for gonf Sources.
//
// Document-based Sources are built from independent parts: a ReadFunc that
// delivers the bytes (where), a Format that parses them (how) and,
// optionally, a key naming scheme (WithKeyMapper). New combines them. Providers live in subpackages (source/file,
// source/env, source/memory), formats in format/... packages.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
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
// an empty Source.
type ReadFunc func(ctx context.Context) ([]byte, error)

type docSource struct {
	Meta
	read   ReadFunc
	format Format
}

// New returns a Source that reads content with read and parses it with
// format. Reading and parsing happen on Load, not here. name identifies the
// Source in Origin and error messages.
func New(name string, read ReadFunc, format Format, opts ...Option) gonf.Source {
	s := &docSource{
		Meta:   NewMeta(name, DefaultOrdinal, opts...),
		read:   read,
		format: format,
	}
	if s.mapKey != nil {
		return mappedDocSource{s}
	}
	return s
}

// mappedDocSource is a docSource with WithKeyMapper. It is a separate type
// so that Sources without a mapper do not implement gonf.KeyMapper.
type mappedDocSource struct {
	*docSource
}

var _ gonf.KeyMapper = mappedDocSource{}

func (s mappedDocSource) MapKey(key string) string { return s.mapKey(key) }

func (s *docSource) Load(ctx context.Context) (map[string]gonf.Value, error) {
	data, err := s.read(ctx)
	if err != nil {
		if s.IsOptional() && errors.Is(err, fs.ErrNotExist) {
			return map[string]gonf.Value{}, nil
		}
		return nil, err
	}
	// Tolerate a UTF-8 BOM written by some editors.
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	values, err := s.format.Parse(s.Name(), data)
	if err != nil || s.Profile() == "" {
		return values, err
	}
	return withProfile(s.Profile(), values)
}

// withProfile prefixes every key with "%profile.". The name rule matches
// gonf's: no '.', ',' or whitespace.
func withProfile(profile string, values map[string]gonf.Value) (map[string]gonf.Value, error) {
	if strings.ContainsAny(profile, ".,") || strings.IndexFunc(profile, unicode.IsSpace) >= 0 {
		return nil, fmt.Errorf("invalid profile name %q", profile)
	}
	out := make(map[string]gonf.Value, len(values))
	for k, v := range values {
		out["%"+profile+"."+k] = v
	}
	return out, nil
}
