| Key | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `db.url` | `DB_URL` | `string` | required | Address of the database, such as postgres://localhost/app. |
| `db.user` | `DB_USER` | `string` | `app` | User to log in as. |
| `db.password` | `DB_PASSWORD` | `string` (secret) | required | Password of the user. |
| `db.pool.max-size` | `DB_POOL_MAX_SIZE` | `int` | `10` | Maximum number of open connections. |
| `db.pool.timeout` | `DB_POOL_TIMEOUT` | `time.Duration` | `5s` | How long to wait for a free connection. |
