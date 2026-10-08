# gonf

[![Go Reference](https://pkg.go.dev/badge/github.com/efuturetoday/gonf.svg)](https://pkg.go.dev/github.com/efuturetoday/gonf) [![Build Status](https://img.shields.io/github/actions/workflow/status/efuturetoday/gonf/test.yml?branch=main)](https://github.com/efuturetoday/gonf/actions) [![Go Version](https://img.shields.io/github/go-mod/go-version/efuturetoday/gonf)](https://go.dev/) [![License](https://img.shields.io/github/license/efuturetoday/gonf)](./LICENSE)

Layered configuration for Go, modeled on SmallRye/MicroProfile Config as used
by Quarkus: Sources with ordinals, profiles, `${...}` expressions, typed
structs that report all problems at once, secrets, and a generated
configuration reference. The core uses only the standard library.

```go
type DB struct {
	URL      string              `desc:"Address of the database."`
	Password gonf.Secret[string] `desc:"Password of the user."`
	Pool     struct {
		MaxSize int           `default:"10"`
		Timeout time.Duration `default:"5s"`
	}
}

var dbMapping = gonf.NewMapping[DB]("db")

cfg, err := boot.Load(ctx, boot.With(gonf.WithMappings(dbMapping)))
if err != nil {
	log.Fatal(err) // every missing or invalid key, at once
}
db := dbMapping.Get(cfg)
```

```properties
# config/app.properties
db.url=postgres://prod-db/app
%dev.db.url=postgres://localhost/app
```

```sh
GONF_PROFILE=dev DB_PASSWORD=secret DB_POOL_MAX_SIZE=20 ./app
```

## Getting started

```sh
go get github.com/efuturetoday/gonf
```

gonf requires Go 1.23 or newer. Runnable examples:

- [`examples/basic`](./examples/basic): layered Sources, profiles and expressions, set up by hand
- [`examples/boot`](./examples/boot): loading by convention
- [`examples/library-module`](./examples/library-module): a library that
  brings its own configuration, and a `-config-doc` flag

## Features

**Sources and ordinals.** Every Source has an ordinal; for each key the
highest ordinal wins, and of equal ordinals the Source registered later.
Included: environment variables, files (`.properties`, `.json`, `.env`) and
in-memory values. A Source is a small interface, so Vault or a database fit
in, and a `SourceFactory` creates Sources that need configuration
themselves.

**Convention with `boot`.** `boot.Load` reads, highest priority first:

| Ordinal | Source |
| --- | --- |
| 300 | environment variables, `DB_POOL_MAX_SIZE` for `db.pool.max-size` |
| 295 | `.env` in the working directory |
| 260 | `config/app.<ext>` in the working directory |
| 250 | `app.<ext>` embedded in the binary (`boot.WithFS`) |

**Profiles.** `%dev.db.url` applies only while the profile `dev` is active,
set with `GONF_PROFILE=dev,local` or `gonf.WithProfiles`. `boot` also reads
`app-dev.<ext>`.

**Expressions.** `db.url=postgres://${db.host:localhost}/app`; `$${` is a
literal `${`.

**Typed structs.** `Bind` and Mappings fill a struct from the keys under a
prefix: field names in kebab case (`MaxSize` → `max-size`), `cfg` and
`default` tags, pointers for optional values, slices (`a,b` or `hosts[0]`),
maps, nested structs, `time.Duration`, `encoding.TextUnmarshaler`, and any
type with a converter (`gonf.WithConverter(url.Parse)`). A struct may check
itself with `Validate() error`. Unknown keys under a prefix are errors with
a suggestion: `db.pool.max-szie: unknown key, did you mean db.pool.max-size?`

**Secrets.** A `gonf.Secret[T]` field prints as `****` in fmt, JSON and
slog. `Explain` and `Describe` mask secret fields and keys named like
`password`, `secret`, `token` or `credential`.

**Where does a value come from?** `cfg.Explain("db.url")` tells the winning
value, its file and line, and what it overrides:

```text
db.url = postgres://localhost/app (file:config/app.properties:2, profile dev)
  overrides postgres://prod-db/app (file:config/app.properties:1)
```

**Configuration reference.** `gonf.Describe` lists every key the Mappings
read, from the same options as `Load` but without reading any value, and
`gonf.WriteMarkdown` writes it as a table:

| Key | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `db.url` | `DB_URL` | `string` | required | Address of the database. |
| `db.password` | `DB_PASSWORD` | `string` (secret) | required | Password of the user. |
| `db.pool.max-size` | `DB_POOL_MAX_SIZE` | `int` | `10` |  |

## Coming from Quarkus

| Quarkus / SmallRye Config | gonf |
| --- | --- |
| `ConfigSource` with ordinal | `gonf.Source` with `Ordinal()` |
| `ConfigSourceFactory` | `gonf.SourceFactory` |
| `%dev.` profiles, `quarkus.profile` | `%dev.` profiles, `gonf.profile` (in `boot`) |
| `@ConfigMapping(prefix = "db")` | `gonf.NewMapping[DB]("db")` |
| `@WithName`, `@WithDefault` | `cfg:"..."`, `default:"..."` tags |
| Javadoc in the generated config reference | `desc:"..."` tag, `gonf.Describe` |
| `Secret<T>` | `gonf.Secret[T]` |
| `Converter`, `@WithConverter` | `gonf.WithConverter` (per type) |
| `ConfigValue` (source, ordinal) | `cfg.Explain`, `cfg.Lookup` |

gonf has no global state: `Load` returns a `*Config` and Mappings are
registered explicitly with `WithMappings`.

## Contributing

Issues and pull requests are welcome. Before sending a change, run:

```sh
go vet ./...
go test -race ./...
golangci-lint run
```

## License

[Apache License 2.0](./LICENSE)
