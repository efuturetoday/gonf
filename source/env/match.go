package env

import (
	"regexp"
	"strings"

	"github.com/efuturetoday/gonf"
)

// Match returns values, whose keys are environment variable names, under
// the keys they match (see gonf.MappedSource). A name matches a key if it
// is MapKey(key): DB_URL matches db.url. Where the key has <name> or <n>,
// the name has the entry name or index: DB_POOLS_MAIN_URL matches
// db.pools.<name>.url as db.pools.main.url, DB_HOSTS_2_ matches
// db.hosts[<n>] as db.hosts[2]. An entry name is lower case; written
// between double underscores, as in DB_POOLS__EU_WEST__URL, it is quoted:
// db.pools."eu_west".url. A name that starts with _PROFILE_ matches the
// key of that profile: _DEV_DB_URL is %dev.db.url.
//
// Names that match no key are left out. If a name matches several keys,
// the key whose <name> and <n> parts are shortest wins, then the first.
//
// Pass Match to source.WithKeyMatcher for .env files.
func Match(values map[string]gonf.Value, keys []string) map[string]gonf.Value {
	matched, _ := match(values, keys)
	return matched
}

// match is Match that also returns the names that match no key.
func match(values map[string]gonf.Value, keys []string) (map[string]gonf.Value, []string) {
	m := newMatcher(keys)
	out := make(map[string]gonf.Value, len(values))
	var unmatched []string
	for name, v := range values {
		key, ok := m.match(name)
		if !ok {
			unmatched = append(unmatched, name)
			continue
		}
		out[key] = v
	}
	return out, unmatched
}

// matcher matches names against keys: fixed keys by their name, keys with
// <name> or <n> by a pattern.
type matcher struct {
	fixed    map[string]string
	patterns []pattern
}

// pattern matches the names of a key with <name> or <n>. prefix and suffix
// are the literal start and end of every such name, checked before re.
type pattern struct {
	re             *regexp.Regexp
	prefix, suffix string
	// parts are the literal parts of the key around its placeholders,
	// isName tells for each placeholder whether it is <name>.
	parts  []string
	isName []bool
}

func newMatcher(keys []string) *matcher {
	m := &matcher{fixed: make(map[string]string, len(keys))}
	for _, key := range keys {
		p, ok := compile(key)
		if !ok {
			if name := MapKey(key); m.fixed[name] == "" {
				m.fixed[name] = key
			}
			continue
		}
		m.patterns = append(m.patterns, p)
	}
	return m
}

// compile returns the pattern of key, false if key has no placeholder.
func compile(key string) (pattern, bool) {
	var p pattern
	var re strings.Builder
	re.WriteByte('^')
	rest := key
	for {
		i, isName, size := placeholder(rest)
		if i < 0 {
			break
		}
		p.parts = append(p.parts, rest[:i])
		p.isName = append(p.isName, isName)
		re.WriteString(regexp.QuoteMeta(MapKey(rest[:i])))
		if isName {
			re.WriteString("(.+)")
		} else {
			re.WriteString("([0-9]+)")
		}
		rest = rest[i+size:]
	}
	if len(p.parts) == 0 {
		return pattern{}, false
	}
	p.parts = append(p.parts, rest)
	re.WriteString(regexp.QuoteMeta(MapKey(rest)))
	re.WriteByte('$')
	p.prefix, p.suffix = MapKey(p.parts[0]), MapKey(rest)
	// The literal parts are quoted, so the pattern always compiles.
	p.re = regexp.MustCompile(re.String())
	return p, true
}

// placeholder returns the index of the first <name> or <n> in s, whether
// it is <name>, and its length; -1 if there is none.
func placeholder(s string) (int, bool, int) {
	n := strings.Index(s, "<n>")
	name := strings.Index(s, "<name>")
	switch {
	case name >= 0 && (n < 0 || name < n):
		return name, true, len("<name>")
	case n >= 0:
		return n, false, len("<n>")
	}
	return -1, false, 0
}

// match returns the key of name, or of the profile name starts with.
func (m *matcher) match(name string) (string, bool) {
	if key, ok := m.matchBase(name); ok {
		return key, true
	}
	if !strings.HasPrefix(name, "_") {
		return "", false
	}
	// _MY_ENV_DB_URL may be %my.env_db.url or %my_env.db.url; the
	// shorter profile that leaves a key wins.
	for i := 2; i < len(name)-1; i++ {
		if name[i] != '_' {
			continue
		}
		key, ok := m.matchBase(name[i+1:])
		key = "%" + strings.ToLower(name[1:i]) + "." + key
		if ok && MapKey(key) == name {
			return key, true
		}
	}
	return "", false
}

// matchBase returns the key of name without a profile.
func (m *matcher) matchBase(name string) (string, bool) {
	if key, ok := m.fixed[name]; ok {
		return key, true
	}
	best, bestLen := "", -1
	for _, p := range m.patterns {
		if len(name) <= len(p.prefix)+len(p.suffix) || !strings.HasPrefix(name, p.prefix) || !strings.HasSuffix(name, p.suffix) {
			continue
		}
		sm := p.re.FindStringSubmatch(name)
		if sm == nil {
			continue
		}
		key, size := p.fill(sm[1:])
		// A name with characters MapKey never writes, such as lower case
		// letters, does not match.
		if (bestLen < 0 || size < bestLen) && MapKey(key) == name {
			best, bestLen = key, size
		}
	}
	return best, bestLen >= 0
}

// fill returns the key of p with captures in place of its placeholders,
// and the length of the captures.
func (p pattern) fill(captures []string) (string, int) {
	var b strings.Builder
	size := 0
	for i, c := range captures {
		size += len(c)
		b.WriteString(p.parts[i])
		if p.isName[i] {
			c = entryName(c)
		}
		b.WriteString(c)
	}
	b.WriteString(p.parts[len(p.parts)-1])
	return b.String(), size
}

// entryName returns the map entry name of the part c of a name: lower
// case, and quoted if written between double underscores, as _EU_WEST_ in
// POOLS__EU_WEST__URL.
func entryName(c string) string {
	name := strings.ToLower(c)
	if len(c) > 2 && c[0] == '_' && c[len(c)-1] == '_' {
		return `"` + name[1:len(name)-1] + `"`
	}
	return name
}
