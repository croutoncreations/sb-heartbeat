# SB Heartbeat — Product Specification and Implementation Plan

Status: Draft for implementation  
Specification version: 0.3  
Last updated: 2026-08-04

## 1. Product summary

SB Heartbeat is a small, open-source Go CLI that helps users keep intentionally
retained, low-traffic Supabase projects active by periodically executing
legitimate, read-only database activity.

The product emphasizes:

- least-privilege access;
- user-owned infrastructure;
- straightforward installation;
- clear diagnostics and stable automation behavior;
- safe support for multiple projects;
- agent-friendly setup and documentation; and
- no hosted credential custody.

SB Heartbeat is not a hosted service. Users run it through infrastructure they
already control, initially GitHub Actions or a local scheduler. Cloudflare
Workers, Docker, and additional schedulers remain planned 1.0 targets.

Product name: **SB Heartbeat**. The repository and package name are
`sb-heartbeat`; PostgreSQL objects use the identifier-safe `sb_heartbeat`
prefix. A formal trademark check remains a release prerequisite.

## 2. Problem

Supabase may pause Free Plan projects that receive insufficient user database
activity over a seven-day period. Developers often retain staging projects,
prototypes, demos, personal projects, and infrequently used internal tools that
are still valuable but do not naturally receive consistent traffic.

Existing solutions are usually small workflow snippets or scripts. They often
lack one or more of:

- an explicit least-privilege database design;
- current support for both legacy anon keys and publishable keys;
- permission and configuration diagnostics;
- multi-project support;
- reliable structured output and exit behavior;
- secure installation and release practices; or
- setup instructions that coding agents can follow safely.

Users need a polished way to create ordinary database activity without
granting elevated access, mutating application data, maintaining a dedicated
fork, or giving credentials to a third-party hosted service.

## 3. Product thesis

The safest default is a scheduled, read-only PostgREST query made with a
low-privilege Supabase client key against a dedicated one-row heartbeat table.
The table is protected by explicit PostgreSQL grants and a narrow Row Level
Security policy.

SB Heartbeat's differentiator is not the HTTP request alone. It is the complete
operational experience:

- generate an auditable least-privilege migration;
- support both familiar legacy anon keys and current publishable keys;
- validate the precise configured operation;
- generate safe scheduler configuration;
- provide actionable failure classifications;
- support noninteractive setup; and
- document installation well enough for both people and coding agents.

The product is best-effort. It must never promise that Supabase will count a
particular request forever or that a project can never be paused.

## 4. Product principles

1. **Read-only by default.** The normal heartbeat cannot mutate data.
2. **Least privilege is declared, not assumed.** Generated SQL explicitly
   revokes broad access and grants only the access the heartbeat needs.
3. **Low-privilege keys only.** Both publishable and legacy anon keys are
   supported. Secret and service-role keys are rejected.
4. **User-owned execution.** SB Heartbeat does not host checks or retain keys.
5. **Safe automation.** Results are deterministic, secrets are redacted, and
   exit codes remain consistent for single- and multi-project runs.
6. **Strict inputs.** Configuration, URLs, queries, and responses are parsed
   conservatively.
7. **Approachable defaults.** Familiar terminology is retained while guiding
   new users toward publishable keys.
8. **Agent readiness without repository interference.** Agent documentation
   is task-oriented and must not overwrite a downstream repository's existing
   `AGENTS.md` or other instruction files.

## 5. Goals

### 5.1 MVP goals

The MVP must:

1. Run a real Supabase database query on demand.
2. Support one or more configured projects.
3. Support both a publishable key and a legacy anon key.
4. Prefer publishable keys in new examples while explaining anon keys clearly.
5. Reject secret and service-role credentials.
6. Generate a collision-safe, explicit least-privilege heartbeat migration.
7. Execute only the fixed read-only heartbeat-table select.
8. Validate the expected response with bounded resource use.
9. Produce useful text and JSON output.
10. Use a small, stable exit-code contract.
11. Provide `init`, `run`, `doctor`, and migration commands.
12. Generate a GitHub Actions workflow with three daily runs by default.
13. Allow the schedule to be configured.
14. Run locally through standard cron or another scheduler.
15. Support fully noninteractive setup.
16. Provide safe, task-oriented agent installation documentation.
17. Avoid requiring users to fork a dedicated repository.
18. Never require an elevated Supabase key for normal usage.

### 5.2 1.0 goals and tracked follow-up work

The following remain part of the product plan but are deferred from the MVP:

- existing-table custom selects with expanded validation;
- RPC queries with explicit warnings;
- constrained raw PostgREST paths;
- Cloudflare Worker and Cron Trigger generation;
- Docker image and multi-architecture publishing;
- local status history and optional durable result backends;
- failure notifications;
- backup checks and archive guidance;
- paused-project inventory and optional Management API integration;
- metrics export and additional scheduler generators;
- shell completions;
- `llms.txt` and, if it proves useful, generated `llms-full.txt`;
- agent installation tests with multiple coding agents; and
- signed artifacts and provenance where practical.

