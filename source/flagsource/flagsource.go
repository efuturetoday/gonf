// Package flagsource provides command-line flags as a gonf Source (New) and
// defines the flags of an application's Mappings (Define).
//
// The flag name is the key itself, so -db.url=postgres://x sets db.url. Only
// flags that were set on the command line count, not the defaults of the
// flags. The package is named flagsource so that it does not clash with the
// standard flag package it uses.
package flagsource

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Ordinal is the default ordinal of the flag Source. It is above the
// environment (300), so that a flag overrides an environment variable.
const Ordinal = 400

type flagSource struct {
	source.Meta
	fs *flag.FlagSet
}

// New returns a Source with the flags of fs that were set, named "flag:".
// Parse fs before Load; Load does not parse anything. A flag that was not
// set yields no key, so it never hides a value of another Source.
func New(fs *flag.FlagSet, opts ...source.Option) gonf.Source {
	return &flagSource{Meta: source.NewMeta("flag:", Ordinal, opts...), fs: fs}
}

func (s *flagSource) Load(context.Context) (map[string]gonf.Value, error) {
	out := map[string]gonf.Value{}
	s.fs.Visit(func(f *flag.Flag) {
		out[f.Name] = gonf.Value{Raw: f.Value.String(), Origin: gonf.Origin{Location: "flag:-" + f.Name}}
	})
	return out, nil
}

// Define defines one string flag on fs for every key of props, such as the
// result of gonf.Describe or boot.Describe. The usage text is the
// description of the key; the default of the key is shown by
// fs.PrintDefaults. The default of a secret is never shown.
//
// Keys with <name> or <n> are skipped, as a flag cannot be defined for
// them; they can still be set by other Sources. A key of type bool (or
// *bool) gets a bool flag, so -debug alone means -debug=true; -debug=false
// and the other spellings gonf reads, such as -debug=no, work too. All other
// flags are strings, as gonf converts the type when it binds. Define returns an error, and defines
// nothing, if a flag with the name of a key already exists.
func Define(fs *flag.FlagSet, props []gonf.Property) error {
	var fixed []gonf.Property
	for _, p := range props {
		if strings.Contains(p.Key, "<") {
			continue
		}
		if fs.Lookup(p.Key) != nil {
			return fmt.Errorf("flag -%s is already defined", p.Key)
		}
		fixed = append(fixed, p)
	}
	for _, p := range fixed {
		def := ""
		if p.Default != nil && !p.Secret {
			def = *p.Default
		}
		usage := p.Doc
		if p.Secret {
			usage = strings.TrimSpace(usage + " (secret)")
		}
		if p.Type == "bool" || p.Type == "*bool" {
			fs.Var(&boolValue{raw: def}, p.Key, usage)
			continue
		}
		fs.String(p.Key, def, usage)
	}
	return nil
}

// boolValue is a flag that may be given without a value. It keeps the text,
// as gonf checks and converts it when it binds.
type boolValue struct{ raw string }

func (b *boolValue) String() string {
	if b == nil {
		return ""
	}
	return b.raw
}

func (b *boolValue) Set(s string) error {
	b.raw = s
	return nil
}

// IsBoolFlag lets the flag package accept -name without a value as -name=true.
func (b *boolValue) IsBoolFlag() bool { return true }
