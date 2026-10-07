// Package boot loads configuration by convention, like
// ConfigProvider.getConfig() in MicroProfile Config and Quarkus.
//
// Load reads, highest priority first:
//
//	300  environment variables (env.New)
//	295  .env in the working directory (format/dotenv, env key names)
//	260  config/app.<ext> in the working directory
//	250  app.<ext> in an embedded file system (WithFS)
//
// for every known extension ("properties" and "json", more with
// WithFormat). All files are optional. The active profiles come from the
// key gonf.profile, e.g. GONF_PROFILE=dev,local; for each of them Load also
// reads app-<profile>.<ext> next to every app.<ext>, with the same ordinal.
// Files of inactive profiles are never read.
//
// boot is extended from outside: WithFormat adds formats, WithFS a packaged
// default configuration, and With passes any gonf.Option through, e.g.
// more Sources or SourceFactories.
package boot

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/format/dotenv"
	"github.com/efuturetoday/gonf/format/json"
	"github.com/efuturetoday/gonf/format/properties"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/env"
	"github.com/efuturetoday/gonf/source/file"
)

const (
	// BaseName is the name of the configuration files without extension.
	BaseName = "app"
	// ProfileKey lists the active profiles, e.g. "dev,local".
	ProfileKey = "gonf.profile"
	// ConfigDir is the directory below the working directory that holds
	// the configuration files.
	ConfigDir = "config"
	// DotenvFile is the .env file in the working directory.
	DotenvFile = ".env"
)

// Ordinals of the files boot reads. Environment variables keep env.Ordinal
// (300).
const (
	// DotenvOrdinal is just below the environment, as in Quarkus.
	DotenvOrdinal    = 295
	ConfigDirOrdinal = 260
	EmbeddedOrdinal  = 250
)

// Option configures Load.
type Option func(*options)

type options struct {
	formats  []format
	embedded fs.FS
	core     []gonf.Option
}

type format struct {
	ext    string
	format source.Format
}

// WithFormat makes Load read app.<ext> files with format, e.g.
// WithFormat("yaml", yaml.Format). A known extension is replaced.
func WithFormat(ext string, f source.Format) Option {
	ext = strings.TrimPrefix(ext, ".")
	return func(o *options) {
		for i := range o.formats {
			if o.formats[i].ext == ext {
				o.formats[i].format = f
				return
			}
		}
		o.formats = append(o.formats, format{ext: ext, format: f})
	}
}

// WithFS makes Load read app.<ext> from the root of fsys, typically an
// embed.FS with default configuration packaged into the binary. Use fs.Sub
// for a subdirectory.
func WithFS(fsys fs.FS) Option {
	return func(o *options) { o.embedded = fsys }
}

// With passes options to gonf.Load, after the ones of boot. Sources added
// this way count as registered after the files of boot; WithProfiles wins
// over the profile key.
func With(opts ...gonf.Option) Option {
	return func(o *options) { o.core = append(o.core, opts...) }
}

// Load loads the configuration by convention, see the package
// documentation.
func Load(ctx context.Context, opts ...Option) (*gonf.Config, error) {
	return load(ctx, ".", opts...)
}

// location is a place that may hold app.<ext> files.
type location struct {
	// open returns a Source for name in this location.
	open    func(name string, f source.Format, opts ...source.Option) gonf.Source
	ordinal int
}

// load is Load with the working directory as a parameter, for tests.
func load(ctx context.Context, workDir string, opts ...Option) (*gonf.Config, error) {
	o := options{formats: []format{
		{ext: "properties", format: properties.Format},
		{ext: "json", format: json.Format},
	}}
	for _, opt := range opts {
		opt(&o)
	}

	var locations []location
	if o.embedded != nil {
		locations = append(locations, location{
			open: func(name string, f source.Format, opts ...source.Option) gonf.Source {
				return file.FS(o.embedded, name, f, opts...)
			},
			ordinal: EmbeddedOrdinal,
		})
	}
	dir := filepath.Join(workDir, ConfigDir)
	locations = append(locations, location{
		open: func(name string, f source.Format, opts ...source.Option) gonf.Source {
			return file.New(filepath.Join(dir, name), f, opts...)
		},
		ordinal: ConfigDirOrdinal,
	})

	var srcs []gonf.Source
	for _, loc := range locations {
		for _, f := range o.formats {
			srcs = append(srcs, loc.open(BaseName+"."+f.ext, f.format, source.Optional(), source.WithOrdinal(loc.ordinal)))
		}
	}
	srcs = append(srcs,
		file.New(filepath.Join(workDir, DotenvFile), dotenv.Format,
			source.Optional(), source.WithOrdinal(DotenvOrdinal), source.WithKeyMapper(env.MapKey)),
		env.New(),
	)

	core := []gonf.Option{
		gonf.WithSources(srcs...),
		gonf.WithProfileKey(ProfileKey),
		gonf.WithFactories(profileFiles(locations, o.formats)),
	}
	return gonf.Load(ctx, append(core, o.core...)...)
}

// profileFiles returns a factory that reads app-<profile>.<ext> for every
// active profile in every location.
func profileFiles(locations []location, formats []format) gonf.SourceFactory {
	return func(_ context.Context, cfg *gonf.Config) ([]gonf.Source, error) {
		var srcs []gonf.Source
		for _, profile := range cfg.Profiles() {
			for _, loc := range locations {
				for _, f := range formats {
					name := fmt.Sprintf("%s-%s.%s", BaseName, profile, f.ext)
					srcs = append(srcs, loc.open(name, f.format,
						source.Optional(), source.WithOrdinal(loc.ordinal), source.WithProfile(profile)))
				}
			}
		}
		return srcs, nil
	}
}
