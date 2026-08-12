# Release verification

Tags do not publish immediately. The release workflow first calls the complete
reusable test workflow, including normal tests, race detection, vet, full SQL
and workflow goldens, and the disposable PostgreSQL suite. It then requires a
live test against a dedicated hosted Supabase project and actual calendar
delivery from disposable macOS `launchd` and Linux `systemd --user` schedules
before GoReleaser runs. The scheduler probes make no network requests and
receive no repository secrets.

## One-time immutable release setup

Before the next release, a repository owner must open repository **Settings**,
find **Releases**, and select **Enable release immutability**. This protects
future release tags and assets from replacement and lets GitHub create a signed
release attestation when a draft is published. Immediately before pushing a
release tag, an authenticated repository administrator must verify the setting:

```bash
gh api repos/croutoncreations/sb-heartbeat/immutable-releases \
  --jq '.enabled == true'
```

The ephemeral workflow token cannot read this repository-administration
endpoint, so the release workflow deliberately does not carry a long-lived
administration credential. It rechecks draft state immediately before
publication and then requires the published release's public `immutable` field
to be true. It never changes the administrative setting itself.

GoReleaser uploads the six platform archives and `checksums.txt` to a draft.
The workflow verifies the exact local artifact set and checksums, confirms the
release is still a draft, creates signed build provenance for the archives and
checksum file using the verified manifest as the exact archive subject list,
downloads the draft assets, and verifies the remote bytes again.
It rejects any additional uploaded asset and confirms the remote tag still
identifies the workflow's triggering commit immediately before publication.
Only then does it publish the draft. Consequently, no release is published if
attestation fails, an asset differs, the final identity check detects a changed
tag, or the release is unexpectedly public. GitHub does not expose a
conditional publish operation, so repository writers remain trusted during the
brief final check-to-publication interval.

After publishing a version such as `v0.2.0`, verify the immutable release and a
downloaded asset with GitHub CLI:

```bash
gh release verify v0.2.0
gh release verify-asset v0.2.0 sb-heartbeat_0.2.0_linux_amd64.tar.gz
gh attestation verify sb-heartbeat_0.2.0_linux_amd64.tar.gz \
  -R croutoncreations/sb-heartbeat
```

`gh release verify` checks GitHub's immutable-release attestation;
`gh release verify-asset` checks that the local bytes match that release; and
`gh attestation verify` validates the workflow's signed build-provenance
attestation. Source archives generated on demand by GitHub are outside the
uploaded-asset verification set.

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

### Cloudflare live release fixture

The release also deploys a uniquely named generated Worker, inspects its
deployed configuration, observes an actual deployed Cron Trigger, and deletes
the Worker after the check. Configure the six inputs below
in the protected `cloudflare-live-release` environment:

- `SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID`
- `SB_HEARTBEAT_CLOUDFLARE_API_TOKEN`
- `SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL`
- `SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY`
- `SB_HEARTBEAT_CLOUDFLARE_ANON_URL`
- `SB_HEARTBEAT_CLOUDFLARE_ANON_KEY`

Use two distinct dedicated Supabase projects: one fixture must use a current
publishable key and the other a legacy anon key. Install the normal managed
heartbeat table in each project and verify both with `sb-heartbeat doctor`
before storing the values. Do not use production, staging, or downstream
heartbeat projects as release fixtures.

Create a Cloudflare API token scoped to the fixture account only, with the
least privilege needed by the harness: `Workers Scripts: Edit` and
`Workers Tail: Read`. Do not grant
zone, DNS, billing, user, or account-management permissions. The harness uses
that access to create and remove one uniquely named Worker, read its source,
bindings, private subdomain settings, and Cron Trigger, and observe its sanitized
logs. It refuses to remove a Worker unless the downloaded source
contains its per-run ownership marker.

Configure the GitHub environment with required reviewers and restrict its
protected branches and tags to the default branch and release tags. The
reusable workflow independently accepts only version tags, or a manual dispatch
from `main`. Set values through protected prompts or standard input, never as
command arguments:

```bash
for name in \
  SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID \
  SB_HEARTBEAT_CLOUDFLARE_API_TOKEN \
  SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL \
  SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY \
  SB_HEARTBEAT_CLOUDFLARE_ANON_URL \
  SB_HEARTBEAT_CLOUDFLARE_ANON_KEY; do
  printf '%s' "${!name}" |
    gh secret set "$name" \
      --env cloudflare-live-release \
      --repo croutoncreations/sb-heartbeat
done
```

Dependency installation and generated contract tests run in a separate step
before these secrets enter the job environment. The checked npm lock pins the
entire dependency graph with integrity hashes; lifecycle scripts are disabled
during installation. Missing credentials, a reused Supabase origin, unexpected
Cloudflare state, failed execution, or unprovable cleanup fails closed.

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
