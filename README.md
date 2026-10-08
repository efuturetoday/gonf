# gonf

<p align="center">
  <img src="docs/logo.png" alt="The Go gopher lifting a gear that forms the O of GONF" width="420">
</p>

[![Go Reference](https://pkg.go.dev/badge/github.com/efuturetoday/gonf.svg)](https://pkg.go.dev/github.com/efuturetoday/gonf) [![Build Status](https://img.shields.io/github/actions/workflow/status/efuturetoday/gonf/test.yml?branch=main)](https://github.com/efuturetoday/gonf/actions) [![Go Version](https://img.shields.io/github/go-mod/go-version/efuturetoday/gonf)](https://go.dev/) [![License](https://img.shields.io/github/license/efuturetoday/gonf)](./LICENSE)

Layered configuration for Go: files, `.env` and the environment, with
profiles, bound to typed structs that report every problem at once. The
core uses only the standard library.

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
260  config/app.properties          app.db.url=...
250  app.properties in the binary   (embedded with boot.WithFS)
```

A value for an active profile, such as `%prod.app.port`, beats the plain
value of the same ordinal. Values may refer to other keys:
`app.db.url=postgres://${app.db.host:localhost}/app`.

## Guides

- **[Profiles](docs/profiles.md)**: `%dev.` keys, `app-dev.properties`,
  activating profiles.
- **[Structs](docs/binding.md)**: names, defaults, optional fields, lists,
  maps, validation, unknown keys, and converters for types of other
  packages.
- **[Secrets](docs/secrets.md)**: `gonf.Secret[T]` and what gets masked.
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

`format/yaml` is a module of its own; run the same commands in its
directory.

## License

[Apache License 2.0](./LICENSE)

The gonf logo is based on the Go gopher by [Renée French](https://reneefrench.blogspot.com/),
licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
