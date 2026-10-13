# Reloading

`boot.Watch` loads the configuration like `boot.Load` and keeps it current
while the application runs:

```go
live, err := boot.Watch(ctx, boot.WithMappings(appConfig))
if err != nil {
	log.Fatal(err)
}

http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	app := appConfig.Get(live.Config()) // the current configuration
	...
})
```

A `Config`, and a struct bound from it, never changes. Read
`live.Config()` again for every unit of work, such as a request; a struct
kept from the start keeps the values of the start.

## What happens on a change

Edit `config/app.properties` while [`examples/reload`](../examples/reload)
runs:

```console
$ go run .
2026/10/08 14:19:18 hello
2026/10/08 14:19:18 INFO gonf: configuration reloaded
2026/10/08 14:19:19 hi there
2026/10/08 14:19:20 WARN gonf: reload failed, keeping the previous configuration error="gonf: bind \"app\": app.interval: invalid duration \"soon\", want e.g. 5s or 1h30m (from file:config/app.properties:2)"
2026/10/08 14:19:21 hi there
2026/10/08 14:19:24 INFO gonf: configuration reloaded
2026/10/08 14:19:25 bye
```

A change starts a complete load: all files, profiles and Mappings, with
the same checks as at the start. If it succeeds, `live.Config()` returns
the new configuration; if it fails, the previous one stays and the error is
logged. Since everything is loaded again, a new `gonf.profile` in a file
also switches the profile files.

## What is watched

| Source | Watched |
| --- | --- |
| `config/app.*`, `config/app-<profile>.*`, `.env` | checked every 2 s |
| files embedded with `boot.WithFS` | no, they never change |
| environment variables | no, they do not change while the process runs |

Check every 5 s instead:

```go
boot.Watch(ctx,
	boot.WithFileTrigger(func(string) source.Trigger { return source.Every(5 * time.Second) }),
)
```

`WithFileTrigger` gets the path of every file, so a Trigger driven by file
system events can replace polling.

## Reacting and logging

`boot.Watch` logs to `slog.Default()`: every reload at level Info, failed
reloads and Sources that failed at level Warn. `boot.WithLogger` sets
another logger. To react to reloads, e.g. for a metric or a health check,
pass a function:

```go
boot.Watch(ctx, boot.OnReload(func(cfg *gonf.Config, err error) {
	if err != nil {
		reloadFailures.Inc()
	}
}))
```

## Your own Sources

If `Watch` of a Source fails, gonf reports the error like a failed reload,
watches again after 1 s, then after twice as long each time up to 1 min,
and reloads with each retry, since changes may have been missed.

A Source that notices changes itself implements `gonf.Watcher`. A Source
reading from a database could check a version every 5 s and report a
change only when it differs from what `Load` read:

```go
func (s *dbSource) Watch(ctx context.Context, changed func()) error {
	return source.Every(5*time.Second)(ctx, func() {
		var version int64
		err := s.db.QueryRowContext(ctx, "SELECT max(version) FROM config").Scan(&version)
		if err == nil && version != s.loaded.Load() {
			changed()
		}
	})
}
```

Who closes a Source depends on who created it:

- A Source passed with `gonf.WithSources` belongs to you. Open its
  connection before and close it after, e.g. with `defer db.Close()`.
- A Source returned by a factory belongs to gonf. If it has a
  `Close() error` method, gonf calls it when the Source is no longer used:
  `Load` right after loading, `Watch` when a reload replaces it or watching
  stops. A factory may return the same Source again to keep it open.

## Without boot

`gonf.Watch` takes the same options as `gonf.Load`, plus `gonf.OnReload`
and `gonf.WithLogger`, which `gonf.Load` does not accept. Files from
`file.New` are checked every 2 s; `source.WithTrigger` changes that:

```go
live, err := gonf.Watch(ctx,
	gonf.WithSources(
		file.New("app.properties", properties.Format),
		env.New(),
	),
	gonf.WithMappings(appConfig),
)
```

Watching stops when `ctx` ends.
