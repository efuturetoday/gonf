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

A test leaves out `.env`, which holds the settings of one developer's
machine, with `boot.WithoutDotenv()`; the environment still applies, so
whoever runs the tests decides, for example, which database they use.
`boot.WithDir(dir)` reads `config/` and `.env` in another directory than
the working directory, such as the root of the module from a test in a
package below it.

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
| `source.Verbatim()` | values are text, never expressions; for values others may write |
| `source.WithKeyMatcher(env.Match)` | keys are written like environment variables, as in `.env` |
| `source.WithTrigger(source.Every(5*time.Second))` | while `gonf.Watch` watches, check for changes every 5 s instead of every 2 s |

## Directory with one file per key

`dir.New(path)` (`source/dir`, ordinal 100, name `dir:<path>`) reads a
directory as Kubernetes mounts a Secret or ConfigMap: every regular file
directly in it is a key. The file name is the key (`db.password`), the
content the value.

```go
cfg, err := boot.Load(ctx,
	boot.With(gonf.WithSources(dir.New("/etc/secrets", source.Optional()))),
)
```

- Content is kept exactly. A file written with `echo` ends in a newline,
  and so does the value. `dir.New(path, dir.TrimNewline())` removes one trailing
  newline (`\n` or `\r\n`) from every value.
- Files and directories starting with `..` (`..data`, `..2026_01_02...`)
  are ignored, and so are subdirectories.
- Symbolic links are followed, but only inside the directory. A link that
  points outside, a broken link and a file over 1 MiB (`dir.MaxFileSize`)
  are errors naming the file.
- `source.Optional()` makes a missing directory yield no values.
- `gonf.Watch` notices changed, new and removed files and the atomic swap
  of `..data` that Kubernetes does on an update. It polls every 2 s; see
  `source.WithTrigger`.
- Gonf shows a value unless the key name looks secret (see
  [Secrets](secrets.md)) or the Mapping field is a `gonf.Secret`.

## Formats

**`.properties`** (`format/properties`): `key=value`, `key: value` or
`key value`; comments start with `#` or `!`; a trailing `\` continues the
line; `\uXXXX` escapes. Every value has its line as location.

**JSON** (`format/json`): nested objects become `a.b`, arrays `a[0]`;
numbers keep their text; `null` produces no key; a name twice in an object
is an error; every value has its line as location.

**YAML** (`github.com/efuturetoday/gonf/format/yaml`, a module of its own,
`go get github.com/efuturetoday/gonf/format/yaml`): names are joined like `.properties` keys, so `db.url: x` equals
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
environment, give it a Mapping. Any other variable is read explicitly with
the function `env`: `${env::HOME}`.

A Source of your own with such names implements `gonf.MappedSource`; it
gets the keys and returns its values under them.

### Unknown and clashing names

A variable that starts like a Mapping but matches no key is not an error:
a Kubernetes service named `db` sets `DB_PORT` and `DB_SERVICE_HOST` in
every pod. `Load` reports it as a warning instead, `_DEV_DB_X` included:

```go
for _, w := range cfg.Warnings() {
	log.Println(w) // env: DB_PROT matches no key, did you mean DB_PORT?
}
```

A name is under a Mapping with prefix `db` if it starts with `DB_`. The
Source decides that, so gonf itself knows nothing about variable names. A
Source of your own can do the same by implementing `gonf.NameReporter`.

Two keys that give the same variable, such as `db.max-size` and
`db.max.size` as `DB_MAX_SIZE`, are a warning of kind `WarnNameConflict`
too, because an application that never sets them through the environment
works. `Describe` fails for them with `gonf.ErrNameConflict`, since a
reference with one name for two keys is wrong.

## Which Sources were loaded

`cfg.Sources()` lists the Sources, the highest priority first, e.g. for a
status page:

| Field | Meaning |
| --- | --- |
| `Name`, `Ordinal` | the Source and its priority |
| `Profile` | the profile of a Source made with `source.WithProfile` |
| `Keys` | how many keys it delivered |
| `IsOptional`, `IsMissing` | an optional file that was not found delivers nothing |

## Command-line flags

`flagsource.New(fs)` (`source/flagsource`) reads the flags of a standard
`flag.FlagSet` that were set on the command line. The flag name is the key:
`-db.url=postgres://x` sets `db.url`. Flags that were not set give no key,
so they never hide a value of another Source. A flag has ordinal 400, so it
overrides the environment.

