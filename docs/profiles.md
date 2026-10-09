# Profiles

A profile is a named set of values that applies only while the profile is
active, such as `dev`, `test` or `prod`.

## Writing profile values

Prefix a key with `%profile.`:

```properties
db.url=postgres://prod-db/app
%dev.db.url=postgres://localhost/app
```

With `dev` active, `db.url` is `postgres://localhost/app`; otherwise it is
`postgres://prod-db/app`. This works in every Source:

| Where | Profile value |
| --- | --- |
| `.properties`, `.yaml`, `.json` | `%dev.db.url` (in YAML: `"%dev": {db: {url: ...}}`) |
| environment, `.env` | `_DEV_DB_URL` |
| own file per profile (`boot`) | `db.url` in `config/app-dev.properties` |

`boot` reads `app-<profile>.<ext>` next to every `app.<ext>`, only for
active profiles. Every key in such a file belongs to the profile; a key
with a profile of its own, such as `%prod.db.url`, is an error. To do the
same with your own Sources, use `source.WithProfile("dev")`.

## Activating profiles

With `boot`, set the key `gonf.profile` to a comma-separated list, for
example in the environment:

```sh
GONF_PROFILE=dev ./app
```

The key can also be set in `.env` or in `config/app.properties`. Without
`boot`, pass the profiles to `gonf.Load`:

```go
gonf.Load(ctx,
	gonf.WithProfiles(strings.Split(os.Getenv("APP_PROFILE"), ",")...),
	gonf.WithSources(...),
)
```

or name a key to read them from with `gonf.WithProfileKey("app.profile")`.
`WithProfiles` wins over the key. There is no default profile: without
one, only plain values apply.

A profile name must not contain `.`, `,` or whitespace.

## Which value wins

1. The highest ordinal wins, whether the value belongs to a profile or not.
   An environment variable (300) therefore beats `%dev.db.url` in
   `config/app.properties` (260).
2. At the same ordinal, a value of an active profile beats the plain value.
3. Of several active profiles, the last one wins: with `GONF_PROFILE=dev,local`,
   `%local.db.url` beats `%dev.db.url`.

`cfg.Explain` shows which profile a value came from (see
[Where does a value come from?](explain.md)):

```
app.port = 81 (file:config/app-prod.properties:1, profile prod)
  overrides 80 (file:config/app.properties:2, profile prod)
  default 8080 (main.go:14)
```
