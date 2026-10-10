# gonf

<p align="center">
  <img src="docs/logo.png" alt="The Go gopher lifting a gear that forms the O of GONF" width="420">
</p>

[![Go Reference](https://pkg.go.dev/badge/github.com/efuturetoday/gonf.svg)](https://pkg.go.dev/github.com/efuturetoday/gonf) [![Build Status](https://img.shields.io/github/actions/workflow/status/efuturetoday/gonf/test.yml?branch=main)](https://github.com/efuturetoday/gonf/actions) [![Go Version](https://img.shields.io/github/go-mod/go-version/efuturetoday/gonf)](https://go.dev/) [![License](https://img.shields.io/github/license/efuturetoday/gonf)](./LICENSE)

Layered configuration for Go: files, `.env`, the environment, flags and
secret directories, with profiles, bound to typed structs that report every
problem at once. The core uses only the standard library.

## Why gonf

I built gonf because tracing a wrong value through environments, files and
structs should not take an hour, and because every service should be
configured the same way without losing flexibility.

- **One way for every service.** `boot.Load` reads `config/`, `.env` and
  the environment by convention; [profiles](docs/profiles.md), formats and
  your own [sources](docs/sources.md) plug in.
- **Values that build on each other.**
  `db.url=postgres://${db.host}:${db.port:5432}/app`, resolved across all
  sources.
- **Environment checked against your structs.** Only variables that match
  a field are read, map entries too. `DB_PROT` is a warning with the name
  you meant, not a silent miss; Kubernetes service variables do not
  trip it.
- **Functions in values.** `${env::HOME}`, `${file::/run/secrets/tls.crt}`,
  `${base64::...}`, `${uuid::}` and `${aes-gcm::...}` nest, and the result
  of a secret one stays masked. See [Sources](docs/sources.md#expressions).
- **Flags and Kubernetes secret directories.** [`flagsource`](docs/sources.md#command-line-flags)
  turns a `flag.FlagSet` into a Source, [`dir`](docs/sources.md#directory-with-one-file-per-key)
  reads a mounted Secret with one file per key.
- **See where a value comes from.** [`cfg.Explain`](docs/explain.md) shows
  the source, file and line, and what it overrides.
- **Every problem at start, all at once.** [Binding](docs/binding.md)
  reports all of them with their origin, and suggests the key you meant.
- **Secrets that do not leak.** [`gonf.Secret[T]`](docs/secrets.md) prints
  `****`; values can be stored encrypted.
- **Safe live reload.** [`boot.Watch`](docs/reload.md) keeps the running
  configuration when a change is broken.
- **Docs that cannot drift.** [`boot.Describe`](docs/reference.md) writes
  the configuration reference from the structs, and
  [`boot.Schema`](docs/reference.md#json-schema) a JSON Schema so your
  editor completes and checks the files.
- **Standard library only.** The core has no dependencies.

## Quick start

```sh
go get github.com/efuturetoday/gonf
```

Declare what your application needs as a struct:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/boot"
)

type Config struct {
	Port int `default:"8080"`
	DB   struct {
		URL      string
		Password gonf.Secret[string]
	}
}

var appConfig = gonf.NewMapping[Config]("app")

func main() {
	cfg, err := boot.Load(context.Background(), boot.WithMappings(appConfig))
	if err != nil {
		log.Fatal(err)
	}
	c := appConfig.Get(cfg)
	fmt.Println(c.Port, c.DB.URL, c.DB.Password)
}
```

Put the values in `config/app.properties`:

```properties
app.db.url=postgres://localhost/app
%prod.app.port=80
```

Properties are one choice: JSON, YAML and `.env` files, the environment and
your own sources work too.

Run it:

```console
$ go run .
gonf: bind "app": app.db.password: required, no value

$ APP_DB_PASSWORD=s3cret go run .
8080 postgres://localhost/app ****

$ GONF_PROFILE=prod APP_DB_PASSWORD=s3cret go run .
80 postgres://localhost/app ****
```

A missing value stops the start with a clear message, the environment
fills it in (`APP_DB_PASSWORD` for `app.db.password`), the profile `prod`
switches the port, and the password never shows up in output. A typo is
caught too:

```console
gonf: bind "app": app.db.ulr: unknown key, did you mean app.db.url? (from file:config/app.properties:3)
```

## gonf, viper or koanf?

All three load layered configuration. They differ in what they take care
of for you.

| | gonf | viper | koanf |
| --- | --- | --- | --- |
| Shape | typed structs (Mappings), checked at start | keys read by name, `Unmarshal` into structs | you assemble providers and parsers |
| State | no global state | a global instance by default | an instance you create |
| Keys | case-sensitive | case-insensitive | case-sensitive |
| Environment | matched against your structs | derived by `AutomaticEnv` and `BindEnv` | `env` provider with a transform callback |

Choose **gonf** for services that want typed structs checked at start with
all errors at once, the origin of every value (`cfg.Explain`), profiles,
secrets, environment variables matched against the structs, and docs and
a schema generated from the code. Choose **viper** for its large ecosystem
and remote providers. Choose **koanf** if you want to assemble providers
and parsers yourself with few opinions.

Coming from viper or koanf:

- A Mapping per struct (`gonf.NewMapping[Config]("app")`) replaces
  `Unmarshal`. Unknown keys and missing values fail at start.
- Environment names come from the keys (`app.db.url` is `APP_DB_URL`), so
  there is nothing to bind or transform.
- Values may refer to others and call functions: `${app.db.host}`,
  `${env::HOME}`.

## How it works

Every value comes from a Source, and every Source has an ordinal. For each
key the highest ordinal wins. `boot.Load` reads, highest first:

```
300  environment variables          APP_DB_URL=...
295  .env in the working directory  APP_DB_URL=...
260  config/app.<ext>               app.db.url=...
250  app.<ext> in the binary        (embedded with boot.WithFS)
```

Flags (`boot.WithFlags`, 400) rank above all of these, and a directory of
files such as a mounted Secret (`source/dir`, 100) below. `<ext>` is `properties` or `json`; other formats plug in, such as YAML with
`boot.WithFormat("yaml", yaml.Format)` from the module
[`format/yaml`](format/yaml). For an active profile, `app-<profile>.<ext>`
is read too, and a value such as `%prod.app.port` beats the plain value of
the same ordinal. Values may refer to other keys:
`app.db.url=postgres://${app.db.host:localhost}/app`.

## Guides

- **[Profiles](docs/profiles.md)**: `%dev.` keys, `app-dev.properties`,
  activating profiles.
- **[Structs](docs/binding.md)**: names, defaults, optional fields, lists,
  maps, validation, unknown keys, and converters for types of other
  packages.
- **[Secrets](docs/secrets.md)**: `gonf.Secret[T]`, what gets masked, and encrypted values.
- **[Where does a value come from?](docs/explain.md)**: `cfg.Explain` shows
  the winning value, its file and line, and what it overrides; `cfg.Warnings()`
  and `cfg.Sources()` show what was skipped and loaded.
- **[Configuration reference](docs/reference.md)**: generate a table of all
  keys with their environment variables, types and defaults, and a JSON
  Schema for editors.
- **[Sources and formats](docs/sources.md)**: `.properties`, JSON, YAML,
  `.env`, the environment, flags, secret directories, expressions with
  functions, and your own Sources.
- **[Reloading](docs/reload.md)**: `boot.Watch` keeps the configuration
  current when files change, and keeps the previous one if a change is
  invalid.

The API reference is on [pkg.go.dev](https://pkg.go.dev/github.com/efuturetoday/gonf).

## Examples

- [`examples/basic`](./examples/basic): Sources, profiles and expressions,
  set up by hand
- [`examples/boot`](./examples/boot): loading by convention
- [`examples/library-module`](./examples/library-module): a library that
  brings its own configuration, and a `-config-doc` flag
- [`examples/reload`](./examples/reload): a configuration that follows
  changes to its files

## Contributing

Issues and pull requests are welcome. Before sending a change, run:

```sh
go vet ./...
go test -race ./...
golangci-lint run
```

`format/yaml` and `cmd/gonf` are modules of their own; run the same
commands in their directories.

## License

[Apache License 2.0](./LICENSE)

The gonf logo is based on the Go gopher by [Renée French](https://reneefrench.blogspot.com/),
licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