`flagsource.Define` defines one flag per key, with the description as help
text and the default shown by `PrintDefaults`. It skips keys with `<name>` or
`<n>`, and never shows the default of a secret. A key of type `bool` gets a
bool flag: `-debug` means `-debug=true`, and `-debug=false` or `-debug=no`
work too. All other flags are strings; gonf converts the type when it binds. A flag that matches no key is reported as
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
rules as other lookups, so profiles apply.

`${name::arg}` calls a Function. boot registers these:

| Function | Result |
| --- | --- |
| `${env::HOME}` | the environment variable, also one no Mapping has |
| `${file::/run/secrets/tls.crt}` | the content of a file, at most 1 MiB; secret |
| `${base64::...}`, `${base64-decode::...}` | the argument encoded or decoded |
| `${uuid::}` | a random UUID |
| `${slug::feature/Login}` | `feature-login`: lower case letters, digits and `-`, at most 63, for a tag of an image or a name in Kubernetes |
| `${aes-gcm::...}` | a decrypted value; secret, see [Secrets](secrets.md) |

The argument is expanded first, so calls nest:
`${base64::${file::/run/secrets/tls.crt}}`. A result that comes from a
secret Function is masked, also through the Functions around it. A Function
runs once per load for the same argument, so `${uuid::}` gives one value in
every key that uses it; a Function that must stay the same across reloads
keeps its result itself. A fallback nests too: `${app.home:${env::HOME}}`.

`function/git` has what git knows of a directory. boot does not register
it, as it needs the directory; `gonf.WithFunctions(git.Function("."))` or
`boot.WithFunctions(...)` does:

```yaml
app.image.tags:
  - ${git::version:latest}
  - ${slug::${git::branch}}-${git::commit}
```

| Argument | Value |
| --- | --- |
| `commit`, `sha` | the short and the full commit of HEAD |
| `branch` | the branch; with a detached HEAD, as in CI, from `GITHUB_HEAD_REF`, `GITHUB_REF_NAME`, `CI_COMMIT_REF_NAME` or `BRANCH_NAME` |
| `tag` | a tag on HEAD |
| `dirty` | whether there are changes that are not committed, untracked files included |
| `changes` | a short hash of these changes: the same changes give the same hash |
| `version` | the short commit, or `<commit>-dirty.<changes>` |
| `remote` | the web address of `origin`, such as `https://github.com/acme/hello`, without credentials; in CI without a remote from `GITHUB_SERVER_URL` and `GITHUB_REPOSITORY`, or `CI_PROJECT_URL` |

A default follows a `:`, as for a key: `${git::version:latest}` outside a
repository or without the `git` command. Without one, the value fails with
the reason. git runs only for a value that calls it, once per load.

Your own Function is a `gonf.Function{Name, IsSecret, Call, Mapping}`,
passed to `boot.WithFunctions` or `gonf.WithFunctions`; one with the name of
a default replaces it. `Mapping` declares the keys the Function reads, such
as the key of `aes-gcm`, so they are documented and the environment can set
them.

Functions run with the rights of the process: `file` reads any file it
may open, `env` any variable, `git` runs a command. Every Source is
therefore trusted like the code. Values others may write, such as those of
a shared key store, come from a Source made `source.Verbatim()`: its values
are text, `${` in them stays as written and calls nothing, also when another
key refers to them. A Source of its own does the same by implementing
`gonf.VerbatimSource`.

