# Secrets

A field of type `gonf.Secret[T]` holds a value that must not show up in
output:

```go
type DB struct {
	User     string
	Password gonf.Secret[string]
}

db := dbMapping.Get(cfg)
fmt.Println(db.Password)          // ****
fmt.Printf("%+v\n", db)           // {User:app Password:****}
slog.Info("db", "config", db)     // ... Password:****
json.Marshal(db)                  // {"User":"app","Password":"****"}
connect(db.Password.Value())      // the real value
```

The value is converted as for a field of type `T`, so `Secret[int]` or a
`Secret` of a type with `UnmarshalText` work too. An invalid value is
reported without the value itself:

```
db.pin: invalid int, value hidden (from env:DB_PIN)
```

A `Secret` field is required like any other; use `*gonf.Secret[T]` for an
optional one. In tests, create one with `gonf.NewSecret("s3cret")`.

## What gets masked

`cfg.Explain`, `cfg.Explanations` and `gonf.Describe` replace secret values
with `****`. A key is secret if

- a Mapping binds it to a `Secret`, or
- its name contains `password`, `secret`, `token` or `credential`, in any
  case, such as `api-token` or `AWS_SECRET_ACCESS_KEY`.

A value that refers to a secret key is masked in that part only:

```
db.url = postgres://app:****@db/app (file:config/app.properties:3)
```

A field holds what it binds, so a `string` field for
`db.url=postgres://app:${db.password}@db/app` would hold the real password
and print it. gonf therefore reports such a field when it loads:

```
gonf: bind "db": db.url: refers to the secret db.password, make the field a gonf.Secret (from file:config/app.properties:3)
```

This holds for every field that is no `Secret` and refers to a secret key,
also through other keys or in a default, and also when the secret belongs
to another Mapping.

`cfg.Get` and `cfg.Lookup` return the real values: masking applies to
output, not to reading.
