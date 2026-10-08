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

`Describe` returns `[]gonf.Property` with `Key`, `Env`, `Type`, `Default`,
`Required`, `Secret` and `Doc`. The fields have JSON tags, so
`json.Marshal(props)` gives a machine-readable reference. Defaults of secret
fields are masked.

## Always in sync

`Describe` and `Load` read a struct through the same model. A key that the
table marks as required is exactly a key that `Load` reports as missing, and
a field type that `Load` rejects is the same error from `Describe`.