`cfg.Eval` evaluates an expression without a key, for example
`cfg.Eval("${env::HOME}/logs")`. Like `cfg.Get`, it is for a value read on
purpose; the configuration of an application goes through Mappings (see
[Structs](binding.md)).

## Your own Source

A Source implements three methods:

```go
type Source interface {
	Name() string // with a scheme, e.g. "vault:secret/app"
	Ordinal() int
	Load(ctx context.Context) (map[string]gonf.Value, error)
}
```

The name is checked only by `Load`, not when the Source is created: a name
without a valid scheme (at least two characters, as in `file:`) makes `Load`
fail with `gonf.ErrInvalidSourceName`, wrapped in a `gonf.SourceError`.

`Load` returns flat keys; a key `%dev.x` belongs to the profile `dev`. Set
`Value.Raw` and, if known, `Value.Origin.Location`; gonf fills in the rest.
For content in a known format, `source.New(name, read, format)` is enough.

### Optional interfaces

A Source gets more behavior by implementing further interfaces. gonf finds
them by a type assertion, so nothing has to be registered; implement all
methods of an interface or none of it:

| Interface | Implement it if the Source … |
| --- | --- |
| `gonf.MappedSource` (`LoadMapped`) | has names that are not keys, such as environment variables; gets the known keys and returns the values it matches |
| `gonf.NameReporter` (`LoadReport`) | is a `MappedSource` that can also name the entries that look meant for a Mapping but match no key (they become warnings) |
| `gonf.KeyMapper` (`MapKey`) | has its own naming scheme; `Describe` and `Explain` show it, e.g. as the variable of a key |
| `gonf.OptionalSource` | may have nothing to read, such as a missing file; `Config.Sources` shows it |
| `gonf.ProfileSource` | holds only the keys of one profile |
| `gonf.VerbatimSource` (`IsVerbatim`) | has values that are text, never expressions, such as values others may write |
| `gonf.Watcher` (`Watch`) | notices changes itself, see [Reloading](reload.md) |
| `io.Closer` | needs closing; gonf closes the Sources a factory creates, never those passed with `WithSources` |

`source.New` and `source.Meta` already implement the common ones for
Sources with a document.

`source/sourcetest` checks that a Source keeps this contract: a scheme in
the name, the expected values from `Load` (or `LoadMapped`), the same
values a second time, valid profile keys, and a `Watch` that ends with its
context. Run it in the tests of the Source:

```go
func TestContract(t *testing.T) {
	sourcetest.Run(t, newVaultSource(addr), map[string]string{"db.url": "postgres://db"})
}
```

A Source that needs configuration itself, such as a Vault Source whose
address is in `app.properties`, is created by a factory. The factory brings
its own Mapping: `Load` binds it from the other Sources, including the
profile files, and passes the struct to the factory. Its keys are checked,
documented and set from the environment like any other:

```go
type Vault struct {
	Address  *string             `desc:"Address of the Vault; without it no Vault is used."`
	ClientID string              `default:""`
	Secret   *gonf.Secret[string]
}

var vaultMapping = gonf.NewMapping[Vault]("vault")

vault := gonf.NewFactory(vaultMapping, func(ctx context.Context, v Vault) ([]gonf.Source, error) {
	if v.Address == nil {
		return nil, nil // no Vault configured
	}
	return []gonf.Source{newVaultSource(*v.Address, v.ClientID, v.Secret)}, nil
})

cfg, err := boot.Load(ctx, boot.With(gonf.WithFactories(vault)))
```

`VAULT_ADDRESS` in the environment, `vault.address` in a file or
`/mnt/secrets/vault.secret` through the dir Source all work. Values the
factory's Sources return, such as `db.password`, are bound with the other
Mappings afterwards.

A factory's Sources must not set a key under the prefix of any factory,
since that configuration was read before they existed: `Load` fails and
names the key. There are two phases, no chain: a factory cannot be
configured by the Sources of another factory.

