package gonf

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SourceInfo describes a loaded Source, see Config.Sources.
type SourceInfo struct {
	Name    string `json:"name"`
	Ordinal int    `json:"ordinal"`
	// Profile is the profile every key of the Source belongs to (see
	// ProfileSource), empty if the Source is not bound to one.
	Profile string `json:"profile,omitempty"`
	// Keys is the number of keys the Source delivered, profile keys
	// included.
	Keys int `json:"keys"`
	// IsOptional reports that a missing document is not an error (see
	// OptionalSource), IsMissing that the document was missing: the Source
	// delivered nothing.
	IsOptional bool `json:"optional,omitempty"`
	IsMissing  bool `json:"missing,omitempty"`
}

// ProfileSource is implemented by Sources whose keys all belong to one
// profile, such as the files of source.WithProfile.
type ProfileSource interface {
	Profile() string
}

// OptionalSource is implemented by Sources that may have nothing to read,
// such as a file made source.Optional. IsMissing reports whether the last
// Load found nothing; it is only asked right after Load.
type OptionalSource interface {
	IsOptional() bool
	IsMissing() bool
}

// NameReporter is a MappedSource that also tells which of its names look
// meant for a Mapping but match no key. prefixes are the prefixes of the
// Mappings, such as "db"; the Source decides how they look in its naming
// scheme, e.g. DB_ in the environment, and returns the names that start
// like that and match none of keys. Load calls LoadReport instead of
// LoadMapped, and the names become Warnings.
type NameReporter interface {
	MappedSource
	LoadReport(ctx context.Context, keys, prefixes []string) (values map[string]Value, unknown []string, err error)
}

// WarningKind tells what a Warning is about.
type WarningKind string

const (
	// WarnUnknownName: a Source has a name under the prefix of a Mapping
	// that matches no key, such as DB_PORT when only DB_URL is read.
	// Typically a typo, but also what a platform adds, such as the
	// variables of a Kubernetes service link, so it is no error.
	WarnUnknownName WarningKind = "unknown-name"
	// WarnNameConflict: several keys have the same name in a Source, such
	// as db.max-size and db.max.size as DB_MAX_SIZE, so a value cannot
	// tell which one it is for.
	WarnNameConflict WarningKind = "name-conflict"
)

// Warning is something Load noticed that does not make the configuration
// invalid; see Config.Warnings.
type Warning struct {
	Kind WarningKind `json:"kind"`
	// Source is the name of the Source concerned.
	Source string `json:"source,omitempty"`
	// Name is the name in the Source: the unknown one, or the one the Keys
	// share.
	Name string `json:"name"`
	// Keys are the conflicting keys, sorted; empty for WarnUnknownName.
	Keys []string `json:"keys,omitempty"`
	// Message is the warning in words, with a suggestion if one is close.
	Message string `json:"message"`
}

// String returns the Message, with the Source in front.
func (w Warning) String() string {
	if w.Source == "" {
		return w.Message
	}
	return w.Source + " " + w.Message
}

// unknownWarnings returns a Warning for every name that src found no key
// for, with the closest name of a key as suggestion if src is a KeyMapper.
func unknownWarnings(src Source, unknown []string, in loadInput) []Warning {
	if len(unknown) == 0 {
		return nil
	}
	var candidates []string
	if m, ok := src.(KeyMapper); ok {
		for _, key := range in.keys() {
			if !strings.Contains(key, "<") {
				candidates = append(candidates, m.MapKey(key))
			}
		}
	}
	out := make([]Warning, 0, len(unknown))
	for _, name := range slices.Sorted(slices.Values(unknown)) {
		msg := name + " matches no key"
		if s := suggestName(name, candidates); s != "" {
			msg += ", did you mean " + s + "?"
		}
		out = append(out, Warning{Kind: WarnUnknownName, Source: src.Name(), Name: name, Message: msg})
	}
	return out
}

// suggestName returns the candidate closest to name, or "" if none is
// close. A name that starts with '_' belongs to a profile, as _DEV_DB_URL:
// it is also compared without the profile, and the profile is kept.
func suggestName(name string, candidates []string) string {
	best, bestDistance := "", maxSuggestDistance+1
	try := func(head, rest string) {
		for _, c := range candidates {
			if d := distance(rest, c); d < bestDistance {
				best, bestDistance = head+c, d
			}
		}
	}
	try("", name)
	if strings.HasPrefix(name, "_") {
		for i := 2; i < len(name)-1; i++ {
			if name[i] == '_' {
				try(name[:i+1], name[i+1:])
			}
		}
	}
	return best
}

// ErrNameConflict is wrapped by the error Describe returns for keys that
// have the same name in a Source, see WarnNameConflict.
var ErrNameConflict = errors.New("gonf: keys with the same name")

// nameConflict is a name that several keys share.
type nameConflict struct {
	name string
	keys []string
}

func (c nameConflict) String() string {
	return fmt.Sprintf("%s are all %s", strings.Join(c.keys, ", "), c.name)
}

// nameConflicts returns the names of props that several keys share, sorted
// by name.
func nameConflicts(props []Property) []nameConflict {
	byName := map[string][]string{}
	for _, p := range props {
		if p.Env != "" {
			byName[p.Env] = append(byName[p.Env], p.Key)
		}
	}
	var out []nameConflict
	for name, keys := range byName {
		if len(keys) > 1 {
			slices.Sort(keys)
			out = append(out, nameConflict{name, keys})
		}
	}
	slices.SortFunc(out, func(a, b nameConflict) int { return cmp.Compare(a.name, b.name) })
	return out
}

// conflictWarnings returns a Warning for every name that several keys of
// props share in src.
func conflictWarnings(props []Property, src Source) []Warning {
	if src == nil {
		return nil
	}
	var out []Warning
	for _, c := range nameConflicts(props) {
		out = append(out, Warning{
			Kind:    WarnNameConflict,
			Source:  src.Name(),
			Name:    c.name,
			Keys:    c.keys,
			Message: "keys " + c.String() + ", a value cannot tell which one it is for",
		})
	}
	return out
}

// conflictError returns the error Describe reports for props, nil if there
// is no conflict.
func conflictError(props []Property) error {
	var errs []error
	for _, c := range nameConflicts(props) {
		errs = append(errs, fmt.Errorf("%w: %s", ErrNameConflict, c))
	}
	return errors.Join(errs...)
}
