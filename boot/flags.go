package boot

import (
	"flag"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/flagsource"
)

// WithFlags makes Load read the flags of fs that were set, with ordinal
// flagsource.Ordinal (400), above the environment. boot does not parse fs;
// the application calls fs.Parse before Load. See flagsource.Define for
// defining a flag per key.
func WithFlags(fs *flag.FlagSet) Option {
	return With(gonf.WithSources(flagsource.New(fs)))
}
