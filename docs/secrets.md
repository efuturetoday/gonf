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
  case, such as `api-token` or `AWS_SECRET_ACCESS_KEY`. Add more words
  with `gonf.WithSecretWords("passphrase")`.
- one of its values is encrypted, see below.

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

Values from a mounted secret directory (`dir.New`, see
[Sources](sources.md#directory-with-one-file-per-key)) follow the same
rules: only the key name, here the file name, or a `Secret` field makes
them secret. A file called `api-key` is shown by `Explain` unless its
Mapping field is a `gonf.Secret`.

## Encrypted values

A value can be written encrypted, so that the file holds no plain secret:

```properties
db.password=${aes-gcm::DBu-8QjM2A1WGDV6fnfS-upDsjQYk4iWBPRvYwW1GZC0z8E}
```

Encrypt a value with the `gonf` command. It reads the key from
`GONF_SECRET_AES_GCM_KEY` and the value without echo, or from a pipe, and
prints the line to paste:

```console
$ export GONF_SECRET_AES_GCM_KEY=...
$ go run github.com/efuturetoday/gonf/cmd/gonf@latest encrypt
Value:
${aes-gcm::DBu-8QjM2A1WGDV6fnfS-upDsjQYk4iWBPRvYwW1GZC0z8E}
```

Neither the key nor the value is an argument, so neither ends up in the
shell history. `-key-file` reads the key from a file.

`boot.Load` and `boot.Watch` decrypt such values without any code. The
key comes from the configuration key `gonf.secret.aes-gcm.key`, which boot
registers with the Mapping `aesgcm.Mapping`, so `GONF_SECRET_AES_GCM_KEY` in
the environment is enough:

```console
$ GONF_SECRET_AES_GCM_KEY=... go run .
```

Any Source may provide the key: the environment, `.env`, a database or a
key store. It must not come from the Source of the encrypted value, since a
key next to the values it protects protects nothing; Load reports that.
Keep the key out of the repository. If the application gets the key
elsewhere, pass it in code:

```go
cfg, err := boot.Load(ctx, boot.WithFunctions(aesgcm.WithKey(key)))
```

Without boot, register the Function with Load:

```go
cfg, err := gonf.Load(ctx,
	gonf.WithFunctions(aesgcm.Function()),
	...
)
```

A decrypted value is a secret: `Explain` shows `****`, and a field for it
must be a `gonf.Secret`, also when it refers to it with `${...}`:

```
gonf: bind "db": db.password: comes from the secret function aes-gcm, make the field a gonf.Secret (from file:config/app.properties:3)
```

A wrong or missing key or a damaged value stops the start without showing
the value:

```
gonf: bind "db": db.password: aes-gcm: wrong key or damaged value (from file:config/app.properties:3)
gonf: bind "db": db.password: aes-gcm: the key gonf.secret.aes-gcm.key has no value (from file:config/app.properties:3)
```

Other ways to decrypt, such as a cloud key service, plug in the same way:
a `gonf.Function` with `IsSecret: true`, passed to `boot.WithFunctions` (or
`gonf.WithFunctions`) and called as `${name::value}`. boot also provides
the Functions `env`, `file` (secret), `base64`, `base64-decode` and `uuid`,
as in `${file::/run/secrets/db-password}`.
