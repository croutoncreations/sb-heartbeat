# Release verification

Tags do not publish immediately. The release workflow first calls the complete
reusable test workflow, including normal tests, race detection, vet, full SQL
and workflow goldens, and the disposable PostgreSQL suite. It then requires a
live test against a dedicated hosted Supabase project before GoReleaser runs.

## One-time hosted fixture setup

Create a dedicated Supabase project used only by SB Heartbeat release
verification. Do not reuse a development, staging, production, or downstream
heartbeat project. From a trusted shell with its database URL available, mark
the project and install the normal managed heartbeat table:

```bash
psql "$SB_HEARTBEAT_HOSTED_DATABASE_URL" \
  --set ON_ERROR_STOP=1 \
  --file scripts/release-fixture-install.sql

go build -o /tmp/sb-heartbeat ./cmd/sb-heartbeat
/tmp/sb-heartbeat migration install | \
  psql "$SB_HEARTBEAT_HOSTED_DATABASE_URL" --set ON_ERROR_STOP=1
```

The fixture installer is collision-safe and idempotent. It creates an exact
marker in the locked `sb_heartbeat_release` schema and refuses to replace a
same-name object it does not recognize. The marker is deliberately separate
from `public.sb_heartbeat`; only that separate marker authorizes release tests
to reset the managed heartbeat table.

Configure these GitHub Actions secrets for that project in the protected
`hosted-supabase-release` environment:

- `SB_HEARTBEAT_HOSTED_DATABASE_URL`
- `SB_HEARTBEAT_HOSTED_URL`
- `SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY`
- `SB_HEARTBEAT_HOSTED_ANON_KEY`

They can be set without placing their values in command history:

```bash
for name in \
  SB_HEARTBEAT_HOSTED_DATABASE_URL \
  SB_HEARTBEAT_HOSTED_URL \
  SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY \
  SB_HEARTBEAT_HOSTED_ANON_KEY; do
  printf '%s' "${!name}" |
    gh secret set "$name" \
      --env hosted-supabase-release \
      --repo croutoncreations/sb-heartbeat
done
```

The database credential is used only to apply the generated guarded migration
and inspect effective grants. The two low-privilege keys each perform the fixed
live heartbeat. Tests do not print values or response bodies. Missing inputs
fail the release; they are never treated as a skip.

Configure the same project's URL and one publishable key for the lightweight
scheduled heartbeat. These are repository-level bindings because this job must
run without a protected-environment approval prompt:

```bash
printf '%s' "$SB_HEARTBEAT_HOSTED_URL" |
  gh variable set SB_HEARTBEAT_RELEASE_FIXTURE_URL \
    --repo croutoncreations/sb-heartbeat

printf '%s' "$SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY" |
  gh secret set SB_HEARTBEAT_RELEASE_FIXTURE_API_KEY \
    --repo croutoncreations/sb-heartbeat

gh variable set SB_HEARTBEAT_RELEASE_FIXTURE_ENABLED \
  --body true \
  --repo croutoncreations/sb-heartbeat
```

Set the enable flag last. Until it is exactly `true`, scheduled and manually
dispatched fixture-heartbeat jobs are skipped. After enabling it, dispatch
`Release fixture heartbeat` once and confirm a healthy result. Thereafter it
runs three times daily with only the low-privilege key. GitHub scheduling is
best-effort, so failed or disabled scheduled runs must still be monitored.

Release validation verifies the private fixture marker before arming cleanup,
temporarily removes the managed heartbeat table, tests a clean install and
uninstall, and restores the table on success or failure. Its final live check
waits for bounded PostgREST schema-cache propagation before failing with a
diagnostic. No manual clearing, project resumption, or post-release repair is
part of the normal release flow.

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

Before pushing a tag, confirm that the scheduled fixture heartbeat is healthy,
then also confirm the product-name/trademark prerequisite, inspect the generated
artifacts, and verify that the repository's protected release environment
limits access to the hosted integration secrets.
