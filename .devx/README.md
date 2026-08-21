# SB Heartbeat DevX sessions

Create a session after registering the project:

```bash
devx session create my-change --project sb-heartbeat
```

Sessions run on the macOS host and allocate an isolated `POSTGRES` port. The
`database` window starts a disposable PostgreSQL 16 container matching the CI
integration service. It binds only to `127.0.0.1`, stores its database on a
temporary filesystem, and is removed when its pane exits.

The setup never sources `.env` and never starts `sb-heartbeat run` or
`sb-heartbeat doctor`. This prevents a development session from contacting a
real Supabase project. Generated smoke-test files stay in a temporary directory.

Once the database pane reports readiness, run the complete local smoke check:

```bash
.devx/smoke.sh
```

The smoke check builds the CLI, exercises non-networked commands, and runs the
disposable PostgreSQL integration suite. Normal contributor gates remain:

```bash
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
```

The disposable database is available at
`postgres://postgres:postgres@127.0.0.1:$POSTGRES/postgres?sslmode=disable`.
Only pass that URL to `scripts/integration-postgres.sh`; never point the suite
at a shared or production database.