These items must remain visible in the roadmap and issue tracker. Deferring them
does not remove them from the intended polished 1.0 release.

## 6. Non-goals

The MVP will not:

- host checks on behalf of users;
- store user credentials centrally;
- guarantee that Supabase will never pause a project;
- provide a web dashboard;
- manage billing or automatically create projects;
- require or accept service-role, secret, database, or Management API
  credentials for heartbeats;
- apply database migrations automatically;
- run arbitrary shell commands;
- support direct raw PostgreSQL connections;
- become a general-purpose uptime monitoring platform; or
- replace backups or an appropriate paid plan for production workloads.

## 7. Target users and core stories

The primary user is a developer with one or more low-traffic Supabase projects
who is comfortable with a terminal and may use GitHub Actions or a coding agent
for setup. A secondary user runs cron, a NAS, a home server, or an internal CI
system and needs a portable binary with predictable output.

Core stories:

- Initialize SB Heartbeat in an existing repository or standalone directory.
- Generate SQL without handing SB Heartbeat database credentials.
- Apply the SQL separately and verify the resulting heartbeat.
- Reference a publishable or anon key through an environment variable.
- Check all projects or one named project.
- Consume results as stable JSON.
- Generate a secure GitHub Actions workflow.
- Configure a schedule while receiving a safe three-times-daily default.
- Give a coding agent a documentation URL and have it complete all safe steps
  without exposing keys or overwriting repository instructions.

## 8. Default heartbeat design

### 8.1 Database object

The generated installation migration must be explicit, repeatable, and fail
closed if its object name is already in use. SB Heartbeat marks its table with the
exact comment `sb-heartbeat:managed:v1`. A rerun proceeds only when the existing
object is an ordinary table with that marker and the expected v1 columns,
primary key, and single-row check constraint. Otherwise the migration raises an
exception before changing grants, policies, data, or RLS state.

The normative migration follows this structure; the generated version contains
the complete catalog checks rather than treating this example as pseudocode:

```sql
do $sb_heartbeat$
begin
  if to_regclass('public.sb_heartbeat') is null then
    create table public.sb_heartbeat (
      id boolean primary key,
      created_at timestamptz not null default now(),
      constraint sb_heartbeat_single_row check (id is true)
    );

    comment on table public.sb_heartbeat
      is 'sb-heartbeat:managed:v1';
  else
    if not exists (
      select 1
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where c.oid = 'public.sb_heartbeat'::regclass
        and n.nspname = 'public'
        and c.relkind = 'r'
        and obj_description(c.oid, 'pg_class') = 'sb-heartbeat:managed:v1'
        and (
          select count(*)
          from pg_attribute a
          where a.attrelid = c.oid and a.attnum > 0 and not a.attisdropped
        ) = 2
        and exists (
          select 1 from pg_attribute a
          where a.attrelid = c.oid and a.attname = 'id'
            and a.atttypid = 'pg_catalog.bool'::regtype and a.attnotnull
        )
        and exists (
          select 1 from pg_attribute a
          where a.attrelid = c.oid and a.attname = 'created_at'
            and a.atttypid = 'pg_catalog.timestamptz'::regtype and a.attnotnull
            and exists (
              select 1 from pg_attrdef d
              where d.adrelid = a.attrelid and d.adnum = a.attnum
            )
        )
        and exists (
          select 1
          from pg_constraint k
          join pg_attribute a
            on a.attrelid = k.conrelid and a.attnum = k.conkey[1]
          where k.conrelid = c.oid and k.contype = 'p'
            and cardinality(k.conkey) = 1 and a.attname = 'id'
        )
        and exists (
          select 1 from pg_constraint k
          where k.conrelid = c.oid
            and k.conname = 'sb_heartbeat_single_row'
            and k.contype = 'c'
            and lower(regexp_replace(
              pg_get_expr(k.conbin, k.conrelid), '[[:space:]()]', '', 'g'
            )) = 'idistrue'
        )
    ) then
      raise exception
        'public.sb_heartbeat exists but is not a valid SB Heartbeat v1 table';
    end if;
  end if;
end
$sb_heartbeat$;

insert into public.sb_heartbeat (id)
values (true)
on conflict (id) do nothing;

alter table public.sb_heartbeat enable row level security;

revoke all on table public.sb_heartbeat
from public, anon, authenticated, service_role;

grant select (id)
on table public.sb_heartbeat
to anon;

drop policy if exists "sb_heartbeat_read"
on public.sb_heartbeat;

create policy "sb_heartbeat_read"
on public.sb_heartbeat
for select
to anon
using (id is true);
```

The generated `DO` block checks `pg_class`, `pg_namespace`, `pg_attribute`,
`pg_constraint`, and `obj_description`. It creates no helper object, avoiding an
elevated or generally executable validation function.

Design notes:

- The migration does not rely on Supabase's project-level default grants.
- An unrelated table, view, materialized view, foreign table, or malformed
  partial installation with the same name causes a hard failure.
