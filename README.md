# gonf

<p align="center">
  <img src="docs/logo.png" alt="The Go gopher lifting a gear that forms the O of GONF" width="420">
</p>

[![Go Reference](https://pkg.go.dev/badge/github.com/efuturetoday/gonf.svg)](https://pkg.go.dev/github.com/efuturetoday/gonf) [![Build Status](https://img.shields.io/github/actions/workflow/status/efuturetoday/gonf/test.yml?branch=main)](https://github.com/efuturetoday/gonf/actions) [![Go Version](https://img.shields.io/github/go-mod/go-version/efuturetoday/gonf)](https://go.dev/) [![License](https://img.shields.io/github/license/efuturetoday/gonf)](./LICENSE)

Layered configuration for Go: files, `.env` and the environment, with
profiles, bound to typed structs that report every problem at once. The
core uses only the standard library.

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
- **See where a value comes from.** [`cfg.Explain`](docs/explain.md) shows
  the source, file and line, and what it overrides.
- **Every problem at start, all at once.** [Binding](docs/binding.md)
  reports all of them with their origin, and suggests the key you meant.
- **Secrets that do not leak.** [`gonf.Secret[T]`](docs/secrets.md) prints
  `****`; values can be stored encrypted.
- **Safe live reload.** [`boot.Watch`](docs/reload.md) keeps the running
  configuration when a change is broken.
- **Docs that cannot drift.** [`boot.Describe`](docs/reference.md) writes
  the configuration reference from the structs.
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

func main() {
	cfg, err := boot.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	c, err := gonf.Bind[Config](cfg, "app")
	if err != nil {
		log.Fatal(err)
	}
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

## How it works

Every value comes from a Source, and every Source has an ordinal. For each
key the highest ordinal wins. `boot.Load` reads, highest first:

```
300  environment variables          APP_DB_URL=...
295  .env in the working directory  APP_DB_URL=...
260  config/app.<ext>               app.db.url=...
250  app.<ext> in the binary        (embedded with boot.WithFS)
```

`<ext>` is `properties` or `json`; other formats plug in, such as YAML with
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
  the winning value, its file and line, and what it overrides.
- **[Configuration reference](docs/reference.md)**: generate a table of all
  keys with their environment variables, types and defaults.
- **[Sources and formats](docs/sources.md)**: `.properties`, JSON, YAML,
  `.env`, the environment, and your own Sources.
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
