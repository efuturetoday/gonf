// Package boot loads configuration by convention, from files in known
// places, a .env file and the environment, with profiles.
//
// Load reads, highest priority first:
//
//	400  command-line flags (WithFlags, only if given)
//	300 environment variables (env.New)
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
// Values may call the Functions env, file, base64, base64-decode, uuid and
// aes-gcm, as in ${env::HOME} or ${file::/run/secrets/db}. Values written
// as ${aes-gcm::...} are decrypted with secret/aesgcm. Its key is
// gonf.secret.aes-gcm.key, e.g. GONF_SECRET_AES_GCM_KEY, from any Source
// but the one with the encrypted value. The gonf encrypt command encrypts
// values.
//
// boot is extended from outside: WithFormat adds formats, WithFS a packaged
// default configuration, WithFunctions Functions, and With
// passes any gonf.Option through, e.g. more Sources or SourceFactories.
//
// Describe lists the keys Load reads, for a configuration reference.
//
// Watch reads the same files as Load and reloads them when they change, see
// gonf.Watch. The files on disk, including .env, are checked every
// source.DefaultPollInterval (WithFileTrigger changes that); embedded files
// and environment variables do not change while the process runs and are
// not checked.
package boot

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/format/dotenv"
	"github.com/efuturetoday/gonf/format/json"
	"github.com/efuturetoday/gonf/format/properties"
	"github.com/efuturetoday/gonf/function/base64"
	secretfile "github.com/efuturetoday/gonf/function/file"
	"github.com/efuturetoday/gonf/function/uuid"
	"github.com/efuturetoday/gonf/schema"
	"github.com/efuturetoday/gonf/secret/aesgcm"
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
	// DotenvOrdinal is just below the environment, which overrides .env.
	DotenvOrdinal    = 295
	ConfigDirOrdinal = 260
	EmbeddedOrdinal  = 250
)

// Option configures Load.
type Option func(*options)