- `anon` can select only the `id` column.
- `authenticated` receives no heartbeat-table access by default because the
  heartbeat does not need it.
- No insert, update, or delete policy is created.
- The owner inserts the fixed row while applying the migration.
- The migration does not alter schema-wide grants because doing so could affect
  unrelated application objects. `doctor` reports the observable denial and
  lists missing `USAGE` as one possible cause rather than silently broadening
  access or claiming to prove the cause.

The uninstall migration uses the same ownership and shape checks. It is a
no-op when the object is absent and aborts when a same-named object is not a
valid SB Heartbeat v1 table. Only a validated SB Heartbeat-owned table is removed:

```sql
do $sb_heartbeat$
begin
  if to_regclass('public.sb_heartbeat') is null then
    return;
  end if;

  if not exists (
    select 1
    from pg_class c
    join pg_namespace n on n.oid = c.relnamespace
    where c.oid = 'public.sb_heartbeat'::regclass
      and n.nspname = 'public'
      and c.relkind = 'r'
      and obj_description(c.oid, 'pg_class') = 'sb-heartbeat:managed:v1'
      and (
        select count(*)
        from pg_attribute a
        where a.attrelid = c.oid and a.attnum > 0 and not a.attisdropped
      ) = 2
      and exists (
        select 1 from pg_attribute a
        where a.attrelid = c.oid and a.attname = 'id'
          and a.atttypid = 'pg_catalog.bool'::regtype and a.attnotnull
      )
      and exists (
        select 1 from pg_attribute a
        where a.attrelid = c.oid and a.attname = 'created_at'
          and a.atttypid = 'pg_catalog.timestamptz'::regtype and a.attnotnull
          and exists (
            select 1 from pg_attrdef d
            where d.adrelid = a.attrelid and d.adnum = a.attnum
          )
      )
      and exists (
        select 1
        from pg_constraint k
        join pg_attribute a
          on a.attrelid = k.conrelid and a.attnum = k.conkey[1]
        where k.conrelid = c.oid and k.contype = 'p'
          and cardinality(k.conkey) = 1 and a.attname = 'id'
      )
      and exists (
        select 1 from pg_constraint k
        where k.conrelid = c.oid
          and k.conname = 'sb_heartbeat_single_row'
          and k.contype = 'c'
          and lower(regexp_replace(
            pg_get_expr(k.conbin, k.conrelid), '[[:space:]()]', '', 'g'
          )) = 'idistrue'
      )
  ) then
    raise exception
      'refusing to remove non-SB Heartbeat object public.sb_heartbeat';
  end if;

  drop table public.sb_heartbeat;
end
$sb_heartbeat$;
```

As with installation, uninstall uses the validation predicate inline and
creates no persistent helper function.

By default, migration commands print SQL to standard output. They may write a
file when an explicit output path is supplied. They never apply SQL.

### 8.2 Fixed MVP request

The MVP request is fixed and is equivalent to:

```http
GET /rest/v1/sb_heartbeat?select=id&id=eq.true&limit=1
```

Credential headers depend on the key type:

- Publishable key: send `apikey: sb_publishable_...` and no bearer header.
- Legacy anon JWT: send `apikey: <key>` and
  `Authorization: Bearer <key>`.

The MVP configuration cannot change the schema, table, columns, filter, limit,
or expectation. Generic existing-table selects are a 1.0 capability and require
their own schema-exposure, `Accept-Profile`, operator, data-sensitivity, and
expectation contracts before implementation.

The implementation must have tests for both key forms. Header behavior should be
verified against a disposable Supabase integration-test project before release.

The runner validates:

- a successful HTTP response;
- a JSON content type and strictly parseable JSON body;
- exactly one row;
- an `id` value of `true`;
- no redirect;
- a bounded response body; and
- completion within the configured timeout.

### 8.3 Why a table

The dedicated table produces user database activity, requires only anonymous
read access, avoids application data, performs no ongoing writes, and is easier
to audit than a security-definer function.

## 9. Key support and credential handling

### 9.1 Supported keys

SB Heartbeat supports:

- Supabase publishable keys (`sb_publishable_...`), recommended for new setup;
- legacy Supabase anon JWT keys, supported because they remain common and
  familiar.

SB Heartbeat rejects:

- Supabase secret keys (`sb_secret_...`);
- legacy JWT keys whose decoded role is `service_role`; and
- keys that are missing or do not match a supported low-privilege form.

Key classification is a safety check, not authentication. Supabase remains the
authority that validates a key.

### 9.2 Storage guidance

Configuration references environment variables by default. Generated examples
recommend GitHub secrets or repository/environment variables and explain the
tradeoff:

- Publishable and anon keys are client keys whose effective access is governed
  by database grants and RLS.
- Keeping them out of version control is still useful defense-in-depth and
  reduces accidental reuse or quota abuse.
- Users may deliberately store a low-privilege public key in their own
  repository; SB Heartbeat warns but does not prohibit this.
