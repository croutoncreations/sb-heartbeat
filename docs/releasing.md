# Release verification

Tags do not publish immediately. The release workflow first calls the complete
reusable test workflow, including normal tests, race detection, vet, full SQL
and workflow goldens, and the disposable PostgreSQL suite. It then requires a
live test against a dedicated hosted Supabase project before GoReleaser runs.

Configure these GitHub Actions secrets only for a disposable integration
project in the protected `hosted-supabase-release` environment:

- `SB_HEARTBEAT_HOSTED_DATABASE_URL`
- `SB_HEARTBEAT_HOSTED_URL`
- `SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY`
- `SB_HEARTBEAT_HOSTED_ANON_KEY`

The database credential is used only to apply and remove the generated guarded
migration and inspect effective grants. The two low-privilege keys each perform
the fixed live heartbeat. Tests do not print values or response bodies. Missing
inputs fail the release; they are never treated as a skip.

To run the SQL suite locally, create an otherwise disposable PostgreSQL database
whose cluster does not already contain the `anon`, `authenticated`, or
`service_role` fixture roles, then run:

```bash
go build -o /tmp/sb-heartbeat ./cmd/sb-heartbeat
SB_HEARTBEAT_ALLOW_DISPOSABLE_DATABASE=1 \
SB_HEARTBEAT_TEST_DATABASE_URL='postgresql:///your_disposable_database' \
  scripts/integration-postgres.sh /tmp/sb-heartbeat
```

The explicit opt-in is required because the suite creates and removes fixture
roles and the exact `public.sb_heartbeat` test object. Never point it at a
production or shared database.

Before pushing a tag, also confirm the product-name/trademark prerequisite,
inspect the generated artifacts, and verify that the repository's protected
release environment limits access to the hosted integration secrets.