type options struct {
	formats   []format
	embedded  fs.FS
	trigger   func(path string) source.Trigger
	functions []gonf.Function
	core      []gonf.Option
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

// WithFunctions makes Load resolve ${name::arg} expressions with fns, see
// gonf.WithFunctions. A Function with a known name replaces it, e.g. to
// pass the aes-gcm key in code:
//
//	boot.WithFunctions(aesgcm.WithKey(key))
//
// Replacing aes-gcm also drops aesgcm.Mapping, the Mapping of the default
// Function.
func WithFunctions(fns ...gonf.Function) Option {
	return func(o *options) {
		for _, fn := range fns {
			i := slices.IndexFunc(o.functions, func(f gonf.Function) bool { return f.Name == fn.Name })
			if i < 0 {
				o.functions = append(o.functions, fn)
				continue
			}
			o.functions[i] = fn
		}
	}
}

// WithFS makes Load read app.<ext> from the root of fsys, typically an
// embed.FS with default configuration packaged into the binary. Use fs.Sub
// for a subdirectory.
func WithFS(fsys fs.FS) Option {
	return func(o *options) { o.embedded = fsys }
}

// WithFileTrigger makes Watch check the files on disk whenever the Trigger
// that trigger returns for their path says so, instead of every
// source.DefaultPollInterval:
//
//	boot.WithFileTrigger(func(string) source.Trigger { return source.Every(5 * time.Second) })
//
// Load ignores it.
func WithFileTrigger(trigger func(path string) source.Trigger) Option {
	return func(o *options) { o.trigger = trigger }
}

// With passes options to gonf.Load, after the ones of boot. Sources added
// this way count as registered after the files of boot; WithProfiles wins
// over the profile key.
func With(opts ...gonf.Option) Option {
	return func(o *options) { o.core = append(o.core, opts...) }
}

// WithMappings registers Mappings, which Load binds and validates; it is
// short for With(gonf.WithMappings(mappings...)).
func WithMappings(mappings ...gonf.AnyMapping) Option {
	return With(gonf.WithMappings(mappings...))
}

// Load loads the configuration by convention, see the package
// documentation.
func Load(ctx context.Context, opts ...Option) (*gonf.Config, error) {
	return gonf.Load(ctx, build(".", opts)...)
}

// Watch loads the configuration like Load and keeps it current until ctx
// ends, see gonf.Watch and the package documentation.
func Watch(ctx context.Context, opts ...Option) (*gonf.Live, error) {
	return gonf.Watch(ctx, build(".", opts)...)
}

// Describe lists the keys of the Mappings registered with WithMappings or
// With, with their environment variable names, see gonf.Describe. It takes
// the same options as Load and reads no file:
//
//	opts := []boot.Option{boot.WithMappings(db.Mapping)}
//	props, err := boot.Describe(opts...)
//	...
//	cfg, err := boot.Load(ctx, opts...)
func Describe(opts ...Option) ([]gonf.Property, error) {
	return gonf.Describe(build(".", opts)...)
}

// location is a place that may hold app.<ext> files.
type location struct {
	// open returns a Source for name in this location.
	open    func(name string, f source.Format, opts ...source.Option) gonf.Source
	ordinal int
}

// build returns the options for gonf.Load and gonf.Watch, with workDir as
// the working directory.
func build(workDir string, opts []Option) []gonf.Option {
	o := options{formats: []format{
		{ext: "properties", format: properties.Format},
		{ext: "json", format: json.Format},
	}, functions: []gonf.Function{
		env.Function(),
		secretfile.Function(),
		base64.Encode(),
		base64.Decode(),
		uuid.Function(),
		aesgcm.Function(),
	}}
	for _, opt := range opts {
		opt(&o)
	}

	onDisk := func(path string, f source.Format, opts ...source.Option) gonf.Source {
		if o.trigger != nil {
			opts = append(opts, source.WithTrigger(o.trigger(path)))
		}
		return file.New(path, f, opts...)
	}
	var locations []location
	if o.embedded != nil {
		locations = append(locations, location{
			open: func(name string, f source.Format, opts ...source.Option) gonf.Source {
				return file.FS(o.embedded, name, f, append(opts, source.WithTrigger(never))...)
			},
			ordinal: EmbeddedOrdinal,
		})
	}
	dir := filepath.Join(workDir, ConfigDir)
	locations = append(locations, location{
		open: func(name string, f source.Format, opts ...source.Option) gonf.Source {
			return onDisk(filepath.Join(dir, name), f, opts...)
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
		onDisk(filepath.Join(workDir, DotenvFile), dotenv.Format,
			source.Optional(), source.WithOrdinal(DotenvOrdinal), source.WithKeyMatcher(env.Match)),
		env.New(),
	)

	core := []gonf.Option{
		gonf.WithSources(srcs...),
		gonf.WithProfileKey(ProfileKey),
		gonf.WithProfileSources(profileFiles(locations, o.formats)),
		gonf.WithFunctions(o.functions...),
	}
	return append(core, o.core...)
}

// never is the Trigger of embedded files, which never change.
func never(ctx context.Context, _ func()) error {
	<-ctx.Done()
	return nil
}

// profileFiles returns the Sources app-<profile>.<ext> of a profile in
// every location.
func profileFiles(locations []location, formats []format) func(profile string) []gonf.Source {
	return func(profile string) []gonf.Source {
		var srcs []gonf.Source
		for _, loc := range locations {
			for _, f := range formats {
				name := fmt.Sprintf("%s-%s.%s", BaseName, profile, f.ext)
				srcs = append(srcs, loc.open(name, f.format,
					source.Optional(), source.WithOrdinal(loc.ordinal), source.WithProfile(profile)))
			}
		}
		return srcs
	}
}

// Schema returns the JSON Schema of the keys Load reads, see schema.New. It
// takes the same options as Load and reads no file:
//
//	doc, err := boot.Schema(boot.WithMappings(db.Mapping))
func Schema(opts ...Option) ([]byte, error) {
	props, err := Describe(opts...)
	if err != nil {
		return nil, err
	}
	return schema.New(props)
}