- The SB Heartbeat development repository itself must never contain real project
  keys, `.env` files, or integration credentials.

Inline key values are excluded from the MVP. They remain a possible 1.0 feature
only if a concrete use case outweighs the ambiguity and leakage risk.

### 9.3 Legacy-key lifecycle

Legacy anon support is a compatibility feature, not the long-term preferred
path. SB Heartbeat supports it throughout the 0.x series and in 1.0 only while the
hosted Supabase platform still accepts it. Hosted behavior is tested before
each release. A platform-driven warning or removal can occur in a future minor
release without changing the generic `api_key` configuration field; an actual
SB Heartbeat-driven removal requires a major release and migration documentation.

## 10. Configuration

Default file: `sb-heartbeat.yaml`

```yaml
version: 1

defaults:
  timeout: 10s
  retries: 1
  retry_backoff: 2s
  concurrency: 4
  output: text

scheduler:
  cron: "37 3,11,19 * * *"

projects:
  - name: travally-staging
    url:
      env: TRAVALLY_SUPABASE_URL
    api_key:
      env: TRAVALLY_SUPABASE_API_KEY
```

Configuration requirements:

- Strict decoding rejects unknown fields and duplicate mapping keys.
- The only MVP query is the fixed heartbeat described in section 8.2; there is
  no `query` or `expect` configuration surface.
- Project names match `[a-z][a-z0-9_-]{0,62}` and are unique.
- Explicit environment variable names match `[A-Z_][A-Z0-9_]{0,126}`.
- Suggested environment names normalize hyphens to underscores. Initialization
  rejects collisions after normalization, such as `a-b` and `a_b`.
- Durations and retry counts are bounded.
- A published JSON Schema is planned for 1.0 and may be added earlier if cheap.

Suggested implicit environment variable names for `travally-staging` are:

```text
SB_HEARTBEAT_TRAVALLY_STAGING_URL
SB_HEARTBEAT_TRAVALLY_STAGING_API_KEY
```

Explicit environment names always take precedence.

Configuration discovery is deterministic:

1. `--config PATH`, when supplied;
2. otherwise exactly `./sb-heartbeat.yaml` in the current working directory.

There is no upward directory search and no configuration-path environment
variable in the MVP. A missing file is exit 2. Relative paths are resolved from
the process working directory and the resolved path is shown in diagnostics.

### 10.1 Runtime limits and retry semantics

Normative defaults and hard limits are:

| Setting | Default | Allowed range |
| --- | ---: | ---: |
| Per-attempt timeout | 10 seconds | 1–60 seconds |
| Retries after the first attempt | 1 | 0–3 |
| Initial retry backoff | 2 seconds | 100 ms–30 seconds |
| Concurrent projects | 4 | 1–16 |
| Response body | 64 KiB | fixed in MVP |

`retries: 1` means at most two total attempts. Timeout is per attempt. Backoff
occurs between attempts and counts toward the project's total elapsed duration.
Backoff doubles after each retry and is capped at 30 seconds. A valid
`Retry-After` value is honored only up to that same cap.

Retries apply to temporary DNS/connect/TLS transport failures, timeouts, and
HTTP 408, 425, 429, 502, 503, 504, and 544. They do not apply to HTTP 400, 401,
403, 404, 406, 409, 422, or 540, invalid JSON, an oversized body, an unexpected
row, or a locally rejected credential. Cancellation stops outstanding and
future attempts promptly.

Each project result includes `attempts` and total `latency_ms`, measured from
the first attempt until the final result, including retry backoff. Output order
remains configuration order.

### 10.2 Hosted URL contract

The MVP accepts only hosted Supabase project origins in this form:

```text
https://<project-ref>.supabase.co
```

`<project-ref>` must be one nonempty lowercase ASCII DNS label matching
`[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`. The input may have one trailing `/`,
which is removed during normalization. It may not contain userinfo, an explicit
port, another path, query, fragment, uppercase/Unicode host spelling, a trailing
DNS dot, an IP literal, or additional labels. The parsed hostname must consist
of exactly `<project-ref>`, `supabase`, `co`; suffix string matching is
insufficient.

Custom domains, self-hosted installations, preview-specific URL forms that do
not satisfy this rule, and HTTP development endpoints are deferred to 1.0. They
will require an explicit opt-in and a separate, fully specified policy.

## 11. Scheduling

The default schedule is three runs each day:

```cron
37 3,11,19 * * *
```

The schedule is configurable through configuration and generator flags. The
non-round minute reduces scheduler congestion. A later release may derive a
stable per-project or per-installation minute to distribute load.

SB Heartbeat documentation must state that:

- schedulers are not guaranteed to run at an exact instant;
- GitHub scheduled workflows can be delayed or dropped under load;
- public-repository schedules may be disabled after prolonged repository
  inactivity under GitHub's current policy;
- manual dispatch should be retained for diagnosis; and
- Supabase's activity classifier can change.

