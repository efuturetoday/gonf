# Structs

gonf fills a struct from the keys under a prefix. A Mapping declares the
struct and its prefix once; `Load` binds every Mapping and reports all
problems together, at start:

```go
type Config struct {
	URL  string
	Pool struct {
		MaxSize int           `default:"10"`
		Timeout time.Duration `default:"5s"`
	}
}

var Mapping = gonf.NewMapping[Config]("db")

cfg, err := boot.Load(ctx, boot.WithMappings(Mapping))
...
c := Mapping.Get(cfg)
```

A library declares its Mapping next to its struct, and the application
registers it. Because `Load` knows every Mapping, a broken reload is
rejected, `Explain` masks the Secret fields and `boot.Describe` lists the
keys. Keys whose names are known only at runtime, such as one block per
tenant, go into a map field: `Tenants map[string]Tenant` binds
`tenants.<name>.…`.

`Get` panics if the Mapping was not registered, because that is a
programming error; every configuration error is reported by `Load`.

`Get` binds nothing; it returns the value bound at `Load`. Slices, maps and
pointers in it are copies, so changing them does not change what other
callers get. A struct without them is returned as is, without any copy.
Values of types with a converter or `UnmarshalText`, such as `*url.URL`,
are shared and must not be changed.

## Names

A field is bound to `prefix.name`, where the name is the field name in kebab
case: `MaxSize` becomes `max-size`, `HTTPPort` becomes `http-port`. A `cfg`
tag sets another name, `cfg:"-"` skips the field:

```go
type Config struct {
	URL      string `cfg:"address"` // db.address
	Internal string `cfg:"-"`       // not bound
	Common          // embedded: its fields are bound to db.*
}
```

Environment variables are found by their usual name: `DB_POOL_MAX_SIZE` for
`db.pool.max-size`.

## Required, defaults and optional fields

A field without a value is an error, unless:

| Field | Without a value |
| --- | --- |
| has a `default:"..."` tag | gets the default; `${...}` in it is expanded |
| is a pointer, such as `*int` or `*Pool` | stays nil |
| is a slice or a map | stays empty |

An empty value counts as not set, so `DB_URL=` does not hide a default. An
empty default (`default:""`) leaves the zero value.

## Types

| Type | Written as |
| --- | --- |
| `string` | as is |
| `bool` | `true`/`false`, `1`/`0`, `yes`/`no`, `on`/`off`, in any case |
| integers | `8080`, `1_000`, `0x1f`, `0o644`, `0b101`; a leading zero as in `0644` is an error, since it reads as octal in Go but as decimal elsewhere |
| floats | `0.5`, `1e3` |
| `time.Duration` | `5s`, `1h30m` |
| any type with `UnmarshalText` | its own format, e.g. `netip.Addr`, `slog.Level`, `time.Time` |
| `gonf.Secret[T]` | as T, see [Secrets](secrets.md) |
| a struct | its fields under `prefix.name` |
| a slice | `a,b,c` (`\,` for a literal comma), or indexed keys `hosts[0]`, `hosts[1].port` |
| a map | one entry per key segment: `labels.team=x` gives `labels["team"]` |

A list of structs is written with indexed keys:

```properties
db.replicas[0].host=r1
db.replicas[1].host=r2
```

A map key containing a dot is quoted: `db.servers."eu.west".port=8080`.

## Converters

A type of another package without `UnmarshalText`, such as `*url.URL`, needs
a converter. Register it for `Load`:

```go
gonf.Load(ctx, gonf.WithConverter(url.Parse), ...)
```

It applies wherever the type appears: in fields, pointers, secrets, slice
elements and map keys and values. It replaces the built-in rules, e.g. to
read a bare number as seconds:

```go
gonf.WithConverter(func(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(s)
})
```

A converter matches its type exactly: one for `*url.URL` is not used for
`url.URL`. Two converters for the same type are an error of `Load`.

## Validation

A struct that implements `Validate() error` checks itself after its fields
are bound without problems; inner structs come first:

```go
func (c *Config) Validate() error {
	if c.Pool.MaxSize < 1 {
		return fmt.Errorf("pool.max-size must be at least 1, got %d", c.Pool.MaxSize)
	}
	return nil
}
```

To use a validation library for every struct, pass its function to `Load`:

```go
v := validator.New()
gonf.Load(ctx, gonf.WithValidator(v.Struct), ...)
```

## Unknown keys

A key under the prefix that no field binds is an error that names the
closest known key:

```
db.pool.max-szie: unknown key, did you mean db.pool.max-size?
```

Keys that a bound value refers to with `${...}` count as known. Mappings
may share a prefix; a key is unknown only if none of them binds it.
`gonf.NewMapping[Config]("db", gonf.AllowUnknown())` turns the check off.
Environment variables are not checked, since they cannot be told apart from
variables of other programs.

## Errors

`Load` reports every problem at once, with the file and line of the value:

```
gonf: bind "db": 3 problems:
  db.url: required, no value
  db.pool.max-size: invalid int "ten" (from file:config/app.properties:1)
  db.pool.timeout: invalid duration "30", want e.g. 5s or 1h30m (from env:DB_POOL_TIMEOUT)
```

A field type that gonf cannot read, such as a `func` field or a default on a
struct, is reported before any value is read. The error is a
`*gonf.BindError` whose `Problems` list the keys; `errors.Is(err,
gonf.ErrNotFound)` reports a missing value.
