# Where does a value come from?

With values from files, `.env`, the environment and profiles, the question
"why is the port 9000?" needs a quick answer. `cfg.Explain` gives it:

```go
fmt.Println(cfg.Explain("app.port"))
```

```
app.port = 9000 (env:APP_PORT)
  overrides 80 (file:config/app.properties:2, profile prod)
  default 8080 (mapping:app)
```

The first line is the value a Mapping binds and where it comes from: the
Source, the file and line, and the profile. The lines below show what it
overrides, highest first, and the default tag of the Mapping. Other lines:

| Line | Meaning |
| --- | --- |
| `app.port = 8080 (mapping:app)` | no Source has a value, the default applies |
| `empty "" (env:APP_PORT)` | a value is set but empty, so the default applies |
| `app.url: gonf: key "app.url": unresolved reference "app.host"` | the value cannot be expanded; `value ...` lines follow |

Secret values are shown as `****`, see [Secrets](secrets.md).

## All values at once

`cfg.Explanations()` explains every key, sorted, e.g. for a debug endpoint
or a log line at startup:

```go
for _, e := range cfg.Explanations() {
	log.Println(e)
}
```

It covers the keys of all files and the keys the Mappings read. Environment
variables that no Mapping reads, such as `PATH`, are left out.

## As data

`Explain` returns an `Explanation`, so the same information can be served
as JSON or rendered otherwise:

| Field | Content |
| --- | --- |
| `Key`, `Value` | the key and the value a Mapping binds, expanded |
| `Origin` | Source, ordinal, profile and location of the value |
| `Candidates` | every value for the key, the winner first |
| `Default` | the default tag of a Mapping, if any |
| `IsSecret` | whether values are masked |
| `Err` | why the key has no value |

Secret values are already masked in every field, so an `Explanation` can be
printed or sent as it is.

For the raw data without masking, `cfg.Lookup(key)` returns the winning
value with its Origin, `cfg.Candidates(key)` all values and `cfg.Keys()` all
keys.