Local cron installation prints a suggested entry and never edits the user's
crontab. Documentation covers absolute paths, environment availability, file
permissions, logs, and exit-code monitoring.

## 12. CLI design

### 12.1 MVP commands

```text
sb-heartbeat init
sb-heartbeat run
sb-heartbeat doctor
sb-heartbeat migration install
sb-heartbeat migration uninstall
sb-heartbeat install github
sb-heartbeat version
```

`sb-heartbeat check` may be a hidden or documented alias for `run` if it adds
negligible maintenance cost.

### 12.2 `init`

Interactive setup gathers a project name, URL environment variable, API-key
environment variable, and optional schedule. It creates configuration and can
generate migration and GitHub workflow files.

Noninteractive setup supports every essential choice:

```bash
sb-heartbeat init \
  --non-interactive \
  --project-name travally-staging \
  --url-env TRAVALLY_SUPABASE_URL \
  --api-key-env TRAVALLY_SUPABASE_API_KEY \
  --scheduler github \
  --cron "37 3,11,19 * * *"
```

Initialization works outside Git repositories. Repository-specific generation
is enabled only when applicable.

MVP file-generation behavior is explicit:

- `init` writes only `./sb-heartbeat.yaml` unless generation flags are supplied.
- `migration install|uninstall` prints to standard output unless
  `--output PATH` is supplied.
- `init --migration-output PATH` writes the install migration to that exact
  path; it does not invent a timestamped migration filename.
- `init --scheduler github` and `install github` target
  `.github/workflows/sb-heartbeat.yml`.
- `--force` atomically replaces only the exact requested generated file. It
  never merges YAML or SQL, creates a backup, follows a symlink target, or
  overwrites a directory.
- Without `--force`, any existing target is an error.
- Commands that would write several files preflight every target and render all
  content before writing. A collision or render error produces no writes.
- Each individual file is written to a same-directory temporary file, synced,
  permissions set, and atomically renamed. A process or filesystem failure
  between multiple renames may still leave a partial multi-file result; the
  command reports every completed path so recovery is explicit.

### 12.3 `run`

```bash
sb-heartbeat run
sb-heartbeat run --project travally-staging
sb-heartbeat run --output json
```

Multiple projects use the section 10.1 concurrency contract. Output ordering
remains configuration order even when requests execute concurrently.

### 12.4 `doctor`

`doctor` validates or directly observes:

- strict configuration syntax and semantics;
- required environment variables;
- supported low-privilege key type;
- HTTPS and host rules;
- the precise request method and redacted path;
- successful heartbeat access;
- expected response shape;
- redirect rejection and timeout configuration; and
- the standard hosted URL contract.

`doctor` must not claim to prove that unrelated tables are inaccessible. It
must not issue insert, update, delete, or potentially mutating RPC requests as a
negative permission test.

With only a low-privilege Data API request, `doctor` also cannot prove whether
a denial came specifically from schema `USAGE`, a table or column grant, RLS,
schema exposure, or Data API configuration. It reports the observable HTTP and
bounded PostgREST error classification, then lists likely causes and optional
administrator-run diagnostic SQL. It never claims elevated introspection.

### 12.5 Migration commands

Migration commands print or write auditable SQL. They do not invoke the
Supabase CLI, connect to PostgreSQL, or apply migrations.

## 13. Output and exit codes

Text output is concise:

```text
✓ travally-staging healthy 184ms
✗ old-prototype paused
```

JSON output is versioned:

```json
{
  "schema_version": 1,
  "started_at": "2026-07-31T13:42:01Z",
  "finished_at": "2026-07-31T13:42:03Z",
  "success": false,
  "projects": [
    {
      "name": "travally-staging",
      "status": "healthy",
      "http_status": 200,
      "latency_ms": 184,
      "attempts": 1,
      "error": null
    },
    {
      "name": "old-prototype",
      "status": "paused",
      "http_status": 540,
      "latency_ms": 97,
      "attempts": 1,
      "error": {
        "code": "project_paused",
        "message": "project appears to be paused"
      }
    }
  ]
}
```

Stable process exit codes:

```text
0  All requested checks passed
1  One or more requested checks failed
2  Invalid invocation, configuration, or missing required input
3  Internal CLI error
```

These codes do not change between single- and multi-project runs. Detailed
classifications belong in text and JSON results.

Result classifications include:

- `healthy`;
- `timeout`;
- `dns_failure`;
- `tls_failure`;
- `credential_rejected`;
- `database_permission_denied`;
- `api_authentication_failed`;
- `temporary_upstream_failure`;
- `project_paused`;
- `unexpected_response`;
- `missing_input`;
- `no_matching_row`;
- `response_too_large`; and
- `internal_error`.

### 13.1 Failure envelope and preflight behavior

JSON is emitted for every exit when `--output json` is requested. Check runs
use the envelope shown above. Invocation, configuration, and internal failures
that prevent a check run use:

```json
{
  "schema_version": 1,
  "success": false,
  "error": {
    "code": "invalid_configuration",
    "message": "configuration could not be loaded"
  }
}
```

