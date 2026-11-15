# Security

Report a vulnerability privately through
[GitHub](https://github.com/efuturetoday/gonf/security/advisories/new),
not in a public issue. You get an answer within a week.

Fixes go into the latest release only.

gonf trusts its Sources like the code: a value may read files and
environment variables through Functions. Values others may write belong
in a Source made `source.Verbatim()`, see
[Sources](docs/sources.md#expressions).
