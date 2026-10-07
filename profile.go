package gonf

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// activeProfiles returns the valid, distinct profile names, strongest
// (last given) first.
func activeProfiles(names []string) ([]string, error) {
	var out []string
	for _, name := range slices.Backward(names) {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(out, name) {
			continue
		}
		if !validProfile(name) {
			return nil, fmt.Errorf("invalid profile name %q", name)
		}
		out = append(out, name)
	}
	return out, nil
}

// splitProfileKey splits "%dev.db.url" into "dev" and "db.url".
func splitProfileKey(key string) (profile, base string, err error) {
	profile, base, ok := strings.Cut(key[1:], ".")
	if !ok || !validProfile(profile) || base == "" {
		return "", "", fmt.Errorf("invalid profile key %q", key)
	}
	return profile, base, nil
}

// validProfile reports whether name can be used in a "%name." prefix. ','
// is reserved for keys of several profiles such as "%dev,test.x".
// Whitespace is rejected because WithProfiles trims names, so a profile
// such as " dev" could never become active.
//
// source.WithProfile applies the same rule; keep both in sync.
func validProfile(name string) bool {
	return name != "" && !strings.ContainsAny(name, ".,") && strings.IndexFunc(name, unicode.IsSpace) < 0
}