Error `code` values and field types are stable within schema version 1. Human
messages are diagnostic and may improve between releases.

Configuration loading, schema validation, duplicate project names, normalized
environment-name collisions, unsupported key forms, and missing required
environment variables are a whole-run preflight. If any project fails
preflight, no project makes a network request and the process exits 2. The
error identifies every detected preflight problem without displaying values.

After preflight succeeds, each project receives a result. A failure in one
request does not cancel other projects. A process-wide failure in the runner or
serializer exits 3; any successfully collected results may be included under a
`partial_projects` field, but consumers must treat them as incomplete.

Field contracts:

- `http_status` is an integer when an HTTP response was received, otherwise
  `null`.
- `latency_ms` is an integer measured from the first attempt through the final
  outcome, including backoff. It is `null` only when no network attempt began.
- `attempts` is an integer from 0 through 4.
- `error` is `null` for healthy results and an object otherwise.
- Response bodies and unbounded upstream messages never appear in JSON.

### 13.2 Classification decision table

Classification uses local validation first, then transport outcome, HTTP
status, and finally a strictly parsed PostgREST error object capped by the same
64 KiB response limit. Precedence is top to bottom:

| Condition | Stable code | Retry |
| --- | --- | --- |
| Locally unsupported elevated key | `credential_rejected` | No; whole-run preflight |
| Missing config or environment input | `missing_input` | No; whole-run preflight |
| DNS lookup fails | `dns_failure` | Yes |
| TLS negotiation or verification fails | `tls_failure` | Yes only for transient handshake errors, never certificate errors |
| Per-attempt deadline expires | `timeout` | Yes |
| Body exceeds 64 KiB | `response_too_large` | No |
| HTTP 540 | `project_paused` | No |
| HTTP 401/403 with PostgREST code `42501` | `database_permission_denied` | No |
| HTTP 401/403 without `42501` | `api_authentication_failed` | No |
| Retryable HTTP status from section 10.1 | `temporary_upstream_failure` after retries | Yes |
| Other non-2xx response | `unexpected_response` | No |
| Non-JSON or malformed bounded body | `unexpected_response` | No |
| 2xx with zero rows | `no_matching_row` | No |
| 2xx with multiple or malformed rows | `unexpected_response` | No |
| Expected single row `{ "id": true }` | `healthy` | Not applicable |

If an error body cannot be parsed safely, classification falls back to its HTTP
status rather than trusting text matching. The CLI does not claim that a
`database_permission_denied` result identifies the precise missing grant or
policy.

## 14. GitHub Actions integration

`sb-heartbeat install github` generates `.github/workflows/sb-heartbeat.yml` with:

- `schedule` using the configured cron expression;
- `workflow_dispatch`;
- `permissions: contents: read`;
- a job timeout;
- a pinned SB Heartbeat release version;
- checksum verification rather than `curl | sh`;
- one static environment mapping per project;
- JSON execution output; and
- a concise GitHub job summary.

The MVP mapping is deterministic:

- each configured URL environment name maps from the GitHub repository or
  environment variable with the same name: `${{ vars.NAME }}`;
- each configured API-key environment name maps from the GitHub repository or
  environment secret with the same name: `${{ secrets.NAME }}`; and
- the generator rejects duplicate or colliding environment names rather than
  silently sharing a binding.

Users who prefer a different GitHub store can edit the generated workflow or
use explicit future generator options; the MVP keeps one predictable mapping.

The workflow checks out the repository because `sb-heartbeat run` reads the tracked
`sb-heartbeat.yaml`. Checkout and any other third-party action are pinned to a full
commit SHA. The workflow installs an exact SB Heartbeat version and verifies its
published checksum.

The generator follows section 12.2 overwrite behavior. It documents that cron
is interpreted in UTC, scheduled workflows use the latest commit on the default
branch, the workflow file must exist on that branch, runs may be delayed or
dropped, and public-repository schedules may be disabled after prolonged
repository inactivity.

Generated instructions explain that low-privilege public client keys may be
stored according to the repository owner's policy, while recommending secrets
or scoped variables as the conservative default.

## 15. Agent readiness

Agent readiness means predictable noninteractive commands, focused docs, safe
defaults, and machine-readable output. It does not mean injecting broad agent
instructions into downstream repositories.

The SB Heartbeat source repository may contain an `AGENTS.md` for contributors. The
installer must never create, replace, append to, or otherwise alter a user's
existing `AGENTS.md`, `CLAUDE.md`, or equivalent instruction file without
explicit approval.

Agent-facing product documentation lives in SB Heartbeat itself, initially:

```text
docs/agent-install.md
docs/agent-prompts.md
```

An optional generated downstream file may use a namespaced location such as:

```text
.sb-heartbeat/agent-install.md
```

Generation is opt-in and must not be required for normal operation.

Agent guidance requires agents to:

