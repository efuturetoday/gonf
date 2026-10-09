# Configuration reference

Operators need to know which keys an application reads. `boot.Describe`
lists them from the Mappings, without reading any value, so it works in a
build or a CI job without a real environment.

## Describing fields

A `desc` tag documents a field:

```go
type Config struct {
	URL      string              `desc:"Address of the database."`
	Password gonf.Secret[string] `desc:"Password of the user."`
	Pool     struct {
		MaxSize int `default:"10" desc:"Maximum number of open connections."`
	}
}
```

## Generating the table

Pass the same options as to `Load`. No file is read; the environment
variable names follow the naming of `boot`:

```go
props, err := boot.Describe(boot.WithMappings(db.Mapping))
if err != nil {
	log.Fatal(err)
}
gonf.WriteMarkdown(os.Stdout, props)
```

| Key | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `db.url` | `DB_URL` | `string` | required | Address of the database. |
| `db.password` | `DB_PASSWORD` | `string` (secret) | required | Password of the user. |
| `db.pool.max-size` | `DB_POOL_MAX_SIZE` | `int` | `10` | Maximum number of open connections. |

The `desc` text is Markdown. Keys of map entries contain `<name>`, keys of
list elements `[<n>]`: `db.servers.<name>.port`, `db.replicas[<n>].host`.
For such a key, "required" means: in every entry or element that exists.

A practical way is a flag of the application itself, which uses the same
options as `Load`:

```go
opts := []boot.Option{boot.WithMappings(db.Mapping)}
if *configDoc {
	props, err := boot.Describe(opts...)
	...
}
cfg, err := boot.Load(ctx, opts...)
```

```sh
go run . -config-doc > CONFIG.md
```

See [`examples/library-module`](../examples/library-module).

Without boot, `gonf.Describe` takes the options of `gonf.Load`; its Sources
are not read and only give the environment variable names:

```go
props, err := gonf.Describe(gonf.WithSources(env.New()), gonf.WithMappings(db.Mapping))
```

## As data

`Describe` returns `[]gonf.Property` with `Key`, `Env`, `Type`, `Default`
(nil without a default tag), `Required`, `Secret`, `Doc`, and `Mapping` and
`Location`: the prefix and the line of `NewMapping` of the Mapping that
reads the key. The fields have JSON tags, so `json.Marshal(props)` gives a
machine-readable reference. Defaults of secret fields are masked.

A loaded Config lists the same with `cfg.Properties()`, and every
`Explanation` of a key a Mapping reads carries its `Property`, see
[Explain](explain.md).

## Names that clash

If two keys have the same environment variable, such as `db.max-size` and
`db.max.size` as `DB_MAX_SIZE`, `Describe` returns an error wrapping
`gonf.ErrNameConflict` and no table. `Load` only warns, see
[Sources](sources.md#unknown-and-clashing-names).

## Always in sync

`Describe` and `Load` read a struct through the same model. A key that the
table marks as required is exactly a key that `Load` reports as missing, and
a field type that `Load` rejects is the same error from `Describe`. The
Properties of `Describe`, of `cfg.Properties()` and of an `Explanation`
come from one function, so they cannot differ.

## JSON Schema

`schema.New` turns the Properties into a JSON Schema (draft 2020-12), so an
editor offers completion, type checks and the `desc` texts in YAML and JSON
configuration files. `boot.Schema` takes the same options as `boot.Describe`:

```go
doc, err := boot.Schema(boot.WithMappings(db.Mapping))
if err != nil {
	log.Fatal(err)
}
os.Stdout.Write(doc)
```

```sh
go run . -config-schema > config/app.schema.json
```

What the schema says:

- Dotted keys become nested objects. `<name>` is `additionalProperties`
  with the schema of the entry, a list is an array. A list of plain values
  may also be a comma-separated string.
- Defaults are `default`, `desc` is `description` and `markdownDescription`
  (VS Code shows the latter). A secret is `writeOnly` and has no default.
- Integers have their bounds, e.g. `uint16` is 0 to 65535. Every type but
  string also accepts a `${...}` expression. A type with its own text
  format is not checked beyond the kind of the value.
- Objects below the prefix of a Mapping reject unknown keys, like `Load`.
  The root stays open for other prefixes. A Mapping with `AllowUnknown` is
  closed in the schema all the same.
- `required` lists the keys `Load` requires inside an object that exists.
  A missing object is not reported, since its values may come from the
  environment.
- Profile sections (`"%dev": {...}`) are allowed at the root with the same
  properties and no `required`. The flat form `%dev.db.url` is accepted
  without checking its value.

In VS Code, with the YAML extension, in `settings.json`:

```json
{
  "yaml.schemas": {
    "./config/app.schema.json": ["config/app*.yaml"]
  }
}
```

For a JSON file, use `json.schemas` with `fileMatch` and `url` instead. In
JetBrains IDEs, open Settings, Languages & Frameworks, Schemas and DTDs,
JSON Schema Mappings, add the file and map it to `config/app*.yaml`. A file
can also name its schema itself, with a first line
`# yaml-language-server: $schema=./app.schema.json`.
