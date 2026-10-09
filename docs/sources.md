# Sources and formats

A Source delivers flat keys such as `db.pool.max-size`. Every Source has an
ordinal; for each key the highest ordinal wins, and of equal ordinals the
Source registered later.

## With boot

`boot.Load` sets up the usual Sources (see the [README](../README.md#how-it-works))
and reads `app.properties` and `app.json`. Other formats and more Sources
are added from outside:

```go
cfg, err := boot.Load(ctx,
	boot.WithFormat("yaml", yaml.Format), // also read app.yaml
	boot.WithFS(embedded),                // app.* packaged into the binary
	boot.With(gonf.WithSources(vaultSource)),
	boot.WithMappings(db.Mapping),
)
```

## Without boot

`gonf.Load` takes the Sources explicitly:

```go
cfg, err := gonf.Load(ctx,
	gonf.WithProfiles("dev"),
	gonf.WithSources(
		memory.Defaults(map[string]string{"db.pool.max-size": "10"}),
		file.New("app.properties", properties.Format),
		file.New("app-dev.properties", properties.Format,
			source.Optional(), source.WithProfile("dev")),
		env.New(),
	),
)
```

| Source | Ordinal | Name |
| --- | --- | --- |
| `flagsource.New(fs)` | 400 | `flag:` |
| `env.New()` | 300 | `env:` |
| `file.New(path, format)` | 100 | `file:<path>` |
| `file.FS(fsys, path, format)`, e.g. an `embed.FS` | 100 | `fs:<path>` |
| `memory.New(values)`, e.g. in tests | 500 | `memory:` |
| `memory.Defaults(values)` | 0 | `memory:defaults` |
| `source.New(name, read, format)`, any content | 100 | your name |

Options for these Sources:

| Option | Effect |
| --- | --- |
| `source.WithOrdinal(n)` | another ordinal |
| `source.WithName(name)` | another name, starting with a scheme such as `file:` |
| `source.Optional()` | a missing file yields no values instead of an error |
| `source.WithProfile("dev")` | every key belongs to the profile `dev` |
| `source.WithKeyMatcher(env.Match)` | keys are written like environment variables, as in `.env` |
| `source.WithTrigger(source.Every(5*time.Second))` | while `gonf.Watch` watches, check for changes every 5 s instead of every 2 s |

## Formats

**`.properties`** (`format/properties`): `key=value`, `key: value` or
`key value`; comments start with `#` or `!`; a trailing `\` continues the
line; `\uXXXX` escapes. Every value has its line as location.

**JSON** (`format/json`): nested objects become `a.b`, arrays `a[0]`;
numbers keep their text; `null` produces no key; a name twice in an object
is an error; every value has its line as location.

**YAML** (`github.com/efuturetoday/gonf/format/yaml`, a module of its own):
names are joined like `.properties` keys, so `db.url: x` equals
`db: {url: x}`; values keep their text, so `yes` and `0644` reach the struct
as written; anchors, aliases, merge keys and several documents work; every
value has its line as location.

**`.env`** (`format/dotenv`): `KEY=value`, `export KEY=value`, double quotes
with escapes, single quotes taken literally, `#` comments. Use it with
`source.WithKeyMatcher(env.Match)` so that `DB_URL` becomes `db.url`.

A format is a function from bytes to keys; implement `source.Format` for
another one.

## Environment variables

A key is found in the environment under its upper-case name with every
character other than letters and digits replaced by `_`:

| Key | Variable |
| --- | --- |
| `db.pool.max-size` | `DB_POOL_MAX_SIZE` |
| `%dev.db.url` | `_DEV_DB_URL` |
| `db.hosts[0]` | `DB_HOSTS_0_` |

gonf works with these keys only. The environment and `.env` turn their
names into keys once, when they are loaded, by matching them against the
keys `Load` knows: those of the Mappings and the profile key. Map entries and list elements match too, so
`DB_POOLS_MAIN_URL` becomes `db.pools.main.url` for a map field `Pools`;
see [Binding](binding.md#types). Variables that match no key, such as
`PATH`, are left out: they are not in `cfg.Keys()`, and `${HOME}` or
`cfg.Get("HOME")` does not find them.

So `cfg.Get` and `${...}` find any key of a file as before, but a key from
the environment only if a Mapping has it. To set a key from the
environment, give it a Mapping. Explicit access to other variables comes
with functions in expressions.

A Source of your own with such names implements `gonf.MappedSource`; it
gets the keys and returns its values under them.

## Command-line flags

`flagsource.New(fs)` (`source/flagsource`) reads the flags of a standard
`flag.FlagSet` that were set on the command line. The flag name is the key:
`-db.url=postgres://x` sets `db.url`. Flags that were not set give no key,
so they never hide a value of another Source. A flag has ordinal 400, so it
overrides the environment.

`flagsource.Define` defines one flag per key, with the description as help
text and the default shown by `PrintDefaults`. It skips keys with `<name>` or
`<n>`, and never shows the default of a secret. All flags are strings; gonf
converts the type when it binds. A flag that matches no key is reported as
an unknown key, like any other Source.

```go
opts := []boot.Option{boot.WithMappings(db.Mapping)}
props, _ := boot.Describe(opts...)

fs := flag.NewFlagSet("app", flag.ExitOnError)
_ = flagsource.Define(fs, props)
_ = fs.Parse(os.Args[1:]) // boot does not parse

cfg, err := boot.Load(ctx, append(opts, boot.WithFlags(fs))...)
```

## Expressions

A value may refer to other keys:

```properties
db.host=localhost
db.url=postgres://${db.host}:${db.port:5432}/app
```

`${key}` is the value of `key` and an error if it has none; `${key:default}`
falls back to `default`; `$${` is a literal `${`. References follow the same
rules as other lookups, so profiles and environment variables apply.

## Your own Source

A Source implements three methods:

```go
type Source interface {
	Name() string // with a scheme, e.g. "vault:secret/app"
	Ordinal() int
	Load(ctx context.Context) (map[string]gonf.Value, error)
}
```

`Load` returns flat keys; a key `%dev.x` belongs to the profile `dev`. Set
`Value.Raw` and, if known, `Value.Origin.Location`; gonf fills in the rest.
For content in a known format, `source.New(name, read, format)` is enough.

A Source that needs configuration itself, such as a Vault Source whose
address is in `app.properties`, is created by a factory. It runs after the
other Sources are loaded and reads them. The Mapping of `vault` lets the
environment set the address too:

```go
vault := func(ctx context.Context, cfg *gonf.Config) ([]gonf.Source, error) {
	addr, err := cfg.Get("vault.address")
	if errors.Is(err, gonf.ErrNotFound) {
		return nil, nil // no Vault configured
	}
	if err != nil {
		return nil, err
	}
	return []gonf.Source{newVaultSource(addr)}, nil
}

type Vault struct {
	Address string `default:""`
}

var vaultMapping = gonf.NewMapping[Vault]("vault")

cfg, err := boot.Load(ctx, boot.WithMappings(vaultMapping), boot.With(gonf.WithFactories(vault)))
```