- inspect existing repository instructions first;
- never print or commit elevated credentials;
- never request secret or service-role keys;
- generate but not apply SQL without authorization and appropriate access;
- avoid modifying unrelated application tables or RLS policies;
- avoid public monitoring endpoints by default;
- run `doctor` and an on-demand heartbeat when inputs are available; and
- report changed files and unresolved manual actions.

## 16. Security invariants

The following are release-blocking invariants:

1. Secret and service-role keys are rejected before a network request.
2. Logs never contain complete keys or authorization headers.
3. Redirects are disabled for every heartbeat request.
4. Standard hosted URLs must satisfy the exact section 10.2 origin rule;
   deceptive suffixes, userinfo, fragments, paths, and ports are rejected.
5. Custom hosts are not accepted in the MVP.
6. Request URLs are built structurally. A user-controlled path cannot replace
   the scheme or host through URL-reference behavior.
7. Every response has a timeout and maximum body size.
8. The MVP request is fixed, uses GET, and has no generic query surface.
9. Generated SQL revokes direct access from `public`, `anon`, `authenticated`,
   and `service_role`, then grants only `SELECT (id)` to `anon`.
10. `doctor` performs no mutation probes.
11. Configuration and debug output redact key values and authorization data.
12. State and result files, if later added, contain no keys or response bodies.
13. Generated files are not overwritten silently.
14. Release installers pin a version and verify integrity.
15. Test fixtures and the source repository contain no live project keys.

Host validation must use parsed URL components rather than string prefix tests
and must reject hosts such as `project.supabase.co.example.com`.

## 17. Suggested Go architecture

```text
cmd/
  sb-heartbeat/
    main.go
internal/
  cli/
    root.go
    init.go
    run.go
    doctor.go
    install.go
    migration.go
  config/
    config.go
    loader.go
    validation.go
  heartbeat/
    runner.go
    result.go
    select.go
  httpclient/
    client.go
    redirect.go
    limits.go
    redact.go
  credentials/
    classify.go
    headers.go
  scheduler/
    github.go
    cron.go
  migration/
    install.go
    uninstall.go
  output/
    text.go
    json.go
  security/
    host_validation.go
    secrets.go
templates/
  github/
docs/
```

Preferred dependencies:

- Cobra for CLI structure;
- Go standard library `net/http`;
- a maintained YAML library with strict decoding; and
- GoReleaser when release automation begins.

Avoid a general JSONPath dependency in the MVP. Typed expectations for the
default select are easier to validate and document.

## 18. Testing strategy

### 18.1 Unit tests

Cover:

- strict configuration and duplicate-key rejection;
- exact configuration discovery and project/environment name normalization;
- environment resolution;
- publishable and legacy anon key classification;
- rejection and redaction of elevated keys;
- header generation for both supported key types;
- URL and host validation edge cases;
- redirect rejection;
- structural query generation and escaping;
- response size and shape validation;
- result classification and exit-code mapping;
- retry eligibility, attempt counts, backoff caps, cancellation, and all hard
  runtime bounds;
- stable multi-project output ordering; and
- golden files for SQL and GitHub workflow generation.

### 18.2 HTTP integration tests

Use `httptest.Server` for:

- healthy responses;
- redirects;
- timeouts;
- 401 and 403 responses;
- HTTP 540 paused responses;
- invalid content types and JSON;
- oversized bodies;
- empty, multiple, or malformed rows; and
- transport failures.

### 18.3 Supabase integration tests

Before release, use a dedicated disposable project to verify:

- generated migration behavior;
- publishable-key headers;
- legacy anon-key headers;
- read access to the fixed row;
- lack of mutation grants; and
- uninstall behavior.

Migration tests also cover an unrelated same-name table, a view with the same
name, a forged or missing ownership marker, malformed columns or constraints, a
valid rerun, a valid uninstall, and refusal to uninstall an unowned object.
Effective table and column privileges are checked for `anon`, `authenticated`,
and `service_role`.

Integration secrets exist only in the CI secret store. Tests never target a
production project and never print keys.

## 19. Release and distribution

Initial releases should provide GitHub release artifacts for macOS, Linux, and
Windows on `amd64` and `arm64`, plus checksums. Homebrew, `go install`, GHCR,
signatures, and provenance are 1.0 targets and may be added incrementally.

Before initializing the public Go module path or embedding release URLs in a
generator, the project must complete its name, repository, and basic trademark
availability decision. Internal experiments may use a clearly temporary module
path, but public artifacts must not require a rename immediately after launch.

Documentation should prefer a package manager or a pinned, checksum-verified
binary. A convenience installer must never silently install an unpinned latest
artifact in generated automation.

## 20. MVP implementation phases

### Phase A: Foundation

- Final project name and public module/repository decision.
- Go module and CLI skeleton.
- Strict YAML model and validation.
- Key classification and redaction.
- URL and HTTP security primitives.
- Result model, text output, JSON output, and four exit codes.

### Phase B: Core heartbeat

- Table-select query generation.
- Single- and multi-project execution.
- Publishable and legacy anon header behavior.
- Response validation and failure classification.
- Unit and HTTP integration tests.

### Phase C: Setup and diagnostics

- `init`, including fully noninteractive operation.
- `doctor` with non-mutating diagnostics.
- Installation and uninstall migration generation.
- Golden tests for generated artifacts.

### Phase D: GitHub Actions and documentation

- Configurable three-times-daily schedule.
- Secure GitHub Actions generator.
- Local cron guidance.
- Quickstart, configuration, security, troubleshooting, uninstall, and
  agent-install documentation.
- End-to-end installation test in a disposable repository.

## 21. MVP acceptance criteria

The MVP is complete when a user can:

1. Install one Go binary from a versioned, integrity-checkable release.
2. Run interactive or noninteractive initialization.
3. Generate a collision-safe, explicit least-privilege migration that can be
   rerun and uninstalled without touching an unrelated same-named object.
4. Apply the migration separately.
5. Configure either a publishable or legacy anon key.
6. See an elevated key rejected before any request.
7. Run `doctor` without mutations.
8. Run one or several heartbeats.
9. Receive stable text or versioned JSON results.
10. Receive one of four consistent process exit codes.
11. Generate a GitHub Actions workflow.
12. Trigger it manually and schedule it three times daily by default.
13. Override the schedule.
14. Complete the documented installation using a coding agent without the
    agent modifying existing repository instruction files.
15. Pass the complete unit, HTTP integration, golden, and disposable Supabase
    integration test suites.

## 22. 1.0 roadmap and TODO backlog

The MVP deliberately establishes one safe path. The 1.0 backlog expands it
without weakening that path.

### Query capabilities

- [ ] Existing-table selects with explicit application-data warnings.
- [ ] RPC mode with clear inability-to-prove-nonmutation warnings.
- [ ] Constrained raw PostgREST mode with path and method allowlists.
- [ ] Evaluate demand for direct PostgreSQL queries; do not implement by
      default.
- [ ] Custom domains, self-hosted HTTPS endpoints, preview-specific URL forms,
      and explicit development-only HTTP handling.

### Execution backends

- [ ] Cloudflare Worker generator and Cron Triggers.
- [ ] Decide how to prevent drift between Go and generated Worker validation.
- [ ] Docker image, non-root execution, read-only filesystem support, and
      multi-architecture builds.
- [ ] `systemd` timer and macOS `launchd` generators.

### Observability

- [ ] Local status history with atomic writes and no sensitive data.
- [ ] Richer GitHub annotations and optional durable result artifacts.
- [ ] Notifications after configurable repeated failures.
- [ ] Prometheus or OpenTelemetry-compatible metrics export.
- [ ] Optional durable backends such as Cloudflare KV.

### Lifecycle and inventory

- [ ] Management API inventory of projects that are already paused, plus guided
      recovery messaging.
- [ ] Project inventory through optional Management API integration.
- [ ] Backup checks and deliberate archive workflows.
- [ ] Make it easy to stop heartbeats for projects users should archive.

### Distribution and agent experience

- [ ] Homebrew tap, `go install`, and GHCR image.
- [ ] Signed artifacts and build provenance.
- [ ] Shell completions.
- [ ] JSON Schema and editor integration.
- [ ] `llms.txt`; add `llms-full.txt` only if consumers demonstrate value.
- [ ] Agent installation tests with at least two coding agents.
- [ ] Copyable prompts without modifying downstream `AGENTS.md` files.

## 23. Risks and mitigations

### Supabase policy changes

Supabase controls pausing and its activity classifier. Keep the schedule
configurable, keep the MVP request deliberately fixed and easy to update in a
release, link to current policy documentation, test periodically, and make no
guarantee.

### Weak standalone demand

A heartbeat can be written in a few lines. Keep the implementation small and
win on safety, diagnostics, installation, and agent readiness. Validate demand
before building every 1.0 integration.

### Misuse perception

Describe ordinary activity for intentionally retained development projects,
not evasion. Encourage paid plans for production and archive workflows for
projects users no longer need.

### Credential confusion

Use `api_key` as the configuration concept, explain both supported client-key
forms, recommend environment variables, reject elevated forms, and emphasize
that RLS and grants—not the public nature of the key—protect data.

### Scheduler reliability

Run three times daily by default, avoid round minutes, keep manual dispatch,
surface missed or failed checks clearly, and document scheduler limitations.

### Scope expansion

Treat the MVP acceptance criteria as the implementation boundary. Track all
deferred work here and in issues, but do not make it release-blocking until the
core path is proven.

## 24. Positioning

Primary tagline:

> Keep dormant Supabase projects active with a least-privilege database heartbeat.

Supporting line:

> A polished Go CLI for GitHub Actions and local schedulers, with safe support
> for Supabase publishable and legacy anon keys—no service-role key or hosted
> credential storage required.

Agent-oriented line:

> Give your coding agent SB Heartbeat's installation guide and let it generate the
> migration, scheduler, and verification steps without touching your existing
> repository instructions.
